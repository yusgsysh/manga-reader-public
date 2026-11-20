package sync

import (
	"context"
	json "encoding/json/v2"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strings"
	stdSync "sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/syncchange"
	"manga-reader/internal/ent/usersynckey"
	"manga-reader/internal/httpx"
	"manga-reader/internal/ident"
	"manga-reader/internal/metrics"
	"manga-reader/internal/requestid"
	"manga-reader/internal/synckey"
)

const (
	// heartbeatInterval keeps proxies from reaping idle SSE streams (a
	// typical reverse-proxy read timeout is 60s).
	heartbeatInterval = 25 * time.Second
)

// Service wires the sync protocol endpoints, the change hub and the sync
// engine onto an ent client.
type Service struct {
	client      *ent.Client
	hub         *Hub
	hostEnabled bool
	keyAuth     func(scopes ...string) gin.HandlerFunc
	engine      *Engine
	// clientActive mirrors ClientConfig.Active(); the outbox is only written
	// when this instance can actually exchange changes with a peer.
	clientActive atomic.Bool
	// pushMu serializes push cycles with a full-resync request. Without it a
	// reset could land between a cycle's state load and its save, and the
	// cycle's stale state would silently overwrite the reset.
	pushMu stdSync.Mutex
	// clientUser is the local account this instance syncs (Nil = the legacy
	// instance-wide exchange). On a pure client, writes for other accounts are
	// not queued so a user-scoped peer never receives them.
	clientUser atomic.Value
	// disableInactiveOutbox makes record() a no-op when no peer is
	// configured. Production opts in; unit tests that exercise the outbox
	// mechanism directly leave it off.
	disableInactiveOutbox bool
}

// recordEnabled reports whether an outbox row is useful: either this instance
// serves sync peers, or it is configured as a sync client. Without this guard
// a single-instance deployment appends a row on every page turn forever.
func (s *Service) recordEnabled() bool {
	if s.hostEnabled || s.clientActive.Load() {
		return true
	}
	return !s.disableInactiveOutbox
}

// syncedUser returns the local account bound to the client configuration, or
// the nil UUID for the legacy instance-wide exchange.
func (s *Service) syncedUser() ident.UUID {
	if v := s.clientUser.Load(); v != nil {
		return v.(ident.UUID)
	}
	return ident.Nil()
}

// Options configures a Service.
type Options struct {
	// HostEnabled mounts the peer-facing routes. Authentication is supplied
	// separately as a node-API-key middleware.
	HostEnabled bool
	// KeyAuth validates Authorization: Bearer <user sync key> and requires
	// the scopes passed to it. It must not accept user JWTs.
	KeyAuth func(scopes ...string) gin.HandlerFunc
	// DisableInactiveOutbox stops appending outbox rows when no peer is
	// configured, so a single-instance deployment does not grow sync_change.
	DisableInactiveOutbox bool
	// RequestTimeout bounds one push exchange (default 30s).
	RequestTimeout time.Duration
}

// NewService builds a Service. The engine must be started with RunEngine.
func NewService(client *ent.Client, opts Options) *Service {
	s := &Service{
		client:                client,
		hub:                   NewHub(),
		hostEnabled:           opts.HostEnabled,
		keyAuth:               opts.KeyAuth,
		disableInactiveOutbox: opts.DisableInactiveOutbox,
	}
	s.engine = newEngine(s)
	if opts.RequestTimeout > 0 {
		s.engine.http.Timeout = opts.RequestTimeout
	}
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
	r.POST("/api/sync/resync", s.handleResync)
	r.GET("/api/sync/keys", s.handleUserKeyList)
	r.POST("/api/sync/keys", s.handleUserKeyCreate)
	r.POST("/api/sync/keys/:id/rotate", s.handleUserKeyRotate)
	r.DELETE("/api/sync/keys/:id", s.handleUserKeyDelete)
	if s.hostEnabled && s.keyAuth != nil {
		r.POST("/api/sync/push", s.keyAuth(synckey.ScopeSyncWrite), s.handlePush)
		r.GET("/api/sync/events", s.keyAuth(synckey.ScopeSyncEvent), s.handlePeerEvents)
	}
}

