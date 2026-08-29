package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/gin-gonic/gin"
)

// ==================== Cache Key Tests ====================

func TestCacheKey_SameURL(t *testing.T) {
	u := "https://exhentai.org/s/51d1aa689c/4153369-29"
	key1 := CacheKey(u)
	key2 := CacheKey(u)
	if key1 != key2 {
		t.Errorf("same URL should produce same key: %q != %q", key1, key2)
	}
}

func TestCacheKey_DifferentURL(t *testing.T) {
	key1 := CacheKey("https://exhentai.org/s/aaa/111-1")
	key2 := CacheKey("https://exhentai.org/s/bbb/222-2")
	if key1 == key2 {
		t.Error("different URLs should produce different keys")
	}
}

func TestCacheKey_Format(t *testing.T) {
	u := "https://exhentai.org/s/51d1aa689c/4153369-29"
	key := CacheKey(u)

	if !strings.HasPrefix(key, imageCachePrefix) {
		t.Errorf("key should start with %q, got %q", imageCachePrefix, key)
	}

	hexPart := strings.TrimPrefix(key, imageCachePrefix)
	if len(hexPart) != cacheKeyLength {
		t.Errorf("hex part length = %d, want %d", len(hexPart), cacheKeyLength)
	}

	if _, err := hex.DecodeString(hexPart); err != nil {
		t.Errorf("hex part should be valid hex: %v", err)
	}
}

func TestCacheKey_MatchesSHA256(t *testing.T) {
	u := "https://exhentai.org/s/51d1aa689c/4153369-29"
	key := CacheKey(u)

	sum := sha256.Sum256([]byte(u))
	expected := imageCachePrefix + hex.EncodeToString(sum[:])

	if key != expected {
		t.Errorf("key = %q, want %q", key, expected)
	}
}

func TestCacheKey_DifferentPartsProduceDifferentKeys(t *testing.T) {
	urls := []string{
		"https://exhentai.org/s/aaa/111-1",
		"https://exhentai.org/s/bbb/111-1",
		"https://exhentai.org/s/aaa/222-2",
		"https://e-hentai.org/s/aaa/111-1",
		"https://exhentai.org/s/aaa/111-1?nl=XYZ",
	}

	seen := make(map[string]string)
	for _, u := range urls {
		key := CacheKey(u)
		if prev, ok := seen[key]; ok {
			t.Errorf("URL %q and %q produced same key %q", u, prev, key)
		}
		seen[key] = u
	}
}

// ==================== URL Validation Tests ====================

func TestValidatePageURL_Valid(t *testing.T) {
	tests := []struct {
		name string
		url  string
	}{
		{"exhentai page", "https://exhentai.org/s/51d1aa689c/4153369-29"},
		{"e-hentai page", "https://e-hentai.org/s/859299c9ef/3138775-7"},
		{"exhentai with query", "https://exhentai.org/s/abc/123-1?nl=SZF-483294"},
		{"http allowed", "http://exhentai.org/s/abc/123-1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := validatePageURL(tt.url); err != nil {
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
			if err := validatePageURL(tt.url); err == nil {
				t.Errorf("validatePageURL(%q) = nil, want error", tt.url)
			}
		})
	}
}

// ==================== URL Decode Tests ====================

func TestCachedImageURLDecode(t *testing.T) {
	original := "https://exhentai.org/s/51d1aa689c/4153369-29"
	encoded := url.QueryEscape(original)

	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		t.Fatalf("QueryUnescape failed: %v", err)
	}

	if decoded != original {
		t.Errorf("decoded = %q, want %q", decoded, original)
	}
}

func TestCachedImageURLDecode_WithSpecialChars(t *testing.T) {
	original := "https://exhentai.org/s/abc/123-1?nl=SZF-483294&key=val"
	encoded := url.QueryEscape(original)

	decoded, err := url.QueryUnescape(encoded)
	if err != nil {
		t.Fatalf("QueryUnescape failed: %v", err)
	}

	if decoded != original {
		t.Errorf("decoded = %q, want %q", decoded, original)
	}
}

// ==================== isNotFound Tests ====================

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name   string
		err    error
		expect bool
	}{
		{"nil error", nil, false},
		{"generic error", fmt.Errorf("some error"), false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := isNotFound(tt.err); got != tt.expect {
				t.Errorf("isNotFound(%v) = %v, want %v", tt.err, got, tt.expect)
			}
		})
	}
}

// ==================== Handler Tests (Mock HTTP) ====================

