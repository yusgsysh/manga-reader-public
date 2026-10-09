// Command manga-reader-desktop is the Wails desktop entrypoint. It boots the
// exact same Gin REST API as the web build (internal/app), then serves the
// existing React frontend inside a WebView through Wails' asset server. There
// is no second API surface: everything the UI can do in a browser it does here,
// against http://127.0.0.1:<dynamic port>.
package main

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/wailsapp/wails/v3/pkg/application"

	"manga-reader/internal/app"
	"manga-reader/internal/config"
	"manga-reader/internal/logx"
)

// frontendDist holds the built frontend. Wails copies frontend/dist here right
// before compiling (SyncFrontendDistToEmbedTarget) and restores the directory
// to a bare .gitkeep afterwards, so the embed always matches the app binary.
//
//go:embed all:dist
var frontendDist embed.FS

// shutdownTimeout bounds graceful shutdown of the in-process API.
const shutdownTimeout = 15 * time.Second

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "manga-reader-desktop:", err)
		os.Exit(1)
	}
}

func run() error {
	configPath := loadEnvFile()
	applyDefaults()

	cfg, err := config.Load()
	if err != nil {
		return err
	}

	logger, closeLogger, setLevel := setupLogger(cfg.LogLevel)
	defer closeLogger()

	a, err := app.New(cfg, logger, app.WithLogSetter(setLevel))
	if err != nil {
		return err
	}

	logger.Info("starting desktop app",
		"db_driver", a.Config.Database.Driver,
		"storage_driver", a.Config.Storage.ResolvedDriver(),
		"log_level", a.Config.LogLevel,
		"config_file", configPath,
	)

	// Always bind loopback: the API is private to this machine. Port 0 picks a
	// free port so several desktop instances never collide.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		a.Close()
		return fmt.Errorf("listen on 127.0.0.1: %w", err)
	}
	apiBaseURL := "http://" + ln.Addr().String()

	serveErr := make(chan error, 1)
	go func() {
		logger.Info("api listening", "addr", ln.Addr().String())
		serveErr <- a.Serve(ln)
	}()

	// Both the production build and the Vite dev server learn the dynamic API
	// port from one small JSON file, written here and removed on exit.
	runtimeFile := runtimeConfigPath()
	if err := writeRuntimeConfig(runtimeFile, runtimeConfig{APIBaseURL: apiBaseURL}); err != nil {
		logger.Warn("could not write runtime config", "path", runtimeFile, "error", err)
		runtimeFile = ""
	} else {
		defer os.Remove(runtimeFile)
	}

	dist, err := fs.Sub(frontendDist, "dist")
	if err != nil {
		a.Close()
		return fmt.Errorf("desktop: locate frontend assets: %w", err)
	}
	assets, err := newDesktopAssets(dist, apiBaseURL, logger)
	if err != nil {
		a.Close()
		return err
	}

	// Create the Wails v3 application.
	wailsApp := application.New(application.Options{
		Name:        "Manga Reader",
		Description: "Manga Reader desktop application",
		Assets: application.AssetOptions{
			Handler:    assets,
			Middleware: nil,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
		Linux: application.LinuxOptions{
			ProgramName: "manga-reader-desktop",
		},
	})

	// On Android, the window is fullscreen and managed by the OS.
	if application.System.IsMobile() {
		wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
			Title: "Manga Reader",
			URL:   "/",
		})
	} else {
		wailsApp.Window.NewWithOptions(application.WebviewWindowOptions{
			Title:     "Manga Reader",
			Width:     1280,
			Height:    820,
			MinWidth:  960,
			MinHeight: 640,
			URL:       "/",
		})
	}

	wailsApp.OnShutdown(func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
		defer cancel()
		if err := a.Shutdown(shutdownCtx); err != nil {
			logger.Error("shutdown failed", "error", err)
		}
	})

	wailsErr := make(chan error, 1)
	go func() {
		wailsErr <- wailsApp.Run()
	}()

	select {
	case err := <-wailsErr:
		a.Close()
		return err
	case err := <-serveErr:
		return fmt.Errorf("api server stopped: %w", err)
	}
}

// setupLogger mirrors the web build's JSON logger into stdout and, when the
// user's data directory is writable, into a file — a double-clicked GUI app has
// no terminal to read.
// setupLogger returns the logger, the closer that releases the log file, and
// the setter that changes the level at runtime (app.WithLogSetter). The desktop
// log file is the only diagnostic a user has, so a failure to open it falls
// back to stdout rather than to silence.
func setupLogger(level string) (*slog.Logger, func(), func(string)) {
	parse := parseLogLevel
	// The base handlers accept every level; logx gates on the mutable one.
	out := &slog.HandlerOptions{Level: slog.LevelDebug}
	stdout := slog.NewJSONHandler(os.Stdout, out)

	var base slog.Handler = stdout
	var f *os.File

	logFile := logFilePath()
	if err := os.MkdirAll(filepath.Dir(logFile), 0o755); err != nil {
		base = stdout
	} else if opened, err := os.OpenFile(logFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644); err != nil {
		base = stdout
	} else {
		f = opened
		base = &teeHandler{a: stdout, b: slog.NewJSONHandler(opened, out)}
	}

	dyn := logx.New(base, parse(level))
	logger := dyn.Logger()
	slog.SetDefault(logger)
	if f == nil {
		logger.Warn("could not open the log file, logging to stdout only", "path", logFile)
	}

	setLevel := func(value string) { dyn.SetLevel(parse(value)) }
	closeLogger := func() {
		if f != nil {
			_ = f.Close()
		}
	}
	return logger, closeLogger, setLevel
}

type teeHandler struct {
	a, b slog.Handler
}

func (t *teeHandler) Enabled(ctx context.Context, l slog.Level) bool {
	return t.a.Enabled(ctx, l)
}

func (t *teeHandler) Handle(ctx context.Context, r slog.Record) error {
	_ = t.a.Handle(ctx, r)
	return t.b.Handle(ctx, r)
}

func (t *teeHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &teeHandler{a: t.a.WithAttrs(attrs), b: t.b.WithAttrs(attrs)}
}

func (t *teeHandler) WithGroup(name string) slog.Handler {
	return &teeHandler{a: t.a.WithGroup(name), b: t.b.WithGroup(name)}
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

// applyDefaults applies platform-specific defaults.
func applyDefaults() {
	applyPlatformDefaults()
}

func setDefault(key, value string) {
	if os.Getenv(key) == "" {
		_ = os.Setenv(key, value)
	}
}

// loadEnvFile reads a KEY=VALUE file so users can configure ExHentai cookies
// without touching their shell environment. The path can be overridden with
// MANGA_READER_CONFIG_FILE; the returned path ("" when no file was found) is
// used in error messages.
func loadEnvFile() string {
	path := os.Getenv("MANGA_READER_CONFIG_FILE")
	if path == "" {
		path = envFilePath()
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	for _, line := range strings.Split(string(raw), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		key = strings.TrimSpace(key)
		value = strings.Trim(strings.TrimSpace(value), `"'`)
		if key == "" || os.Getenv(key) != "" {
			continue
		}
		_ = os.Setenv(key, value)
	}
	return path
}

// runtimeConfig is the JSON blob shared with the Vite dev server.
type runtimeConfig struct {
	APIBaseURL string `json:"apiBaseUrl"`
}

func runtimeConfigPath() string {
	if p := os.Getenv("MANGA_READER_RUNTIME_FILE"); p != "" {
		return p
	}
	return runtimeConfigPathValue()
}

func writeRuntimeConfig(path string, cfg runtimeConfig) error {
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
