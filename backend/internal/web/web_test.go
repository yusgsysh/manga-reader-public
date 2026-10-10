package web

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/metrics"
)

func testFS() fs.FS {
	return fstest.MapFS{
		"index.html":         &fstest.MapFile{Data: []byte("<html><body>app</body></html>")},
		"assets/app-abc.js":  &fstest.MapFile{Data: []byte("console.log(1)")},
		"db.text.js":         &fstest.MapFile{Data: []byte("load_ehtagtranslation_db_text()")},
		"favicon.svg":        &fstest.MapFile{Data: []byte("<svg/>")},
		"missing-deep/route": &fstest.MapFile{Data: []byte("x")},
	}
}

func newRouter(t *testing.T, fsys fs.FS) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	Register(r, fsys)
	return r
}

func do(r *gin.Engine, method, target string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(method, target, nil)
	r.ServeHTTP(w, req)
	return w
}

func TestRegisterServesShellAtRoot(t *testing.T) {
	r := newRouter(t, testFS())
	w := do(r, http.MethodGet, "/")

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if !strings.Contains(w.Body.String(), "<html>") {
		t.Fatalf("body = %q, want HTML shell", w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Fatalf("Cache-Control = %q, want no-store", cc)
	}
	if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("Content-Type = %q, want text/html", ct)
	}
}

func TestRegisterSPAFallbackForDeepLinks(t *testing.T) {
	r := newRouter(t, testFS())
	for _, target := range []string{
		"/library/1234/abcdef",
		"/reader/1234/abcdef/3",
		"/settings/sync",
	} {
		w := do(r, http.MethodGet, target)
		if w.Code != http.StatusOK {
			t.Fatalf("%s: status = %d, want 200", target, w.Code)
		}
		if !strings.Contains(w.Body.String(), "<html>") {
			t.Fatalf("%s: body = %q, want HTML shell", target, w.Body.String())
		}
		if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
			t.Fatalf("%s: Cache-Control = %q, want no-store", target, cc)
		}
	}
}

func TestRegisterServesRealFile(t *testing.T) {
	r := newRouter(t, testFS())

	w := do(r, http.MethodGet, "/assets/app-abc.js")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Body.String() != "console.log(1)" {
		t.Fatalf("body = %q", w.Body.String())
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=31536000, immutable" {
		t.Fatalf("Cache-Control = %q, want immutable", cc)
	}

	w = do(r, http.MethodGet, "/db.text.js")
	if w.Code != http.StatusOK {
		t.Fatalf("db.text.js status = %d, want 200", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "public, max-age=86400" {
		t.Fatalf("db.text.js Cache-Control = %q", cc)
	}

	w = do(r, http.MethodGet, "/favicon.svg")
	if w.Code != http.StatusOK {
		t.Fatalf("favicon status = %d, want 200", w.Code)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "" {
		t.Fatalf("favicon Cache-Control = %q, want unset", cc)
	}
}

func TestRegisterHeadRequest(t *testing.T) {
	r := newRouter(t, testFS())
	w := do(r, http.MethodHead, "/assets/app-abc.js")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Fatalf("HEAD body = %q, want empty", w.Body.String())
	}
}

func TestRegisterAPI404IsJSON(t *testing.T) {
	r := newRouter(t, testFS())
	for _, target := range []string{"/api/nope", "/api/gallery/x", "/api"} {
		w := do(r, http.MethodGet, target)
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", target, w.Code)
		}
		if ct := w.Header().Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			t.Fatalf("%s: Content-Type = %q, want JSON", target, ct)
		}
		if strings.Contains(w.Body.String(), "<html>") {
			t.Fatalf("%s: API 404 must not return the HTML shell", target)
		}
	}
}

func TestRegisterRejectsNonGetMethods(t *testing.T) {
	r := newRouter(t, testFS())
	for _, method := range []string{http.MethodPost, http.MethodPut, http.MethodDelete} {
		w := do(r, method, "/anything")
		if w.Code != http.StatusNotFound {
			t.Fatalf("%s: status = %d, want 404", method, w.Code)
		}
	}
}

func TestRegisterTraversalsStayInsideFS(t *testing.T) {
	r := newRouter(t, testFS())
	for _, target := range []string{"/../etc/passwd", "/assets/../../secret", "//etc/passwd"} {
		w := do(r, http.MethodGet, target)
		if w.Code == http.StatusNotFound {
			continue // acceptable: cleaned path missing from the FS
		}
		if !strings.Contains(w.Body.String(), "<html>") {
			t.Fatalf("%s: status = %d body = %q, want HTML shell fallback", target, w.Code, w.Body.String())
		}
	}
}

func TestRegisterNilFSKeepsAPIOnly(t *testing.T) {
	r := newRouter(t, nil)
	w := do(r, http.MethodGet, "/")
	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404 without embedded frontend", w.Code)
	}
	if strings.Contains(w.Body.String(), "<html>") {
		t.Fatal("API-only build must not serve an HTML shell")
	}
}

// TestRegisterLabelsRouteForMetrics: static files and the SPA shell are served
// from NoRoute, where gin's FullPath() is empty. Without an explicit label
// every 200 for /assets/* and index.html would pile into route="unmatched"
// alongside real 404s, hiding them and diluting the 5xx ratio.
func TestRegisterLabelsRouteForMetrics(t *testing.T) {
	metrics.ResetForTest()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(metrics.GinMiddleware())
	r.GET("/api/ping", func(c *gin.Context) { c.Status(http.StatusOK) })
	Register(r, testFS())

	do(r, http.MethodGet, "/assets/app-abc.js") // static file
	do(r, http.MethodGet, "/library/1/abcdef")  // SPA shell
	do(r, http.MethodGet, "/api/nope")          // JSON 404, no override

	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, req)
	text := rec.Body.String()

	for _, want := range []string{
		`http_requests_total{method="GET",route="/static",status_code="200"} 1`,
		`http_requests_total{method="GET",route="/spa",status_code="200"} 1`,
		`http_requests_total{method="GET",route="unmatched",status_code="404"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in exposition, got:\n%s", want, text)
		}
	}
}

// FS() returns nil in a source checkout (dist holds only .gitkeep) and a
// usable filesystem once the image build dropped frontend/dist in place.
func TestFSNilWithoutBuiltFrontend(t *testing.T) {
	fsys := FS()
	if fsys == nil {
		t.Skip("frontend dist embedded in this build; placeholder case not applicable")
	}
	if _, err := fs.Stat(fsys, "index.html"); err != nil {
		t.Fatalf("embedded index.html: %v", err)
	}
}
