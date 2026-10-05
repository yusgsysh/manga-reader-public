package handler

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	json "encoding/json/v2"

	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	entgallerycache "manga-reader/internal/ent/gallerycache"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/model"
)

// ageGalleryCache backdates the fetched-at timestamps so the online endpoints
// treat the cached row as stale and re-fetch from upstream.
func ageGalleryCache(t *testing.T, client *ent.Client, id int64, token string, age time.Duration) {
	t.Helper()
	old := time.Now().UTC().Add(-age)
	if err := client.GalleryCache.Update().
		Where(entgallerycache.GalleryID(id), entgallerycache.Token(token)).
		SetMetaFetchedAt(old).
		SetDetailsFetchedAt(old).
		SetPagesFetchedAt(old).
		Exec(t.Context()); err != nil {
		t.Fatalf("age gallery cache: %v", err)
	}
}

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
	if err := gallerycache.UpsertDetails(t.Context(), client, id, token, model.GalleryCacheSnapshot{
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
	if err := gallerycache.UpsertPages(t.Context(), client, id, token,
		[]string{"https://exhentai.org/s/abc/1", "https://exhentai.org/s/abc/2"}); err != nil {
		t.Fatalf("seed cache pages: %v", err)
	}
	if err := gallerycache.UpsertThumbnails(t.Context(), client, id, token,
		[]model.GalleryPageThumb{{}, {}}); err != nil {
		t.Fatalf("seed cache thumbnails: %v", err)
	}
}

// ==================== Online endpoints: no cache side effects ====================

func TestGalleryPages_OnlineDoesNotWriteCache(t *testing.T) {
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

	// The online endpoint always scrapes upstream and never writes the cache.
	if _, found, err := gallerycache.Get(t.Context(), client, 12345, "tok12345"); err != nil || found {
		t.Fatalf("online /pages must not write cache: found=%v err=%v", found, err)
	}
}

func TestCachedGalleryPages_MissWritesCache(t *testing.T) {
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

	req := httptest.NewRequest("GET", "/api/gallery-cache/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	stream := parsePagesStream(t, w.Body.Bytes())
	if !stream.Done || len(stream.PageURLs) != 3 {
		t.Fatalf("stream done=%v pages=%d, want done with 3", stream.Done, len(stream.PageURLs))
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

func TestCachedGalleryPages_HitServedWithoutUpstream(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345")

	// errorClient makes any upstream call fail; a cache hit must still be
	// served without touching upstream.
	server := &Server{
		Client: errorClient(),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery-cache/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (cache hit). body: %s", w.Code, w.Body.String())
	}
	stream := parsePagesStream(t, w.Body.Bytes())
	if !stream.Done || len(stream.PageURLs) != 2 {
		t.Fatalf("stream should replay the cached 2 pages: done=%v pages=%d", stream.Done, len(stream.PageURLs))
	}
}

func TestGalleryPages_StaleCacheUpstreamFailure(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345")
	ageGalleryCache(t, client, 12345, "tok12345", 2*time.Hour)

	server := &Server{
		Client: errorClient(),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// A stale list is not served; upstream failure returns 502 so the frontend
	// falls back to the gallery-cache endpoint (and shows the offline badge).
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
	if len(stream.PageURLs) == 0 {
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

	req := httptest.NewRequest("GET", "/api/gallery-cache/12345/tok12345/pages", nil)
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

	req := httptest.NewRequest("GET", "/api/gallery-cache/91001/tok91001/pages", nil)
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
		if len(stream.PageURLs) != 65 {
			t.Errorf("response %d pages = %d, want 65", i, len(stream.PageURLs))
		}
	}
}

// signalOnWrite closes ch the first time a write contains marker.
type signalOnWrite struct {
	http.ResponseWriter
	marker string
	ch     chan struct{}
	once   sync.Once
}

func (w *signalOnWrite) Write(p []byte) (int, error) {
	if strings.Contains(string(p), w.marker) {
		w.once.Do(func() { close(w.ch) })
	}
	return w.ResponseWriter.Write(p)
}

func (w *signalOnWrite) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}

// A request that joins an in-flight scrape must stream the already-scraped
// prefix immediately, before the scrape finishes. Before the shared-stream
// hub, followers blocked until the entire upstream walk completed, leaving the
// gallery page stuck on its loading skeleton.
func TestGalleryPages_FollowerStreamsBeforeScrapeCompletes(t *testing.T) {
	firstServed := make(chan struct{})
	var firstOnce sync.Once
	release := make(chan struct{})

	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("p") == "" {
			firstOnce.Do(func() { close(firstServed) })
			mockPaginatedGalleryHandler(91003, "Slow", 65)(w, r)
			return
		}
		// Hold the second batch so the scrape cannot finish while the follower
		// is expected to stream the replayed first batch.
		<-release
		mockPaginatedGalleryHandler(91003, "Slow", 65)(w, r)
	})
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	var wg sync.WaitGroup
	leaderW := httptest.NewRecorder()
	wg.Go(func() {
		req := httptest.NewRequest("GET", "/api/gallery/91003/tok91003/pages", nil)
		r.ServeHTTP(leaderW, req)
	})

	select {
	case <-firstServed:
	case <-time.After(2 * time.Second):
		t.Fatal("leader never fetched the first upstream document")
	}
	// Let the leader publish the first batch to the shared stream.
	time.Sleep(50 * time.Millisecond)

	followerGotPage := make(chan struct{})
	followerW := httptest.NewRecorder()
	fw := &signalOnWrite{ResponseWriter: followerW, marker: `"index":0`, ch: followerGotPage}
	wg.Go(func() {
		req := httptest.NewRequest("GET", "/api/gallery/91003/tok91003/pages", nil)
		r.ServeHTTP(fw, req)
	})

	select {
	case <-followerGotPage:
	case <-time.After(2 * time.Second):
		t.Fatal("follower blocked on the in-flight scrape instead of streaming its prefix")
	}

	close(release)
	wg.Wait()

	for name, body := range map[string]string{
		"leader":   leaderW.Body.String(),
		"follower": followerW.Body.String(),
	} {
		stream := parsePagesStream(t, []byte(body))
		if !stream.Done || len(stream.PageURLs) != 65 {
			t.Errorf("%s: done=%v pages=%d, want done with 65", name, stream.Done, len(stream.PageURLs))
		}
	}
}

// A request that joins an in-flight scrape must receive the batches already
// scraped immediately, then follow the live scrape. Before the shared-stream
// hub, followers blocked until the whole scrape finished.
func TestGalleryPagesStream_ReplaysPrefixToLateSubscriber(t *testing.T) {
	stream := newGalleryPagesStream()
	stream.append(3, []string{"p0", "p1"}, []model.GalleryPageThumb{{}, {}})

	batches := make(chan int, 4)
	drainErr := make(chan error, 1)
	go func() {
		_, err := stream.drain(t.Context(), 0, func(_ int, urls []string, thumbs []model.GalleryPageThumb) error {
			batches <- len(urls)
			return nil
		})
		drainErr <- err
	}()

	select {
	case n := <-batches:
		if n != 2 {
			t.Fatalf("replayed batch = %d pages, want 2", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("late subscriber did not receive the already-scraped prefix")
	}

	stream.append(3, []string{"p2"}, []model.GalleryPageThumb{{}})
	stream.finish(nil)

	select {
	case n := <-batches:
		if n != 1 {
			t.Fatalf("live batch = %d pages, want 1", n)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("subscriber did not follow the live scrape")
	}

	if err := <-drainErr; err != nil {
		t.Fatalf("drain error = %v, want nil", err)
	}
}

// A subscriber whose context is cancelled (e.g. an abandoned thumbnail
// request) must stop waiting on the shared stream instead of blocking until
// the detached walk finishes — without disturbing the stream itself.
func TestPagesStream_DrainContextCancel(t *testing.T) {
	stream := newGalleryPagesStream() // empty and never finished

	ctx, cancel := context.WithCancel(t.Context())
	drainErr := make(chan error, 1)
	go func() {
		_, err := stream.drain(ctx, 0, func(int, []string, []model.GalleryPageThumb) error { return nil })
		drainErr <- err
	}()

	// Give the drain a moment to park on the condition variable.
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-drainErr:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("drain error = %v, want context.Canceled", err)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("drain kept waiting after its context was cancelled")
	}

	// The stream is unaffected: later subscribers still drain normally.
	stream.append(1, []string{"p0"}, []model.GalleryPageThumb{{}})
	stream.finish(nil)
	if _, err := stream.drain(t.Context(), 0, nil); err != nil {
		t.Fatalf("drain after cancellation = %v, want nil", err)
	}
}

// A finished (but not-yet-released) stream must never be handed to a new
// request, otherwise a later scrape would replay stale data instead of hitting
// upstream again.
func TestPagesHub_AcquireAfterFinishStartsFresh(t *testing.T) {
	hub := newPagesHub()

	first, leader := hub.acquire("k")
	if !leader {
		t.Fatal("first acquire should be the leader")
	}

	same, leader := hub.acquire("k")
	if leader {
		t.Fatal("second acquire while in flight must not be the leader")
	}
	if same != first {
		t.Fatal("second acquire should share the in-flight stream")
	}

	first.finish(nil)

	fresh, leader := hub.acquire("k")
	if !leader {
		t.Fatal("acquire after finish should start a fresh scrape")
	}
	if fresh == first {
		t.Fatal("expected a fresh stream after the previous one finished")
	}
}

func TestGetGallery_OnlineIgnoresCache(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345")

	// The online metadata endpoint always scrapes upstream; a populated cache
	// must not short-circuit it.
	server := &Server{
		Client: errorClient(),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502 (online ignores cache). body: %s", w.Code, w.Body.String())
	}
}

func TestGetGallery_StaleCacheUpstreamFailure(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345")
	ageGalleryCache(t, client, 12345, "tok12345", 2*time.Hour)

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
	stream := parsePagesStream(t, w.Body.Bytes())
	if !stream.Done || len(stream.PageURLs) != 2 {
		t.Fatalf("pages = %d done=%v, want done with 2", len(stream.PageURLs), stream.Done)
	}
	if got := w.Header().Get("Cache-Control"); got != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", got)
	}
}

func TestCachedGalleryPages_MissUpstreamFailure(t *testing.T) {
	client := newTestDB(t)
	server := &Server{
		Client: errorClient(),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	// A miss reads through upstream; when that fails the request returns 502.
	req := httptest.NewRequest("GET", "/api/gallery-cache/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
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

func TestCachedGallery_MissUpstreamFailure(t *testing.T) {
	client := newTestDB(t)
	server := &Server{
		Client: errorClient(),
		DB:     &database.DB{Client: client},
	}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery-cache/12345/tok12345", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want 502", w.Code)
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

func TestGalleryCache_MetaTracksApiPagesFirstWrite(t *testing.T) {
	client := newTestDB(t)
	ctx := t.Context()

	if err := gallerycache.UpsertMeta(ctx, client, 9001, "tok", model.GalleryCacheSnapshot{
		Title:  "First",
		Rating: 4.0,
	}); err != nil {
		t.Fatalf("first meta write: %v", err)
	}
	// A later API response overwrites non-empty fields and fills the uploader.
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
	if row.Title != "Second" {
		t.Errorf("title = %q, want Second (updated from API)", row.Title)
	}
	if row.Rating != 5.0 {
		t.Errorf("rating = %v, want 5.0 (updated from API)", row.Rating)
	}
	if row.Uploader != "uploader" {
		t.Errorf("uploader = %q, want uploader", row.Uploader)
	}

	// An empty incoming subset must not blank out richer stored fields.
	if err := gallerycache.UpsertMeta(ctx, client, 9001, "tok", model.GalleryCacheSnapshot{
		Title: "Second",
	}); err != nil {
		t.Fatalf("third meta write: %v", err)
	}
	row, _, _ = gallerycache.Get(ctx, client, 9001, "tok")
	if row.Rating != 5.0 || row.Uploader != "uploader" {
		t.Errorf("empty fields must not overwrite: rating=%v uploader=%q", row.Rating, row.Uploader)
	}

	// Page URLs merge index by index and never shrink.
	if err := gallerycache.UpsertPages(ctx, client, 9001, "tok", []string{"a"}); err != nil {
		t.Fatalf("first pages write: %v", err)
	}
	if err := gallerycache.UpsertPages(ctx, client, 9001, "tok", []string{"a", "b", "c"}); err != nil {
		t.Fatalf("second pages write: %v", err)
	}
	row, _, _ = gallerycache.Get(ctx, client, 9001, "tok")
	if len(row.Pages) != 3 {
		t.Errorf("pages = %d, want 3 (merged)", len(row.Pages))
	}
	// A shorter list must not truncate the cached one.
	if err := gallerycache.UpsertPages(ctx, client, 9001, "tok", []string{"a"}); err != nil {
		t.Fatalf("shorter pages write: %v", err)
	}
	row, _, _ = gallerycache.Get(ctx, client, 9001, "tok")
	if len(row.Pages) != 3 {
		t.Errorf("pages = %d, want 3 (never shrinks)", len(row.Pages))
	}
}

func TestUpsertThumbnails_BackfillsThumbnailGeometry(t *testing.T) {
	client := newTestDB(t)
	ctx := t.Context()

	// A row cached before sprite geometry existed (pages only, no thumbnails).
	if err := gallerycache.UpsertPages(ctx, client, 9002, "tok", []string{"a", "b"}); err != nil {
		t.Fatalf("pages write: %v", err)
	}

	// A later scrape with the same pages plus geometry fills it in.
	if err := gallerycache.UpsertThumbnails(ctx, client, 9002, "tok",
		[]model.GalleryPageThumb{
			{SpriteURL: "https://cdn.example/1-0.webp", Width: 200, Height: 282},
			{SpriteURL: "https://cdn.example/1-1.webp", Width: 200, Height: 282},
		}); err != nil {
		t.Fatalf("backfill write: %v", err)
	}

	row, _, err := gallerycache.Get(ctx, client, 9002, "tok")
	if err != nil {
		t.Fatalf("cache lookup: %v", err)
	}
	if len(row.Thumbnails) != 2 || row.Thumbnails[0].SpriteURL == "" || row.Thumbnails[1].SpriteURL == "" {
		t.Fatalf("thumbnails should be backfilled with geometry: %+v", row.Thumbnails)
	}

	// A geometry-less list must not downgrade the enriched one.
	if err := gallerycache.UpsertThumbnails(ctx, client, 9002, "tok",
		[]model.GalleryPageThumb{{}, {}}); err != nil {
		t.Fatalf("downgrade write: %v", err)
	}
	row, _, _ = gallerycache.Get(ctx, client, 9002, "tok")
	if row.Thumbnails[0].SpriteURL == "" {
		t.Error("a geometry-less list must not overwrite an enriched one")
	}

	// Same count with changed geometry repairs the entry (page-level update).
	if err := gallerycache.UpsertThumbnails(ctx, client, 9002, "tok",
		[]model.GalleryPageThumb{{SpriteURL: "https://cdn.example/1-0-v2.webp", X: 10}, {}}); err != nil {
		t.Fatalf("repair write: %v", err)
	}
	row, _, _ = gallerycache.Get(ctx, client, 9002, "tok")
	if row.Thumbnails[0].X != 10 {
		t.Errorf("thumb X = %d, want 10 (repaired)", row.Thumbnails[0].X)
	}
}

func TestUpsertThumbnails_NoPagesIsNoop(t *testing.T) {
	client := newTestDB(t)
	ctx := t.Context()

	if err := gallerycache.UpsertThumbnails(ctx, client, 9003, "tok",
		[]model.GalleryPageThumb{{SpriteURL: "https://cdn.example/1-0.webp"}}); err != nil {
		t.Fatalf("upsert on missing row: %v", err)
	}
	if _, found, err := gallerycache.Get(ctx, client, 9003, "tok"); err != nil || found {
		t.Errorf("upsert without pages must not create a row: found=%v err=%v", found, err)
	}
}

func TestReplacePages_Shrinks(t *testing.T) {
	client := newTestDB(t)
	ctx := t.Context()

	if err := gallerycache.UpsertPages(ctx, client, 9004, "tok", []string{"a", "b", "c"}); err != nil {
		t.Fatalf("seed pages: %v", err)
	}
	if err := gallerycache.ReplacePages(ctx, client, 9004, "tok", []string{"x", "y"}); err != nil {
		t.Fatalf("replace pages: %v", err)
	}

	row, found, err := gallerycache.Get(ctx, client, 9004, "tok")
	if err != nil || !found {
		t.Fatalf("cache lookup: found=%v err=%v", found, err)
	}
	if len(row.Pages) != 2 || row.Pages[0] != "x" || row.Pages[1] != "y" {
		t.Errorf("pages = %v, want [x y] (shrunk)", row.Pages)
	}
}

func TestGetByGalleryID(t *testing.T) {
	client := newTestDB(t)
	ctx := t.Context()

	if err := gallerycache.UpsertMeta(ctx, client, 9100, "tokg", model.GalleryCacheSnapshot{Title: "By ID"}); err != nil {
		t.Fatalf("seed meta: %v", err)
	}
	row, found, err := gallerycache.GetByGalleryID(ctx, client, 9100)
	if err != nil || !found {
		t.Fatalf("GetByGalleryID: found=%v err=%v", found, err)
	}
	if row.Token != "tokg" {
		t.Errorf("token = %q, want tokg", row.Token)
	}
	if _, found, err := gallerycache.GetByGalleryID(ctx, client, 424242); err != nil || found {
		t.Errorf("missing gallery: found=%v err=%v, want false/nil", found, err)
	}
}

// refreshPages is the cache-fill path used by the page-count check and image
// self-heal: it replaces the cached list wholesale, so a gallery that lost
// pages shrinks instead of keeping stale trailing entries.
func TestRefreshPages_Shrinks(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryDetailHTML(12345, "Shorter", 2))
	})
	defer mockServer.Close()

	client := newTestDB(t)
	if err := gallerycache.UpsertPages(t.Context(), client, 12345, "tok12345",
		[]string{"a", "b", "c", "d", "e"}); err != nil {
		t.Fatalf("seed pages: %v", err)
	}

	server := &Server{Client: newMockClient(mockServer.URL), DB: &database.DB{Client: client}}
	server.refreshPages(12345, "tok12345")

	row, found, err := gallerycache.Get(t.Context(), client, 12345, "tok12345")
	if err != nil || !found {
		t.Fatalf("cache lookup: found=%v err=%v", found, err)
	}
	if len(row.Pages) != 2 {
		t.Errorf("pages = %d, want 2 (replaced)", len(row.Pages))
	}
}

// A permanently failed cached page URL must trigger a background gallery
// refresh so a stale page list self-heals. A 404 page response is classified
// as a permanent upstream error, which reaches the existing triggerPageRefresh.
func TestCachedImage_PermanentFailureRefreshesGalleryCache(t *testing.T) {
	const id, token = 91005, "tok91005"
	pageURL := "https://exhentai.org/s/abc/91005-1"

	db := newTestDB(t)
	if err := gallerycache.UpsertPages(t.Context(), db, id, token,
		[]string{"https://exhentai.org/s/old/91005-1"}); err != nil {
		t.Fatalf("seed pages: %v", err)
	}

	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/g/") {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, mockGalleryDetailHTML(id, "Healed", 3))
			return
		}
		// The stale /s/ page is gone.
		w.WriteHeader(http.StatusNotFound)
	})
	defer mockServer.Close()

	server := &Server{
		Client: newMockClient(mockServer.URL),
		DB:     &database.DB{Client: db},
		Cache:  newMockImageCache(),
	}
	r := setupMockRouter(server)

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest("GET", "/api/image-cache/page?url="+url.QueryEscape(pageURL), nil))

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusBadGateway, w.Body.String())
	}

	// The refresh runs detached; wait for the page list to be replaced.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		row, found, err := gallerycache.Get(t.Context(), db, id, token)
		if err == nil && found && len(row.Pages) == 3 {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Error("gallery cache was never refreshed after a permanent page failure")
}

