package handler

import (
	"bytes"
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/cache"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/membudget"
)

// ==================== URL Validation Tests ====================

func TestValidatePageURL_Valid(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"exhentai page", "https://exhentai.org/s/51d1aa689c/4153369-29"},
		{"e-hentai page", "https://e-hentai.org/s/859299c9ef/3138775-7"},
		{"exhentai with query", "https://exhentai.org/s/abc/123-1?nl=SZF-483294"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := exhentai.ValidatePageURL(tt.url); err != nil {
				t.Errorf("validatePageURL(%q) = %v, want nil", tt.url, err)
			}
		})
	}
}

func TestValidatePageURL_Invalid(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"no scheme", "exhentai.org/s/abc/123-1"},
		{"ftp scheme", "ftp://exhentai.org/s/abc/123-1"},
		{"http scheme", "http://exhentai.org/s/abc/123-1"},
		{"file scheme", "file:///etc/passwd"},
		{"gopher scheme", "gopher://exhentai.org"},
		{"localhost", "https://localhost/s/abc/123-1"},
		{"loopback", "https://127.0.0.1/s/abc/123-1"},
		{"ipv6 loopback", "https://[::1]/s/abc/123-1"},
		{"private ip 10.x", "https://10.0.0.1/s/abc/123-1"},
		{"private ip 172.16.x", "https://172.16.0.1/s/abc/123-1"},
		{"private ip 192.168.x", "https://192.168.1.1/s/abc/123-1"},
		{"link-local", "https://169.254.1.1/s/abc/123-1"},
		{"wrong domain", "https://example.com/s/abc/123-1"},
		{"subdomain exhentai", "https://evil.exhentai.org/s/abc/123-1"},
		{"subdomain e-hentai", "https://evil.e-hentai.org/s/abc/123-1"},
		{"metadata aws", "http://169.254.169.254/latest/meta-data/"},
		{"metadata gcp", "http://metadata.google.internal/computeMetadata/v1/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := exhentai.ValidatePageURL(tt.url); err == nil {
				t.Errorf("validatePageURL(%q) = nil, want error", tt.url)
			}
		})
	}
}

// ==================== Handler Tests (Mock HTTP) ====================

func TestCachedImage_MissingURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	app := &Server{Client: &http.Client{}}
	r.GET("/api/image-cache/page", app.handleCachedImage)

	req := httptest.NewRequest("GET", "/api/image-cache/page", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCachedImage_InvalidURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	app := &Server{Client: &http.Client{}}
	r.GET("/api/image-cache/page", app.handleCachedImage)

	tests := []struct {
		name string
		url  string
	}{
		{"empty url", "https://"},
		{"localhost", "https://localhost/"},
		{"private ip", "https://192.168.1.1/"},
		{"wrong domain", "https://example.com/"},
		{"ftp scheme", "ftp://exhentai.org/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req := httptest.NewRequest("GET", "/api/image-cache/page?url="+url.QueryEscape(tt.url), nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d for URL %q", w.Code, http.StatusBadRequest, tt.url)
			}
		})
	}
}

func TestCachedImage_MissingMinIO(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	app := &Server{Client: &http.Client{}}
	r.GET("/api/image-cache/page", app.handleCachedImage)

	pageURL := "https://exhentai.org/s/abc/123-1"
	req := httptest.NewRequest("GET", "/api/image-cache/page?url="+url.QueryEscape(pageURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should fail because Cache is nil (no MinIO configured)
	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d when MinIO is not configured", w.Code, http.StatusServiceUnavailable)
	}
}

// ==================== Mock Cache for Handler Tests ====================

type mockCacheEntry struct {
	data        []byte
	contentType string
	exists      bool
}

// ==================== MinIOConfig Tests ====================

func TestMinIOConfig_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		config cache.MinIOConfig
		valid  bool
	}{
		{"complete", cache.MinIOConfig{Endpoint: "minio:9000", AccessKey: "key", SecretKey: "secret", Bucket: "bucket"}, true},
		{"missing endpoint", cache.MinIOConfig{AccessKey: "key", SecretKey: "secret", Bucket: "bucket"}, false},
		{"missing access key", cache.MinIOConfig{Endpoint: "minio:9000", SecretKey: "secret", Bucket: "bucket"}, false},
		{"missing secret key", cache.MinIOConfig{Endpoint: "minio:9000", AccessKey: "key", Bucket: "bucket"}, false},
		{"missing bucket", cache.MinIOConfig{Endpoint: "minio:9000", AccessKey: "key", SecretKey: "secret"}, false},
		{"all empty", cache.MinIOConfig{}, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.config.IsValid(); got != tt.valid {
				t.Errorf("IsValid() = %v, want %v", got, tt.valid)
			}
		})
	}
}

