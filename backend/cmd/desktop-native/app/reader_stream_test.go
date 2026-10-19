package app

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"

	"manga-reader/internal/config"
)

// TestPagesSendsAuthToken covers the page list carrying the embedded
// server's per-run token: it is hand-built rather than sent through
// Client.do, so it used to leave the header off and the reader was answered
// 401 forever.
func TestPagesSendsAuthToken(t *testing.T) {
	var mu sync.Mutex
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Get(config.AuthHeader)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"type":"meta","total":1}`)
		fmt.Fprintln(w, `{"type":"page","page_url":"https://img.example/p0.jpg","index":0}`)
		fmt.Fprintln(w, `{"type":"done","total":1}`)
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetAuthToken("sekret")
	if _, _, err := c.Pages(context.Background(), 1, "tok"); err != nil {
		t.Fatalf("Pages: %v", err)
	}
	mu.Lock()
	seen := got
	mu.Unlock()
	if seen != "sekret" {
		t.Fatalf("page list token header = %q, want sekret", seen)
	}
}

// TestPagesStreamingHandsPagesOverImmediately covers a large gallery: the
// backend scrapes upstream for tens of seconds, so the reader must show the
// pages that arrived instead of waiting for the last one.
func TestPagesStreamingHandsPagesOverImmediately(t *testing.T) {
	const total = 40
	release := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fl, _ := w.(http.Flusher)
		fmt.Fprintf(w, `{"type":"meta","total":%d}`+"\n", total)
		if fl != nil {
			fl.Flush()
		}
		for i := 0; i < total; i++ {
			fmt.Fprintf(w, `{"type":"page","page_url":"https://img.example/p%d.jpg","index":%d}`+"\n", i, i)
			if fl != nil {
				fl.Flush()
			}
			if i == 2 {
				// The rest stays upstream for a while, as a slow scrape does.
				<-release
			}
		}
		fmt.Fprintf(w, `{"type":"done","total":%d}`+"\n", total)
	}))
	defer srv.Close()
	defer close(release)

	c := NewClient(srv.URL)
	first := make(chan int, 4)
	go func() {
		_, _, _ = c.PagesStreaming(context.Background(), 1, "tok", func(index int, _ string, _ int) {
			select {
			case first <- index:
			default:
			}
		})
	}()

	select {
	case i := <-first:
		if i != 0 {
			t.Fatalf("first streamed page = %d, want 0", i)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no page streamed before the list finished")
	}
}

// TestReaderShowsStreamedPages covers the reader with a slow page list: the
// spinner goes away as soon as the first pages arrive, and the toolbar says
// how much has come in.
func TestReaderShowsStreamedPages(t *testing.T) {
	const total = 30
	release := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 1,
			"results": []map[string]any{galleryItem(1, "tok1", "慢画廊", "manga", total)}})
	})
	mux.HandleFunc("/api/gallery/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"id": 1, "token": "tok1", "title": "慢画廊",
			"category": "manga", "page_count": total,
			"tags": []map[string]string{{"namespace": "female", "name": "yuri"}}})
	})
	mux.HandleFunc("/api/gallery/1/tok1/pages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fl, _ := w.(http.Flusher)
		fmt.Fprintf(w, `{"type":"meta","total":%d}`+"\n", total)
		if fl != nil {
			fl.Flush()
		}
		for i := 0; i < total; i++ {
			fmt.Fprintf(w, `{"type":"page","page_url":"https://img.example/p%d.jpg","index":%d}`+"\n", i, i)
			if fl != nil {
				fl.Flush()
			}
			if i == 2 {
				<-release
			}
		}
		fmt.Fprintf(w, `{"type":"done","total":%d}`+"\n", total)
	})
	mux.HandleFunc("/api/progress/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"current_page": 0})
	})
	mux.HandleFunc("/api/bookshelf/1/tok1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"in_bookshelf": false})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()
	defer close(release)

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

	// The backend has sent three pages and is scraping the rest: the reader
	// must already be readable, saying how many arrived.
	waitNoText(t, tt, "正在获取页面列表…")
	waitText(t, tt, fmt.Sprintf("已加载 3 / %d 页…", total))
	if !a.reader.loaded || len(a.reader.pages) != 3 {
		t.Fatalf("reader loaded=%v pages=%d, want 3 streamed", a.reader.loaded, len(a.reader.pages))
	}
}

// TestReaderFailureDoesNotLoop covers a page list that fails at once: the
// reader shows the error and stays there instead of rebuilding the load on
// every frame, which blinked the spinner forever.
func TestReaderFailureDoesNotLoop(t *testing.T) {
	var mu sync.Mutex
	attempts := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 1,
			"results": []map[string]any{galleryItem(1, "tok1", "坏画廊", "manga", 5)}})
	})
	mux.HandleFunc("/api/gallery/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"id": 1, "token": "tok1", "title": "坏画廊",
			"category": "manga", "page_count": 5,
			"tags": []map[string]string{{"namespace": "female", "name": "yuri"}}})
	})
	fail := func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		attempts++
		mu.Unlock()
		http.Error(w, "upstream exploded", http.StatusInternalServerError)
	}
	mux.HandleFunc("/api/gallery/1/tok1/pages", fail)
	mux.HandleFunc("/api/gallery-cache/1/tok1/pages", fail)
	mux.HandleFunc("/api/progress/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"current_page": 0})
	})
	mux.HandleFunc("/api/bookshelf/1/tok1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"in_bookshelf": false})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "坏画廊")
	if err := tt.Click("坏画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)

	waitText(t, tt, "请求失败")
	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		tt.Frame()
	}
	mu.Lock()
	n := attempts
	mu.Unlock()
	// One request for the live endpoint and one for the cache: a loop would
	// ask again on every frame.
	if n > 4 {
		t.Errorf("page list requested %d times after failing: the reader loops", n)
	}
	if !strings.Contains(a.reader.err, "500") {
		t.Errorf("reader err = %q, want the backend's status", a.reader.err)
	}
}