// verifyCachedPageCount is the stale-while-revalidate probe: when upstream's
// ".gpc" total no longer matches the cached count it refreshes the list.
func TestVerifyCachedPageCount_RefreshesOnChange(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryDetailHTML(12345, "Changed", 4))
	})
	defer mockServer.Close()

	client := newTestDB(t)
	seedGalleryCache(t, client, 12345, "tok12345") // cached 2 pages

	server := &Server{Client: newMockClient(mockServer.URL), DB: &database.DB{Client: client}}
	server.verifyCachedPageCount(12345, "tok12345", 2)

	row, found, err := gallerycache.Get(t.Context(), client, 12345, "tok12345")
	if err != nil || !found {
		t.Fatalf("cache lookup: found=%v err=%v", found, err)
	}
	if len(row.Pages) != 4 {
		t.Errorf("pages = %d, want 4 (refreshed on count change)", len(row.Pages))
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
		SetGalleryID(1001).SetToken("tokA").Save(ctx); err != nil {
		t.Fatalf("insert bookshelf: %v", err)
	}
	if _, err := db.Client.ReadingProgress.Create().
		SetGalleryID(1002).SetToken("tokB").Save(ctx); err != nil {
		t.Fatalf("insert progress: %v", err)
	}

	keys, err := db.CleanupGalleryCache(ctx)
	if err != nil {
		t.Fatalf("cleanup: %v", err)
	}
	if len(keys) != 1 {
		t.Fatalf("deleted = %d, want 1", len(keys))
	}
	if keys[0].GalleryID != 1003 || keys[0].Token != "tokC" {
		t.Errorf("deleted key = (%d, %q), want (1003, tokC)", keys[0].GalleryID, keys[0].Token)
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
