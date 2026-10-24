package app

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

// slotServer answers a gallery of pageCount pages whose page list carries only
// listed of them (upstream listing fewer pages than it announced is what a
// short stream stands for), and records the progress the reader saves.
type slotServer struct {
	mu       sync.Mutex
	writes   []map[string]any
	progress map[string]any
	// pages is how many times the page list was asked for, and detail how
	// many times the gallery's live metadata was.
	pages, detail int
}

// newSlotServer starts the fake backend. The page list waits on release after
// the first hold pages when it is not nil, so a test can look at the reader
// while the rest is still being scraped.
func newSlotServer(t *testing.T, pageCount, listed int, hold int, release <-chan struct{}, progress map[string]any) (*slotServer, string) {
	t.Helper()
	s := &slotServer{progress: progress}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 1,
			"results": []map[string]any{galleryItem(1, "tok1", "分页画廊", "manga", pageCount)}})
	})
	mux.HandleFunc("/api/gallery/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.detail++
		s.mu.Unlock()
		writeJSON(w, map[string]any{"id": 1, "token": "tok1", "title": "分页画廊",
			"category": "manga", "page_count": pageCount,
			"tags": []map[string]string{{"namespace": "female", "name": "yuri"}}})
	})
	mux.HandleFunc("/api/gallery/1/tok1/pages", func(w http.ResponseWriter, r *http.Request) {
		s.mu.Lock()
		s.pages++
		s.mu.Unlock()
		w.Header().Set("Content-Type", "application/x-ndjson")
		fl, _ := w.(http.Flusher)
		fmt.Fprintf(w, `{"type":"meta","total":%d}`+"\n", pageCount)
		for i := 0; i < listed; i++ {
			fmt.Fprintf(w, `{"type":"page","page_url":"https://img.example/p%d.jpg","index":%d}`+"\n", i, i)
			if fl != nil {
				fl.Flush()
			}
			if release != nil && i == hold {
				<-release
			}
		}
		fmt.Fprintf(w, `{"type":"done","total":%d}`+"\n", listed)
	})
	mux.HandleFunc("/api/progress/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		// Returns the progress the fake backend keeps, which a test can leave
		// deliberately stale.
		if r.Method == http.MethodGet {
			s.mu.Lock()
			p := s.progress
			s.mu.Unlock()
			if p == nil {
				p = map[string]any{"current_page": 0}
			}
			writeJSON(w, p)
			return
		}
		if r.Method == http.MethodPut {
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			s.mu.Lock()
			s.writes = append(s.writes, body)
			s.mu.Unlock()
			writeJSON(w, map[string]any{"gallery_id": 1, "token": "tok1",
				"current_page": body["current_page"]})
			return
		}
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
	})
	mux.HandleFunc("/api/bookshelf/1/tok1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"in_bookshelf": false})
	})
	return s, apiURL(t, mux)
}

// savedWrites is the current_page of every progress write so far.
func (s *slotServer) savedWrites() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]int, 0, len(s.writes))
	for _, w := range s.writes {
		if n, ok := w["current_page"].(float64); ok {
			out = append(out, int(n))
		}
	}
	return out
}

