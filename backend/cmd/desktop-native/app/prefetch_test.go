package app

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestImageStoreReleaseAbandonsDownload covers the reader's release: a page
// that left the preload window has its download ended instead of finishing
// into a cache nobody shows, and asking for it again starts a fresh one
// rather than waiting out a retry.
func TestImageStoreReleaseAbandonsDownload(t *testing.T) {
	requests := make(chan string, 8)
	srv := apiURL(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case requests <- r.URL.Path:
		default:
		}
		// Held until the test's server is closed: a release ends the request
		// from the client's side, not the server's.
		select {
		case <-r.Context().Done():
		case <-time.After(30 * time.Second):
		}
	}))
	store := NewImageStore(4)
	slow := srv + "/slow"
	stale := srv + "/stale"

	// waiting waits for the server to receive path, asking for the page
	// again meanwhile: the download runs in a goroutine of its own, which a
	// single-CPU runner may not schedule at once, and Get on a page that is
	// already downloading just asks again.
	waiting := func(url, path string) {
		t.Helper()
		deadline := time.Now().Add(5 * time.Second)
		for time.Now().Before(deadline) {
			store.Get(url)
			select {
			case got := <-requests:
				if got == path {
					return
				}
			case <-time.After(2 * time.Millisecond):
			}
		}
		t.Fatalf("the server never received %s", path)
	}
	waiting(slow, "/slow")
	waiting(stale, "/stale")

	// Releasing the stale page drops it; the one still wanted stays.
	store.Release(stale)
	store.mu.Lock()
	_, stalePresent := store.entries[stale]
	_, slowPresent := store.entries[slow]
	store.mu.Unlock()
	if stalePresent {
		t.Fatal("released page is still in the store")
	}
	if !slowPresent {
		t.Fatal("release dropped the page still wanted")
	}

	// Asking again queues a new download, not a retry delay.
	waiting(stale, "/stale")
}

// TestReaderPreloadReleasesPassedPages checks the reader's preload window: as
// pages turn, the ones that left the window are released, so turning fast
// does not leave every passed page downloading ahead of the visible one. The
// page outside the warm window is still asked for once per frame, so a
// failure the store has dropped starts it again instead of leaving a blank
// page (readerPageImage asks again while a placeholder is shown).
func TestReaderPreloadReleasesPassedPages(t *testing.T) {
	gate := newResponseGate()
	defer gate.Close()
	srv := blockingImageServer(t, gate)

	client := NewClient(srv)
	client.UseImageCache(true)
	a := New(client)
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "长画廊")
	if err := tt.Click("长画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "1 / 8 页")

	// The first frame preloads the visible page and two ahead, and keeps the
	// two past them asked for. The window is the store's record of what the
	// reader asked for, so it is complete as soon as the frames that asked
	// are drawn.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(a.reader.preloaded) < 3+readerKeepPages {
		tt.Frame()
		time.Sleep(2 * time.Millisecond)
	}
	if got := len(a.reader.preloaded); got != 3+readerKeepPages {
		t.Fatalf("first window = %v, want the visible page, two ahead and two kept", a.reader.preloaded)
	}

	// Turn far enough that the first page leaves the window: by page 5 the
	// warm window is pages 3 to 7 and page 1 is only kept, past its downloads.
	// Turning the page is what moves the window, so the turns are made
	// directly: a click in the assembled view, with a single usable CPU, can
	// wait for a frame the runtime has not scheduled.
	for i := 0; i < 4; i++ {
		a.update(func() { a.readerNext(&a.reader) })
		tt.Frame()
	}
	page0 := "https://img.example/p0.jpg"
	storeURL := a.client.PageImageURL(page0)
	if a.reader.page != 4 {
		t.Fatalf("page = %d, want 4", a.reader.page)
	}
	if a.reader.preloaded[storeURL] {
		t.Fatalf("page 1 never left the window: %v", a.reader.preloaded)
	}
	// The visible page is still wanted.
	if !a.reader.preloaded[a.client.PageImageURL("https://img.example/p4.jpg")] {
		t.Fatalf("visible page is not preloaded: %v", a.reader.preloaded)
	}

	// A page still downloading outlives the release above, so that the
	// frame showing it is never left without it (see ImageStore.BeginFrame);
	// the release's end of it is checked by
	// TestReaderPageKeepsItsPictureWhileTheWindowMoves. Open the gate so
	// that page finishes, then let a frame be built without it: the store
	// drops it once it is neither wanted nor on screen.
	gate.Close()
	tt.Advance(50 * time.Millisecond)
	gone := false
	for i := 0; i < 20 && !gone; i++ {
		a.pageImages.BeginFrame()
		tt.Frame()
		a.pageImages.mu.Lock()
		gone = a.pageImages.entries[storeURL] == nil
		a.pageImages.mu.Unlock()
	}
	if !gone {
		t.Fatalf("page 1 was not released")
	}

	// Turning back queues page 1 again: a released page is fetched afresh,
	// not left as a failure waiting out the retry delay. The turns go through
	// update, as the view's do: the reader is drawn on the main thread, and
	// the progress save a turn arms reads the page there too.
	for i := 0; i < 4; i++ {
		a.update(func() { a.readerPrev(&a.reader) })
		tt.Frame()
	}
	if a.reader.page != 0 {
		t.Fatalf("page = %d after turning back, want 0", a.reader.page)
	}
	if !a.reader.preloaded[storeURL] {
		t.Fatalf("turning back did not preload page 1: %v", a.reader.preloaded)
	}
	// The gate is still open, so the fresh download may already have
	// arrived: a page is queued again when turning back either way, and one
	// the store has to load is in flight.
	a.pageImages.mu.Lock()
	e, present := a.pageImages.entries[storeURL]
	loading := present && e.loading
	loaded := present && e.bitmap != nil
	a.pageImages.mu.Unlock()
	if !present || !loading && !loaded {
		t.Fatalf("turning back did not queue the page again (present=%v)", present)
	}
}

