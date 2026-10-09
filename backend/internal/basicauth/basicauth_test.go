package basicauth

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
)

func newRouter(t *testing.T, cfg config.BasicAuthConfig) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(Middleware(cfg))
	r.GET("/healthz", func(c *gin.Context) { c.String(http.StatusOK, "ok") })
	r.GET("/api/bookshelf", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"items": []any{}}) })
	r.GET("/", func(c *gin.Context) { c.String(http.StatusOK, "<html>shell</html>") })
	return r
}

func request(r *gin.Engine, target, user, pass string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, target, nil)
	if user != "" || pass != "" {
		req.SetBasicAuth(user, pass)
	}
	r.ServeHTTP(w, req)
	return w
}

func envConfig(user, pass string) config.BasicAuthConfig {
	return config.BasicAuthConfig{Enabled: true, Username: user, Password: pass}
}

func TestDisabledMiddlewareIsPassThrough(t *testing.T) {
	r := newRouter(t, config.BasicAuthConfig{})

	w := request(r, "/", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", w.Code)
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("WWW-Authenticate = %q, want none", got)
	}
	if w = request(r, "/api/bookshelf", "", ""); w.Code != http.StatusOK {
		t.Fatalf("GET /api/bookshelf = %d, want 200", w.Code)
	}
}

func TestEnabledChallengesAnonymousRequests(t *testing.T) {
	r := newRouter(t, envConfig("admin", "hunter2"))

	for _, target := range []string{"/", "/api/bookshelf"} {
		w := request(r, target, "", "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", target, w.Code)
		}
		auth := w.Header().Get("WWW-Authenticate")
		if !strings.HasPrefix(auth, "Basic ") || !strings.Contains(auth, `realm="Manga Reader"`) {
			t.Fatalf("%s: WWW-Authenticate = %q", target, auth)
		}
		if strings.Contains(w.Body.String(), "<html>") {
			t.Fatalf("%s: 401 body must not be the HTML shell", target)
		}
	}
}

func TestEnabledAcceptsOnlyCorrectCredentials(t *testing.T) {
	r := newRouter(t, envConfig("admin", "hunter2"))

	if w := request(r, "/api/bookshelf", "admin", "hunter2"); w.Code != http.StatusOK {
		t.Fatalf("correct credentials: status = %d, want 200", w.Code)
	}
	for _, creds := range [][2]string{{"admin", "wrong"}, {"nobody", "hunter2"}, {"", ""}} {
		w := request(r, "/api/bookshelf", creds[0], creds[1])
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%q/%q: status = %d, want 401", creds[0], creds[1], w.Code)
		}
	}
}

func TestHealthzStaysExempt(t *testing.T) {
	r := newRouter(t, envConfig("admin", "hunter2"))

	w := request(r, "/healthz", "", "")
	if w.Code != http.StatusOK || w.Body.String() != "ok" {
		t.Fatalf("GET /healthz = %d %q, want 200 ok", w.Code, w.Body.String())
	}
	if got := w.Header().Get("WWW-Authenticate"); got != "" {
		t.Fatalf("healthz must not be challenged, got WWW-Authenticate %q", got)
	}
}

func TestHtpasswdFileCredentials(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manga_reader.htpasswd")
	// apr1 hash of "hunter2" (openssl passwd -apr1 -salt saltsalt hunter2).
	content := "admin:$apr1$saltsalt$r/QcFGT5pNL28bNkeDMHR.\n" +
		"bob:$apr1$saltsalt$r/QcFGT5pNL28bNkeDMHR.\n"
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write htpasswd: %v", err)
	}

	r := newRouter(t, config.BasicAuthConfig{Enabled: true, File: path})

	if w := request(r, "/api/bookshelf", "bob", "hunter2"); w.Code != http.StatusOK {
		t.Fatalf("file credentials: status = %d, want 200", w.Code)
	}
	if w := request(r, "/api/bookshelf", "bob", "nope"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong password: status = %d, want 401", w.Code)
	}
	if w := request(r, "/api/bookshelf", "ghost", "hunter2"); w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown user: status = %d, want 401", w.Code)
	}
}

func TestMissingHtpasswdFileFailsClosed(t *testing.T) {
	r := newRouter(t, config.BasicAuthConfig{
		Enabled: true,
		File:    filepath.Join(t.TempDir(), "does-not-exist.htpasswd"),
	})

	if w := request(r, "/", "admin", "anything"); w.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 when the htpasswd file is unreadable", w.Code)
	}
}

// The old proxy layer rate-limited 50 req/s with a burst of 200 and answered
// 429; the same budget must survive the move into the binary.
func TestRateLimitAnswers429AfterBurst(t *testing.T) {
	r := newRouter(t, envConfig("admin", "hunter2"))

	const (
		burst    = 200
		attempts = 300
	)

	limited := 0
	for i := 0; i < attempts; i++ {
		w := request(r, "/api/bookshelf", "admin", "hunter2")
		switch w.Code {
		case http.StatusOK:
			if limited > 0 {
				t.Fatalf("request %d succeeded after the limiter kicked in", i)
			}
		case http.StatusTooManyRequests:
			limited++
			if w.Header().Get("Retry-After") == "" {
				t.Fatal("429 without a Retry-After header")
			}
		default:
			t.Fatalf("request %d: status = %d", i, w.Code)
		}
	}
	if limited == 0 {
		t.Fatalf("no 429 in %d requests (burst is %d)", attempts, burst)
	}
}

// The budget is per client, so one flooded client must not lock others out.
func TestRateLimitIsPerClient(t *testing.T) {
	r := newRouter(t, envConfig("admin", "hunter2"))

	// Exhaust one client's bucket.
	flooded := httptest.NewRecorder()
	for i := 0; i < 300; i++ {
		req := httptest.NewRequest(http.MethodGet, "/api/bookshelf", nil)
		req.RemoteAddr = "198.51.100.7:4444"
		req.SetBasicAuth("admin", "hunter2")
		r.ServeHTTP(flooded, req)
		if flooded.Code == http.StatusTooManyRequests {
			break
		}
	}

	other := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/bookshelf", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	req.SetBasicAuth("admin", "hunter2")
	r.ServeHTTP(other, req)
	if other.Code != http.StatusOK {
		t.Fatalf("unrelated client status = %d, want 200", other.Code)
	}
}