// handlePush is the combined push+pull exchange (see PushRequest).
func (s *Service) handlePush(c *gin.Context) {
	// The body carries full rows (page/thumbnail arrays); bound it so a
	// stolen token cannot stream an unbounded payload into memory.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<20)
	var req PushRequest
	if err := httpx.BindJSON(c, &req); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			httpx.JSON(c, http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	ctx := c.Request.Context()
	identity, ok := synckey.From(c)
	if !ok {
		httpx.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "sync identity missing"})
		return
	}
	// A user sync key scopes the whole exchange to its account: incoming
	// private rows are written under that user and only that user's changes
	// (plus the shared gallery cache) go back.
	owner := identity.UserID
	applied, skipped, err := ApplyChanges(ctx, s.client, req.Changes, owner)
	if err != nil {
		slog.Error("apply sync changes failed", "error", err)
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "apply changes failed"})
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
		// A bootstrap carries the full live snapshot plus outbox tombstones,
		// so deletions made before this peer ever synced still propagate
		// instead of being resurrected by the other side's snapshot.
		changes, cursor, err = BootstrapChanges(ctx, s.client, owner)
	} else {
		changes, cursor, err = ReadChangesSince(ctx, s.client, req.Cursor, owner)
	}
	if err != nil {
		slog.Error("collect sync changes failed", "error", err)
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "collect changes failed"})
		return
	}

	// Entries at or below the caller's cursor are already held there. Only
	// prune when the cursor is plausible for this outbox: a cursor above the
	// outbox max means the host was reset (rewind) or the client is stale, and
	// pruning then would destroy changes the client has never seen. A
	// user-scoped exchange only prunes its own entries.
	if req.Cursor > 0 && req.Cursor <= cursor {
		if err := pruneOutbox(ctx, s.client, req.Cursor, owner); err != nil {
			slog.Warn("prune sync outbox failed", "error", err)
		}
	}

	slog.Info("sync push",
		"request_id", requestid.From(c),
		"user_id", identity.UserID,
		"applied", applied,
		"skipped", skipped,
		"changes_in", len(req.Changes),
		"changes_out", len(changes),
		"sync_request_id", req.RequestID,
		"result", "ok",
	)
	httpx.JSON(c, http.StatusOK, PushResponse{
		Cursor:  cursor,
		Changes: changes,
		Applied: applied,
		Skipped: skipped,
	})
}

// handlePeerEvents streams change notifications to remote sync engines.
func (s *Service) handlePeerEvents(c *gin.Context) {
	if identity, ok := synckey.From(c); ok {
		slog.Info("sync events connected",
			"request_id", requestid.From(c), "user_id", identity.UserID)
	}
	sub, cancel := s.hub.SubscribePeer()
	s.serveEvents(c, sub, cancel, func() any {
		cursor, err := maxOutboxID(c.Request.Context(), s.client)
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
	// Username names the local account this configuration syncs. Empty for a
	// legacy instance-wide config.
	Username string `json:"username,omitempty"`
}

// configOwnerName resolves the configured local account for display.
func (s *Service) configOwnerName(ctx context.Context, cfg ClientConfig) string {
	if cfg.UserID == ident.Nil() {
		return ""
	}
	u, err := s.client.User.Get(ctx, cfg.UserID)
	if err != nil {
		return ""
	}
	return u.Username
}

func (s *Service) handleConfigGet(c *gin.Context) {
	cfg, err := s.LoadClientConfig(c.Request.Context())
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "read sync config failed"})
		return
	}
	resp := configResponse{
		Enabled:   cfg.Enabled,
		ServerURL: cfg.ServerURL,
		Username:  s.configOwnerName(c.Request.Context(), cfg),
	}
	if cfg.Token != "" {
		resp.Token = Mask
	}
	httpx.JSON(c, http.StatusOK, resp)
}