// ==================== SSRF Boundary Tests ====================

func TestValidatePageURL_DomainBoundary(t *testing.T) {
	tests := []struct {
		name    string
		url     string
		wantErr bool
	}{
		{"exact exhentai.org", "https://exhentai.org/s/abc/123-1", false},
		{"exact e-hentai.org", "https://e-hentai.org/s/abc/123-1", false},
		{"subdomain exhentai.org", "https://sub.exhentai.org/s/abc/123-1", true},
		{"subdomain e-hentai.org", "https://sub.e-hentai.org/s/abc/123-1", true},
		{"exhentai.org.evil.com", "https://exhentai.org.evil.com/s/abc/123-1", true},
		{"e-hentai.org.evil.com", "https://e-hentai.org.evil.com/s/abc/123-1", true},
		{"evil-exhentai.org", "https://evil-exhentai.org/s/abc/123-1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := exhentai.ValidatePageURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePageURL(%q) error = %v, wantErr = %v", tt.url, err, tt.wantErr)
			}
		})
	}
}

// ==================== Streaming Cache-Hit Tests ====================

// A warm cache hit must be proxied straight from the store: no buffered Get,
// Content-Length set up front, body byte-identical.
func TestCachedImage_HitStreamsWithoutBuffering(t *testing.T) {
	pageURL := "https://exhentai.org/s/51d1aa689c/4153369-29"
	imgData := mockImageBytes()

	store := newMockImageCache()
	if err := store.Put(context.Background(), cache.CacheKey(pageURL), imgData, "image/webp", ""); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	server := &Server{Client: &http.Client{}, Cache: store}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/image-cache/page?url="+url.QueryEscape(pageURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	if got := w.Header().Get("Content-Type"); got != "image/webp" {
		t.Errorf("Content-Type = %q, want image/webp", got)
	}
	if got := w.Header().Get("Content-Length"); got != fmt.Sprintf("%d", len(imgData)) {
		t.Errorf("Content-Length = %q, want %d", got, len(imgData))
	}
	if !bytes.Equal(w.Body.Bytes(), imgData) {
		t.Errorf("body mismatch: got %d bytes, want %d", w.Body.Len(), len(imgData))
	}
	if got := store.streamGets.Load(); got != 1 {
		t.Errorf("stream Gets = %d, want 1", got)
	}
	if got := store.bufferedGets.Load(); got != 0 {
		t.Errorf("buffered Gets = %d, want 0 (hit must not buffer)", got)
	}
}

// A cache miss streams the miss attempt, then falls back to the buffered
// fetch path (download + store + respond).
func TestCachedImage_MissFallsBackToBufferedFetch(t *testing.T) {
	imgData := mockImageBytes()
	var upstream atomic.Int32
	srv := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/s/") {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, mockPageHTML("https://example.com/image.webp", ""))
			return
		}
		upstream.Add(1)
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(imgData)
	})
	defer srv.Close()

	pageURL := "https://exhentai.org/s/abc123/4153369-29"
	store := newMockImageCache()
	server := &Server{Client: newMockClient(srv.URL), Cache: store}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/image-cache/page?url="+url.QueryEscape(pageURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), imgData) {
		t.Errorf("body mismatch: got %d bytes, want %d", w.Body.Len(), len(imgData))
	}
	if got := upstream.Load(); got != 1 {
		t.Errorf("upstream fetches = %d, want 1", got)
	}
	if got := store.streamGets.Load(); got != 1 {
		t.Errorf("stream Gets = %d, want 1 (miss probe)", got)
	}
	if got := store.bufferedGets.Load(); got != 1 {
		t.Errorf("buffered Gets = %d, want 1 (miss path)", got)
	}
	// The fetched image must now be in the store for the next hit to stream.
	if _, _, _, err := store.GetStream(context.Background(), cache.CacheKey(pageURL)); err != nil {
		t.Errorf("fetched image not stored: %v", err)
	}
}

