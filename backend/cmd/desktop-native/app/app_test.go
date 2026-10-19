package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// fakeImagePNG is a valid 300x400 image for the fake image proxy.
var fakeImagePNG = func() []byte {
	img := image.NewRGBA(image.Rect(0, 0, 300, 400))
	for y := 0; y < 400; y++ {
		for x := 0; x < 300; x++ {
			img.SetRGBA(x, y, color.RGBA{uint8(30 + x/2), uint8(50 + y/3), 140, 255})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, img)
	return buf.Bytes()
}()

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

func galleryItem(id int, token, title, category string, pages int) map[string]any {
	return map[string]any{
		"id":        id,
		"token":     token,
		"title":     title,
		"category":  category,
		"thumbnail": fmt.Sprintf("https://img.example/%d.jpg", id),
		"pages":     pages,
		"rating":    4.5,
		"tags":      []string{"female:yuri"},
	}
}

func newFakeServer(t *testing.T) *httptest.Server {
	t.Helper()
	mux := http.NewServeMux()

	all := []map[string]any{
		galleryItem(1, "tok1", "示例 A", "doujinshi", 3),
		galleryItem(2, "tok2", "示例 B", "manga", 4),
	}
	list := func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"page": 0, "page_size": 25, "total": len(all),
			"results": all,
		})
	}
	mux.HandleFunc("/api/galleries", list)
	mux.HandleFunc("/api/watched", list)
	mux.HandleFunc("/api/popular", list)
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		q := strings.ToLower(r.URL.Query().Get("q"))
		var out []map[string]any
		for _, g := range all {
			if q == "" || strings.Contains(strings.ToLower(g["title"].(string)), q) {
				out = append(out, g)
			}
		}
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": len(out), "results": out})
	})

	detail := map[string]any{
		"id": 1, "token": "tok1", "title": "示例 A", "title_jpn": "サンプル A",
		"category": "doujinshi", "thumbnail": "https://img.example/1.jpg",
		"page_count": 3, "rating": 4.5, "rating_count": 10, "uploader": "tester",
		"posted_at": "2026-01-02", "file_size": "1 MB",
		"tags": []map[string]string{{"namespace": "female", "name": "yuri"}},
	}
	mux.HandleFunc("/api/gallery/1/tok1", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, detail) })
	mux.HandleFunc("/api/gallery-cache/1/tok1", func(w http.ResponseWriter, r *http.Request) { writeJSON(w, detail) })
	mux.HandleFunc("/api/gallery/1/tok1/pages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		flusher, _ := w.(http.Flusher)
		fmt.Fprintln(w, `{"type":"meta","total":3}`)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, `{"type":"page","index":%d,"page_url":"https://img.example/page-%d.jpg"}`+"\n", i, i)
			if flusher != nil {
				flusher.Flush()
			}
		}
		fmt.Fprintln(w, `{"type":"done","total":3}`)
	})

	shelf := []map[string]any{{
		"id": 1, "token": "tok1", "title": "示例 A", "category": "doujinshi",
		"thumbnail": "https://img.example/1.jpg", "pages": 3,
		"reading": map[string]any{"current_page": 1, "progress": 0.5, "completed": false},
	}}
	mux.HandleFunc("/api/bookshelf", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 1, "results": shelf})
	})
	mux.HandleFunc("/api/recently-read", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 1, "results": shelf})
	})
	mux.HandleFunc("/api/bookshelf/1/tok1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"in_bookshelf": false})
	})
	mux.HandleFunc("/api/bookshelf/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			t.Errorf("bookshelf method = %s", r.Method)
		}
		w.WriteHeader(http.StatusOK)
	})

	mux.HandleFunc("/api/progress/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var body struct {
				CurrentPage int `json:"current_page"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			w.WriteHeader(http.StatusOK)
			return
		}
		writeJSON(w, map[string]any{"current_page": 0, "progress": 0, "completed": false})
	})

	image := func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("url") == "" {
			http.Error(w, `{"error":"missing url"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakeImagePNG)
	}
	mux.HandleFunc("/api/image/thumbnail", image)
	mux.HandleFunc("/api/image/page", image)

	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	return srv
}

func newTestApp(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
	srv := newFakeServer(t)
	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	return a, tt
}

// navSettle advances past the router's page transition. A page sliding in is
// offset until it ends, so a click on its controls would miss.
func navSettle(tt *ui.Tester) {
	tt.Advance(400 * time.Millisecond)
}

// waitText renders frames until s shows, failing the test on timeout. Loads
// run in goroutines; each frame picks up whatever has arrived.
func waitText(t *testing.T, tt *ui.Tester, s string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if tt.HasText(s) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q; texts=%q", s, tt.Texts())
}

func waitNoText(t *testing.T, tt *ui.Tester, s string) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if !tt.HasText(s) {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %q to disappear; texts=%q", s, tt.Texts())
}

func TestHomeLoadsAPI(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if !tt.HasText("共 2 个结果") {
		t.Errorf("home count wrong; texts=%q", tt.Texts())
	}
	if got := a.router.Path(); got != "/" {
		t.Errorf("path = %q, want /", got)
	}
}

func TestSidebarNavigation(t *testing.T) {
	a, tt := newTestApp(t)
	if err := tt.Click("书架"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	if got := a.router.Path(); got != "/bookshelf" {
		t.Fatalf("path after clicking 书架 = %q, want /bookshelf", got)
	}
	waitText(t, tt, "收藏的画廊")
	waitText(t, tt, "共 1 个结果")
	if !tt.HasText("读到第 2 页") {
		t.Errorf("bookshelf does not show reading progress; texts=%q", tt.Texts())
	}
}

func TestGalleryToReader(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	if got := a.router.Path(); got != "/gallery/1/tok1" {
		t.Fatalf("path after gallery click = %q", got)
	}
	waitText(t, tt, "开始阅读")
	if !tt.HasText("female:yuri") {
		t.Errorf("gallery tags missing; texts=%q", tt.Texts())
	}

	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	if got := a.router.Path(); got != "/reader/1/tok1" {
		t.Fatalf("path after 开始阅读 = %q", got)
	}
	waitText(t, tt, "1 / 3 页")
	if err := tt.Click("下一页"); err != nil {
		t.Fatal(err)
	}
	waitText(t, tt, "2 / 3 页")
	tt.Key(0, ui.KeyRight)
	waitText(t, tt, "3 / 3 页")
}

func TestSearchFilters(t *testing.T) {
	a, tt := newTestApp(t)
	if err := tt.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "搜索框")
	tt.Type("B")
	waitText(t, tt, "共 1 个结果")
	if !tt.HasText("示例 B") {
		t.Errorf("filtered list does not show 示例 B; texts=%q", tt.Texts())
	}
	waitNoText(t, tt, "示例 A")
	if a.searchQuery != "B" {
		t.Errorf("search query = %q, want B", a.searchQuery)
	}
}

func TestBackFromReader(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "1 / 3 页")
	if err := tt.Click("← 返回"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	if got := a.router.Path(); got != "/gallery/1/tok1" {
		t.Fatalf("path after reader back = %q, want /gallery/1/tok1", got)
	}
}