func (s *Service) handleConfigPut(c *gin.Context) {
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var in ConfigUpdate
	if err := httpx.BindJSON(c, &in); err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if in.ServerURL != nil {
		u := *in.ServerURL
		if u != "" && !hasHTTPScheme(u) {
			httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "server_url must start with http:// or https://"})
			return
		}
	}
	// Bind the configuration to the account that saved it. The token is a
	// user sync key for that account on the peer; the local owner is taken
	// from the verified JWT, never from the request body.
	if principal, ok := auth.PrincipalFrom(c); ok && principal.ID != ident.Nil() {
		in.UserID = &principal.ID
	}
	cfg, err := s.SaveClientConfig(c.Request.Context(), in)
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "save sync config failed"})
		return
	}
	resp := configResponse{
		Enabled:   cfg.Enabled,
		ServerURL: cfg.ServerURL,
		Username:  s.configOwnerName(c.Request.Context(), cfg),
	}
	if cfg.Token != "" {
		resp.Token = Mask
	}
	httpx.JSON(c, http.StatusOK, resp)
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
	// HostEnabled reports that this instance mounts the peer endpoints, so the
	// settings page can offer to mint a user sync key.
	HostEnabled bool `json:"host_enabled"`
}

// handleResync forces a full bidirectional exchange: it clears the replication
// cursor/watermark so the next push sends this instance's whole dataset as a
// snapshot and asks the peer for its own, then wakes the engine. It is the
// manual recovery path when a long outage (or a rebuilt peer database) left
// the two sides out of step.
func (s *Service) handleResync(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, err := s.LoadClientConfig(ctx)
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "read sync config failed"})
		return
	}
	if !cfg.Active() {
		httpx.JSON(c, http.StatusConflict, gin.H{"error": "sync is not configured"})
		return
	}
	if err := s.RequestFullResync(ctx); err != nil {
		slog.Error("sync: full resync failed", "error", err)
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "reset sync state failed"})
		return
	}
	slog.Info("sync: full resync requested", "request_id", requestid.From(c))
	httpx.JSON(c, http.StatusOK, gin.H{"ok": true})
}

type userSyncKeyJSON struct {
	ID         ident.UUID `json:"id"`
	Name       string     `json:"name"`
	KeyPrefix  string     `json:"key_prefix"`
	Status     string     `json:"status"`
	ExpiresAt  *time.Time `json:"expires_at"`
	LastUsedAt *time.Time `json:"last_used_at"`
	CreatedAt  time.Time  `json:"created_at"`
	RevokedAt  *time.Time `json:"revoked_at"`
}

func userSyncKeyResponse(k *ent.UserSyncKey) userSyncKeyJSON {
	return userSyncKeyJSON{
		ID: k.ID, Name: k.Name, KeyPrefix: k.KeyPrefix, Status: k.Status,
		ExpiresAt: k.ExpiresAt, LastUsedAt: k.LastUsedAt,
		CreatedAt: k.CreatedAt, RevokedAt: k.RevokedAt,
	}
}

// currentUserID returns the JWT account, which owns user sync keys.
func currentUserID(c *gin.Context) (ident.UUID, bool) {
	principal, ok := auth.PrincipalFrom(c)
	if !ok || principal.ID == ident.Nil() {
		return ident.Nil(), false
	}
	return principal.ID, true
}

func (s *Service) requireHost(c *gin.Context) bool {
	if !s.hostEnabled {
		httpx.JSON(c, http.StatusConflict, gin.H{"error": "sync host is not enabled"})
		return false
	}
	return true
}

