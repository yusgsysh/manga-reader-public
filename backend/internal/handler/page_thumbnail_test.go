package handler

import (
	"bytes"
	"image"
	"image/color"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"

	"github.com/gen2brain/webp"
)

const testSpriteURL = "https://cdn.hath.network/c2/hash/123-0.webp"

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

func pageThumbRequest(t *testing.T, rawURL string) *http.Request {
	t.Helper()
	return httptest.NewRequest("GET", "/api/page-thumbnail?url="+url.QueryEscape(rawURL)+"&x=0&y=0&w=4&h=4", nil)
}

func TestPageThumbnail_Success(t *testing.T) {
	sprite := makeTestSprite(t)
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(sprite)
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := pageThumbRequest(t, testSpriteURL)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "image/webp" {
		t.Errorf("content-type = %q, want image/webp", ct)
	}

	img, err := webp.Decode(bytes.NewReader(w.Body.Bytes()))
	if err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if got := img.Bounds().Size(); got.X != 4 || got.Y != 4 {
		t.Errorf("cropped size = %v, want 4x4", got)
	}
}

func TestPageThumbnail_CacheHitAvoidsUpstream(t *testing.T) {
	sprite := makeTestSprite(t)
	var requests atomic.Int32
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(sprite)
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL), Cache: newMockImageCache()}
	r := setupMockRouter(server)

	for range 2 {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, pageThumbRequest(t, testSpriteURL))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
		}
	}

	if got := requests.Load(); got != 1 {
		t.Errorf("upstream requests = %d, want 1 (sprite cached)", got)
	}
}

func TestPageThumbnail_OutOfBounds(t *testing.T) {
	sprite := makeTestSprite(t)
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/webp")
		_, _ = w.Write(sprite)
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/page-thumbnail?url="+url.QueryEscape(testSpriteURL)+"&x=0&y=0&w=100&h=4", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestPageThumbnail_UpstreamFailure(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusInternalServerError)
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, pageThumbRequest(t, testSpriteURL))

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestPageThumbnail_InvalidRequests(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	tests := []struct {
		name string
		path string
	}{
		{"missing url", "/api/page-thumbnail?x=0&y=0&w=4&h=4"},
		{"disallowed host", "/api/page-thumbnail?url=" + url.QueryEscape("https://evil.example.com/a.webp") + "&x=0&y=0&w=4&h=4"},
		{"internal host", "/api/page-thumbnail?url=" + url.QueryEscape("https://127.0.0.1/a.webp") + "&x=0&y=0&w=4&h=4"},
		{"http scheme", "/api/page-thumbnail?url=" + url.QueryEscape("http://cdn.hath.network/a.webp") + "&x=0&y=0&w=4&h=4"},
		{"missing x", "/api/page-thumbnail?url=" + url.QueryEscape(testSpriteURL) + "&y=0&w=4&h=4"},
		{"zero width", "/api/page-thumbnail?url=" + url.QueryEscape(testSpriteURL) + "&x=0&y=0&w=0&h=4"},
		{"negative y", "/api/page-thumbnail?url=" + url.QueryEscape(testSpriteURL) + "&x=0&y=-1&w=4&h=4"},
		{"oversized", "/api/page-thumbnail?url=" + url.QueryEscape(testSpriteURL) + "&x=0&y=0&w=5000&h=4"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", tt.path, nil))
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
		})
	}
}
