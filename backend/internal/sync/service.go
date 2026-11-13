package sync

import (
	"context"
	"crypto/subtle"
	json "encoding/json/v2"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/syncchange"
	"manga-reader/internal/metrics"
	"manga-reader/internal/web"
)

const (
	// headerToken authenticates peer-facing sync endpoints. SSE handlers
	// cannot set request headers in browsers, but the sync engine is a Go
	// client, so a single header covers the whole protocol.
	headerToken = "X-Sync-Token"

	// heartbeatInterval keeps proxies from reaping idle SSE streams (a
	// typical reverse-proxy read timeout is 60s).
	heartbeatInterval = 25 * time.Second
)

// Service wires the sync protocol endpoints, the change hub and the sync
// engine onto an ent client.
type Service struct {
	client    *ent.Client
	hub       *Hub
	hostToken string
	// tenantID is the single tenant served by this sync instance. Peer sync
	// carries no client-supplied tenant; keeping one configured tenant
	// prevents a stale or malicious peer from crossing tenant boundaries.
	tenantID int64
	engine   *Engine
	// clientActive mirrors ClientConfig.Active(); the outbox is only written
	// when this instance can actually exchange changes with a peer.
	clientActive atomic.Bool
	// disableInactiveOutbox makes record() a no-op when no peer is
	// configured. Production opts in; unit tests that exercise the outbox
	// mechanism directly leave it off.
	disableInactiveOutbox bool
}

// recordEnabled reports whether an outbox row is useful: either this instance
// serves sync peers, or it is configured as a sync client. Without this guard
// a single-instance deployment appends a row on every page turn forever.
func (s *Service) recordEnabled() bool {
	if s.hostToken != "" || s.clientActive.Load() {
		return true
	}
	return !s.disableInactiveOutbox
}

// Options configures a Service.
type Options struct {
	// HostToken enables the peer-facing endpoints (/api/sync/push,
	// /api/sync/events) when non-empty. Empty means this instance does not
	// accept sync peers.
	HostToken string
	// DisableInactiveOutbox stops appending outbox rows when neither a host
	// token nor a client peer is configured, so a single-instance deployment
	// does not grow sync_change forever.
	DisableInactiveOutbox bool
	// TenantID selects the tenant replicated by the host endpoints and the
	// client engine. Zero means the default tenant (1).
	TenantID int64
}

// NewService builds a Service. The engine must be started with RunEngine.
func NewService(client *ent.Client, opts Options) *Service {
	tenantID := opts.TenantID
	if tenantID == 0 {
		tenantID = config.DefaultTenantID
	}
	s := &Service{
		client:                client,
		hub:                   NewHub(),
		hostToken:             opts.HostToken,
		tenantID:              tenantID,
		disableInactiveOutbox: opts.DisableInactiveOutbox,
	}
	s.engine = newEngine(s)
	return s
}

// RunEngine runs the sync client engine until ctx is cancelled.
func (s *Service) RunEngine(ctx context.Context) { s.engine.Run(ctx) }

// RegisterRoutes mounts sync endpoints on the given router. Peer-facing
// routes are only registered when a host token is configured; without one
// they 404 like any unknown path.
func (s *Service) RegisterRoutes(r gin.IRouter) {
	r.GET("/api/events/changes", s.handleLocalEvents)
	r.GET("/api/sync/config", s.handleConfigGet)
	r.PUT("/api/sync/config", s.handleConfigPut)
	r.GET("/api/sync/status", s.handleStatus)
	if s.hostToken != "" {
		r.POST("/api/sync/push", s.requireToken(), s.handlePush)
		r.GET("/api/sync/events", s.requireToken(), s.handlePeerEvents)
	}
}

func (s *Service) requireToken() gin.HandlerFunc {
	want := []byte(s.hostToken)
	return func(c *gin.Context) {
		got := []byte(c.GetHeader(headerToken))
		if len(got) == 0 || subtle.ConstantTimeCompare(got, want) != 1 {
			web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "invalid sync token"})
			return
		}
		c.Next()
	}
}