// handleUserKeyList lists the current account's sync keys (prefixes only).
func (s *Service) handleUserKeyList(c *gin.Context) {
	if !s.requireHost(c) {
		return
	}
	uid, ok := currentUserID(c)
	if !ok {
		httpx.JSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	rows, err := s.client.UserSyncKey.Query().
		Where(usersynckey.UserIDEQ(uid)).
		Order(ent.Desc(usersynckey.FieldCreatedAt)).
		All(c.Request.Context())
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "list sync keys failed"})
		return
	}
	out := make([]userSyncKeyJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, userSyncKeyResponse(row))
	}
	httpx.JSON(c, http.StatusOK, gin.H{"keys": out})
}

// handleUserKeyCreate mints a key bound to the current account. The body may
// carry a human name and an optional lifetime (days or an absolute RFC 3339
// time); the plaintext is returned exactly once.
func (s *Service) handleUserKeyCreate(c *gin.Context) {
	if !s.requireHost(c) {
		return
	}
	uid, ok := currentUserID(c)
	if !ok {
		httpx.JSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 64<<10)
	var body struct {
		Name          string  `json:"name"`
		ExpiresInDays *int    `json:"expires_in_days"`
		ExpiresAt     *string `json:"expires_at"`
	}
	if err := httpx.BindJSON(c, &body); err != nil && !errors.Is(err, io.EOF) {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	name := strings.TrimSpace(body.Name)
	if len([]rune(name)) > maxSyncKeyName {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "name is too long"})
		return
	}
	expires, err := syncKeyExpiry(time.Now().UTC(), body.ExpiresInDays, body.ExpiresAt)
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	material, err := synckey.GenerateKey()
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "generate sync key failed"})
		return
	}
	create := s.client.UserSyncKey.Create().
		SetUserID(uid).
		SetName(name).
		SetKeyHash(material.Hash).
		SetKeyPrefix(material.Prefix).
		SetStatus(config.StatusActive)
	if expires != nil {
		create.SetExpiresAt(*expires)
	}
	row, err := create.Save(c.Request.Context())
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "create sync key failed"})
		return
	}
	slog.Info("security audit", "action", "sync_key.create",
		"request_id", requestid.From(c), "actor_user_id", uid, "key_id", row.ID, "result", "ok")
	httpx.JSON(c, http.StatusCreated, gin.H{
		"key":     material.Plaintext,
		"api_key": userSyncKeyResponse(row),
	})
}

// maxSyncKeyName bounds the display label so a key cannot store an unbounded
// string.
const maxSyncKeyName = 100

// syncKeyExpiry validates the lifetime input. A day count wins over an
// absolute time, mirroring the old key-management API; both nil means the key
// never expires.
func syncKeyExpiry(now time.Time, days *int, at *string) (*time.Time, error) {
	if days != nil {
		if *days < 1 || *days > 3650 {
			return nil, errors.New("expires_in_days must be between 1 and 3650")
		}
		expires := now.AddDate(0, 0, *days)
		return &expires, nil
	}
	if at != nil && strings.TrimSpace(*at) != "" {
		parsed, err := time.Parse(time.RFC3339, strings.TrimSpace(*at))
		if err != nil {
			return nil, errors.New("expires_at must be an RFC 3339 timestamp")
		}
		if !parsed.After(now) {
			return nil, errors.New("expires_at must be in the future")
		}
		utc := parsed.UTC()
		return &utc, nil
	}
	return nil, nil
}