// openReader walks the app from the home grid to the reader of gallery 1.
func openReader(t *testing.T, a *App, tt *ui.Tester, label string) {
	t.Helper()
	waitText(t, tt, "分页画廊")
	if err := tt.Click("分页画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, label)
	if err := tt.Click(label); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
}

// TestReaderSlotsCoverTheGalleryCount covers the reader's page list being laid
// out for the gallery's whole count from the first frame, as main's reader
// pre-allocates one stable slot per page: a slow stream must not grow the list
// page by page, and the pages that did arrive must be readable meanwhile.
func TestReaderSlotsCoverTheGalleryCount(t *testing.T) {
	const total = 40
	release := make(chan struct{})
	_, url := newSlotServer(t, total, total, 4, release, nil)

	a := New(NewClient(url))
	tt := ui.NewTester(a.View, 1280, 860)
	openReader(t, a, tt, "开始阅读")

	// Five pages have arrived and the rest is still upstream.
	waitText(t, tt, fmt.Sprintf("已加载 5 / %d 页…", total))
	a.mu.Lock()
	slots, page, pageURL := len(a.reader.pages), a.reader.page, a.reader.pages[0]
	a.mu.Unlock()
	close(release)
	if slots != total {
		t.Fatalf("reader laid out %d slots, want %d from the gallery count", slots, total)
	}
	if page != 0 || pageURL == "" {
		t.Fatalf("reader opened on page %d with page 1 %q, want page 1 filled", page, pageURL)
	}
	// Turning works while the list is incomplete, and it saves the page. The
	// reader opens right to left, so the left key goes on.
	tt.Key(0, ui.KeyLeft)
	tt.Frame()
	a.mu.Lock()
	page = a.reader.page
	a.mu.Unlock()
	if page != 1 {
		t.Fatalf("page after one turn = %d, want 1", page)
	}
	// The end of the stream settles the list: every slot is filled now.
	waitNoText(t, tt, "已加载")
	waitText(t, tt, fmt.Sprintf("第 2 / %d 页", total))
}

// TestReaderShortListEndsAtTheLastPage covers a gallery whose upstream list is
// shorter than the count it announced: the reader's empty slots are cut back
// once the stream ends, so the toolbar and the progress bar count the pages
// that are here, and finishing the last of them completes the gallery.
func TestReaderShortListEndsAtTheLastPage(t *testing.T) {
	srv, url := newSlotServer(t, 10, 6, 0, nil, nil)
	a := New(NewClient(url))
	tt := ui.NewTester(a.View, 1280, 860)
	openReader(t, a, tt, "开始阅读")

	waitNoText(t, tt, "已加载")
	waitText(t, tt, "第 1 / 6 页")
	a.mu.Lock()
	slots := len(a.reader.pages)
	a.mu.Unlock()
	if slots != 6 {
		t.Fatalf("reader kept %d slots after a short stream, want 6", slots)
	}

	// Jump to the last page from the page list: the write completes the
	// gallery.
	showReaderPanel(t, a, tt, panelPages)
	a.update(func() { a.reader.jump = "6" })
	tt.Frame()
	if err := tt.Click("跳转"); err != nil {
		t.Fatal(err)
	}
	waitFor(t, tt, "the last page saved", func() bool {
		for _, p := range srv.savedWrites() {
			if p == 5 {
				return true
			}
		}
		return false
	})
	srv.mu.Lock()
	last := srv.writes[len(srv.writes)-1]
	srv.mu.Unlock()
	if last["completed"] != true {
		t.Errorf("last page write completed = %v, want true", last["completed"])
	}
	if p, _ := last["progress"].(float64); p != 1 {
		t.Errorf("last page write progress = %v, want 1", last["progress"])
	}
}

// TestReaderReopensWhereItStopped covers reading a gallery, leaving to the
// lists, and opening it again: the reader must open where it stopped, not at
// the page the backend's list row still reports. Saves land in the backend
// asynchronously, so a list or progress row read right after a visit can carry
// the page from before it.
func TestReaderReopensWhereItStopped(t *testing.T) {
	// The progress endpoint keeps saying page 0: the reader must open where it
	// stopped from its own save, not from the backend's row.
	srv, url := newSlotServer(t, 10, 10, 0, nil, map[string]any{"current_page": 0})
	a := New(NewClient(url))
	tt := ui.NewTester(a.View, 1280, 860)
	openReader(t, a, tt, "开始阅读")
	waitText(t, tt, "第 1 / 10 页")

	for i := 0; i < 3; i++ {
		tt.Key(0, ui.KeyLeft)
	}
	tt.Frame()
	waitText(t, tt, "第 4 / 10 页")
	waitFor(t, tt, "the page written", func() bool {
		for _, p := range srv.savedWrites() {
			if p == 3 {
				return true
			}
		}
		return false
	})

	// Back to the gallery, then the lists, then the gallery again: its button
	// offers the page just read.
	// The page is what the app remembers, whatever the backend's row says.
	a.update(func() { a.router.Push("/") })
	tt.Frame()
	// The list was loaded before the visit: it keeps its items and refreshes
	// in the background, and the refresh must not drop the page just read.
	waitText(t, tt, "最新画廊")
	// Force the refresh the stale timer would run (the test does not wait
	// minutes), and wait for the new first page to land.
	a.update(func() { a.refreshList(a.lists["/"]) })
	waitFor(t, tt, "the refresh to land", func() bool {
		a.mu.Lock()
		defer a.mu.Unlock()
		s := a.lists["/"]
		return s.loaded && !s.refreshing && !s.loading && len(s.items) > 0
	})
	if g := a.lists["/"].items[0]; g.Reading == nil || g.Reading.CurrentPage != 3 {
		t.Fatalf("list card shows %+v, want page 3 remembered", g.Reading)
	}
	if err := tt.Click("分页画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	// The detail page is already known, so it shows at once, and its reading
	// progress is asked for again in the background: the button offers the
	// page just read from this app's own record even before that lands.
	// (A plain "Start reading" here would be the very bug this covers.)
	waitText(t, tt, "继续阅读（第 4 页）")
	if err := tt.Click("继续阅读（第 4 页）"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "4 / 10 页")
	a.mu.Lock()
	page := a.reader.page
	a.mu.Unlock()
	if page != 3 {
		t.Fatalf("reader reopened on page %d, want 3", page)
	}
	// Its own save, not the backend row: the reader shows it even though the
	// progress endpoint still reports 0.
	srv.mu.Lock()
	reported := srv.progress
	srv.mu.Unlock()
	if reported["current_page"] != 0 {
		t.Fatalf("fake backend progress = %v, want the stale 0 this test is about", reported)
	}
}
