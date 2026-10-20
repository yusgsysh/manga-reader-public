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
	mux.HandleFunc("/api/gallery/1/tok1", func(w http.ResponseWriter, r *http.Request) {
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
	waitText(t, tt, "开始阅读")

	// As the detail page's thumbnail grid does: ask for page 3, then open.
	a.readerStart = 2
	a.router.Push("/reader/1/tok1")
	navSettle(tt)

	waitText(t, tt, "3 / 5 页")
	// Let the second half of the stream land, which used to undo the choice.
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		tt.Frame()
		time.Sleep(2 * time.Millisecond)
	}
	if got := a.reader.page; got != 2 {
		t.Fatalf("page after the list finished = %d, want 2 (the start page)", got)
	}
	if !tt.HasText("3 / 5 页") {
		t.Errorf("reader shows %q, want 3 / 5 页", tt.Texts())
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
	waitText(t, tt, "开始阅读")
	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)

	waitText(t, tt, "4 / 5 页")
}
