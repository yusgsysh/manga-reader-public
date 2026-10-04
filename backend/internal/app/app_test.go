package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
)

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestCORSMiddleware_SetsHeadersAndHandlesPreflight(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(CORSMiddleware())
	r.GET("/api/ping", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })

	req := httptest.NewRequest(http.MethodGet, "/api/ping", nil)
	req.Header.Set("Origin", "wails://wails")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("GET status = %d", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}

	req = httptest.NewRequest(http.MethodOptions, "/api/ping", nil)
	req.Header.Set("Origin", "wails://wails")
	req.Header.Set("Access-Control-Request-Method", "POST")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS status = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Methods"); got == "" {
		t.Fatal("Access-Control-Allow-Methods missing on preflight")
	}
}

func newTestConfig(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "1")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "hash")
	t.Setenv("MANGA_READER_DB_DRIVER", "sqlite")
	t.Setenv("MANGA_READER_DB_PATH", filepath.Join(t.TempDir(), "test.db"))
	t.Setenv("MANGA_READER_STORAGE_DRIVER", "local")
	t.Setenv("MANGA_READER_STORAGE_DIR", filepath.Join(t.TempDir(), "cache"))

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func TestNew_BootsDatabaseStorageAndRoutes(t *testing.T) {
	cfg := newTestConfig(t)

	a, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if a.Storage == nil {
		t.Fatal("Storage is nil")
	}
	if a.DB == nil || a.DB.Client == nil {
		t.Fatal("DB is nil")
	}

	r := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	a.HTTPHandler().ServeHTTP(r, req)
	if r.Code != http.StatusOK {
		t.Fatalf("/healthz = %d, want 200", r.Code)
	}
}

func TestNew_UsesLocalDriverWhenConfigured(t *testing.T) {
	cfg := newTestConfig(t)
	a, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if cfg.Storage.ResolvedDriver() != config.StorageDriverLocal {
		t.Fatalf("driver = %q, want local", cfg.Storage.ResolvedDriver())
	}
}

func TestServeAndShutdown(t *testing.T) {
	cfg := newTestConfig(t)
	a, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}

	ln, err := a.Listen()
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}

	done := make(chan error, 1)
	go func() { done <- a.Serve(ln) }()

	resp, err := http.Get("http://" + ln.Addr().String() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("Shutdown: %v", err)
	}
	if err := <-done; err != nil {
		t.Fatalf("Serve returned: %v", err)
	}
	// Shutdown is idempotent.
	if err := a.Shutdown(ctx); err != nil {
		t.Fatalf("second Shutdown: %v", err)
	}
}