// A burst of concurrent cold requests for the same image must be charged for
// the payload once, not once per request: the leader reserves the buffer and
// the waiters share that reservation. Before the fix every request acquired the
// estimate *before* joining the singleflight and released its own reservation,
// so N requests reserved N copies of a buffer only one of them ever held.
func TestCachedImage_ConcurrentMissSharesOneReservation(t *testing.T) {
	if limit := membudget.Limit(); limit < 2*membudget.DefaultWeight {
		t.Skipf("budget %d too small for this test's assumption", limit)
	}

	entered := make(chan struct{})
	release := make(chan struct{})
	var enterOnce sync.Once

	pageURL := "https://exhentai.org/s/abc123/4153369-29"
	imgData := mockImageBytes()

	var upstream atomic.Int32
	srv := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/s/") {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, mockPageHTML("https://example.com/image.webp", ""))
			return
		}
		// Block the single upstream image fetch so every request has time to
		// join the shared flight before the reservation is sampled.
		upstream.Add(1)
		enterOnce.Do(func() { close(entered) })
		<-release
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(imgData)
	})
	defer srv.Close()

	server := &Server{Client: newMockClient(srv.URL), Cache: newMockImageCache()}
	r := setupMockRouter(server)

	const n = 12
	start := make(chan struct{})
	codes := make(chan int, n)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			<-start
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/api/image-cache/page?url="+url.QueryEscape(pageURL), nil))
			codes <- w.Code
		})
	}
	close(start)

	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("leader never reached the upstream image fetch")
	}
	time.Sleep(200 * time.Millisecond)

	if got := membudget.Reserved(); got > 2*membudget.DefaultWeight {
		t.Errorf("reserved with %d requests sharing one payload = %d bytes, want <= %d (one reservation, not %d)",
			n, got, 2*membudget.DefaultWeight, n)
	}
	close(release)
	wg.Wait()
	close(codes)

	for code := range codes {
		if code != http.StatusOK {
			t.Errorf("status = %d, want 200", code)
		}
	}
	if got := upstream.Load(); got != 1 {
		t.Errorf("upstream image fetches = %d, want 1", got)
	}
	if got := membudget.Reserved(); got != 0 {
		t.Errorf("reserved after requests completed = %d, want 0", got)
	}
}

// The thumbnail cache-hit path shares the streaming helper; assert it too.
func TestCachedThumbnail_HitStreamsWithoutBuffering(t *testing.T) {
	thumbURL := "https://s.exhentai.org/w/00/999/15582-3owak8q3.webp"
	imgData := mockImageBytes()

	store := newMockImageCache()
	if err := store.Put(context.Background(), ThumbnailCacheKey(thumbURL), imgData, "image/webp", ""); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	server := &Server{Client: &http.Client{}, Cache: store}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/image-cache/thumbnail?url="+url.QueryEscape(thumbURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	if !bytes.Equal(w.Body.Bytes(), imgData) {
		t.Errorf("body mismatch: got %d bytes, want %d", w.Body.Len(), len(imgData))
	}
	if got := store.streamGets.Load(); got != 1 {
		t.Errorf("stream Gets = %d, want 1", got)
	}
	if got := store.bufferedGets.Load(); got != 0 {
		t.Errorf("buffered Gets = %d, want 0 (hit must not buffer)", got)
	}
}
