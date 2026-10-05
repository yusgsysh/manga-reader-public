// Package app wires configuration, storage, database and HTTP routing
// together. Both entrypoints (the web server in backend/main.go and the
// desktop app in backend/cmd/desktop) build an App and serve it, so the two
// share exactly one REST surface.
package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
	"manga-reader/internal/database"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/handler"
	"manga-reader/internal/settings"
	"manga-reader/internal/storage"
	synclib "manga-reader/internal/sync"
)

// App owns every long lived resource of the service.
type App struct {
	Config   *config.Config
	Logger   *slog.Logger
	DB       *database.DB
	Storage  storage.Storage
	Handler  *handler.Server
	Router   *gin.Engine
	Settings *settings.Service
	Sync     *synclib.Service

	setLogLevel func(level string)
	syncCancel  context.CancelFunc
	server      *http.Server
	closed      bool
}

// Option customises App construction.
type Option func(*App)

// WithLogSetter lets the settings page change LOG_LEVEL while the process is
// running. The entrypoint that builds the logger supplies it; without it the
// level stays whatever the logger was created with.
func WithLogSetter(set func(level string)) Option {
	return func(a *App) { a.setLogLevel = set }
}

// storageConnectTimeout bounds how long saving storage settings waits for the
// object store to answer, so a bad endpoint is a rejected save rather than a
// hung request.
const storageConnectTimeout = 15 * time.Second

