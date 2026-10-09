package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/basicauth"
	"manga-reader/internal/cache"
	"manga-reader/internal/config"
	"manga-reader/internal/database"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/handler"
	"manga-reader/internal/metrics"
	synclib "manga-reader/internal/sync"
	"manga-reader/internal/web"
)

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

// securityHeadersMiddleware carries over the headers the removed proxy layer
// used to add. CSP stays out on purpose: a strict policy risks breaking the
// React app, SSE and the image proxy.
func securityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		h := c.Writer.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Next()
	}
}

func parseLogLevel(value string) slog.Level {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "debug":
		return slog.LevelDebug
	case "info":
		return slog.LevelInfo
	case "warn", "warning":
		return slog.LevelWarn
	case "error":
		return slog.LevelError
	default:
		return slog.LevelWarn
	}
}

func setupLogger(level string) *slog.Logger {
	logLevel := parseLogLevel(level)
	handler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: logLevel,
	})
	logger := slog.New(handler)
	slog.SetDefault(logger)
	return logger
}

func main() {
	if err := run(); err != nil {
		slog.Error("application failed", "error", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger := setupLogger(cfg.LogLevel)
	logger.Info("starting server",
		"port", cfg.Port,
		"environment", cfg.Environment,
		"db_driver", cfg.Database.Driver,
	)

	client, err := exhentai.CreateHTTPClient(&exhentai.CookieConfig{
		IpbMemberID: cfg.Cookie.IpbMemberID,
		IpbPassHash: cfg.Cookie.IpbPassHash,
		Igneous:     cfg.Cookie.Igneous,
		SK:          cfg.Cookie.SK,
	})
	if err != nil {
		logger.Error("http client init failed", "error", err)
		return err
	}

	dbDriver := database.DriverSQLite
	dbDSN := cfg.Database.Path
	if cfg.Database.IsPostgres() {
		dbDriver = database.DriverPostgres
		dbDSN = cfg.Database.DSN
	}
	db, err := database.NewDB(database.Options{Driver: dbDriver, DSN: dbDSN})
	if err != nil {
		logger.Error("database init failed", "error", err)
		return err
	}
	defer db.Close()

	syncSvc := synclib.NewService(db.Client, synclib.Options{HostToken: cfg.SyncToken})
	if cfg.SyncToken != "" {
		logger.Info("sync host enabled")
	}

	// Startup sweep of cache rows orphaned by the last run; moved after
	// syncSvc exists so removals propagate as tombstones to the peer.
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

	handlerCfg := handler.Config{
		Client:   client,
		DB:       db,
		DevTools: cfg.DevTools,
		Sync:     syncSvc,
	}

	if cfg.MinIO.IsValid() {
		minioCache, err := cache.NewMinIOCache(&cache.MinIOConfig{
			Endpoint:  cfg.MinIO.Endpoint,
			AccessKey: cfg.MinIO.AccessKey,
			SecretKey: cfg.MinIO.SecretKey,
			Bucket:    cfg.MinIO.Bucket,
			UseSSL:    cfg.MinIO.UseSSL,
			Region:    cfg.MinIO.Region,
		})
		if err != nil {
			logger.Error("minio cache init failed", "error", err)
			return err
		}
		handlerCfg.Cache = minioCache
		logger.Info("minio cache enabled", "bucket", cfg.MinIO.Bucket)
	} else {
		logger.Warn("minio config not set, cached-image/cached-thumbnail endpoints will return 503")
	}

	srv := handler.New(handlerCfg)

	// Production runs release mode: gin's debug mode dumps the full route
	// table at startup and logs every request line (including query strings).
	if cfg.Environment == "production" {
		gin.SetMode(gin.ReleaseMode)
	}
	r := gin.Default()
	r.Use(CORSMiddleware())
	r.Use(securityHeadersMiddleware())
	r.Use(basicauth.Middleware(cfg.BasicAuth))

	if cfg.BasicAuth.Enabled {
		logger.Info("basic auth enabled", "source", map[bool]string{true: "htpasswd file", false: "environment"}[cfg.BasicAuth.File != ""])
	}

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	// Prometheus exposition for upstream traffic. Registered before
	// basicauth.Middleware so it is covered by Basic Auth like the API.
	r.GET("/metrics", gin.WrapH(metrics.Handler()))

	srv.RegisterRoutes(r)

	// Static frontend: embedded at build time (see internal/web). A binary
	// built without dist stays API-only.
	if fsys := web.FS(); fsys != nil {
		web.Register(r, fsys)
		logger.Info("embedded frontend enabled")
	} else {
		logger.Warn("no embedded frontend in this binary; serving the API only")
	}

	port := cfg.Port
	if port[0] != ':' {
		port = ":" + port
	}

	httpServer := &http.Server{
		Addr:              port,
		Handler:           r,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		IdleTimeout:       120 * time.Second,
		// WriteTimeout intentionally unset: ZIP/image streaming can run long.
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	// Sync client engine: pushes local changes and maintains the SSE
	// subscription to the configured peer. Exits with ctx on shutdown.
	go syncSvc.RunEngine(ctx)

	errCh := make(chan error, 1)
	go func() {
		logger.Info("listening", "addr", port)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		srv.StopPrefill()
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	srv.StopPrefill()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		if errors.Is(err, context.DeadlineExceeded) {
			// Open SSE streams (/api/events/changes, /api/sync/events) keep
			// their connections alive and Shutdown waits for them forever;
			// force-close instead of failing the exit status.
			logger.Warn("graceful shutdown timed out; closing open connections")
			_ = httpServer.Close()
			return nil
		}
		logger.Error("graceful shutdown failed", "error", err)
		return err
	}
	return nil
}
