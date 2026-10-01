package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	json "encoding/json/v2"

	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/model"
)

// errorTransport makes every upstream request fail, simulating ExHentai being
// unreachable.
type errorTransport struct{}

func (errorTransport) RoundTrip(*http.Request) (*http.Response, error) {
	return nil, errors.New("upstream unreachable")
}

func errorClient() *http.Client {
	return &http.Client{Transport: errorTransport{}}
}

// failPaginatedTransport redirects requests to the mock server but fails every
// request carrying ?p= (the second and later thumbnail pages), simulating
// ExHentai dropping out in the middle of a scrape.
type failPaginatedTransport struct {
	mockURL string
}

func (t *failPaginatedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if req.URL.Query().Has("p") {
		return nil, errors.New("upstream failed mid-scrape")
	}
	return (&mockTransport{mockURL: t.mockURL}).RoundTrip(req)
}

func seedGalleryCache(t *testing.T, client *ent.Client, id int64, token string) {
	t.Helper()
	if err := gallerycache.UpsertMeta(t.Context(), client, id, token, model.GalleryCacheSnapshot{
		Title:       "Cached Gallery",
		TitleJPN:    "キャッシュ",
		Category:    string(model.CategoryManga),
		Thumbnail:   "https://example.com/thumb.webp",
		PageCount:   2,
		Rating:      4.5,
		RatingCount: 10,
		Uploader:    "cached_uploader",
		Posted:      "2024-01-01",
		Language:    "Chinese",
		FileSize:    "15 MB",
		Tags:        []model.Tag{{Namespace: "female", Name: "yuri"}},
	}); err != nil {
		t.Fatalf("seed cache meta: %v", err)
	}
	if err := gallerycache.UpsertPages(t.Context(), client, id, token, []model.CachedPage{
		{PageURL: "https://exhentai.org/s/abc/1", Index: 0},
		{PageURL: "https://exhentai.org/s/abc/2", Index: 1},
	}); err != nil {
		t.Fatalf("seed cache pages: %v", err)
	}
}

// ==================== Online endpoints: write cache, no fallback ====================

func TestGalleryPages_WritesCache(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryDetailHTML(12345, "Pages Test", 3))
	})
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	row, found, err := gallerycache.Get(t.Context(), client, 12345, "tok12345")
	if err != nil || !found {
		t.Fatalf("cache lookup: found=%v err=%v", found, err)
	}
	if len(row.Pages) != 3 {
		t.Errorf("cached pages = %d, want 3", len(row.Pages))
	}
	if row.PagesFetchedAt == nil {
		t.Error("pages_fetched_at should be set")
	}
}

func TestGalleryPages_UpstreamFailure(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345")

	server := &Server{
		Client: errorClient(),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Online endpoint must NOT fall back to cache; the frontend does that via
	// the gallery-cache endpoint instead.
	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}

func TestGalleryPages_MidStreamFailureWritesNothing(t *testing.T) {
	mockServer := newMockServer(mockPaginatedGalleryHandler(12345, "Partial", 65))
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: &http.Client{Transport: &failPaginatedTransport{mockURL: mockServer.URL}},
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// The meta line is already on the wire, so the status stays 200; the
	// failure is reported as a terminal error line instead.
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	stream := parsePagesStream(t, w.Body.Bytes())
	if stream.Done {
		t.Fatal("stream must not finish with done after a mid-scrape failure")
	}
	if stream.Error == "" {
		t.Fatal("terminal line must be an error")
	}
	if len(stream.Pages) == 0 {
		t.Fatal("pages should have been streamed before the failure")
	}

	// A failed scrape must never write partial pages into the cache.
	if _, found, err := gallerycache.Get(t.Context(), client, 12345, "tok12345"); err != nil || found {
		t.Fatalf("cache must stay empty on failure: found=%v err=%v", found, err)
	}
}

// ==================== Stream completeness semantics ====================

func TestGalleryPages_MissingTotalRejected(t *testing.T) {
	// A document whose ".gpc" counter cannot be parsed must fail the scrape
	// before anything is emitted: total == 0 means "unknown", never "0 pages".
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><body><div id="gdt">
<a href="https://exhentai.org/s/abc/1-1">p1</a>
<a href="https://exhentai.org/s/abc/1-2">p2</a>
</div></body></html>`)
	})
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (nothing emitted). body: %s", w.Code, w.Body.String())
	}
	if _, found, err := gallerycache.Get(t.Context(), client, 12345, "tok12345"); err != nil || found {
		t.Fatalf("cache must stay empty: found=%v err=%v", found, err)
	}
}

func TestGalleryPages_EmptyBatchMidStreamWritesNothing(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Has("p") {
			// The next thumbnail page exists but has no links: pages are
			// missing while the declared total says otherwise.
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, `<div class="gpc">Showing 41 - 65 of 65 images</div><div id="gdt"></div>`)
			return
		}
		mockPaginatedGalleryHandler(12345, "EmptyBatch", 65)(w, r)
	})
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	stream := parsePagesStream(t, w.Body.Bytes())
	if stream.Done {
		t.Fatal("stream must not finish with done when pages are missing")
	}
	if stream.Error == "" {
		t.Fatal("terminal line must be an error")
	}
	if _, found, err := gallerycache.Get(t.Context(), client, 12345, "tok12345"); err != nil || found {
		t.Fatalf("cache must stay empty: found=%v err=%v", found, err)
	}
}

func TestGalleryPages_TotalMismatchWritesNothing(t *testing.T) {
	// More page links than the declared total: received != total is never a
	// successful scrape.
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><body>
<div class="gpc">Showing 1 - 2 of 1 images</div>
<div id="gdt">
<a href="https://exhentai.org/s/abc/1-1">p1</a>
<a href="https://exhentai.org/s/abc/1-2">p2</a>
</div></body></html>`)
	})
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	stream := parsePagesStream(t, w.Body.Bytes())
	if stream.Done {
		t.Fatal("stream must not finish with done on a total mismatch")
	}
	if stream.Error == "" {
		t.Fatal("terminal line must be an error")
	}
	if _, found, err := gallerycache.Get(t.Context(), client, 12345, "tok12345"); err != nil || found {
		t.Fatalf("cache must stay empty: found=%v err=%v", found, err)
	}
}