// CORSMiddleware allows the browser to reach the API from a different origin
// (the Wails asset origin in desktop builds). Gin serves only the API, so
// allowing any origin stays safe.
func CORSMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("Access-Control-Allow-Origin", "*")
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// New builds the full application described by cfg.
//
// cfg is the configuration derived from the process environment. Persisted
// settings are layered on top of it once the database is open, so everything
// downstream — the cookie jar, the object store, the log level, the dev tools
// flag — uses the merged result and never the raw environment.
//
// It only returns an error for failures that leave the service unusable
// (database or storage). Rejected persisted settings are logged and ignored so
// the settings page can still be reached to fix them.
func New(cfg *config.Config, logger *slog.Logger, opts ...Option) (*App, error) {
	if logger == nil {
		logger = slog.Default()
	}

	dbDriver := database.DriverSQLite
	dbDSN := cfg.Database.Path
	if cfg.Database.IsPostgres() {
		dbDriver = database.DriverPostgres
		dbDSN = cfg.Database.DSN
	}
	db, err := database.NewDB(database.Options{Driver: dbDriver, DSN: dbDSN})
	if err != nil {
		return nil, fmt.Errorf("database init failed: %w", err)
	}

	settingsSvc := settings.New(db.Client, logger)
	merged, settingsErr := settingsSvc.Load(context.Background(), cfg)
	if settingsErr != nil {
		logger.Error("settings overlay failed, running with the environment configuration",
			"error", settingsErr)
	}

	client, jar := exhentai.NewHTTPClient(merged.Cookie)
	if !merged.Cookie.IsValid() {
		logger.Warn("ExHentai cookies are not configured; set them on the settings page before searching")
	}

	syncSvc := synclib.NewService(db.Client, synclib.Options{HostToken: merged.SyncToken})
	if merged.SyncToken != "" {
		logger.Info("sync host enabled")
	}

	// Record gallery cache cleanup tombstones after syncSvc is available.
	if cacheKeys, cleanErr := db.CleanupGalleryCache(context.Background()); cleanErr != nil {
		logger.Warn("gallery cache startup cleanup failed", "error", cleanErr)
	} else if len(cacheKeys) > 0 {
		logger.Info("gallery cache startup cleanup", "deleted", len(cacheKeys))
		syncKeys := make([]synclib.Key, len(cacheKeys))
		for i, k := range cacheKeys {
			syncKeys[i] = synclib.Key{GalleryID: k.GalleryID, Token: k.Token}
		}
		if recErr := syncSvc.RecordDeletes(context.Background(), synclib.EntityGalleryCache, syncKeys); recErr != nil {
			logger.Warn("record sync cache cleanup failed", "error", recErr)
		}
	}

	initial, err := storage.New(context.Background(), merged.Storage)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("storage init failed: %w", err)
	}
	// Handlers keep one storage handle for the lifetime of the process; a
	// settings save swaps what is behind it.
	store := storage.NewSwitchable(initial)
	logger.Info("storage enabled", "driver", merged.Storage.ResolvedDriver())

	srv := handler.New(handler.Config{
		Client:   client,
		DB:       db,
		Cache:    store,
		DevTools: merged.DevTools,
		Settings: settingsSvc,
		Sync:     syncSvc,
	})

	port := merged.Port
	if port != "" && !strings.HasPrefix(port, ":") {
		port = ":" + port
	}

	a := &App{
		Config:   merged,
		Logger:   logger,
		DB:       db,
		Storage:  store,
		Handler:  srv,
		Settings: settingsSvc,
		Sync:     syncSvc,
		server: &http.Server{
			Addr:              port,
			Handler:           gin.New(),
			ReadHeaderTimeout: 10 * time.Second,
			ReadTimeout:       30 * time.Second,
			IdleTimeout:       120 * time.Second,
			// WriteTimeout intentionally unset: ZIP/image streaming can run long.
		},
	}
	for _, opt := range opts {
		opt(a)
	}

	// The hooks are bound before any request can arrive, so the settings API
	// never sees a partially wired process.
	settingsSvc.Bind(settings.Hooks{
		Storage: func(c config.StorageConfig) error {
			// A settings save must not hang on an unreachable endpoint, and it
			// must not inherit the HTTP request's deadline either.
			ctx, cancel := context.WithTimeout(context.Background(), storageConnectTimeout)
			defer cancel()
			next, err := storage.New(ctx, c)
			if err != nil {
				return err
			}
			store.Replace(next)
			return nil
		},
		Cookies:  jar.Replace,
		LogLevel: a.setLogLevel,
		DevTools: srv.SetDevTools,
	})
	if a.setLogLevel != nil {
		a.setLogLevel(merged.LogLevel)
	}

	r := gin.Default()
	r.Use(CORSMiddleware())
	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})
	srv.RegisterRoutes(r)
	a.Router = r
	a.server.Handler = r

	// Sync client engine: pushes local changes and maintains the SSE
	// subscription to the configured peer. Idle until sync is enabled from
	// the settings page; exits with the context on shutdown.
	engCtx, engCancel := context.WithCancel(context.Background())
	a.syncCancel = engCancel
	go syncSvc.RunEngine(engCtx)

	return a, nil
}

// HTTPHandler exposes the HTTP handler without starting a listener. Used by
// the desktop app, which serves through Wails' own asset-server plumbing.
func (a *App) HTTPHandler() http.Handler { return a.server.Handler }

// Addr returns the configured listen address (":8080" style).
func (a *App) Addr() string { return a.server.Addr }

// Serve serves on ln until Shutdown is called.
func (a *App) Serve(ln net.Listener) error {
	if err := a.server.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

// Close releases every resource. It is the right call on error paths where the
// server never started serving.
func (a *App) Close() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return a.Shutdown(ctx)
}

// Listen opens the configured TCP address for serving.
func (a *App) Listen() (net.Listener, error) {
	ln, err := net.Listen("tcp", a.server.Addr)
	if err != nil {
		return nil, fmt.Errorf("app: listen %s: %w", a.server.Addr, err)
	}
	return ln, nil
}

// Shutdown stops background work, drains in-flight requests and releases the
// database. It is safe to call more than once.
func (a *App) Shutdown(ctx context.Context) error {
	if a.closed {
		return nil
	}
	a.closed = true

	if a.syncCancel != nil {
		a.syncCancel()
	}
	a.Handler.StopPrefill()

	var errs []error
	if err := a.server.Shutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := a.DB.Close(); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("app shutdown: %v", errs)
	}
	return nil
}
