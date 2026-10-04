//go:build android

package main

import (
	"context"
	"log/slog"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strings"

	"github.com/wailsapp/wails/v3/pkg/application"

	"manga-reader/internal/app"
	"manga-reader/internal/config"
)

func init() {
	// Register main function to be called when the Android app initializes
	// This is necessary because in c-shared build mode, main() is not automatically called
	application.RegisterAndroidMain(runAndroid)
}

// runAndroid is the Android-specific entry point.
func runAndroid() {
	// On Android, the WebView is created by the Java host, so we just need
	// to initialize the application and start the Gin server.
	configPath := loadEnvFile()
	applyDefaults()

	cfg, err := config.Load()
	if err != nil {
		return
	}

	logger, closeLogger, setLevel := setupLogger(cfg.LogLevel)
	defer closeLogger()

	a, err := app.New(cfg, logger, app.WithLogSetter(setLevel))
	if err != nil {
		return
	}

	logger.Info("starting android app",
		"db_driver", a.Config.Database.Driver,
		"storage_driver", a.Config.Storage.ResolvedDriver(),
		"log_level", a.Config.LogLevel,
		"config_file", configPath,
	)

	// Start Gin server on loopback
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		a.Close()
		return
	}
	apiBaseURL := "http://" + ln.Addr().String()

	go func() {
		logger.Info("api listening", "addr", ln.Addr().String())
		_ = a.Serve(ln)
	}()

	// Write runtime config for frontend
	runtimeFile := runtimeConfigPath()
	_ = writeRuntimeConfig(runtimeFile, runtimeConfig{APIBaseURL: apiBaseURL})

	// Create the Wails v3 application.
	wailsApp := application.New(application.Options{
		Name:        "Manga Reader",
		Description: "Manga Reader Android application",
		Assets: application.AssetOptions{
			Handler:    &androidAssets{apiBaseURL: apiBaseURL, logger: logger},
			Middleware: nil,
		},
	})

	// On Android, the window is created by the Java host
	wailsApp.OnShutdown(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := a.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown failed", "error", err)
		}
	})

	// Run the application
	_ = wailsApp.Run()
}

// androidAssets serves the embedded frontend and proxies API requests.
type androidAssets struct {
	apiBaseURL string
	logger     *slog.Logger
}

func (a *androidAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Proxy API requests to the loopback Gin server
	if r.URL.Path == "/healthz" || r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		target, _ := url.Parse(a.apiBaseURL)
		proxy := httputil.NewSingleHostReverseProxy(target)
		proxy.FlushInterval = -1
		proxy.ErrorLog = slog.NewLogLogger(a.logger.Handler(), slog.LevelWarn)
		proxy.ServeHTTP(w, r)
		return
	}

	// Serve embedded frontend
	// For Android, we need to serve from the embedded dist
	http.FileServer(http.FS(frontendDist)).ServeHTTP(w, r)
}