// blockingImageServer serves the pages of one gallery over a gate that holds
// every page image until the test closes it, so the test can watch which
// downloads are in flight and which were abandoned. The handler counts the
// pages asked for by itself, so the assertions never wait on a server
// goroutine (see responseGate).
func blockingImageServer(t *testing.T, gate *responseGate) string {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 1,
			"results": []map[string]any{galleryItem(1, "tok1", "长画廊", "manga", 8)}})
	})
	mux.HandleFunc("/api/gallery/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, galleryDetail(1, "长画廊", 8))
	})
	pages := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"type":"meta","total":8}`)
		for i := 0; i < 8; i++ {
			fmt.Fprintf(w, `{"type":"page","index":%d,"page_url":"https://img.example/p%d.jpg"}`+"\n", i, i)
		}
		fmt.Fprintln(w, `{"type":"done"}`)
	}
	mux.HandleFunc("/api/gallery/1/tok1/pages", pages)
	mux.HandleFunc("/api/gallery-cache/1/tok1/pages", pages)
	mux.HandleFunc("/api/progress/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			w.WriteHeader(http.StatusOK)
			return
		}
		writeJSON(w, map[string]any{"current_page": 0})
	})
	mux.HandleFunc("/api/bookshelf/1/tok1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"in_bookshelf": false})
	})
	mux.HandleFunc("/api/image/page", gate.serve)
	mux.HandleFunc("/api/image-cache/page", gate.serve)
	thumb := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakeImagePNG)
	}
	mux.HandleFunc("/api/image/thumbnail", thumb)
	mux.HandleFunc("/api/image-cache/thumbnail", thumb)
	mux.HandleFunc("/api/image-cache/page-thumbnail", thumb)
	return apiURL(t, mux)
}

// TestReaderPageKeepsItsPictureWhileTheWindowMoves covers the reader asked for
// the same page at two speeds: the pages around it change as it turns or as
// the list streams in, while the page on screen is drawn every frame. The
// screen must keep the bitmap it was already showing: releasing the pictures
// that leave the preload window must never reach the one a frame draws, or it
// would leave the view, come back, and the page would blink between its
// gradient and its picture.
func TestReaderPageKeepsItsPictureWhileTheWindowMoves(t *testing.T) {
	gate := newResponseGate()
	srv := blockingImageServer(t, gate)
	client := NewClient(srv)
	client.UseImageCache(true)
	a := New(client)
	tt := ui.NewTester(a.View, 1280, 860)
	defer gate.Close()
	waitText(t, tt, "长画廊")

	// Open the reader directly: the page list loads as the page first draws,
	// and the blocking image server is what the assertions watch, not the
	// clicks that would lay out the list underneath.
	a.router.Push("/reader/1/tok1")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !a.reader.loaded {
		tt.Frame()
		time.Sleep(2 * time.Millisecond)
	}
	if !a.reader.loaded || len(a.reader.pages) != 8 {
		t.Fatalf("reader loaded=%v pages=%d, want the 8 of the blocking server", a.reader.loaded, len(a.reader.pages))
	}
	visible := a.client.PageImageURL(a.reader.pages[0])
	neighbour := a.client.PageImageURL(a.reader.pages[2])

	// Every frame asks for the pages it shows, as the fast path does.
	ask := func() {
		for i := 0; i < 10; i++ {
			a.pageImages.Get(visible)
		}
		a.preloadPages(&a.reader)
	}
	ask()

	// Open the gate: the page and its neighbours arrive.
	gate.Close()
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		ask()
		if a.pageImages.Get(visible) != nil && a.pageImages.Get(neighbour) != nil {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	picture := a.pageImages.Get(visible)
	if picture == nil {
		t.Fatal("the visible page never arrived, so the test cannot see it blink")
	}

	// Move the window the way a burst of turns does, and keep asking for the
	// page: each round makes some page enter and some leave.
	for turn := 1; turn <= 4; turn++ {
		a.update(func() { a.reader.page = turn })
		ask()
		tt.Frame()
		if got := a.pageImages.Get(visible); got != picture {
			t.Fatalf("turn %d: the visible page left its picture (%v); the page would blink", turn, got == nil)
		}
	}
}
