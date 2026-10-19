package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/egoist/mygo/ui"
)

// offlineServer serves the gallery both live and cached, and counts which
// page-list endpoint the UI asked. A downloaded gallery must read the cache.
func offlineServer(t *testing.T) (*httptest.Server, *int, *int) {
	t.Helper()
	var mu sync.Mutex
	live, cached := 0, 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 1,
			"results": []map[string]any{galleryItem(1, "tok1", "离线画廊", "manga", 3)}})
	})
	mux.HandleFunc("/api/gallery/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, galleryDetail(1, "离线画廊", 3))
	})
	mux.HandleFunc("/api/gallery-cache/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, galleryDetail(1, "离线画廊", 3))
	})
	stream := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"type":"meta","total":3}`)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, `{"type":"page","page_url":"https://img.example/p%d.jpg","index":%d}`+"\n", i, i)
		}
		fmt.Fprintln(w, `{"type":"done","total":3}`)
	}
	mux.HandleFunc("/api/gallery/1/tok1/pages", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		live++
		mu.Unlock()
		stream(w, r)
	})
	mux.HandleFunc("/api/gallery-cache/1/tok1/pages", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		cached++
		mu.Unlock()
		stream(w, r)
	})
	mux.HandleFunc("/api/progress/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"current_page": 0})
	})
	mux.HandleFunc("/api/bookshelf/1/tok1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"in_bookshelf": false})
	})
	mux.HandleFunc("/api/prefill", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"jobs": []map[string]any{{
			"id": "job-1", "gallery_id": 1, "token": "tok1", "title": "离线画廊",
			"status": "completed", "total": 3, "failed_count": 0,
		}}})
	})
	srv := httptest.NewServer(mux)
	return srv, &live, &cached
}

func galleryDetail(id int, title string, pages int) map[string]any {
	return map[string]any{
		"id": id, "token": "tok1", "title": title, "category": "manga",
		"page_count": pages, "tags": []map[string]string{{"namespace": "female", "name": "yuri"}},
	}
}

// TestOfflineReadingUsesCache covers the download page's offline entry: a
// finished job offers 离线阅读, which opens the reader on the cached copy
// without asking the live endpoint first.
func TestOfflineReadingUsesCache(t *testing.T) {
	srv, live, cached := offlineServer(t)
	defer srv.Close()

	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "离线画廊")

	if err := tt.Click("下载管理"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "已完成")
	waitText(t, tt, "离线阅读")

	// A failed or slow upstream must not matter: the reader goes straight
	// to the cache.
	if err := tt.Click("离线阅读"); err != nil {
		t.Fatalf("click 离线阅读: %v", err)
	}
	navSettle(tt)
	waitText(t, tt, "1 / 3 页")
	waitText(t, tt, "离线")

	if *live != 0 {
		t.Errorf("offline reading asked the live page list %d times, want 0", *live)
	}
	if *cached == 0 {
		t.Error("offline reading never asked the cached page list")
	}
	if !a.reader.offline {
		t.Error("reader did not record offline mode")
	}
}

// TestOnlineReaderStillPrefersLive checks the other side: opening the reader
// from the gallery page asks upstream first, as before.
func TestOnlineReaderStillPrefersLive(t *testing.T) {
	srv, live, _ := offlineServer(t)
	defer srv.Close()

	New2 := New(NewClient(srv.URL))
	tt := ui.NewTester(New2.View, 1280, 860)
	waitText(t, tt, "离线画廊")
	if err := tt.Click("离线画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "1 / 3 页")

	if *live == 0 {
		t.Error("the online reader never asked the live page list")
	}
	if New2.reader.offline {
		t.Error("the online reader recorded offline mode")
	}
}
