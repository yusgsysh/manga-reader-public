package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/egoist/mygo/ui"
)

// manyGalleries serves a list long enough to scroll.
func manyGalleries(t *testing.T, n int) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()
	all := make([]map[string]any, n)
	for i := range all {
		all[i] = galleryItem(i+1, fmt.Sprintf("tok%d", i+1), fmt.Sprintf("画廊 %02d", i+1), "manga", 5)
	}
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": n, "results": all})
	})
	mux.HandleFunc("/api/gallery/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, galleryDetail(1, "画廊 01", 5))
	})
	mux.HandleFunc("/api/gallery/1/tok1/pages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"type":"meta","total":5}`)
		for i := 0; i < 5; i++ {
			fmt.Fprintf(w, `{"type":"page","page_url":"https://img.example/p%d.jpg","index":%d}`+"\n", i, i)
		}
		fmt.Fprintln(w, `{"type":"done","total":5}`)
	})
	mux.HandleFunc("/api/progress/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"current_page": 0})
	})
	mux.HandleFunc("/api/bookshelf/1/tok1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"in_bookshelf": false})
	})
	return httptest.NewServer(mux)
}

// TestListKeepsItemsWhenReturning covers back-navigation: the list state is
// kept per path, so a gallery opened from it and closed again does not reset
// the loaded pages or the grid's scroll state.
func TestListKeepsItemsWhenReturning(t *testing.T) {
	srv := manyGalleries(t, 60)
	defer srv.Close()

	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "画廊 01")

	home := a.lists["/"]
	if home == nil || len(home.items) == 0 {
		t.Fatal("home list did not load")
	}
	grid, items := home.grid, len(home.items)

	if err := tt.Click("画廊 01"); err != nil {
		t.Fatalf("click gallery: %v", err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")

	if err := tt.Click("← 返回"); err != nil {
		if err2 := tt.Click("首页"); err2 != nil {
			t.Fatalf("no way back: %v / %v", err, err2)
		}
	}
	navSettle(tt)

	back := a.lists["/"]
	if back == nil {
		t.Fatal("home list state was dropped on return")
	}
	if back.grid != grid {
		t.Error("returning built a new grid state, so the scroll position is lost")
	}
	if len(back.items) != items {
		t.Errorf("returning kept %d items, want %d: the list reloaded", len(back.items), items)
	}
}
