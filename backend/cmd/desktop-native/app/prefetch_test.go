package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// blockingImageServer serves the pages of one gallery over a hook that holds
// every page image until the test closes release, so the test can watch which
// downloads are in flight and which were abandoned.
func blockingImageServer(t *testing.T, onPage func(url string)) (*httptest.Server, chan struct{}) {
	t.Helper()
	release := make(chan struct{})
	held := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakeImagePNG)
	})
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
	page := func(w http.ResponseWriter, r *http.Request) {
		if onPage != nil {
			onPage(r.URL.Query().Get("url"))
		}
		held(w, r)
	}
	mux.HandleFunc("/api/image/page", page)
	mux.HandleFunc("/api/image-cache/page", page)
	thumb := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakeImagePNG)
	}
	mux.HandleFunc("/api/image/thumbnail", thumb)
	mux.HandleFunc("/api/image-cache/thumbnail", thumb)
	mux.HandleFunc("/api/image-cache/page-thumbnail", thumb)
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv, release
}

// TestImageStoreReleaseAbandonsDownload covers the reader's release: a page
// that left the preload window has its download ended instead of finishing
// into a cache nobody shows, and asking for it again starts a fresh one
// rather than waiting out a retry.
func TestImageStoreReleaseAbandonsDownload(t *testing.T) {
	arrived := make(chan string, 8)
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		arrived <- r.URL.Path
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakeImagePNG)
	}))
	defer srv.Close()
	defer close(release)

	store := NewImageStore(4)
	slow := srv.URL + "/slow"
	stale := srv.URL + "/stale"

	store.Get(slow)
	<-arrived
	store.Get(stale)
	<-arrived

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
	store.Get(stale)
	select {
	case got := <-arrived:
		if got != "/stale" {
			t.Fatalf("re-requested %q, want /stale", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("released page was not fetched again")
	}
}

// TestReaderPreloadReleasesPassedPages checks the reader's preload window: as
// pages turn, the ones that left the window are released, so turning fast
// does not leave every passed page downloading ahead of the visible one.
func TestReaderPreloadReleasesPassedPages(t *testing.T) {
	var mu sync.Mutex
	fetched := map[string]int{}
	srv, release := blockingImageServer(t, func(url string) {
		mu.Lock()
		fetched[url]++
		mu.Unlock()
	})
	defer close(release)

	client := NewClient(srv.URL)
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

	// The first frame preloads the visible page and two ahead.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && len(a.reader.preloaded) < 3 {
		tt.Frame()
		time.Sleep(2 * time.Millisecond)
	}
	if len(a.reader.preloaded) != 3 {
		t.Fatalf("first window = %v, want the visible page and two ahead", a.reader.preloaded)
	}

	// Turn far enough that the first page leaves the window: by page 5 the
	// window is pages 4 to 7.
	for i := 0; i < 4; i++ {
		if err := tt.Click("下一页"); err != nil {
			t.Fatalf("turn %d: %v", i, err)
		}
	}
	page0 := "https://img.example/p0.jpg"
	storeURL := a.client.PageImageURL(page0)
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		a.pageImages.mu.Lock()
		_, present := a.pageImages.entries[storeURL]
		a.pageImages.mu.Unlock()
		if !present {
			break
		}
		tt.Frame()
		time.Sleep(2 * time.Millisecond)
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
	mu.Lock()
	n := fetched[page0]
	mu.Unlock()
	if n != 1 {
		t.Fatalf("page 1 fetched %d times, want once", n)
	}

	// Turning back queues page 1 again: a released page is fetched afresh,
	// not left as a failure waiting out the retry delay.
	for i := 0; i < 4; i++ {
		if err := tt.Click("上一页"); err != nil {
			t.Fatalf("turn back %d: %v", i, err)
		}
	}
	waitText(t, tt, "1 / 8 页")
	if !a.reader.preloaded[storeURL] {
		t.Fatalf("turning back did not preload page 1: %v", a.reader.preloaded)
	}
	mu.Lock()
	n = fetched[page0]
	mu.Unlock()
	if n < 2 {
		t.Fatalf("page 1 fetched %d times after turning back, want a fresh request", n)
	}
}
