package handler

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"log/slog"
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

// indexThumbPathFor addresses a specific gallery so tests that exercise the
// shared pages-walk hub do not collide with each other's in-flight streams.
func indexThumbPathFor(base, id, token string, index int) string {
	return fmt.Sprintf("%s?id=%s&token=%s&index=%d", base, id, token, index)
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

// Crops are never persisted: each request re-reads the sprite from MinIO and
// crops it fresh, so the only object the endpoint writes is the sprite itself.
func TestCachedPageThumbnail_CachesSpriteOnly(t *testing.T) {
	var requests atomic.Int32
	srv := spriteServer(t, &requests)
	cache := newMockImageCache()
	server := &Server{Client: newMockClient(srv.URL), Cache: cache}
	r := setupMockRouter(server)

	// Distinct crop rects plus a repeat: one sprite download, one cache object.
	paths := []string{
		fmt.Sprintf("%s?url=%s&x=0&y=0&w=1&h=1", cachedThumbPath, url.QueryEscape(testSpriteURL)),
		fmt.Sprintf("%s?url=%s&x=1&y=0&w=1&h=1", cachedThumbPath, url.QueryEscape(testSpriteURL)),
	}
	for _, path := range append(paths, paths[0]) {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
		}
	}
	if got := requests.Load(); got != 1 {
		t.Errorf("upstream requests = %d, want 1 (sprite cached)", got)
	}
	keys := cache.keys()
	if len(keys) != 1 {
		t.Fatalf("cache objects = %d (%v), want 1 (sprite only)", len(keys), keys)
	}
	if !strings.HasPrefix(keys[0], spriteCachePrefix) {
		t.Errorf("cache key = %q, want %q prefix", keys[0], spriteCachePrefix)
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
			// Distinct crop rects => distinct singleflight keys, so only the
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
	if err := gallerycache.UpsertPages(t.Context(), client, testGalleryIDInt, thumbToken,
		[]string{"https://exhentai.org/s/a/1-1"}); err != nil {
		t.Fatalf("seed pages: %v", err)
	}
	if err := gallerycache.UpsertThumbnails(t.Context(), client, testGalleryIDInt, thumbToken,
		[]model.GalleryPageThumb{
			{
				SpriteURL: testSpriteURLTwo,
				X:         0, Y: 0, Width: 4, Height: 4,
			},
		}); err != nil {
		t.Fatalf("seed thumbnails: %v", err)
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

// ==================== index resolve via the shared pages walk ====================

// gallerySpriteServer serves the two-page gallery HTML (with sprite geometry)
// on /g/ requests and the sprite image on everything else. onGalleryHit, when
// non-nil, is closed before the first gallery HTML is answered and that first
// answer blocks until release is closed — keeping the shared walk in flight so
// a second resolve is guaranteed to subscribe to it.
func gallerySpriteServer(
	t *testing.T,
	galleryHits *atomic.Int32,
	onFirstHit func(),
	release chan struct{},
) *httptest.Server {
	t.Helper()
	sprite := makeTestSprite(t)
	srv := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/g/") {
			if galleryHits.Add(1) == 1 && onFirstHit != nil {
				onFirstHit()
				<-release
			}
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

type thumbResponse struct {
	code int
	body string
}

func requestThumb(r http.Handler, path string) thumbResponse {
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", path, nil))
	return thumbResponse{code: w.Code, body: w.Body.String()}
}

// Concurrent index resolves for the same gallery must share one upstream walk
// instead of each scraping the gallery page on its own.
func TestCachedPageThumbnail_ByIndexCoalescesWithPagesStream(t *testing.T) {
	var galleryHits atomic.Int32
	firstHit := make(chan struct{})
	release := make(chan struct{})
	srv := gallerySpriteServer(t, &galleryHits, func() { close(firstHit) }, release)

	const id, token = "777001", "coalesce"
	db := newTestDB(t)
	server := &Server{
		Client: newMockClient(srv.URL),
		DB:     &database.DB{Client: db},
		Cache:  newMockImageCache(),
	}
	r := setupMockRouter(server)

	first := make(chan thumbResponse, 1)
	go func() {
		first <- requestThumb(r, indexThumbPathFor(cachedThumbPath, id, token, 0))
	}()

	select {
	case <-firstHit:
	case <-time.After(2 * time.Second):
		t.Fatal("leader never started the shared pages walk")
	}

	second := make(chan thumbResponse, 1)
	go func() {
		second <- requestThumb(r, indexThumbPathFor(cachedThumbPath, id, token, 1))
	}()

	// The walk is still blocked in its gallery fetch, so the second resolve
	// has time to subscribe to the in-flight stream before it completes.
	time.Sleep(200 * time.Millisecond)
	close(release)

	for name, ch := range map[string]chan thumbResponse{"first": first, "second": second} {
		select {
		case res := <-ch:
			if res.code != http.StatusOK {
				t.Errorf("%s: status = %d, want 200. body: %s", name, res.code, res.body)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: request never finished", name)
		}
	}

	if got := galleryHits.Load(); got != 1 {
		t.Errorf("gallery page fetches = %d, want 1 (shared walk)", got)
	}

	// Drain the detached walk before teardown so its cache write does not
	// race the database close.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if row, found, err := gallerycache.Get(t.Context(), db, 777001, token); err == nil && found && len(row.Pages) == 2 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("shared walk never backfilled gallery_cache")
}

// Completing the shared walk backfills gallery_cache, so later index resolves
// are served from the database without touching upstream.
func TestCachedPageThumbnail_ByIndexWalkPopulatesCache(t *testing.T) {
	var galleryHits atomic.Int32
	srv := gallerySpriteServer(t, &galleryHits, nil, nil)

	db := newTestDB(t)
	server := &Server{
		Client: newMockClient(srv.URL),
		DB:     &database.DB{Client: db},
		Cache:  newMockImageCache(),
	}
	r := setupMockRouter(server)

	const id, token = "777002", "cachefill"
	res := requestThumb(r, indexThumbPathFor(cachedThumbPath, id, token, 0))
	if res.code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", res.code, res.body)
	}

	// The resolve returns as soon as its batch arrives; the detached walk
	// persists the list afterwards.
	backfilled := false
	deadline := time.Now().Add(3 * time.Second)
	for !backfilled && time.Now().Before(deadline) {
		row, found, err := gallerycache.Get(t.Context(), db, 777002, token)
		if err != nil || !found || len(row.Pages) != 2 || len(row.Thumbnails) != 2 {
			time.Sleep(20 * time.Millisecond)
			continue
		}
		backfilled = true
		for i := range row.Pages {
			if row.Thumbnails[i].SpriteURL == "" {
				t.Errorf("page %d cached without thumbnail geometry", i)
			}
		}
	}
	if !backfilled {
		t.Fatal("shared walk did not backfill gallery_cache")
	}
	if got := galleryHits.Load(); got != 1 {
		t.Errorf("gallery page fetches = %d, want 1", got)
	}
}

// An index past the gallery total resolves to 404 as soon as the walk reports
// the total, without waiting for the remaining pages.
func TestCachedPageThumbnail_ByIndexOutOfRangeCached(t *testing.T) {
	var galleryHits atomic.Int32
	srv := gallerySpriteServer(t, &galleryHits, nil, nil)

	server := &Server{
		Client: newMockClient(srv.URL),
		Cache:  newMockImageCache(),
	}
	r := setupMockRouter(server)

	res := requestThumb(r, indexThumbPathFor(cachedThumbPath, "777003", "range", 5))
	if res.code != http.StatusNotFound {
		t.Errorf("status = %d, want 404. body: %s", res.code, res.body)
	}
	if got := galleryHits.Load(); got != 1 {
		t.Errorf("gallery page fetches = %d, want 1", got)
	}
}

// When the shared walk does not deliver the page within pageThumbResolveTimeout
// the resolve falls back to a direct one-page scrape instead of stalling.
func TestCachedPageThumbnail_ByIndexResolveTimeoutFallsBack(t *testing.T) {
	prev := pageThumbResolveTimeout
	pageThumbResolveTimeout = 200 * time.Millisecond
	t.Cleanup(func() { pageThumbResolveTimeout = prev })

	db := newTestDB(t)

	var galleryHits atomic.Int32
	firstHit := make(chan struct{})
	release := make(chan struct{})
	srv := gallerySpriteServer(t, &galleryHits, func() { close(firstHit) }, release)
	t.Cleanup(func() {
		// Let the stuck walk finish before the database teardown so its cache
		// write does not race a closed connection.
		select {
		case <-release:
		default:
			close(release)
		}
		deadline := time.Now().Add(3 * time.Second)
		for time.Now().Before(deadline) {
			// t.Context() is already cancelled inside cleanup, so the detached
			// walk's cache write must be observed with a live context.
			row, found, err := gallerycache.Get(context.Background(), db, 777004, "fallback")
			if err == nil && found && len(row.Pages) == 2 {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		t.Error("shared walk never backfilled gallery_cache")
	})

	server := &Server{
		Client: newMockClient(srv.URL),
		DB:     &database.DB{Client: db},
		Cache:  newMockImageCache(),
	}
	r := setupMockRouter(server)

	// The resolve is issued in the background: the walk's gallery fetch must
	// be the blocked first hit while the resolve itself is still waiting on
	// its (shortened) timeout.
	resCh := make(chan thumbResponse, 1)
	go func() {
		resCh <- requestThumb(r, indexThumbPathFor(cachedThumbPath, "777004", "fallback", 0))
	}()

	select {
	case <-firstHit:
	case <-time.After(2 * time.Second):
		t.Fatal("leader never started the shared pages walk")
	}

	// The direct scrape is the second gallery fetch; the first (the blocked
	// walk) only completes after the resolve already fell back.
	select {
	case res := <-resCh:
		if res.code != http.StatusOK {
			t.Fatalf("status = %d, want 200. body: %s", res.code, res.body)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("resolve never fell back to the direct scrape")
	}
	if got := galleryHits.Load(); got != 2 {
		t.Errorf("gallery page fetches = %d, want 2 (walk + fallback scrape)", got)
	}
}

// ==================== Sprite Expiration Retry Tests ====================

// galleryHTMLWithSprite returns a gallery HTML snippet with the given sprite URL.
func galleryHTMLWithSprite(spriteURL string) string {
	return fmt.Sprintf(`<html><body>
<div class="gpc">Showing 1 - 2 of 2 images</div>
<div id="gdt" class="gt200">
<a href="https://exhentai.org/s/a/1-1"><div><div title="Page 1" style="width:4px;height:4px;background:transparent url(%s) -0px 0 no-repeat"></div></div></a>
<a href="https://exhentai.org/s/b/1-2"><div><div title="Page 2" style="width:4px;height:4px;background:transparent url(%s) -4px 0 no-repeat"></div></div></a>
</div></body></html>`, spriteURL, spriteURL)
}

// TestCachedPageThumbnail_SpriteExpired_RefetchAndRetry tests that when a cached
// sprite URL returns 404 (expired), the handler triggers a gallery cache refresh,
// re-resolves the sprite URL from the updated gallery cache, and retries the
// thumbnail request successfully.
func TestCachedPageThumbnail_SpriteExpired_RefetchAndRetry(t *testing.T) {
	const id, token = "999001", "sprite-expire-retry"

	// 1. Seed gallery_cache with OLD sprite URL
	db := newTestDB(t)
	oldSpriteURL := "https://cdn.hath.network/c2/OLD/123-0.webp"
	if err := gallerycache.UpsertPages(t.Context(), db, 999001, token,
		[]string{"https://exhentai.org/s/a/1-1", "https://exhentai.org/s/b/1-2"}); err != nil {
		t.Fatalf("seed pages: %v", err)
	}
	if err := gallerycache.UpsertThumbnails(t.Context(), db, 999001, token,
		[]model.GalleryPageThumb{
			{SpriteURL: oldSpriteURL, X: 0, Y: 0, Width: 4, Height: 4},
			{SpriteURL: oldSpriteURL, X: 4, Y: 0, Width: 4, Height: 4},
		}); err != nil {
		t.Fatalf("seed thumbnails: %v", err)
	}

	// 2. Mock upstream server
	var spriteFetchCount atomic.Int32
	var galleryScrapeCount atomic.Int32

	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		slog.Debug("mock server request", "path", r.URL.Path, "query", r.URL.RawQuery)
		switch {
		case strings.Contains(r.URL.Path, "/g/"): // Gallery page scrape
			galleryScrapeCount.Add(1)
			count := galleryScrapeCount.Load()
			if count == 1 {
				// First scrape returns OLD sprite (matches cache)
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, galleryHTMLWithSprite(oldSpriteURL))
			} else {
				// Second scrape (after refresh) returns NEW sprite
				newSpriteURL := "https://cdn.hath.network/c2/NEW/123-0.webp"
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, galleryHTMLWithSprite(newSpriteURL))
			}

		case strings.HasSuffix(r.URL.Path, "-0.webp"): // Sprite image
			spriteFetchCount.Add(1)
			requestedURL := r.URL.String()
			if strings.Contains(requestedURL, "OLD") {
				// First sprite fetch: 404 (expired)
				w.WriteHeader(http.StatusNotFound)
			} else if strings.Contains(requestedURL, "NEW") {
				// Second sprite fetch: 200
				sprite := makeTestSprite(t)
				w.Header().Set("Content-Type", "image/webp")
				_, _ = w.Write(sprite)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	})
	defer mockServer.Close()

	// 3. Server with mock cache (empty = miss on sprite)
	cache := newMockImageCache()
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: db},
		Cache:  cache,
	}
	r := setupMockRouter(server)

	// 4. Request thumbnail by index (first page, index 0)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", indexThumbPathFor(cachedThumbPath, id, token, 0), nil))

	// 5. Wait for refresh to complete by polling gallery cache
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		if got := galleryScrapeCount.Load(); got >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 6. Assertions
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	if got := decodeSize(t, w.Body.Bytes()); got.X != 4 || got.Y != 4 {
		t.Errorf("cropped size = %v, want 4x4", got)
	}
	// Gallery should have been re-scraped twice (initial resolve + refresh)
	if got := galleryScrapeCount.Load(); got != 2 {
		t.Errorf("gallery scrapes = %d, want 2 (initial + refresh)", got)
	}
	// Sprite should have been fetched twice (OLD=404, NEW=200)
	if got := spriteFetchCount.Load(); got != 2 {
		t.Errorf("sprite fetches = %d, want 2", got)
	}
	// Gallery cache should now have NEW sprite URL
	row, found, err := gallerycache.Get(t.Context(), db, 999001, token)
	if err != nil || !found {
		t.Fatalf("gallery cache get failed: %v, found=%v", err, found)
	}
	if row.Thumbnails[0].SpriteURL != "https://cdn.hath.network/c2/NEW/123-0.webp" {
		t.Errorf("gallery cache not updated with new sprite URL: got %q", row.Thumbnails[0].SpriteURL)
	}
}

// TestCachedPageThumbnail_SpriteExpired_ConcurrentRequests tests that concurrent
// requests for the same expired sprite only trigger one gallery refresh.
func TestCachedPageThumbnail_SpriteExpired_ConcurrentRequests(t *testing.T) {
	const id, token = "999002", "sprite-expire-concurrent"

	// 1. Seed gallery_cache with OLD sprite URL
	db := newTestDB(t)
	oldSpriteURL := "https://cdn.hath.network/c2/OLD/456-0.webp"
	if err := gallerycache.UpsertPages(t.Context(), db, 999002, token,
		[]string{"https://exhentai.org/s/a/1-1"}); err != nil {
		t.Fatalf("seed pages: %v", err)
	}
	if err := gallerycache.UpsertThumbnails(t.Context(), db, 999002, token,
		[]model.GalleryPageThumb{
			{SpriteURL: oldSpriteURL, X: 0, Y: 0, Width: 4, Height: 4},
		}); err != nil {
		t.Fatalf("seed thumbnails: %v", err)
	}

	// 2. Mock upstream server
	var spriteFetchCount atomic.Int32
	var galleryScrapeCount atomic.Int32

	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.Contains(r.URL.Path, "/g/"):
			galleryScrapeCount.Add(1)
			if galleryScrapeCount.Load() == 1 {
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, galleryHTMLWithSprite(oldSpriteURL))
			} else {
				newSpriteURL := "https://cdn.hath.network/c2/NEW/456-0.webp"
				w.Header().Set("Content-Type", "text/html")
				fmt.Fprint(w, galleryHTMLWithSprite(newSpriteURL))
			}

		case strings.HasSuffix(r.URL.Path, "-0.webp"):
			spriteFetchCount.Add(1)
			requestedURL := r.URL.String()
			if strings.Contains(requestedURL, "OLD") {
				w.WriteHeader(http.StatusNotFound)
			} else if strings.Contains(requestedURL, "NEW") {
				sprite := makeTestSprite(t)
				w.Header().Set("Content-Type", "image/webp")
				_, _ = w.Write(sprite)
			} else {
				w.WriteHeader(http.StatusNotFound)
			}
		}
	})
	defer mockServer.Close()

	// 3. Server with mock cache
	cache := newMockImageCache()
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: db},
		Cache:  cache,
	}
	r := setupMockRouter(server)

	// 4. Fire concurrent requests
	const n = 5
	var wg sync.WaitGroup
	results := make(chan int, n)
	for range n {
		wg.Go(func() {
			w := httptest.NewRecorder()
			r.ServeHTTP(w, httptest.NewRequest("GET", indexThumbPathFor(cachedThumbPath, id, token, 0), nil))
			results <- w.Code
		})
	}
	wg.Wait()
	close(results)

	// 5. Wait for refresh to complete (poll gallery scrape count)
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if got := galleryScrapeCount.Load(); got >= 2 {
			break
		}
		time.Sleep(50 * time.Millisecond)
	}

	// 6. Assertions
	successCount := 0
	for code := range results {
		if code == http.StatusOK {
			successCount++
		}
	}
	if successCount != n {
		t.Errorf("successful requests = %d, want %d", successCount, n)
	}
	// Only ONE gallery re-scrape should happen (the refresh) - initial + one refresh
	if got := galleryScrapeCount.Load(); got != 2 {
		t.Errorf("gallery scrapes = %d, want 2 (initial + one refresh)", got)
	}
	// Sprite fetched twice (OLD=404 once, NEW=200 once due to singleflight)
	if got := spriteFetchCount.Load(); got != 2 {
		t.Errorf("sprite fetches = %d, want 2", got)
	}
	// Gallery cache should have NEW sprite URL
	row, found, err := gallerycache.Get(t.Context(), db, 999002, token)
	if err != nil || !found {
		t.Fatalf("gallery cache get failed: %v, found=%v", err, found)
	}
	if row.Thumbnails[0].SpriteURL != "https://cdn.hath.network/c2/NEW/456-0.webp" {
		t.Errorf("gallery cache not updated with new sprite URL: got %q", row.Thumbnails[0].SpriteURL)
	}
}
