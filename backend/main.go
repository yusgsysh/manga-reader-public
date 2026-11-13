package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/auth"
	"manga-reader/internal/cache"
	"manga-reader/internal/config"
	"manga-reader/internal/database"
	"manga-reader/internal/handler"
	"manga-reader/internal/metrics"
	"manga-reader/internal/node"
	"manga-reader/internal/requestid"
	synclib "manga-reader/internal/sync"
	"manga-reader/internal/usercookie"
	"manga-reader/internal/web"
)

// corsOriginSet returns extra origins (comma-separated
// MANGA_READER_CORS_ORIGINS) that may call the API cross-origin. The SPA is
// served same-origin and dev uses Vite's proxy, so the default is empty.
func corsOriginSet() map[string]struct{} {
	set := make(map[string]struct{})
	for _, origin := range strings.Split(os.Getenv("MANGA_READER_CORS_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			set[origin] = struct{}{}
		}
	}
	// `bun dev` runs Vite on :5173 and calls the API on :8080 cross-origin.
	// Both are loopback origins; other origins must be listed explicitly.
	set["http://localhost:5173"] = struct{}{}
	set["http://127.0.0.1:5173"] = struct{}{}
	return set
}

// sameOrigin reports whether origin (scheme://host[:port]) targets host.
func sameOrigin(origin, host string) bool {
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" {
		return false
	}
	return strings.EqualFold(u.Host, host)
}

func splitCSV(s string) []string {
	var out []string
	for _, part := range strings.Split(s, ",") {
		if part = strings.TrimSpace(part); part != "" {
			out = append(out, part)
		}
	}
	return out
}

func originAllowed(origin, host string, extra map[string]struct{}) bool {
	if _, ok := extra[origin]; ok {
		return true
	}
	return sameOrigin(origin, host)
}

func CORSMiddleware(extra map[string]struct{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		if origin := c.GetHeader("Origin"); origin != "" && originAllowed(origin, c.Request.Host, extra) {
			c.Header("Access-Control-Allow-Origin", origin)
			c.Header("Access-Control-Allow-Credentials", "true")
			c.Header("Vary", "Origin")
		}
		c.Header("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")
		c.Header("Access-Control-Allow-Headers", "Origin, Content-Type, Accept, Authorization")

		if c.Request.Method == http.MethodOptions {
			c.AbortWithStatus(http.StatusNoContent)
			return
		}

		c.Next()
	}
}

// csrfGuard rejects state-changing requests that a browser marks as
// cross-site. Cookie-based JWT sessions are attached automatically to
// same-origin subrequests, so without this a malicious page could POST to
// /api/reading-progress/cleanup and wipe the user's history. Requests with no
// Origin/Sec-Fetch-Site header (curl, the Go desktop client) are allowed.
func csrfGuard(extra map[string]struct{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}
		if origin := c.GetHeader("Origin"); origin != "" {
			if !originAllowed(origin, c.Request.Host, extra) {
				web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "cross-origin request rejected"})
				return
			}
		} else if site := c.GetHeader("Sec-Fetch-Site"); site != "" && site != "same-origin" && site != "none" {
			web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "cross-site request rejected"})
			return
		}
		c.Next()
	}
}

// adminOnlyAPIs are instance-wide controls. A normal user must never change
// the peer sync configuration or simulate an upstream outage for everyone.
var adminOnlyAPIs = map[string]struct{}{
	"/api/sync/config":       {},
	"/api/sync/status":       {},
	"/api/dev/upstream-down": {},
}

func adminOnlyMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if _, restricted := adminOnlyAPIs[c.FullPath()]; !restricted {
			c.Next()
			return
		}
		principal, ok := auth.PrincipalFrom(c)
		if !ok || principal.Role != config.RoleAdmin {
			web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "administrator required"})
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
		"db_driver", cfg.Database.Driver,
	)

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

	// Resolve the JWT signing key. JWT_SECRET in the environment wins; without
	// it the key is loaded from the database (generated and persisted on the
	// very first boot), so sessions and encrypted ExHentai cookies survive
	// restarts with no operator configuration.
	secret, secretGenerated, err := auth.ResolveSecret(context.Background(), db.Client, cfg.Auth.JWTSecret)
	if err != nil {
		logger.Error("resolve JWT secret failed", "error", err)
		return err
	}
	cfg.Auth.JWTSecret = secret

	// ExHentai sessions are per user, stored encrypted in user_setting.
	userCookies := usercookie.New(db.Client, cfg.Auth.JWTSecret)
	authSvc := auth.New(db.Client, cfg.Auth)
	createdAdmin, err := authSvc.EnsureAdmin(context.Background())
	if err != nil {
		logger.Error("auth initialization failed", "error", err)
		return err
	}
	if createdAdmin {
		logger.Info("administrator account initialized", "username", cfg.Auth.AdminUsername)
	} else if initialized, err := authSvc.IsInitialized(context.Background()); err != nil {
		logger.Error("check initialization failed", "error", err)
		return err
	} else if !initialized {
		logger.Info("no administrator yet; create the first admin account through the web setup page")
	}
	logger.Info("JWT auth enabled", "issuer", cfg.Auth.JWTIssuer, "expires_in", cfg.Auth.ExpiresInSeconds())
	if secretGenerated {
		logger.Info("generated and persisted a new JWT secret (JWT_SECRET was not set)")
	}

	nodeSvc := node.New(db.Client)
	syncSvc := synclib.NewService(db.Client, synclib.Options{
		HostEnabled:           cfg.Sync.Enabled,
		NodeAuth:              nodeSvc.Middleware,
		RequestTimeout:        cfg.Sync.RequestTimeout,
		DisableInactiveOutbox: true,
	})
	if cfg.Sync.Enabled {
		logger.Info("node sync host enabled")
	}

	// No startup sweep of gallery_cache: the read-through cache deliberately
	// holds galleries the user only browsed, so deleting everything not on the
	// shelf/history on every restart wiped the offline cache and replicated
	// tombstones to the peer. Explicit cleanup (bookshelf removal / history
	// purge) still runs and propagates deletions.

	handlerCfg := handler.Config{
		DB:          db,
		DevTools:    cfg.DevTools,
		Sync:        syncSvc,
		UserCookies: userCookies,
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
		// The /api/image-cache/* handlers delegate to their live /api/image/*
		// counterparts when Cache is nil (see handler/image.go), so a MinIO-less
		// deployment still serves images; only the object-store caching is off.
		logger.Warn("minio config not set, image-cache endpoints will fall back to live upstream proxies")
	}

	srv := handler.New(handlerCfg)

	// Release mode avoids gin's debug route dump; request lines are still
	// logged by gin.Logger() below. LOG_LEVEL=debug opts into gin debug mode.
	if strings.EqualFold(strings.TrimSpace(cfg.LogLevel), "debug") {
		gin.SetMode(gin.DebugMode)
	} else {
		gin.SetMode(gin.ReleaseMode)
	}
	// gin.New() rather than gin.Default(): the metrics middleware has to be
	// the outermost one. Recovery below converts a handler panic into a 500
	// *before* the metrics middleware records the response, so panics still
	// land in http_requests_total; with gin.Default() the Recovery would sit
	// outside metrics and unwind past its recording code, hiding every panic.
	r := gin.New()
	// Trust client-supplied X-Forwarded-For only when the operator names the
	// reverse-proxy CIDRs. Gin trusts everything by default, which lets a
	// client rotate X-Forwarded-For and defeat per-IP rate limits.
	if err := r.SetTrustedProxies(splitCSV(os.Getenv("MANGA_READER_TRUSTED_PROXIES"))); err != nil {
		logger.Error("invalid MANGA_READER_TRUSTED_PROXIES", "error", err)
		return err
	}
	corsOrigins := corsOriginSet()

	// Outermost app middleware: counts every request including auth failures.
	r.Use(metrics.GinMiddleware())
	r.Use(gin.Logger())
	r.Use(requestid.Middleware())
	r.Use(gin.Recovery())
	r.Use(CORSMiddleware(corsOrigins))
	r.Use(securityHeadersMiddleware())
	r.Use(csrfGuard(corsOrigins))
	// Default-deny for /api/*: only publicAPIs and node-key sync routes pass
	// without a valid user JWT.
	r.Use(authSvc.Middleware())
	r.Use(adminOnlyMiddleware())

	r.GET("/healthz", func(c *gin.Context) {
		web.JSON(c, http.StatusOK, gin.H{"status": "ok"})
	})

	// Readiness is deliberately separate from liveness: it only says whether
	// the datastore is reachable and never exposes DSNs, paths or secrets.
	r.GET("/readyz", func(c *gin.Context) {
		if db.Conn == nil {
			web.JSON(c, http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		ctx, cancel := context.WithTimeout(c.Request.Context(), 2*time.Second)
		defer cancel()
		if err := db.Conn.PingContext(ctx); err != nil {
			web.JSON(c, http.StatusServiceUnavailable, gin.H{"status": "unavailable"})
			return
		}
		web.JSON(c, http.StatusOK, gin.H{"status": "ready"})
	})

	// Prometheus exposition is its own credential class: loopback-only when
	// no token is configured, otherwise a dedicated metrics bearer token.
	if cfg.Metrics.Enabled {
		r.GET("/metrics", metrics.AuthMiddleware(cfg.Metrics.Token), gin.WrapH(metrics.Handler()))
	}

	authSvc.RegisterRoutes(r)
	srv.RegisterRoutes(r)

	// Static frontend: embedded at build time (see internal/web). A binary
	// built without dist stays API-only.
	if fsys := web.FS(); fsys != nil {
		web.Register(r, fsys)
		logger.Info("embedded frontend enabled")
	} else {
		logger.Warn("no embedded frontend in this binary; serving the API only")
	}

	// Accept ":8080", "8080" and "127.0.0.1:8080" (or "[::1]:8080").
	port := cfg.Port
	if !strings.Contains(port, ":") {
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
