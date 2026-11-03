package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// slowListServer answers the page list in two halves, with saved progress
// pointing at page 1. A thumbnail click asks for page 3, so the test sees
// whether the late half of the stream overrides the choice.
func slowListServer(t *testing.T, savedPage int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 1,
			"results": []map[string]any{galleryItem(1, "tok1", "慢画廊", "manga", 5)}})
	})
	mux.HandleFunc("/api/gallery/1/tok1/details", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, galleryDetail(1, "慢画廊", 5))
	})
	mux.HandleFunc("/api/gallery/1/tok1/pages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fl, _ := w.(http.Flusher)
		fmt.Fprintln(w, `{"type":"meta","total":5}`)
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, `{"type":"page","page_url":"https://img.example/p%d.jpg","index":%d}`+"\n", i, i)
			if fl != nil {
				fl.Flush()
			}
			if i == 2 {
				// Hold the rest back so the reader is running on a partial
				// list when the target is applied.
				time.Sleep(50 * time.Millisecond)
			}
		}
		fmt.Fprintln(w, `{"type":"done","total":5}`)
	})
	mux.HandleFunc("/api/progress/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"current_page": savedPage})
	})
	mux.HandleFunc("/api/bookshelf/1/tok1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"in_bookshelf": false})
	})
	return httptest.NewServer(mux)
}

// TestReaderTargetBeatsLateProgress covers the race a streamed page list
// opened: the reader marks itself loaded on its first page, a thumbnail
// click sets the page, and the stream's final result then restored the saved
// progress over it. The target must win however late the list arrives.
func TestReaderTargetBeatsLateProgress(t *testing.T) {
	srv := slowListServer(t, 1)
	defer srv.Close()

	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "慢画廊")
	if err := tt.Click("慢画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "继续阅读")

	// As the detail page's thumbnail grid does: ask for page 3, then open.
	a.readerStart = 2
	a.router.Push("/reader/1/tok1")
	navSettle(tt)

	waitReaderPage(t, tt, a, 2)
	// Let the second half of the stream land, which used to undo the choice.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		tt.Frame()
		time.Sleep(2 * time.Millisecond)
	}
	if got := a.reader.page; got != 2 {
		t.Fatalf("page after the list finished = %d, want 2 (the start page)", got)
	}
}

// TestReaderKeepsATurnWhileTheListStreams covers reading on while a long
// gallery's page list is still arriving. The page the reader opened at was
// applied again on every page that streamed in, so a turn made in the middle
// of the stream was pulled back to the opening page, which read as the reader
// blinking back to where the visit began.
func TestReaderKeepsATurnWhileTheListStreams(t *testing.T) {
	const total = 40
	release := make(chan struct{})
	_, url := newSlotServer(t, total, total, 4, release,
		map[string]any{"current_page": 3})
	a := New(NewClient(url))
	a.rememberReading(1, ReadingProgress{CurrentPage: 3})
	tt := ui.NewTester(a.View, 1280, 860)
	openReader(t, a, tt, "继续阅读 · 第 4 页")

	// The saved page has streamed in and opened the reader; the rest of the
	// list is still upstream. Turn on while it is.
	waitReaderPage(t, tt, a, 3)
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	waitReaderPage(t, tt, a, 4)

	// Let the rest of the stream arrive: the reader must stay on the page the
	// person turned to.
	close(release)
	var settled bool
	var page int
	waitFor(t, tt, "the stream to finish", func() bool {
		a.update(func() { settled, page = a.reader.settled, a.reader.page })
		return settled
	})
	if page != 4 {
		t.Fatalf("page after the list finished = %d, want 4 (the turn)", page)
	}
}

// TestReaderDoesNotJumpToLateSavedProgress covers the saved page the backend
// reports only when the list ends. A person who has begun reading from the
// first page must not be pulled to it by the stream's last frame, which is
// the same blink as a turn being undone.
func TestReaderDoesNotJumpToLateSavedProgress(t *testing.T) {
	const total = 40
	release := make(chan struct{})
	_, url := newSlotServer(t, total, total, 4, release,
		map[string]any{"current_page": 30})
	a := New(NewClient(url))
	tt := ui.NewTester(a.View, 1280, 860)
	// Visit the gallery so the reader knows how many slots to lay out, then
	// open the reader itself: the detail page's button would ask for its own
	// resumed page, and the point here is a reader that starts at the first
	// page and only learns the saved page when the stream ends.
	waitText(t, tt, "分页画廊")
	if err := tt.Click("分页画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	a.router.Push("/reader/1/tok1")
	navSettle(tt)

	// Read on while the saved page is still only upstream.
	waitReaderPage(t, tt, a, 0)
	for i := 0; i < 4; i++ {
		tt.Key(0, ui.KeyLeft)
	}
	tt.Frame()
	waitReaderPage(t, tt, a, 4)

	close(release)
	var settled bool
	var page int
	waitFor(t, tt, "the stream to finish", func() bool {
		a.update(func() { settled, page = a.reader.settled, a.reader.page })
		return settled
	})
	if page != 4 {
		t.Fatalf("page after the list finished = %d, want 4 (where the read got to)", page)
	}
}

// TestReaderResumesSavedProgress is the other side: with no target, the saved
// page is what the reader opens at.
func TestReaderResumesSavedProgress(t *testing.T) {
	srv := slowListServer(t, 3)
	defer srv.Close()

	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "慢画廊")
	if err := tt.Click("慢画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "继续阅读 · 第 4 页")
	if err := tt.Click("继续阅读 · 第 4 页"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)

	waitReaderPage(t, tt, a, 3)
}