// disconnectOnWrite cancels the request context as soon as the marker bytes
// reach the client, simulating a disconnect right after the pages were
// streamed.
type disconnectOnWrite struct {
	http.ResponseWriter
	cancel context.CancelFunc
	marker string
}

func (w *disconnectOnWrite) Write(p []byte) (int, error) {
	n, err := w.ResponseWriter.Write(p)
	if strings.Contains(string(p), w.marker) {
		w.cancel()
	}
	return n, err
}

func (w *disconnectOnWrite) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

func TestGalleryPages_CacheSurvivesClientDisconnect(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryDetailHTML(12345, "Disconnect", 3))
	})
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/pages", nil)
	ctx, cancel := context.WithCancel(req.Context())
	defer cancel()
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()
	r.ServeHTTP(&disconnectOnWrite{ResponseWriter: w, cancel: cancel, marker: `"index":2`}, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	stream := parsePagesStream(t, w.Body.Bytes())
	if !stream.Done {
		t.Fatalf("stream must still finish with done: error = %q", stream.Error)
	}

	// The scrape completed before the client vanished, so the verified list
	// must reach the cache despite the canceled request context.
	row, found, err := gallerycache.Get(t.Context(), client, 12345, "tok12345")
	if err != nil || !found {
		t.Fatalf("cache lookup after disconnect: found=%v err=%v", found, err)
	}
	if len(row.Pages) != 3 {
		t.Errorf("cached pages = %d, want 3", len(row.Pages))
	}
}

// failWritesResponseWriter fails every write, simulating a client that is
// already gone when the streaming response starts.
type failWritesResponseWriter struct {
	http.ResponseWriter
}

func (w *failWritesResponseWriter) Write([]byte) (int, error) {
	return 0, errors.New("client disconnected")
}

func (w *failWritesResponseWriter) Flush() {}

func TestGalleryPages_MultiBatchWriteFailureStillCaches(t *testing.T) {
	// 100 pages = three thumbnail batches (40 + 40 + 20). The client is gone
	// from the first write, but the detached scrape must still walk every
	// batch and cache the complete list.
	var reqCount atomic.Int32
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		mockPaginatedGalleryHandler(91001, "Detached", 100)(w, r)
	})
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/91001/tok91001/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(&failWritesResponseWriter{ResponseWriter: w}, req)

	if got := reqCount.Load(); got != 3 {
		t.Errorf("upstream requests = %d, want 3 (first page + two ?p= batches)", got)
	}

	row, found, err := gallerycache.Get(t.Context(), client, 91001, "tok91001")
	if err != nil || !found {
		t.Fatalf("cache lookup after write failure: found=%v err=%v", found, err)
	}
	if len(row.Pages) != 100 {
		t.Errorf("cached pages = %d, want 100", len(row.Pages))
	}
}