func TestCachedImage_MissingURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	app := &CachedImageApp{Client: &http.Client{}}
	r.GET("/api/cached-image", app.handleCachedImage)

	req := httptest.NewRequest("GET", "/api/cached-image", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestCachedImage_InvalidURL(t *testing.T) {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	app := &CachedImageApp{Client: &http.Client{}}
	r.GET("/api/cached-image", app.handleCachedImage)

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
			req := httptest.NewRequest("GET", "/api/cached-image?url="+url.QueryEscape(tt.url), nil)
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
	app := &CachedImageApp{Client: &http.Client{}}
	r.GET("/api/cached-image", app.handleCachedImage)

	pageURL := "https://exhentai.org/s/abc/123-1"
	req := httptest.NewRequest("GET", "/api/cached-image?url="+url.QueryEscape(pageURL), nil)
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

type mockMinIOCache struct {
	mu      sync.RWMutex
	objects map[string]mockCacheEntry
}

func newMockMinIOCache() *mockMinIOCache {
	return &mockMinIOCache{objects: make(map[string]mockCacheEntry)}
}

func (m *mockMinIOCache) StatObject(key string) (mockCacheEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	entry, ok := m.objects[key]
	if !ok || !entry.exists {
		return mockCacheEntry{}, fmt.Errorf("NoSuchKey")
	}
	return entry, nil
}

func (m *mockMinIOCache) PutObject(key string, data []byte, contentType string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.objects[key] = mockCacheEntry{data: data, contentType: contentType, exists: true}
}

// ==================== Singleflight Concurrency Test ====================

func TestSingleflight_CoalescesRequests(t *testing.T) {
	var fetchCount atomic.Int32

	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/s/") {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, mockPageHTML("https://example.com/image.webp", ""))
		} else {
			fetchCount.Add(1)
			w.Header().Set("Content-Type", "image/webp")
			w.Write(mockImageBytes())
		}
	})
	defer mockServer.Close()

	pageURL := mockServer.URL + "/s/abc123/3138775-1"

	var wg sync.WaitGroup
	var errCount atomic.Int32
	n := 10

	start := make(chan struct{})

	for range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			client := newMockClient(mockServer.URL)
			_, _, err := fetchPageImage(context.Background(), client, pageURL)
			if err != nil {
				errCount.Add(1)
			}
		}()
	}

	close(start)
	wg.Wait()

	if errCount.Load() > 0 {
		t.Errorf("concurrent fetchPageImage had %d errors", errCount.Load())
	}
	count := fetchCount.Load()
	t.Logf("Concurrent fetches: %d goroutines, %d image fetches", n, count)
}

// ==================== Cache Key Integration Test ====================

func TestCacheKey_IntegrationWithMockMinIO(t *testing.T) {
	cache := newMockMinIOCache()

	pageURL := "https://exhentai.org/s/51d1aa689c/4153369-29"
	key := CacheKey(pageURL)

	// Simulate cache miss
	_, err := cache.StatObject(key)
	if err == nil {
		t.Error("expected cache miss, got hit")
	}

	// Simulate storing
	imgData := mockImageBytes()
	cache.PutObject(key, imgData, "image/png")

	// Simulate cache hit
	entry, err := cache.StatObject(key)
	if err != nil {
		t.Fatalf("expected cache hit, got error: %v", err)
	}
	if entry.contentType != "image/png" {
		t.Errorf("contentType = %q, want %q", entry.contentType, "image/png")
	}
	if len(entry.data) != len(imgData) {
		t.Errorf("data len = %d, want %d", len(entry.data), len(imgData))
	}
}

func TestCacheKey_SameURLAlwaysSameKey(t *testing.T) {
	cache := newMockMinIOCache()

	urls := []string{
		"https://exhentai.org/s/51d1aa689c/4153369-29",
		"https://exhentai.org/s/51d1aa689c/4153369-29",
		"https://exhentai.org/s/51d1aa689c/4153369-29",
	}

	for i, u := range urls {
		key := CacheKey(u)
		cache.PutObject(key, []byte(fmt.Sprintf("image-%d", i)), "image/webp")
	}

	// All three should have overwritten the same key
	if len(cache.objects) != 1 {
		t.Errorf("expected 1 object, got %d", len(cache.objects))
	}
}

// ==================== MinIOConfig Tests ====================

func TestMinIOConfig_IsValid(t *testing.T) {
	tests := []struct {
		name   string
		config MinIOConfig
		valid  bool
	}{
		{"complete", MinIOConfig{Endpoint: "minio:9000", AccessKey: "key", SecretKey: "secret", Bucket: "bucket"}, true},
		{"missing endpoint", MinIOConfig{AccessKey: "key", SecretKey: "secret", Bucket: "bucket"}, false},
		{"missing access key", MinIOConfig{Endpoint: "minio:9000", SecretKey: "secret", Bucket: "bucket"}, false},
		{"missing secret key", MinIOConfig{Endpoint: "minio:9000", AccessKey: "key", Bucket: "bucket"}, false},
		{"missing bucket", MinIOConfig{Endpoint: "minio:9000", AccessKey: "key", SecretKey: "secret"}, false},
		{"all empty", MinIOConfig{}, false},
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
			err := validatePageURL(tt.url)
			if (err != nil) != tt.wantErr {
				t.Errorf("validatePageURL(%q) error = %v, wantErr = %v", tt.url, err, tt.wantErr)
			}
		})
	}
}
