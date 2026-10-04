package main

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"

	"manga-reader/internal/app"
	"manga-reader/internal/config"
)

// e2eDist is the smallest frontend the WebView router needs: a shell page plus
// one real static asset.
func e2eDist() fstest.MapFS {
	return fstest.MapFS{
		"index.html": &fstest.MapFile{Data: []byte(
			`<html><head><title>Manga Reader</title></head><body><div id="root"></div></body></html>`,
		)},
		"favicon.svg":    &fstest.MapFile{Data: []byte(`<svg/>`)},
		"assets/app.js":  &fstest.MapFile{Data: []byte(`console.log(1)`)},
		"assets/app.css": &fstest.MapFile{Data: []byte(`body{}`)},
	}
}

// newE2EServer wires the desktop middleware in front of the real in-process Gin
// server, so a request that arrives shaped like a WebView request executes the
// same handler chain the web build uses.
func newE2EServer(t *testing.T) (*desktopAssets, http.Handler) {
	t.Helper()

	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "1")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "hash")
	t.Setenv("MANGA_READER_DB_DRIVER", "sqlite")
	t.Setenv("MANGA_READER_DB_PATH", filepath.Join(t.TempDir(), "e2e.db"))
	t.Setenv("MANGA_READER_STORAGE_DRIVER", "local")
	t.Setenv("MANGA_READER_STORAGE_DIR", filepath.Join(t.TempDir(), "cache"))

	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	a, err := app.New(cfg, logger)
	if err != nil {
		t.Fatalf("app.New: %v", err)
	}
	t.Cleanup(func() { _ = a.Close() })

	api := httptest.NewServer(a.HTTPHandler())
	t.Cleanup(api.Close)

	assets, err := newDesktopAssets(e2eDist(), api.URL, logger)
	if err != nil {
		t.Fatalf("newDesktopAssets: %v", err)
	}

	// Wails' asset server: hand static files back from the embedded frontend.
	static := http.FileServer(http.FS(assets.dist))
	return assets, assets.Middleware(static)
}

func TestE2E_APIRequestsReachTheRealGinServer(t *testing.T) {
	_, h := newE2EServer(t)

	for _, path := range []string{"/healthz", "/api/recently-read?page=0"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		req.Header.Set("Origin", "wails://wails.localhost")
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)

		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200 (body %q)", path, w.Code, w.Body.String())
		}
		if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
			t.Errorf("GET %s Access-Control-Allow-Origin = %q, want *", path, got)
		}
	}

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if body := w.Body.String(); !strings.Contains(body, `"ok"`) {
		t.Errorf("GET /healthz body = %q, want the real handler payload", body)
	}
}

func TestE2E_PreflightIsAnsweredWithCORSHeaders(t *testing.T) {
	_, h := newE2EServer(t)

	req := httptest.NewRequest(http.MethodOptions, "/api/search", nil)
	req.Header.Set("Origin", "wails://wails.localhost")
	req.Header.Set("Access-Control-Request-Method", "GET")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusNoContent {
		t.Fatalf("OPTIONS /api/search = %d, want 204", w.Code)
	}
	if got := w.Header().Get("Access-Control-Allow-Origin"); got != "*" {
		t.Fatalf("Access-Control-Allow-Origin = %q, want *", got)
	}
}

func TestE2E_ShellAndStaticAssetsAreNotProxied(t *testing.T) {
	_, h := newE2EServer(t)

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Errorf("GET / Content-Type = %q, want text/html", ct)
	}
	if body := w.Body.String(); !strings.Contains(body, "http://127.0.0.1:") {
		t.Errorf("GET / index is missing the injected API origin: %q", body)
	} else if !strings.Contains(body, `window.__MANGA_READER_CONFIG__=`) {
		t.Errorf("GET / index is missing the runtime config script: %q", body)
	}

	// An SPA route must fall back to the shell rather than 404.
	req = httptest.NewRequest(http.MethodGet, "/gallery/123/tok", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /gallery/123/tok = %d, want 200", w.Code)
	}

	// A real file must be served from the frontend, not proxied to Gin.
	req = httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	w = httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("GET /assets/app.js = %d, want 200", w.Code)
	}
	if body := w.Body.String(); body != "console.log(1)" {
		t.Errorf("GET /assets/app.js body = %q, want the embedded asset", body)
	}
}

func TestE2E_UnreachableAPIServerReturnsBadGateway(t *testing.T) {
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	assets, err := newDesktopAssets(e2eDist(), "http://127.0.0.1:1", logger)
	if err != nil {
		t.Fatalf("newDesktopAssets: %v", err)
	}
	h := assets.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("static handler should not run for /healthz")
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Fatalf("GET /healthz = %d, want 502 when the API is down", w.Code)
	}
}