func TestGalleryPages_ConcurrentScrapesShareSingleflight(t *testing.T) {
	// Each upstream response is delayed so both requests overlap: the first
	// becomes the singleflight leader, the second waits and replays.
	var reqCount atomic.Int32
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		time.Sleep(100 * time.Millisecond)
		mockPaginatedGalleryHandler(91002, "Shared", 65)(w, r)
	})
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	bodies := make([]string, 2)
	var wg sync.WaitGroup
	for i := range bodies {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			req := httptest.NewRequest("GET", "/api/gallery/91002/tok91002/pages", nil)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			bodies[i] = w.Body.String()
		}(i)
	}
	wg.Wait()

	if got := reqCount.Load(); got != 2 {
		t.Errorf("upstream requests = %d, want 2 (one shared scrape: first page + ?p=1)", got)
	}
	for i, body := range bodies {
		stream := parsePagesStream(t, []byte(body))
		if !stream.Done {
			t.Errorf("response %d did not finish with done: error=%q", i, stream.Error)
		}
		if len(stream.Pages) != 65 {
			t.Errorf("response %d pages = %d, want 65", i, len(stream.Pages))
		}
	}
}

func TestGetGallery_UpstreamFailure(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345")

	server := &Server{
		Client: errorClient(),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}

func TestGalleryDetails_UpstreamFailure(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345")

	server := &Server{
		Client: errorClient(),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/details", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
	}
}

// ==================== Cache-only endpoints ====================

func TestCachedGalleryPages_Hit(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345")

	server := &Server{DB: &database.DB{Client: client}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery-cache/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	var resp struct {
		Total int                `json:"total"`
		Pages []model.CachedPage `json:"pages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if resp.Total != 2 || len(resp.Pages) != 2 {
		t.Errorf("pages = %d (total %d), want 2", len(resp.Pages), resp.Total)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestCachedGalleryPages_Miss(t *testing.T) {
	client := newTestDB(t)
	server := &Server{DB: &database.DB{Client: client}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery-cache/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

func TestCachedGallery_Metadata(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345")

	server := &Server{DB: &database.DB{Client: client}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery-cache/12345/tok12345", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	var gallery model.Gallery
	if err := json.Unmarshal(w.Body.Bytes(), &gallery); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if gallery.Title != "Cached Gallery" {
		t.Errorf("title = %q, want %q", gallery.Title, "Cached Gallery")
	}
	if gallery.PageCount != 2 || gallery.Rating != 4.5 {
		t.Errorf("page_count=%d rating=%v, want 2 / 4.5", gallery.PageCount, gallery.Rating)
	}
	if len(gallery.Tags) != 1 {
		t.Errorf("tags = %d, want 1", len(gallery.Tags))
	}
}

func TestCachedGalleryDetails(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345")

	server := &Server{DB: &database.DB{Client: client}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery-cache/12345/tok12345/details", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	var details struct {
		ID       int         `json:"id"`
		Title    string      `json:"title"`
		Uploader string      `json:"uploader"`
		Cover    string      `json:"cover"`
		Tags     []model.Tag `json:"tags"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &details); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if details.ID != 12345 || details.Title != "Cached Gallery" {
		t.Errorf("id=%d title=%q, want 12345 / Cached Gallery", details.ID, details.Title)
	}
	if details.Uploader != "cached_uploader" || details.Cover == "" {
		t.Errorf("uploader=%q cover=%q, want cached values", details.Uploader, details.Cover)
	}
	if len(details.Tags) != 1 {
		t.Errorf("tags = %d, want 1", len(details.Tags))
	}
}

func TestCachedGallery_Miss(t *testing.T) {
	client := newTestDB(t)
	server := &Server{DB: &database.DB{Client: client}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery-cache/12345/tok12345", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("status = %d, want 404", w.Code)
	}
}

// ==================== Prefetch / first-write-wins / cleanup ====================

func TestPrefetchGallery_WritesCache(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryDetailHTML(55555, "Prefetch Test", 4))
	})
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: client},
	}

	seed := &model.Gallery{
		ID:        55555,
		Token:     "pf55555",
		Title:     "Prefetch Test",
		Category:  model.CategoryManga,
		Thumbnail: "https://example.com/pf.webp",
		PageCount: 4,
	}
	server.prefetchGallery(55555, "pf55555", seed)

	row, found, err := gallerycache.Get(t.Context(), client, 55555, "pf55555")
	if err != nil || !found {
		t.Fatalf("cache lookup: found=%v err=%v", found, err)
	}
	if row.Title != "Prefetch Test" {
		t.Errorf("title = %q, want %q", row.Title, "Prefetch Test")
	}
	if len(row.Pages) != 4 {
		t.Errorf("cached pages = %d, want 4", len(row.Pages))
	}
}

