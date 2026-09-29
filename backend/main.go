package main

import (
	"log/slog"
	"net/http"
	"os"
	"strings"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/cache"
	"manga-reader/internal/config"
	"manga-reader/internal/database"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/handler"
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
		"db_path", cfg.DBPath,
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

	db, err := database.NewDB(cfg.DBPath)
	if err != nil {
		logger.Error("database init failed", "error", err)
		return err
	}
	defer db.Close()

	handlerCfg := handler.Config{
		Client: client,
		DB:     db,
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

	r := gin.Default()
	r.Use(CORSMiddleware())

	r.GET("/healthz", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"status": "ok"})
	})

	srv.RegisterRoutes(r)

	port := cfg.Port
	if port[0] != ':' {
		port = ":" + port
	}
	logger.Info("listening", "addr", port)
	return r.Run(port)
}