// handlePush is the combined push+pull exchange (see PushRequest).
func (s *Service) handlePush(c *gin.Context) {
	// The body carries full rows (page/thumbnail arrays); bound it so a
	// stolen token cannot stream an unbounded payload into memory.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)
	var req PushRequest
	if err := web.BindJSON(c, &req); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			web.JSON(c, http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	ctx := c.Request.Context()

	applied, skipped, err := ApplyChangesForTenant(ctx, s.client, s.tenantID, req.Changes)
	if err != nil {
		slog.Error("apply sync changes failed", "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "apply changes failed"})
		return
	}
	if applied > 0 {
		// Local frontends must see the incoming rows; the originator already
		// knows its own changes, so peers are not woken.
		s.hub.NotifyLocal()
	}

	var changes []Change
	var cursor int
	if req.Snapshot && req.Cursor == 0 {
		// Read the watermark before the export so a row written in between
		// keeps an outbox id above the returned cursor instead of landing in
		// neither the snapshot nor the delta.
		cursor, err = maxOutboxID(ctx, s.client, s.tenantID)
		if err != nil {
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "collect cursor failed"})
			return
		}
		changes, err = ExportSnapshot(ctx, s.client, s.tenantID)
	} else {
		changes, cursor, err = ReadChangesSince(ctx, s.client, s.tenantID, req.Cursor)
	}
	if err != nil {
		slog.Error("collect sync changes failed", "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "collect changes failed"})
		return
	}

	// Entries at or below the caller's cursor are already held there. Only
	// prune when the cursor is plausible for this outbox: a cursor above the
	// outbox max means the host was reset (rewind) or the client is stale, and
	// pruning then would destroy changes the client has never seen.
	if req.Cursor > 0 && req.Cursor <= cursor {
		if err := pruneOutbox(ctx, s.client, s.tenantID, req.Cursor); err != nil {
			slog.Warn("prune sync outbox failed", "error", err)
		}
	}

	web.JSON(c, http.StatusOK, PushResponse{
		Cursor:  cursor,
		Changes: changes,
		Applied: applied,
		Skipped: skipped,
	})
}

// handlePeerEvents streams change notifications to remote sync engines.
func (s *Service) handlePeerEvents(c *gin.Context) {
	sub, cancel := s.hub.SubscribePeer()
	s.serveEvents(c, sub, cancel, func() any {
		cursor, err := maxOutboxID(c.Request.Context(), s.client, s.tenantID)
		if err != nil {
			return nil
		}
		return gin.H{"cursor": cursor}
	})
}

// handleLocalEvents streams change notifications to local frontends
// (web UI and the desktop WebView) so React Query caches refresh instantly.
func (s *Service) handleLocalEvents(c *gin.Context) {
	sub, cancel := s.hub.SubscribeLocal()
	s.serveEvents(c, sub, cancel, func() any {
		return gin.H{}
	})
}

func (s *Service) serveEvents(c *gin.Context, sub <-chan struct{}, cancel func(), payload func() any) {
	// SSE stays open for the life of the client; exclude it from the HTTP
	// latency histogram (it is still counted in http_requests_total).
	metrics.MarkLongRunning(c)
	c.Header("Content-Type", "text/event-stream")
	c.Header("Cache-Control", "no-cache")
	c.Header("Connection", "keep-alive")
	c.Header("X-Accel-Buffering", "no")
	defer cancel()

	write := func(data any) bool {
		body, err := json.Marshal(data)
		if err != nil {
			return false
		}
		if _, err := c.Writer.WriteString("data: " + string(body) + "\n\n"); err != nil {
			return false
		}
		c.Writer.Flush()
		return true
	}

	// Emit the current state immediately so a fresh subscriber catches up
	// without waiting for the next change.
	if payload != nil {
		if p := payload(); p != nil {
			if !write(p) {
				return
			}
		}
	}

	heartbeat := time.NewTicker(heartbeatInterval)
	defer heartbeat.Stop()

	for {
		select {
		case <-c.Request.Context().Done():
			return
		case <-sub:
			if payload != nil {
				if p := payload(); p != nil {
					if !write(p) {
						return
					}
				}
			}
		case <-heartbeat.C:
			if _, err := c.Writer.WriteString(": ping\n\n"); err != nil {
				return
			}
			c.Writer.Flush()
		}
	}
}

type configResponse struct {
	Enabled   bool   `json:"enabled"`
	ServerURL string `json:"server_url"`
	Token     string `json:"token"`
}

func (s *Service) handleConfigGet(c *gin.Context) {
	cfg, err := s.LoadClientConfig(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read sync config failed"})
		return
	}
	resp := configResponse{Enabled: cfg.Enabled, ServerURL: cfg.ServerURL}
	if cfg.Token != "" {
		resp.Token = Mask
	}
	web.JSON(c, http.StatusOK, resp)
}

func (s *Service) handleConfigPut(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var in ConfigUpdate
	if err := web.BindJSON(c, &in); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if in.ServerURL != nil {
		u := *in.ServerURL
		if u != "" && !hasHTTPScheme(u) {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": "server_url must start with http:// or https://"})
			return
		}
	}
	cfg, err := s.SaveClientConfig(c.Request.Context(), in)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "save sync config failed"})
		return
	}
	resp := configResponse{Enabled: cfg.Enabled, ServerURL: cfg.ServerURL}
	if cfg.Token != "" {
		resp.Token = Mask
	}
	web.JSON(c, http.StatusOK, resp)
}

func hasHTTPScheme(u string) bool {
	return strings.HasPrefix(u, "http://") || strings.HasPrefix(u, "https://")
}

type statusResponse struct {
	Enabled      bool       `json:"enabled"`
	Configured   bool       `json:"configured"`
	ServerURL    string     `json:"server_url"`
	SSEConnected bool       `json:"sse_connected"`
	LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	Cursor       int        `json:"cursor"`
	LastPushedID int        `json:"last_pushed_id"`
	Pending      int        `json:"pending"`
}

func (s *Service) handleStatus(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, err := s.LoadClientConfig(ctx)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read sync status failed"})
		return
	}
	st, err := s.LoadEngineState(ctx)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read sync status failed"})
		return
	}
	pending := 0
	if cfg.Active() {
		pending, err = s.client.SyncChange.Query().
			Where(syncchange.IDGT(st.LastPushedID), syncchange.TenantIDEQ(s.tenantID)).
			Count(ctx)
		if err != nil {
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "count pending changes failed"})
			return
		}
	}
	web.JSON(c, http.StatusOK, statusResponse{
		Enabled:      cfg.Enabled,
		Configured:   cfg.Active(),
		ServerURL:    cfg.ServerURL,
		SSEConnected: s.engine.sseConnected(),
		LastSyncAt:   st.LastSyncAt,
		LastError:    st.LastError,
		Cursor:       st.Cursor,
		LastPushedID: st.LastPushedID,
		Pending:      pending,
	})
}

// LogRecordError logs a failed outbox append with context; handlers use it
// so a lost notification is visible but does not fail the user operation.
func LogRecordError(err error, entity string, galleryID int64, token string) {
	if err != nil {
		slog.Warn("sync record failed",
			"entity", entity, "id", galleryID, "token", token, "error", err)
	}
}
