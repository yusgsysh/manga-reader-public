package main

import (
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

func testAssets(t *testing.T) *desktopAssets {
	t.Helper()
	dist := fstest.MapFS{
		"index.html":     &fstest.MapFile{Data: []byte("<html><head><title>Manga Reader</title></head><body><div id=root></div></body></html>")},
		"favicon.svg":    &fstest.MapFile{Data: []byte("<svg/>")},
		"assets/app.js":  &fstest.MapFile{Data: []byte("console.log(1)")},
		"assets/app.css": &fstest.MapFile{Data: []byte("body{}")},
	}
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	return &desktopAssets{
		logger: logger,
		dist:   dist,
		index:  injectRuntimeConfig(index, "http://127.0.0.1:9999"),
		proxy: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"proxied":"` + r.URL.Path + `"}`))
		}),
	}
}

func TestMiddleware_RoutesAPIRequestsToProxy(t *testing.T) {
	d := testAssets(t)
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("static handler called for %s", r.URL.Path)
	})
	h := d.Middleware(next)

	for _, p := range []string{"/api/gallery/1/tok", "/api/search?q=x", "/healthz"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"proxied"`) {
			t.Errorf("GET %s = %d %q, want proxied response", p, w.Code, w.Body.String())
		}
	}
}

func TestMiddleware_PreflightGoesToProxyToo(t *testing.T) {
	d := testAssets(t)
	h := d.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("static handler called for OPTIONS")
	}))
	req := httptest.NewRequest(http.MethodOptions, "/api/prefill", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("OPTIONS status = %d", w.Code)
	}
}

func TestMiddleware_ServesRealStaticFiles(t *testing.T) {
	d := testAssets(t)
	h := d.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	}))

	for _, p := range []string{"/favicon.svg", "/assets/app.js", "/assets/app.css"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusTeapot {
			t.Errorf("GET %s = %d, want static handler (418)", p, w.Code)
		}
	}
}

func TestMiddleware_SPARoutesFallBackToIndex(t *testing.T) {
	d := testAssets(t)
	h := d.Middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("static handler called for %s", r.URL.Path)
	}))

	for _, p := range []string{"/", "/index.html", "/gallery/123/token", "/missing.js", "/../etc/passwd"} {
		req := httptest.NewRequest(http.MethodGet, p, nil)
		w := httptest.NewRecorder()
		h.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", p, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
			t.Errorf("GET %s Content-Type = %q, want text/html", p, ct)
		}
		if !strings.Contains(w.Body.String(), "window.__MANGA_READER_CONFIG__") {
			t.Errorf("GET %s body is missing the runtime config script", p)
		}
	}
}

