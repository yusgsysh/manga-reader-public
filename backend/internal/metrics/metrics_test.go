package metrics

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

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
	for _, want := range []string{
		`exhentai_upstream_requests_total`,
		`endpoint="page_image"`,
		`outcome="ok"`,
		// Declared and actual bytes agree here; the counter follows reads,
		// not Content-Length.
		`exhentai_upstream_response_bytes_total{endpoint="page_image"} 4`,
		// Everything finished, so nothing may stay in flight.
		`exhentai_upstream_inflight_requests 0`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in exposition, got:\n%s", want, text)
		}
	}
}

// TestTransportCountsBodyBytesWithoutContentLength: the byte counter must
// follow what callers read. Transparent gunzip and chunked responses both
// report ContentLength -1, so counting the header would silently drop them.
func TestTransportCountsBodyBytesWithoutContentLength(t *testing.T) {
	ResetForTest()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("he"))
		// Flush without a Content-Length forces a chunked response.
		w.(http.Flusher).Flush()
		_, _ = w.Write([]byte("llo"))
	}))
	defer upstream.Close()

	client := &http.Client{Transport: NewTransport(upstream.Client().Transport)}
	resp, err := client.Get(upstream.URL + "/s/abc/1-1")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if resp.ContentLength != -1 {
		t.Fatalf("ContentLength = %d, want -1 (test needs a chunked response)", resp.ContentLength)
	}
	body, err := io.ReadAll(resp.Body)
	if cerr := resp.Body.Close(); cerr != nil {
		t.Fatalf("close body: %v", cerr)
	}
	if err != nil {
		t.Fatalf("read body: %v", err)
	}
	if string(body) != "hello" {
		t.Fatalf("body = %q, want hello", body)
	}

	text := gather(t)
	want := `exhentai_upstream_response_bytes_total{endpoint="page_image"} 5`
	if !strings.Contains(text, want) {
		t.Fatalf("expected %q in exposition, got:\n%s", want, text)
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
	ObserveClassification("gallery", OutcomeSadPanda, http.StatusOK)
	ObserveClassification("gallery", OutcomeIPBanned, http.StatusForbidden)
	ObserveClassification("api", OutcomeHTTPError, http.StatusServiceUnavailable)
	ObserveClassification("page_image", OutcomeHTTPError, http.StatusNotFound)

	text := gather(t)
	for _, want := range []string{
		`exhentai_upstream_classification_total{code="200",endpoint="gallery",outcome="sad_panda"} 1`,
		`exhentai_upstream_classification_total{code="403",endpoint="gallery",outcome="ip_banned"} 1`,
		`exhentai_upstream_classification_total{code="503",endpoint="api",outcome="http_error"} 1`,
		`exhentai_upstream_classification_total{code="404",endpoint="page_image",outcome="http_error"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in exposition, got:\n%s", want, text)
		}
	}
}

func TestUpstreamRetryCounter(t *testing.T) {
	ResetForTest()
	ObserveUpstreamRetry("page_image", "thumbnail")
	ObserveUpstreamRetry("page_image", "thumbnail")
	ObserveUpstreamRetry("page_image", "nl_fallback")

	text := gather(t)
	for _, want := range []string{
		`exhentai_upstream_retries_total{endpoint="page_image",reason="thumbnail"} 2`,
		`exhentai_upstream_retries_total{endpoint="page_image",reason="nl_fallback"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in exposition, got:\n%s", want, text)
		}
	}
}

func TestMinIOCacheCounters(t *testing.T) {
	ResetForTest()
	ObserveMinIOCache("get", "ok", 2*time.Millisecond)
	ObserveMinIOCache("get", "not_found", time.Millisecond)
	ObserveMinIOCache("put", "error", 3*time.Millisecond)

	text := gather(t)
	for _, want := range []string{
		`minio_cache_requests_total{op="get",result="ok"} 1`,
		`minio_cache_requests_total{op="get",result="not_found"} 1`,
		`minio_cache_requests_total{op="put",result="error"} 1`,
		`minio_cache_operation_duration_seconds_count{op="get"} 2`,
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

// TestGinMiddlewareCountsPanics mirrors main.go: metrics must be outermost so
// Recovery can turn a panic into a 500 *before* the recording code runs. If
// Recovery wrapped the metrics middleware instead, the panic would unwind past
// the recording code and the 500 would never be counted.
func TestGinMiddlewareCountsPanics(t *testing.T) {
	ResetForTest()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GinMiddleware())
	r.Use(gin.Recovery())
	r.GET("/api/boom", func(c *gin.Context) {
		panic("boom")
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/boom", nil))

	text := gather(t)
	want := `http_requests_total{method="GET",route="/api/boom",status_code="500"} 1`
	if !strings.Contains(text, want) {
		t.Fatalf("expected %q in exposition, got:\n%s", want, text)
	}
}

// TestGinMiddlewareRouteOverride: handlers that run outside gin's route table
// (static files, SPA shell) label themselves so they do not all pile into
// route="unmatched" together with real 404s.
func TestGinMiddlewareRouteOverride(t *testing.T) {
	ResetForTest()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GinMiddleware())
	r.NoRoute(func(c *gin.Context) {
		if strings.HasPrefix(c.Request.URL.Path, "/assets/") {
			SetRouteLabel(c, "/static")
		}
		c.String(http.StatusOK, "served")
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/assets/app.js", nil))
	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/index.html", nil))

	text := gather(t)
	for _, want := range []string{
		`http_requests_total{method="GET",route="/static",status_code="200"} 1`,
		`http_requests_total{method="GET",route="unmatched",status_code="200"} 1`,
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected %q in exposition, got:\n%s", want, text)
		}
	}
}

// TestGinMiddlewareSkipsLongRunningDuration: SSE/NDJSON/ZIP streams stay open
// for hours; observing them would park every sample in the +Inf bucket.
func TestGinMiddlewareSkipsLongRunningDuration(t *testing.T) {
	ResetForTest()
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(GinMiddleware())
	r.GET("/api/stream", func(c *gin.Context) {
		MarkLongRunning(c)
		c.String(http.StatusOK, "ok")
	})

	r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/stream", nil))

	text := gather(t)
	if !strings.Contains(text, `http_requests_total{method="GET",route="/api/stream",status_code="200"} 1`) {
		t.Fatalf("long-running request must still be counted, got:\n%s", text)
	}
	if strings.Contains(text, `http_request_duration_seconds_count{method="GET",route="/api/stream"}`) {
		t.Fatalf("long-running request must not be observed in the histogram, got:\n%s", text)
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