func TestPrefetchGallery_IncompletePagesNotCached(t *testing.T) {
	mockServer := newMockServer(mockPaginatedGalleryHandler(77777, "Incomplete", 65))
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: &http.Client{Transport: &failPaginatedTransport{mockURL: mockServer.URL}},
		DB:     &database.DB{Client: client},
	}

	seed := &model.Gallery{
		ID:        77777,
		Token:     "pf77777",
		Title:     "Incomplete",
		Category:  model.CategoryManga,
		Thumbnail: "https://example.com/incomplete.webp",
		PageCount: 65,
	}
	server.prefetchGallery(77777, "pf77777", seed)

	row, found, err := gallerycache.Get(t.Context(), client, 77777, "pf77777")
	if err != nil || !found {
		t.Fatalf("cache lookup: found=%v err=%v", found, err)
	}
	// Metadata is written independently, but a partial page list must not be
	// cached: first-write-wins would lock it in forever.
	if row.PagesFetchedAt != nil {
		t.Error("pages_fetched_at must not be set on a failed scrape")
	}
	if len(row.Pages) != 0 {
		t.Errorf("cached pages = %d, want 0", len(row.Pages))
	}
}

func TestGalleryCache_FirstValueWins(t *testing.T) {
	client := newTestDB(t)
	ctx := t.Context()

	if err := gallerycache.UpsertMeta(ctx, client, 9001, "tok", model.GalleryCacheSnapshot{
		Title:  "First",
		Rating: 4.0,
	}); err != nil {
		t.Fatalf("first meta write: %v", err)
	}
	// Second write must not overwrite existing fields, but should fill the
	// still-empty uploader.
	if err := gallerycache.UpsertMeta(ctx, client, 9001, "tok", model.GalleryCacheSnapshot{
		Title:    "Second",
		Rating:   5.0,
		Uploader: "uploader",
	}); err != nil {
		t.Fatalf("second meta write: %v", err)
	}

	row, found, err := gallerycache.Get(ctx, client, 9001, "tok")
	if err != nil || !found {
		t.Fatalf("cache lookup: found=%v err=%v", found, err)
	}
	if row.Title != "First" {
		t.Errorf("title = %q, want First (no overwrite)", row.Title)
	}
	if row.Rating != 4.0 {
		t.Errorf("rating = %v, want 4.0 (no overwrite)", row.Rating)
	}
	if row.Uploader != "uploader" {
		t.Errorf("uploader = %q, want uploader (fill empty)", row.Uploader)
	}

	// Pages are stored first-write only.
	if err := gallerycache.UpsertPages(ctx, client, 9001, "tok", []model.CachedPage{
		{PageURL: "a", Index: 0},
	}); err != nil {
		t.Fatalf("first pages write: %v", err)
	}
	if err := gallerycache.UpsertPages(ctx, client, 9001, "tok", []model.CachedPage{
		{PageURL: "a", Index: 0},
		{PageURL: "b", Index: 1},
		{PageURL: "c", Index: 2},
	}); err != nil {
		t.Fatalf("second pages write: %v", err)
	}
	row, _, _ = gallerycache.Get(ctx, client, 9001, "tok")
	if len(row.Pages) != 1 {
		t.Errorf("pages = %d, want 1 (no overwrite)", len(row.Pages))
	}
}

func TestCleanupGalleryCache_RemovesOrphans(t *testing.T) {
	db := newTestDatabase(t)
	ctx := t.Context()

	// A: referenced by bookshelf. B: referenced by reading progress. C: orphan.
	seedGalleryCache(t, db.Client, 1001, "tokA")
	seedGalleryCache(t, db.Client, 1002, "tokB")
	seedGalleryCache(t, db.Client, 1003, "tokC")

	if _, err := db.Client.Bookshelf.Create().
		SetGalleryID(1001).SetToken("tokA").SetTitle("A").Save(ctx); err != nil {
		t.Fatalf("insert bookshelf: %v", err)
	}
	if _, err := db.Client.ReadingProgress.Create().
		SetGalleryID(1002).SetToken("tokB").Save(ctx); err != nil {
		t.Fatalf("insert progress: %v", err)
	}

	deleted, err := db.CleanupGalleryCache(ctx)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if deleted != 1 {
		t.Errorf("deleted = %d, want 1", deleted)
	}

	if _, found, _ := gallerycache.Get(ctx, db.Client, 1001, "tokA"); !found {
		t.Error("bookshelf-referenced cache should be kept")
	}
	if _, found, _ := gallerycache.Get(ctx, db.Client, 1002, "tokB"); !found {
		t.Error("progress-referenced cache should be kept")
	}
	if _, found, _ := gallerycache.Get(ctx, db.Client, 1003, "tokC"); found {
		t.Error("orphan cache should be deleted")
	}
}