func TestMiddleware_StaticFilesBypassTheInjectedIndex(t *testing.T) {
	d := testAssets(t)
	h := d.Middleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	req := httptest.NewRequest(http.MethodGet, "/assets/app.js", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	if strings.Contains(w.Body.String(), "__MANGA_READER_CONFIG__") {
		t.Fatal("static asset got the SPA index body")
	}
}

func TestIsAPIPath(t *testing.T) {
	tests := map[string]bool{
		"/api":                     true,
		"/api/":                    true,
		"/api/gallery/1/tok":       true,
		"/healthz":                 true,
		"/":                        false,
		"/index.html":              false,
		"/assets/app.js":           false,
		"/apifoo":                  false,
		"/gallery/api/not-a-route": false,
	}
	for p, want := range tests {
		if got := isAPIPath(p); got != want {
			t.Errorf("isAPIPath(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestStaticAssetName(t *testing.T) {
	tests := []struct {
		in     string
		wantOK bool
	}{
		{"/", false},
		{"/index.html", false},
		{"/favicon.svg", true},
		{"/assets/app.js", true},
		{"/../secret", true}, // cleaned to "/secret", then stat'd against the embed FS
	}
	for _, tt := range tests {
		name, ok := staticAssetName(tt.in)
		if ok != tt.wantOK {
			t.Errorf("staticAssetName(%q) ok = %v, want %v", tt.in, ok, tt.wantOK)
		}
		if ok && (strings.HasPrefix(name, "/") || strings.Contains(name, "..")) {
			t.Errorf("staticAssetName(%q) = %q, must be a relative name", tt.in, name)
		}
	}
}

func TestInjectRuntimeConfig_InsertsBeforeHeadClose(t *testing.T) {
	in := []byte("<html><head><TITLE>x</TITLE></head><body></body></html>")
	out := injectRuntimeConfig(in, "http://127.0.0.1:1234")

	if !strings.Contains(string(out), `window.__MANGA_READER_CONFIG__={"apiBaseUrl":"http://127.0.0.1:1234"}`) {
		t.Fatalf("injected script missing: %s", out)
	}
	if strings.Count(string(out), "</html>") != 1 || strings.Count(string(out), "<html>") != 1 {
		t.Fatalf("html structure changed: %s", out)
	}
	if !strings.Contains(string(out), "<TITLE>x</TITLE>") {
		t.Fatalf("original head content lost: %s", out)
	}
	// The script must sit before </head> so it runs before the app bundle.
	if strings.Index(string(out), "MANGA_READER_CONFIG__") > strings.Index(strings.ToLower(string(out)), "</head>") {
		t.Fatalf("script injected after </head>: %s", out)
	}
}

func TestInjectRuntimeConfig_NoHeadFallsBackToPrefix(t *testing.T) {
	out := injectRuntimeConfig([]byte("<body>hi</body>"), "http://127.0.0.1:1")
	if !strings.HasPrefix(string(out), "<script>window.__MANGA_READER_CONFIG__=") {
		t.Fatalf("expected prefix injection, got %s", out)
	}
}

func TestRuntimeConfig_RoundTrip(t *testing.T) {
	path := filepath.Join(t.TempDir(), "runtime.json")
	if err := writeRuntimeConfig(path, runtimeConfig{APIBaseURL: "http://127.0.0.1:5555"}); err != nil {
		t.Fatalf("writeRuntimeConfig: %v", err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile: %v", err)
	}
	if string(raw) != `{"apiBaseUrl":"http://127.0.0.1:5555"}` {
		t.Fatalf("runtime config = %s", raw)
	}
}

func TestRuntimeConfigPath_EnvOverride(t *testing.T) {
	t.Setenv("MANGA_READER_RUNTIME_FILE", "/tmp/custom.json")
	if got := runtimeConfigPath(); got != "/tmp/custom.json" {
		t.Fatalf("runtimeConfigPath() = %q", got)
	}
	t.Setenv("MANGA_READER_RUNTIME_FILE", "")
	if got := runtimeConfigPath(); got == "" || !strings.HasSuffix(got, "manga-reader-desktop.json") {
		t.Fatalf("runtimeConfigPath() = %q", got)
	}
}

func TestApplyDesktopDefaults_DoesNotClobberExistingEnv(t *testing.T) {
	t.Setenv("MANGA_READER_STORAGE_DRIVER", "s3")
	t.Setenv("MANGA_READER_DB_DRIVER", "postgres")
	t.Setenv("MANGA_READER_DB_PATH", "")
	t.Setenv("MANGA_READER_STORAGE_DIR", "")
	t.Setenv("LOG_LEVEL", "")

	applyDesktopDefaults()

	if got := os.Getenv("MANGA_READER_STORAGE_DRIVER"); got != "s3" {
		t.Fatalf("storage driver overwritten: %q", got)
	}
	if got := os.Getenv("MANGA_READER_DB_DRIVER"); got != "postgres" {
		t.Fatalf("db driver overwritten: %q", got)
	}
	if os.Getenv("MANGA_READER_DB_PATH") == "" {
		t.Fatal("db path default not applied")
	}
	if os.Getenv("MANGA_READER_STORAGE_DIR") == "" {
		t.Fatal("storage dir default not applied")
	}
	if got := os.Getenv("LOG_LEVEL"); got != "info" {
		t.Fatalf("log level default = %q, want info", got)
	}
}

func TestLoadEnvFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.env")
	content := "# comment\n\nEHENTAI_COOKIE_IPB_MEMBER_ID=123\nEHENTAI_COOKIE_IPB_PASS_HASH=\"abc\"\nMALFORMED\nPRESET=from-shell\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}

	t.Setenv("MANGA_READER_CONFIG_FILE", path)
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "")
	t.Setenv("PRESET", "from-shell")

	if got := loadEnvFile(); got != path {
		t.Fatalf("loadEnvFile() = %q, want %q", got, path)
	}
	if os.Getenv("EHENTAI_COOKIE_IPB_MEMBER_ID") != "123" {
		t.Fatalf("member id = %q", os.Getenv("EHENTAI_COOKIE_IPB_MEMBER_ID"))
	}
	if os.Getenv("EHENTAI_COOKIE_IPB_PASS_HASH") != "abc" {
		t.Fatalf("pass hash = %q", os.Getenv("EHENTAI_COOKIE_IPB_PASS_HASH"))
	}
	if os.Getenv("PRESET") != "from-shell" {
		t.Fatalf("existing env was overwritten: %q", os.Getenv("PRESET"))
	}
}

func TestLoadEnvFile_MissingFileIsNotAnError(t *testing.T) {
	t.Setenv("MANGA_READER_CONFIG_FILE", filepath.Join(t.TempDir(), "nope.env"))
	if got := loadEnvFile(); got != "" {
		t.Fatalf("loadEnvFile() = %q, want empty", got)
	}
}

func TestParseLogLevel(t *testing.T) {
	if got := parseLogLevel("DEBUG"); got != slog.LevelDebug {
		t.Fatalf("parseLogLevel(DEBUG) = %v, want DEBUG", got)
	}
	if got := parseLogLevel("nonsense"); got != slog.LevelWarn {
		t.Fatalf("parseLogLevel(nonsense) = %v, want WARN", got)
	}
}
