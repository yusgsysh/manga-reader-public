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
		a.readerNext(&a.reader)
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

	a.pageImages.mu.Lock()
	_, present := a.pageImages.entries[storeURL]
	a.pageImages.mu.Unlock()
	if present {
		t.Fatalf("page 1 was not released")
	}

	// Turning back queues page 1 again: a released page is fetched afresh,
	// not left as a failure waiting out the retry delay.
	for i := 0; i < 4; i++ {
		a.readerPrev(&a.reader)
		tt.Frame()
	}
	if a.reader.page != 0 {
		t.Fatalf("page = %d after turning back, want 0", a.reader.page)
	}
	if !a.reader.preloaded[storeURL] {
		t.Fatalf("turning back did not preload page 1: %v", a.reader.preloaded)
	}
	a.pageImages.mu.Lock()
	e, present := a.pageImages.entries[storeURL]
	loading := present && e.loading
	a.pageImages.mu.Unlock()
	if !present || !loading {
		t.Fatalf("turning back did not queue a fresh download (present=%v)", present)
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
