package handler

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gen2brain/webp"

	"manga-reader/internal/database"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/model"
)

const (
	testSpriteURL    = "https://cdn.hath.network/c2/hash/123-0.webp"
	testSpriteURLTwo = "https://cdn.hath.network/c2/hash/1-0.webp"
	liveThumbPath    = "/api/image/page-thumbnail"
	cachedThumbPath  = "/api/image-cache/page-thumbnail"
	testGalleryIDStr = "123456"
	thumbToken       = "abcdef"
	testGalleryIDInt = 123456
)

// makeTestSprite builds a small WebP sprite (two 4x4 cells).
func makeTestSprite(t *testing.T) []byte {
	t.Helper()

	src := image.NewRGBA(image.Rect(0, 0, 8, 4))
	for y := range 4 {
		for x := range 8 {
			c := color.RGBA{G: 255, A: 255}
			if x >= 4 {
				c = color.RGBA{B: 255, A: 255}
			}
			src.SetRGBA(x, y, c)
		}
	}

	var buf bytes.Buffer
	if err := webp.Encode(&buf, src, webp.Options{Lossless: true}); err != nil {
		t.Fatalf("encode sprite: %v", err)
	}
	return buf.Bytes()
}

func directThumbPath(base, rawURL string) string {
	return base + "?url=" + url.QueryEscape(rawURL) + "&x=0&y=0&w=4&h=4"
}

func indexThumbPath(base string, index int) string {
	return fmt.Sprintf("%s?id=%s&token=%s&index=%d", base, testGalleryIDStr, thumbToken, index)
}

func decodeSize(t *testing.T, body []byte) image.Point {
	t.Helper()
	img, err := webp.Decode(bytes.NewReader(body))
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return img.Bounds().Size()
}

func spriteServer(t *testing.T, requests *atomic.Int32) *httptest.Server {
	t.Helper()
	sprite := makeTestSprite(t)
	srv := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if requests != nil {
			requests.Add(1)
		}
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(sprite)
	})
	t.Cleanup(srv.Close)
	return srv
}

// ==================== live endpoint ====================

func TestPageThumbnail_Live_Success(t *testing.T) {
	srv := spriteServer(t, nil)
	server := &Server{Client: newMockClient(srv.URL)}
	r := setupMockRouter(server)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", directThumbPath(liveThumbPath, testSpriteURL), nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/webp" {
		t.Errorf("content-type = %q, want image/webp", ct)
	}
	if got := decodeSize(t, w.Body.Bytes()); got.X != 4 || got.Y != 4 {
		t.Errorf("cropped size = %v, want 4x4", got)
	}
}

func TestPageThumbnail_Live_DoesNotCache(t *testing.T) {
	var requests atomic.Int32
	srv := spriteServer(t, &requests)
	// Cache is configured, but the live endpoint must not use it.
	server := &Server{Client: newMockClient(srv.URL), Cache: newMockImageCache()}
	r := setupMockRouter(server)

	for range 2 {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", directThumbPath(liveThumbPath, testSpriteURL), nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("upstream requests = %d, want 2 (live does not cache)", got)
	}
}

func TestPageThumbnail_Live_OutOfBounds(t *testing.T) {
	srv := spriteServer(t, nil)
	server := &Server{Client: newMockClient(srv.URL)}
	r := setupMockRouter(server)

	path := liveThumbPath + "?url=" + url.QueryEscape(testSpriteURL) + "&x=0&y=0&w=100&h=4"
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestPageThumbnail_Live_UpstreamFailure(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", directThumbPath(liveThumbPath, testSpriteURL), nil))

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// ==================== cached endpoint ====================

func TestCachedPageThumbnail_Success(t *testing.T) {
	srv := spriteServer(t, nil)
	server := &Server{Client: newMockClient(srv.URL), Cache: newMockImageCache()}
	r := setupMockRouter(server)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", directThumbPath(cachedThumbPath, testSpriteURL), nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := decodeSize(t, w.Body.Bytes()); got.X != 4 || got.Y != 4 {
		t.Errorf("cropped size = %v, want 4x4", got)
	}
}

func TestCachedPageThumbnail_CacheHitAvoidsUpstream(t *testing.T) {
	var requests atomic.Int32
	srv := spriteServer(t, &requests)
	server := &Server{Client: newMockClient(srv.URL), Cache: newMockImageCache()}
	r := setupMockRouter(server)

	for range 2 {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", directThumbPath(cachedThumbPath, testSpriteURL), nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("upstream requests = %d, want 1 (sprite cached)", got)
	}
}

func TestCachedPageThumbnail_ConcurrentCropsShareSpriteDownload(t *testing.T) {
	var requests atomic.Int32
	sprite := makeTestSprite(t)
	// A slow sprite server keeps every concurrent request in flight until the
	// first download completes, exposing any missing coalescing.
	srv := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(sprite)
	})
	defer srv.Close()

	server := &Server{Client: newMockClient(srv.URL), Cache: newMockImageCache()}
	r := setupMockRouter(server)

	const n = 8
	var wg sync.WaitGroup
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			// Distinct crop rects => distinct tile cache keys, so only the
			// shared sprite fetch can be coalesced.
			path := fmt.Sprintf("%s?url=%s&x=%d&y=0&w=1&h=1", cachedThumbPath, url.QueryEscape(testSpriteURL), i)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
			if w.Code != http.StatusOK {
				t.Errorf("request %d: status = %d, want 200", i, w.Code)
			}
		}(i)
	}
	wg.Wait()

	if got := requests.Load(); got != 1 {
		t.Errorf("upstream sprite requests = %d, want 1 (coalesced)", got)
	}
}

