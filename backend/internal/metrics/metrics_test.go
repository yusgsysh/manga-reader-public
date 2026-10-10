package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestEndpointClassification(t *testing.T) {
	cases := []struct {
		url  string
		want string
	}{
		{"https://api.e-hentai.org/api.php", "api"},
		{"https://exhentai.org/s/abc123/12345-1", "page_image"},
		{"https://exhentai.org/g/123456/token/", "gallery"},
		{"https://exhentai.org/?f_search=nature", "search"},
		{"https://exhentai.org/watched", "listing"},
		{"https://exhentai.org/popular", "listing"},
		{"https://exhentai.org/", "listing"},
		{"https://exhentai.org/?page=2", "listing"},
		{"https://ehgt.org/a1/b2/thumb.jpg", "image_cdn"},
		{"https://hath.serverfarm.example/h/xyz/file.png", "image_cdn"},
		{"https://exhentai.org/torrents/123456/token", "other"},
	}
	for _, tc := range cases {
		if got := Endpoint(tc.url); got != tc.want {
			t.Errorf("Endpoint(%q) = %q, want %q", tc.url, got, tc.want)
		}
	}
}

func TestTransportRecordsRequest(t *testing.T) {
	ResetForTest()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Length", "4")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("pong"))
	}))
	defer upstream.Close()

	client := &http.Client{Transport: NewTransport(upstream.Client().Transport)}
	resp, err := client.Get(upstream.URL + "/s/abc/1-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()

	text := gather(t)
	if !strings.Contains(text, `exhentai_upstream_requests_total`) {
		t.Fatalf("expected requests_total in exposition, got:\n%s", text)
	}
	if !strings.Contains(text, `endpoint="page_image"`) {
		t.Fatalf("expected page_image endpoint label, got:\n%s", text)
	}
	if !strings.Contains(text, `outcome="ok"`) {
		t.Fatalf("expected outcome=ok, got:\n%s", text)
	}
}

func TestTransportRecordsError(t *testing.T) {
	ResetForTest()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	url := upstream.URL
	upstream.Close() // force connection error

	client := &http.Client{Transport: NewTransport(http.DefaultTransport)}
	if resp, getErr := client.Get(url); getErr == nil {
		_ = resp.Body.Close()
	}

	text := gather(t)
	if !strings.Contains(text, `outcome="error"`) {
		t.Fatalf("expected outcome=error, got:\n%s", text)
	}
}

func TestClassificationCounter(t *testing.T) {
	ResetForTest()
	ObserveClassification("gallery", OutcomeSadPanda)
	ObserveClassification("gallery", OutcomeIPBanned)
	ObserveClassification("api", OutcomeHTTPError)

	text := gather(t)
	for _, want := range []string{
		`exhentai_upstream_classification_total{endpoint="gallery",outcome="sad_panda"} 1`,
		`exhentai_upstream_classification_total{endpoint="gallery",outcome="ip_banned"} 1`,
		`exhentai_upstream_classification_total{endpoint="api",outcome="http_error"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in exposition, got:\n%s", want, text)
		}
	}
}

func TestFetchCacheCounters(t *testing.T) {
	ResetForTest()
	ObserveFetchCache("hit")
	ObserveFetchCache("miss")
	ObserveFetchCache("coalesced")
	SetFetchCacheEntries(3)

	text := gather(t)
	for _, want := range []string{
		`exhentai_fetchcache_requests_total{result="hit"} 1`,
		`exhentai_fetchcache_requests_total{result="miss"} 1`,
		`exhentai_fetchcache_requests_total{result="coalesced"} 1`,
		`exhentai_fetchcache_entries 3`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in exposition, got:\n%s", want, text)
		}
	}
}

func TestImageCacheCounters(t *testing.T) {
	ResetForTest()
	ObserveImageCache("page_image", "hit")
	ObserveImageCache("page_image", "miss")
	ObserveImageCache("thumbnail", "hit")
	ObserveImageCache("sprite", "miss")
	ObserveImageCache("sprite", "miss")

	text := gather(t)
	for _, want := range []string{
		`image_cache_requests_total{kind="page_image",result="hit"} 1`,
		`image_cache_requests_total{kind="page_image",result="miss"} 1`,
		`image_cache_requests_total{kind="thumbnail",result="hit"} 1`,
		`image_cache_requests_total{kind="sprite",result="miss"} 2`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in exposition, got:\n%s", want, text)
		}
	}
}

func TestGalleryCacheCounters(t *testing.T) {
	ResetForTest()
	ObserveGalleryCache("gallery", "hit")
	ObserveGalleryCache("details", "miss")
	ObserveGalleryCache("pages", "hit")
	ObserveGalleryCache("page_thumb", "miss")

	text := gather(t)
	for _, want := range []string{
		`gallery_cache_requests_total{kind="gallery",result="hit"} 1`,
		`gallery_cache_requests_total{kind="details",result="miss"} 1`,
		`gallery_cache_requests_total{kind="pages",result="hit"} 1`,
		`gallery_cache_requests_total{kind="page_thumb",result="miss"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in exposition, got:\n%s", want, text)
		}
	}
}

func TestGinMiddlewareRecordsRequests(t *testing.T) {
	ResetForTest()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GinMiddleware())
	r.GET("/api/ping", func(c *gin.Context) {
		c.JSON(http.StatusOK, gin.H{"ok": true})
	})
	r.GET("/api/fail", func(c *gin.Context) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "boom"})
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/ping", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/api/ping", nil)) // 404, unmatched route
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/fail", nil))

	text := gather(t)
	for _, want := range []string{
		`http_requests_total{method="GET",route="/api/ping",status_code="200"} 1`,
		`http_requests_total{method="POST",route="unmatched",status_code="404"} 1`,
		`http_requests_total{method="GET",route="/api/fail",status_code="500"} 1`,
		`http_request_duration_seconds_count{method="GET",route="/api/fail"} 1`,
		`go_goroutines `,
		`process_resident_memory_bytes `,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in exposition, got:\n%s", want, text)
		}
	}
}

func gather(t *testing.T) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics handler status = %d", rec.Code)
	}
	return rec.Body.String()
}