// handleUserKeyRotate replaces a key with a freshly generated one in a single
// transaction. The name is kept, and a still-valid expiry is carried over; the
// old key is deleted, so it stops working as soon as the rotation commits.
func (s *Service) handleUserKeyRotate(c *gin.Context) {
	if !s.requireHost(c) {
		return
	}
	uid, ok := currentUserID(c)
	if !ok {
		httpx.JSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	keyID, err := ident.Parse(c.Param("id"))
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	ctx := c.Request.Context()
	old, err := s.client.UserSyncKey.Query().
		Where(usersynckey.ID(keyID), usersynckey.UserIDEQ(uid)).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			httpx.JSON(c, http.StatusNotFound, gin.H{"error": "sync key not found"})
			return
		}
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "read sync key failed"})
		return
	}
	material, err := synckey.GenerateKey()
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "generate sync key failed"})
		return
	}

	tx, err := s.client.Tx(ctx)
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "rotate sync key failed"})
		return
	}
	create := tx.UserSyncKey.Create().
		SetUserID(uid).
		SetName(old.Name).
		SetKeyHash(material.Hash).
		SetKeyPrefix(material.Prefix).
		SetStatus(config.StatusActive)
	if old.ExpiresAt != nil && old.ExpiresAt.After(time.Now().UTC()) {
		create.SetExpiresAt(*old.ExpiresAt)
	}
	created, err := create.Save(ctx)
	if err != nil {
		_ = tx.Rollback()
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "rotate sync key failed"})
		return
	}
	if _, err := tx.UserSyncKey.Delete().
		Where(usersynckey.ID(keyID), usersynckey.UserIDEQ(uid)).
		Exec(ctx); err != nil {
		_ = tx.Rollback()
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "rotate sync key failed"})
		return
	}
	if err := tx.Commit(); err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "rotate sync key failed"})
		return
	}
	slog.Info("security audit", "action", "sync_key.rotate",
		"request_id", requestid.From(c), "actor_user_id", uid,
		"old_key_id", keyID, "new_key_id", created.ID, "result", "ok")
	httpx.JSON(c, http.StatusOK, gin.H{
		"key":     material.Plaintext,
		"api_key": userSyncKeyResponse(created),
	})
}

// handleUserKeyDelete removes one of the current account's keys. Deleting an
// active key invalidates it immediately, exactly like a revocation would.
func (s *Service) handleUserKeyDelete(c *gin.Context) {
	if !s.requireHost(c) {
		return
	}
	uid, ok := currentUserID(c)
	if !ok {
		httpx.JSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	keyID, err := ident.Parse(c.Param("id"))
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid key id"})
		return
	}
	deleted, err := s.client.UserSyncKey.Delete().
		Where(usersynckey.ID(keyID), usersynckey.UserIDEQ(uid)).
		Exec(c.Request.Context())
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete sync key failed"})
		return
	}
	if deleted == 0 {
		httpx.JSON(c, http.StatusNotFound, gin.H{"error": "sync key not found"})
		return
	}
	slog.Info("security audit", "action", "sync_key.delete",
		"request_id", requestid.From(c), "actor_user_id", uid, "key_id", keyID, "result", "ok")
	httpx.JSON(c, http.StatusOK, gin.H{"deleted": true})
}

func (s *Service) handleStatus(c *gin.Context) {
	ctx := c.Request.Context()
	cfg, err := s.LoadClientConfig(ctx)
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "read sync status failed"})
		return
	}
	st, err := s.LoadEngineState(ctx)
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "read sync status failed"})
		return
	}
	pending := 0
	if cfg.Active() {
		query := s.client.SyncChange.Query().Where(syncchange.IDGT(st.LastPushedID))
		if cfg.UserID != ident.Nil() {
			query = query.Where(syncchange.Or(
				syncchange.UserIDEQ(cfg.UserID),
				syncchange.UserIDEQ(ident.Nil()),
			))
		}
		pending, err = query.Count(ctx)
		if err != nil {
			httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "count pending changes failed"})
			return
		}
	}
	httpx.JSON(c, http.StatusOK, statusResponse{
		Enabled:      cfg.Enabled,
		Configured:   cfg.Active(),
		ServerURL:    cfg.ServerURL,
		SSEConnected: s.engine.sseConnected(),
		LastSyncAt:   st.LastSyncAt,
		LastError:    st.LastError,
		Cursor:       st.Cursor,
		LastPushedID: st.LastPushedID,
		Pending:      pending,
		HostEnabled:  s.hostEnabled,
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