func TestCachedPageThumbnail_NoCache(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", directThumbPath(cachedThumbPath, testSpriteURL), nil))

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("status = %d, want %d", w.Code, http.StatusServiceUnavailable)
	}
}

// ==================== gallery-index addressing ====================

const testGalleryThumbHTML = `<html><body>
<div class="gpc">Showing 1 - 2 of 2 images</div>
<div id="gdt" class="gt200">
<a href="https://exhentai.org/s/a/1-1"><div><div title="Page 1" style="width:4px;height:4px;background:transparent url(https://cdn.hath.network/c2/hash/1-0.webp) -0px 0 no-repeat"></div></div></a>
<a href="https://exhentai.org/s/b/1-2"><div><div title="Page 2" style="width:4px;height:4px;background:transparent url(https://cdn.hath.network/c2/hash/1-0.webp) -4px 0 no-repeat"></div></div></a>
</div></body></html>`

func newGallerySpriteServer(t *testing.T) *httptest.Server {
	t.Helper()
	sprite := makeTestSprite(t)
	srv := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/g/") {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, testGalleryThumbHTML)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(sprite)
	})
	t.Cleanup(srv.Close)
	return srv
}

func TestPageThumbnail_Live_ByIndex(t *testing.T) {
	srv := newGallerySpriteServer(t)
	server := &Server{Client: newMockClient(srv.URL)}
	r := setupMockRouter(server)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", indexThumbPath(liveThumbPath, 1), nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := decodeSize(t, w.Body.Bytes()); got.X != 4 || got.Y != 4 {
		t.Errorf("cropped size = %v, want 4x4", got)
	}
}

func TestPageThumbnail_Live_ByIndexOutOfRange(t *testing.T) {
	srv := newGallerySpriteServer(t)
	server := &Server{Client: newMockClient(srv.URL)}
	r := setupMockRouter(server)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", indexThumbPath(liveThumbPath, 5), nil))

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
}

func TestCachedPageThumbnail_ByIndexUsesCachedGeometry(t *testing.T) {
	client := newTestDB(t)
	if err := gallerycache.UpsertPages(t.Context(), client, testGalleryIDInt, thumbToken, []model.CachedPage{
		{
			PageURL: "https://exhentai.org/s/a/1-1",
			Index:   0,
			Thumbnail: &model.GalleryPageThumb{
				SpriteURL: testSpriteURLTwo,
				X:         0, Y: 0, Width: 4, Height: 4,
			},
		},
	}); err != nil {
		t.Fatalf("seed pages: %v", err)
	}

	sprite := makeTestSprite(t)
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/g/") {
			t.Errorf("cached index request must not scrape the gallery page")
			http.Error(w, "unexpected gallery scrape", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(sprite)
	})
	defer mockServer.Close()

	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: client},
		Cache:  newMockImageCache(),
	}
	r := setupMockRouter(server)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", indexThumbPath(cachedThumbPath, 0), nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if got := decodeSize(t, w.Body.Bytes()); got.X != 4 || got.Y != 4 {
		t.Errorf("cropped size = %v, want 4x4", got)
	}
}

// ==================== validation ====================

func TestPageThumbnail_InvalidRequests(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	tests := []struct {
		name string
		path string
	}{
		{"no addressing", liveThumbPath},
		{"missing rect", liveThumbPath + "?url=" + url.QueryEscape(testSpriteURL)},
		{"disallowed host", liveThumbPath + "?url=" + url.QueryEscape("https://evil.example.com/a.webp") + "&x=0&y=0&w=4&h=4"},
		{"internal host", liveThumbPath + "?url=" + url.QueryEscape("https://127.0.0.1/a.webp") + "&x=0&y=0&w=4&h=4"},
		{"http scheme", liveThumbPath + "?url=" + url.QueryEscape("http://cdn.hath.network/a.webp") + "&x=0&y=0&w=4&h=4"},
		{"missing x", liveThumbPath + "?url=" + url.QueryEscape(testSpriteURL) + "&y=0&w=4&h=4"},
		{"zero width", liveThumbPath + "?url=" + url.QueryEscape(testSpriteURL) + "&x=0&y=0&w=0&h=4"},
		{"negative y", liveThumbPath + "?url=" + url.QueryEscape(testSpriteURL) + "&x=0&y=-1&w=4&h=4"},
		{"oversized", liveThumbPath + "?url=" + url.QueryEscape(testSpriteURL) + "&x=0&y=0&w=5000&h=4"},
		{"index missing", liveThumbPath + "?id=1&token=t"},
		{"index invalid", liveThumbPath + "?id=1&token=t&index=abc"},
		{"id invalid", liveThumbPath + "?id=abc&token=t&index=0"},
		{"token missing", liveThumbPath + "?id=1&index=0"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
			}
		})
	}
}
