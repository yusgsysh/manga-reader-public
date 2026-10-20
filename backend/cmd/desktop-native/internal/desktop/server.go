package desktop

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/cache"
	"manga-reader/internal/config"
	"manga-reader/internal/database"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/handler"
	synclib "manga-reader/internal/sync"
)

// ErrMissingCookie is returned by Start when no usable ExHentai cookies were
// configured. The app cannot talk to the upstream without them.
var ErrMissingCookie = errors.New("missing ExHentai cookies")

// Options configures the embedded API server.
type Options struct {
	Cookie   config.CookieConfig
	DBPath   string
	CacheDir string
	// CacheMaxBytes bounds the image cache; 0 means unlimited.
	CacheMaxBytes int64
	// AuthToken, when set, is required on every request (header or
	// access_token query). The desktop app generates one per run so another
	// local process cannot drive the loopback API.
	AuthToken string
	// DevTools enables the /api/dev/* debug endpoints (off by default).
	DevTools bool
	// SyncToken enables the peer-facing sync endpoints; empty disables them.
	SyncToken string
	// Logger overrides slog.Default for the embedded server.
	Logger *slog.Logger
}

// Server is the in-process API server. BaseURL is the loopback address the
// native UI's client points at.
type Server struct {
	baseURL    string
	httpServer *http.Server
	handlerSrv *handler.Server
	db         *database.DB
	fsCache    *cache.FSCache
	syncSvc    *synclib.Service

	cancel context.CancelFunc
	done   chan struct{}

	closeOnce sync.Once
	closeErr  error
}

// BaseURL returns the http://127.0.0.1:<port> address the server listens on.
func (s *Server) BaseURL() string { return s.baseURL }

// SetSyncToken turns accepting sync peers on (non-empty token) or off at
// run time, for the settings page.
func (s *Server) SetSyncToken(token string) {
	if s.syncSvc != nil {
		s.syncSvc.SetHostToken(token)
	}
}

// ClearCache empties the on-disk image cache. Used by the settings page.
func (s *Server) ClearCache() error {
	if s.fsCache == nil {
		return nil
	}
	return s.fsCache.Clear()
}

// CacheInfo reports the cache root and its size in bytes, for the settings
// page.
func (s *Server) CacheInfo() (root string, size int64, err error) {
	if s.fsCache == nil {
		return "", 0, nil
	}
	size, err = s.fsCache.Size()
	return s.fsCache.Root(), size, err
}

// CacheMaxBytes returns the image cache size limit; 0 means unlimited.
func (s *Server) CacheMaxBytes() int64 {
	if s.fsCache == nil {
		return 0
	}
	return s.fsCache.MaxBytes()
}

// SetCacheMaxBytes changes the image cache size limit. A value <= 0 disables
// the limit. When the cache is already larger, it prunes in the background.
func (s *Server) SetCacheMaxBytes(maxBytes int64) {
	if s.fsCache == nil {
		return
	}
	s.fsCache.SetMaxBytes(maxBytes)
}

