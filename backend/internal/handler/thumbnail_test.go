package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/minio/minio-go/v7"

	"manga-reader/internal/cache"
	"manga-reader/internal/exhentai"
)

const testThumbURL = "https://s.exhentai.org/w/00/999/15582-3owak8q3.webp"

// ==================== Cache Key Tests ====================

func TestThumbnailCacheKey_SameURL(t *testing.T) {
	key1 := ThumbnailCacheKey(testThumbURL)
	key2 := ThumbnailCacheKey(testThumbURL)
	if key1 != key2 {
		t.Errorf("same URL should produce same key: %q != %q", key1, key2)
	}
}

func TestThumbnailCacheKey_DifferentURL(t *testing.T) {
	u1 := testThumbURL
	u2 := testThumbURL + "?x=1"
	if ThumbnailCacheKey(u1) == ThumbnailCacheKey(u2) {
		t.Error("different URLs should produce different keys")
	}
}

func TestThumbnailCacheKey_Format(t *testing.T) {
	key := ThumbnailCacheKey(testThumbURL)

	if !strings.HasPrefix(key, thumbnailCachePrefix) {
		t.Errorf("key should start with %q, got %q", thumbnailCachePrefix, key)
	}

	hexPart := strings.TrimPrefix(key, thumbnailCachePrefix)
	if len(hexPart) != cache.CacheKeyLength {
		t.Errorf("hex part length = %d, want %d", len(hexPart), cache.CacheKeyLength)
	}

	if _, err := hex.DecodeString(hexPart); err != nil {
		t.Errorf("hex part should be valid hex: %v", err)
	}
}

func TestThumbnailCacheKey_MatchesSHA256(t *testing.T) {
	sum := sha256.Sum256([]byte(testThumbURL))
	expected := thumbnailCachePrefix + hex.EncodeToString(sum[:])

	if key := ThumbnailCacheKey(testThumbURL); key != expected {
		t.Errorf("key = %q, want %q", key, expected)
	}
}

func TestThumbnailCacheKey_DifferentPartsProduceDifferentKeys(t *testing.T) {
	urls := []string{
		testThumbURL,
		"https://s.exhentai.org/w/00/999/15582-3owak8q4.webp",
		"https://ehgt.org/w/00/999/15582-3owak8q3.webp",
		"https://s.exhentai.org/w/01/000/15582-3owak8q3.webp",
		testThumbURL + "?nl=XYZ",
	}

	seen := make(map[string]string)
	for _, u := range urls {
		key := ThumbnailCacheKey(u)
		if prev, ok := seen[key]; ok {
			t.Errorf("URL %q and %q produced same key %q", u, prev, key)
		}
		seen[key] = u
	}
}

// ==================== URL Validation Tests ====================

func TestValidateThumbnailURL_Valid(t *testing.T) {
	valid := []string{
		testThumbURL,
		"https://s.exhentai.org/w/00/999/15582-3owak8q3.webp?x=1",
		"https://ehgt.org/w/00/999/thumb.jpg",
		"https://ul.e-hentai.org/w/00/999/thumb.png",
	}

	for _, u := range valid {
		if err := exhentai.ValidateThumbnailURL(u); err != nil {
			t.Errorf("validateThumbnailURL(%q) = %v, want nil", u, err)
		}
	}
}

func TestValidateThumbnailURL_Invalid(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"empty", ""},
		{"http scheme", "http://s.exhentai.org/w/00/999/t.webp"},
		{"no scheme", "s.exhentai.org/w/00/999/t.webp"},
		{"ftp scheme", "ftp://s.exhentai.org/x"},
		{"file scheme", "file:///etc/passwd"},
		{"localhost", "https://localhost/x"},
		{"loopback", "https://127.0.0.1/x"},
		{"ipv6 loopback", "https://[::1]/x"},
		{"private ip 10.x", "https://10.0.0.1/x"},
		{"private ip 172.x", "https://172.16.0.1/x"},
		{"private ip 192.168.x", "https://192.168.1.1/x"},
		{"link-local", "https://169.254.169.254/latest/meta-data/"},
		{"metadata gcp", "https://metadata.google.internal/computeMetadata/v1/"},
		{"wrong domain", "https://example.com/x"},
		{"base exhentai.org", "https://exhentai.org/w/00/999/t.webp"},
		{"subdomain not allowed", "https://evil.exhentai.org/x"},
		{"suffix trick", "https://s.exhentai.org.evil.com/x"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := exhentai.ValidateThumbnailURL(tt.url); err == nil {
				t.Errorf("validateThumbnailURL(%q) = nil, want error", tt.url)
			}
		})
	}
}

// ==================== Source Proxy Handler Tests ====================