// Start boots the API on a random loopback port and returns once it is
// accepting connections. The caller must Close it on shutdown.
func Start(ctx context.Context, opts Options) (*Server, error) {
	if !opts.Cookie.IsValid() {
		return nil, ErrMissingCookie
	}

	logger := opts.Logger
	if logger == nil {
		logger = slog.Default()
	}

	// gin release mode also routes gin's internal debug output away from the
	// desktop process's stderr.
	if !opts.DevTools {
		gin.SetMode(gin.ReleaseMode)
	}

	client, err := exhentai.CreateHTTPClient(&exhentai.CookieConfig{
		IpbMemberID: opts.Cookie.IpbMemberID,
		IpbPassHash: opts.Cookie.IpbPassHash,
		Igneous:     opts.Cookie.Igneous,
		SK:          opts.Cookie.SK,
	})
	if err != nil {
		return nil, fmt.Errorf("create upstream client: %w", err)
	}

	db, err := database.NewDB(database.Options{Driver: database.DriverSQLite, DSN: opts.DBPath})
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	fsCache, err := cache.NewFSCache(opts.CacheDir, opts.CacheMaxBytes)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("open image cache: %w", err)
	}
	// A previous run may have left the cache over the limit; prune it now
	// rather than waiting for the first write.
	fsCache.SetMaxBytes(opts.CacheMaxBytes)

	syncSvc := synclib.NewService(db.Client, synclib.Options{HostToken: opts.SyncToken})

	// Same startup sweep as the server build: cache rows orphaned by the
	// previous run are removed and mirrored to the sync peer as tombstones.
	if keys, cleanErr := db.CleanupGalleryCache(context.Background()); cleanErr != nil {
		logger.Warn("gallery cache startup cleanup failed", "error", cleanErr)
	} else if len(keys) > 0 {
		syncKeys := make([]synclib.Key, len(keys))
		for i, k := range keys {
			syncKeys[i] = synclib.Key{GalleryID: k.GalleryID, Token: k.Token}
		}
		if recErr := syncSvc.RecordDeletes(context.Background(), synclib.EntityGalleryCache, syncKeys); recErr != nil {
			logger.Warn("record sync cache cleanup failed", "error", recErr)
		}
	}

	handlerSrv := handler.New(handler.Config{
		Client:   client,
		DB:       db,
		Cache:    fsCache,
		DevTools: opts.DevTools,
		Sync:     syncSvc,
	})

	r := gin.New()
	r.Use(gin.Recovery())
	r.Use(localSecurityHeaders())
	if opts.AuthToken != "" {
		r.Use(authTokenMiddleware(opts.AuthToken))
	}
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	handlerSrv.RegisterRoutes(r)

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("listen on loopback: %w", err)
	}

	engineCtx, cancel := context.WithCancel(ctx)
	httpServer := &http.Server{
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout intentionally unset: ZIP/image streaming can run long.
	}

	s := &Server{
		baseURL:    "http://" + listener.Addr().String(),
		httpServer: httpServer,
		handlerSrv: handlerSrv,
		db:         db,
		fsCache:    fsCache,
		syncSvc:    syncSvc,
		cancel:     cancel,
		done:       make(chan struct{}),
	}

	// Sync engine idles without a configured peer and exits with engineCtx.
	go syncSvc.RunEngine(engineCtx)

	go func() {
		defer close(s.done)
		if err := httpServer.Serve(listener); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("embedded API server stopped", "error", err)
		}
	}()

	logger.Info("embedded API server listening",
		"addr", s.baseURL,
		"db", opts.DBPath,
		"cache", opts.CacheDir,
	)
	return s, nil
}

// Close gracefully stops the server, the sync engine, background prefill and
// the database. It is safe to call more than once.
func (s *Server) Close() error {
	s.closeOnce.Do(func() {
		if s.cancel != nil {
			s.cancel()
		}
		if s.handlerSrv != nil {
			s.handlerSrv.StopPrefill()
		}
		if s.httpServer != nil {
			shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := s.httpServer.Shutdown(shutdownCtx); err != nil {
				s.closeErr = err
				// Open SSE streams can outlive the graceful window; force the
				// close so the process can exit.
				_ = s.httpServer.Close()
			}
		}
		<-s.done
		if s.db != nil {
			if err := s.db.Close(); err != nil && s.closeErr == nil {
				s.closeErr = err
			}
		}
	})
	return s.closeErr
}

// localSecurityHeaders mirrors the server's non-CSP headers. CORS is omitted:
// the desktop client is same-process, not a browser origin.
func localSecurityHeaders() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

// authTokenMiddleware requires the per-run token on every request. It accepts
// the X-Manga-Reader-Token header (used by the API and image clients) or the
// access_token query parameter (a fallback for URL-only image loads).
func authTokenMiddleware(token string) gin.HandlerFunc {
	return func(c *gin.Context) {
		got := c.GetHeader(config.AuthHeader)
		if got == "" {
			got = c.Query(config.AuthQueryParam)
		}
		if subtle.ConstantTimeCompare([]byte(got), []byte(token)) != 1 {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}