func TestThumbnail_MissingURL(t *testing.T) {
	r := setupRouter()
	app := &Server{Client: &http.Client{}}
	r.GET("/api/thumbnail", app.handleThumbnail)

	req := httptest.NewRequest("GET", "/api/thumbnail", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if resp["error"] != "missing url parameter" {
		t.Errorf("error = %q, want %q", resp["error"], "missing url parameter")
	}
}

func TestThumbnail_InvalidURL(t *testing.T) {
	r := setupRouter()
	app := &Server{Client: &http.Client{}}
	r.GET("/api/thumbnail", app.handleThumbnail)

	tests := []string{
		"",
		"https://",
		"http://s.exhentai.org/x",
		"https://localhost/x",
		"https://192.168.1.1/x",
		"https://example.com/x",
		"https://exhentai.org/x",
		"ftp://s.exhentai.org/x",
	}

	for _, u := range tests {
		req := httptest.NewRequest("GET", "/api/thumbnail?url="+url.QueryEscape(u), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d for URL %q", w.Code, http.StatusBadRequest, u)
		}
	}
}

func TestThumbnail_Success(t *testing.T) {
	imgData := mockImageBytes()
	var gotReferer, gotUA string
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		gotReferer = r.Header.Get("Referer")
		gotUA = r.Header.Get("User-Agent")
		w.Header().Set("Content-Type", "image/webp")
		w.Write(imgData)
	})
	defer mockServer.Close()

	r := setupRouter()
	app := &Server{Client: newMockClient(mockServer.URL)}
	r.GET("/api/thumbnail", app.handleThumbnail)

	req := httptest.NewRequest("GET", "/api/thumbnail?url="+url.QueryEscape(testThumbURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "image/webp" {
		t.Errorf("Content-Type = %q, want %q", w.Header().Get("Content-Type"), "image/webp")
	}
	if len(w.Body.Bytes()) != len(imgData) {
		t.Errorf("body len = %d, want %d", len(w.Body.Bytes()), len(imgData))
	}
	if gotReferer == "" {
		t.Error("Referer header not set on upstream request")
	}
	if gotUA == "" {
		t.Error("User-Agent header not set on upstream request")
	}
}

func TestThumbnail_SourceFailure(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer mockServer.Close()

	r := setupRouter()
	app := &Server{Client: newMockClient(mockServer.URL)}
	r.GET("/api/thumbnail", app.handleThumbnail)

	req := httptest.NewRequest("GET", "/api/thumbnail?url="+url.QueryEscape(testThumbURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// ==================== Mock Image Cache ====================

type mockImageCache struct {
	mu      sync.RWMutex
	objects map[string]mockCacheEntry
}

func newMockImageCache() *mockImageCache {
	return &mockImageCache{objects: make(map[string]mockCacheEntry)}
}

func (m *mockImageCache) Head(_ context.Context, key string) (minio.ObjectInfo, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if e, ok := m.objects[key]; ok && e.exists {
		return minio.ObjectInfo{ContentType: e.contentType}, nil
	}
	return minio.ObjectInfo{}, minio.ErrorResponse{Code: "NoSuchKey"}
}

func (m *mockImageCache) Get(_ context.Context, key string) ([]byte, string, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	e, ok := m.objects[key]
	if !ok || !e.exists {
		return nil, "", minio.ErrorResponse{Code: "NoSuchKey"}
	}
	return e.data, e.contentType, nil
}

func (m *mockImageCache) Put(ctx context.Context, key string, data []byte, contentType string, cacheControl string) error {
	return m.PutWithMeta(ctx, key, data, contentType, nil, cacheControl)
}

func (m *mockImageCache) PutWithMeta(_ context.Context, key string, data []byte, contentType string, meta map[string]string, cacheControl string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = mockCacheEntry{data: data, contentType: contentType, exists: true}
	return nil
}

// ==================== Cached Thumbnail Handler Tests ====================

func TestCachedThumbnail_MissingURL(t *testing.T) {
	r := setupRouter()
	app := &Server{Client: &http.Client{}}
	r.GET("/api/cached-thumbnail", app.handleCachedThumbnail)

	req := httptest.NewRequest("GET", "/api/cached-thumbnail", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCachedThumbnail_InvalidURL(t *testing.T) {
	r := setupRouter()
	app := &Server{Client: &http.Client{}}
	r.GET("/api/cached-thumbnail", app.handleCachedThumbnail)

	tests := []string{
		"https://",
		"https://localhost/x",
		"https://192.168.1.1/x",
		"https://example.com/x",
		"http://s.exhentai.org/x",
	}

	for _, u := range tests {
		req := httptest.NewRequest("GET", "/api/cached-thumbnail?url="+url.QueryEscape(u), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("status = %d, want %d for URL %q", w.Code, http.StatusBadRequest, u)
		}
	}
}

func TestCachedThumbnail_MissingMinIO(t *testing.T) {
	r := setupRouter()
	app := &Server{Client: &http.Client{}}
	r.GET("/api/cached-thumbnail", app.handleCachedThumbnail)

	req := httptest.NewRequest("GET", "/api/cached-thumbnail?url="+url.QueryEscape(testThumbURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d when MinIO is not configured", w.Code, http.StatusServiceUnavailable)
	}
}

func TestCachedThumbnail_MissThenHit(t *testing.T) {
	var fetchCount atomic.Int32
	imgData := mockImageBytes()
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		w.Header().Set("Content-Type", "image/webp")
		w.Write(imgData)
	})
	defer mockServer.Close()

	cache := newMockImageCache()
	r := setupRouter()
	app := &Server{Client: newMockClient(mockServer.URL), Cache: cache}
	r.GET("/api/cached-thumbnail", app.handleCachedThumbnail)

	escaped := url.QueryEscape(testThumbURL)
	expectedKey := ThumbnailCacheKey(testThumbURL)

	// First request: MISS → ExHentai → MinIO PUT → 200
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, httptest.NewRequest("GET", "/api/cached-thumbnail?url="+escaped, nil))

	if w1.Code != http.StatusOK {
		t.Fatalf("first status = %d, want %d. body: %s", w1.Code, http.StatusOK, w1.Body.String())
	}
	if w1.Header().Get("Content-Type") != "image/webp" {
		t.Errorf("first Content-Type = %q, want %q", w1.Header().Get("Content-Type"), "image/webp")
	}
	if len(w1.Body.Bytes()) != len(imgData) {
		t.Errorf("first body len = %d, want %d", len(w1.Body.Bytes()), len(imgData))
	}
	if got := fetchCount.Load(); got != 1 {
		t.Errorf("fetchCount after first = %d, want 1", got)
	}
	if _, ok := cache.objects[expectedKey]; !ok {
		t.Errorf("expected cached object at key %q", expectedKey)
	}
	if got := cache.objects[expectedKey].contentType; got != "image/webp" {
		t.Errorf("cached contentType = %q, want %q", got, "image/webp")
	}

	// Second request: HIT → MinIO only, no ExHentai access
	w2 := httptest.NewRecorder()
	r.ServeHTTP(w2, httptest.NewRequest("GET", "/api/cached-thumbnail?url="+escaped, nil))

	if w2.Code != http.StatusOK {
		t.Fatalf("second status = %d, want %d. body: %s", w2.Code, http.StatusOK, w2.Body.String())
	}
	if got := fetchCount.Load(); got != 1 {
		t.Errorf("fetchCount after second = %d, want still 1 (no ExHentai access on HIT)", got)
	}
	if len(w2.Body.Bytes()) != len(imgData) {
		t.Errorf("second body len = %d, want %d", len(w2.Body.Bytes()), len(imgData))
	}
}

// ==================== Singleflight Concurrency Test ====================

func TestCachedThumbnail_ConcurrentMissSingleFetch(t *testing.T) {
	var fetchCount atomic.Int32
	imgData := mockImageBytes()
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		fetchCount.Add(1)
		w.Header().Set("Content-Type", "image/webp")
		w.Write(imgData)
	})
	defer mockServer.Close()

	cache := newMockImageCache()
	r := setupRouter()
	app := &Server{Client: newMockClient(mockServer.URL), Cache: cache}
	r.GET("/api/cached-thumbnail", app.handleCachedThumbnail)

	escaped := url.QueryEscape(testThumbURL)

	const n = 10
	var wg sync.WaitGroup
	var errCount atomic.Int32
	start := make(chan struct{})

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", "/api/cached-thumbnail?url="+escaped, nil))
			if w.Code != http.StatusOK {
				errCount.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if errCount.Load() > 0 {
		t.Errorf("concurrent cached-thumbnail had %d errors", errCount.Load())
	}
	count := fetchCount.Load()
	t.Logf("Concurrent cached-thumbnail: %d goroutines, %d upstream fetches", n, count)
	if count != 1 {
		t.Errorf("expected exactly 1 upstream fetch, got %d", count)
	}
}

// ==================== Retry Tests ====================

func TestFetchThumbnail_Retries(t *testing.T) {
	imgData := mockImageBytes()
	var attempts atomic.Int32
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Write(imgData)
	})
	defer mockServer.Close()

	client := newMockClient(mockServer.URL)
	data, contentType, err := fetchThumbnail(context.Background(), client, testThumbURL)
	if err != nil {
		t.Fatalf("fetchThumbnail error: %v", err)
	}
	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3 (initial + 2 retries)", got)
	}
	if contentType != "image/webp" {
		t.Errorf("contentType = %q, want %q", contentType, "image/webp")
	}
	if len(data) != len(imgData) {
		t.Errorf("body len = %d, want %d", len(data), len(imgData))
	}
}

func TestFetchThumbnail_RetriesExhausted(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer mockServer.Close()

	client := newMockClient(mockServer.URL)
	_, _, err := fetchThumbnail(context.Background(), client, testThumbURL)
	if err == nil {
		t.Error("expected error after exhausting retries")
	}
}
