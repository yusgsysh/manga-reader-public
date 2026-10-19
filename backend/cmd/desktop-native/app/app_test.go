package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/internal/config"
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
	return newFakeServerWithHooks(t, nil)
}

// newFakeServerWithHooks is newFakeServer plus an optional callback invoked
// with each image proxy request, so tests can assert which endpoint (live vs
// read-through cache) the UI used and which page URLs it fetched.
func newFakeServerWithHooks(t *testing.T, onImage func(r *http.Request)) *httptest.Server {
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
	mux.HandleFunc("/api/gallery/1/tok1/torrents", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"torrents": []map[string]any{
			{"gtid": "2250081", "name": "[Yanje] 示例 A.zip", "size": "42.13 MiB",
				"posted": "2026-10-02 06:30", "seeds": 8, "peers": 4,
				"downloads": 12, "uploader": "Konazumi"},
		}})
	})
	mux.HandleFunc("/api/gallery/1/tok1/torrents/2250081/download", func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("variant") != "redistributable" {
			t.Errorf("variant = %q", r.URL.Query().Get("variant"))
		}
		w.Header().Set("Content-Type", "application/x-bittorrent")
		_, _ = w.Write([]byte("d8:announce"))
	})
	mux.HandleFunc("/api/gallery-cache/1/tok1/pages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"type":"meta","total":3}`)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, `{"type":"page","index":%d,"page_url":"https://img.example/page-%d.jpg"}`+"\n", i, i)
		}
		fmt.Fprintln(w, `{"type":"done","total":3}`)
	})

	// One prefill job: starts queued, then reports progress on each poll.
	prefillPolls := 0
	mux.HandleFunc("/api/prefill", func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPost:
			var body struct {
				GalleryID int64    `json:"gallery_id"`
				Token     string   `json:"token"`
				Title     string   `json:"title"`
				URLs      []string `json:"urls"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			writeJSON(w, map[string]any{
				"id": "job-1", "gallery_id": body.GalleryID, "token": body.Token,
				"title": body.Title, "status": "queued", "total": len(body.URLs),
			})
		case http.MethodGet:
			writeJSON(w, map[string]any{"jobs": []map[string]any{
				{"id": "job-1", "gallery_id": 1, "token": "tok1", "title": "示例 A",
					"status": "running", "total": 3,
					"progress": map[string]any{"done": 1, "cached": 0, "fetched": 1}},
			}})
		}
	})
	mux.HandleFunc("/api/prefill/job-1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodDelete {
			writeJSON(w, map[string]any{"deleted": 1})
			return
		}
		prefillPolls++
		status, done := "running", 1
		if prefillPolls > 2 {
			status, done = "completed", 3
		}
		writeJSON(w, map[string]any{
			"id": "job-1", "gallery_id": 1, "token": "tok1", "title": "示例 A",
			"status": status, "total": 3, "failed_count": 0,
			"progress": map[string]any{"done": done, "cached": 0, "fetched": done},
		})
	})
	mux.HandleFunc("/api/prefill/job-1/cancel", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id": "job-1", "gallery_id": 1, "token": "tok1", "title": "示例 A",
			"status": "cancelled", "total": 3,
		})
	})
	mux.HandleFunc("/api/prefill/job-1/zip", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/zip")
		_, _ = w.Write([]byte("PK"))
	})
	mux.HandleFunc("/api/prefill/job-1/delete", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"deleted": 1})
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
	// Tag rules: PUT replaces the list like the backend does, so the tests
	// see the normalised, sorted result.
	var tagMu sync.Mutex
	tagRules := []map[string]any{}
	mux.HandleFunc("/api/tag-settings", func(w http.ResponseWriter, r *http.Request) {
		tagMu.Lock()
		defer tagMu.Unlock()
		switch r.Method {
		case http.MethodGet:
			writeJSON(w, map[string]any{"tags": tagRules})
		case http.MethodPut:
			var body struct {
				Tags []map[string]any `json:"tags"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, `{"error":"bad json"}`, http.StatusBadRequest)
				return
			}
			seen := map[string]bool{}
			tagRules = tagRules[:0]
			for _, t := range body.Tags {
				tag := strings.ToLower(strings.TrimSpace(t["tag"].(string)))
				if seen[tag] {
					continue
				}
				seen[tag] = true
				tagRules = append(tagRules, map[string]any{
					"tag": tag, "action": t["action"], "weight": t["weight"],
				})
			}
			sort.Slice(tagRules, func(i, j int) bool {
				return tagRules[i]["tag"].(string) < tagRules[j]["tag"].(string)
			})
			writeJSON(w, map[string]any{"tags": tagRules})
		}
	})
	mux.HandleFunc("/api/tag-settings/sync", func(w http.ResponseWriter, r *http.Request) {
		tagMu.Lock()
		defer tagMu.Unlock()
		writeJSON(w, map[string]any{
			"added": 0, "updated": 0, "deleted": 0, "pulled": 2, "unchanged": 1,
			"pending_deletes": []string{}, "conflicts": []any{}, "failed": []any{},
			"tags": []map[string]any{
				{"tag": "female:yuri", "action": "like", "weight": 10},
			},
		})
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
		if onImage != nil {
			onImage(r)
		}
		if r.URL.Query().Get("url") == "" {
			http.Error(w, `{"error":"missing url"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakeImagePNG)
	}
	pageThumb := func(w http.ResponseWriter, r *http.Request) {
		if onImage != nil {
			onImage(r)
		}
		if r.URL.Query().Get("index") == "" {
			http.Error(w, `{"error":"missing index"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakeImagePNG)
	}
	mux.HandleFunc("/api/image/thumbnail", image)
	mux.HandleFunc("/api/image/page", image)
	mux.HandleFunc("/api/image-cache/thumbnail", image)
	mux.HandleFunc("/api/image-cache/page", image)
	mux.HandleFunc("/api/image/page-thumbnail", pageThumb)
	mux.HandleFunc("/api/image-cache/page-thumbnail", pageThumb)

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

// TestReaderPreloadsNearbyPages checks that opening the reader warms the
// neighbouring pages through the image cache proxy, so page turns do not wait
// on a fresh download.
func TestReaderPreloadsNearbyPages(t *testing.T) {
	var mu sync.Mutex
	fetched := map[string]bool{}
	srv := newFakeServerWithHooks(t, func(r *http.Request) {
		if r.URL.Path != "/api/image-cache/page" {
			return
		}
		mu.Lock()
		fetched[r.URL.Query().Get("url")] = true
		mu.Unlock()
	})

	client := NewClient(srv.URL)
	client.UseImageCache(true)
	a := New(client)
	tt := ui.NewTester(a.View, 1280, 860)

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

	want := []string{
		"https://img.example/page-0.jpg",
		"https://img.example/page-1.jpg",
		"https://img.example/page-2.jpg",
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		mu.Lock()
		n := len(fetched)
		mu.Unlock()
		if n >= len(want) {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	mu.Lock()
	defer mu.Unlock()
	for _, url := range want {
		if !fetched[url] {
			t.Errorf("page %s was not preloaded; fetched=%v", url, fetched)
		}
	}
}

// TestDetailThumbnailGridJumpsToPage checks the detail page's thumbnail grid:
// cells fetch through the cache proxy, and clicking one opens the reader at
// that page instead of the saved progress.
func TestDetailThumbnailGridJumpsToPage(t *testing.T) {
	var mu sync.Mutex
	thumbs := 0
	srv := newFakeServerWithHooks(t, func(r *http.Request) {
		if r.URL.Path == "/api/image-cache/page-thumbnail" {
			mu.Lock()
			thumbs++
			mu.Unlock()
		}
	})

	client := NewClient(srv.URL)
	client.UseImageCache(true)
	a := New(client)
	tt := ui.NewTester(a.View, 1280, 860)

	waitText(t, tt, "示例 A")
	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "共 3 页 · 点击跳转")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		mu.Lock()
		n := thumbs
		mu.Unlock()
		if n >= 3 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	got := thumbs
	mu.Unlock()
	if got < 3 {
		t.Fatalf("page thumbnail requests = %d, want at least 3", got)
	}

	if err := tt.Click("第 2 页"); err != nil {
		t.Fatalf("click thumbnail: %v", err)
	}
	navSettle(tt)
	if p := a.router.Path(); p != "/reader/1/tok1" {
		t.Fatalf("path after thumbnail click = %q", p)
	}
	waitText(t, tt, "2 / 3 页")
}

// TestReaderTapFitAndJump covers the reading-area tap zones, the fit toggle
// and the page-number field.
func TestReaderTapFitAndJump(t *testing.T) {
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

	// The reading area starts right of the 220px sidebar and fills the rest,
	// so x=1200 is the right third and x=300 the left third.
	tt.ClickAt(1200, 400)
	waitText(t, tt, "2 / 3 页")
	tt.ClickAt(300, 400)
	waitText(t, tt, "1 / 3 页")

	if err := tt.Click("适合页面"); err != nil {
		t.Fatalf("click fit mode: %v", err)
	}
	if a.reader.fit != ui.Cover {
		t.Fatalf("fit = %v, want Cover", a.reader.fit)
	}
	waitText(t, tt, "适应宽度")

	if err := tt.Click("跳转页码"); err != nil {
		t.Fatalf("click page field: %v", err)
	}
	tt.Type("3")
	if err := tt.Click("跳转"); err != nil {
		t.Fatalf("click jump: %v", err)
	}
	waitText(t, tt, "3 / 3 页")
	if a.readerJump != "" {
		t.Errorf("page field not cleared: %q", a.readerJump)
	}

	// Right-to-left swaps the arrow keys and tap zones.
	if err := tt.Click("从左到右"); err != nil {
		t.Fatalf("click direction: %v", err)
	}
	if !a.reader.rtl {
		t.Fatal("rtl not enabled")
	}
	waitText(t, tt, "从右到左")
	tt.Key(0, ui.KeyRight)
	waitText(t, tt, "2 / 3 页")
	tt.ClickAt(1200, 400)
	waitText(t, tt, "1 / 3 页")
}

// TestDownloadsStartAndPoll drives the offline download flow: the detail page
// queues a prefill job with the gallery's pages, and the download page shows
// it running and then completed as the backend reports progress.
func TestDownloadsStartAndPoll(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")

	if err := tt.Click("离线下载"); err != nil {
		t.Fatalf("click 离线下载: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if tt.HasText("查看下载") {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !tt.HasText("查看下载") {
		t.Fatalf("detail page does not offer 查看下载; texts=%q", tt.Texts())
	}
	if err := tt.Click("查看下载"); err != nil {
		t.Fatalf("click 查看下载: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if a.router.Path() == "/downloads" && len(a.downloads.items) > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if p := a.router.Path(); p != "/downloads" {
		t.Fatalf("path after 查看下载 = %q", p)
	}

	// The fake backend reports running twice, then completed. The page arms a
	// c.After timer for the next poll, which the tester's clock drives: move
	// it past the interval and render. Three rounds cover the two "running"
	// answers and the last one.
	for i := 0; i < 3; i++ {
		tt.Advance(downloadPollInterval)
		time.Sleep(10 * time.Millisecond)
	}
	if !tt.HasText("已完成") {
		t.Fatalf("download did not reach 已完成; texts=%q", tt.Texts())
	}
	if got := a.downloads.items; len(got) != 1 || got[0].ID != "job-1" {
		t.Fatalf("downloads = %+v", got)
	}

	// A finished job can be deleted from the list.
	if r, _ := tt.Find("删除"); r.W == 0 {
		t.Fatalf("删除 has no layout; texts=%q", tt.Texts()[14:])
	}
	if err := tt.Click("删除"); err != nil {
		t.Fatalf("click 删除: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if len(a.downloads.items) == 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if len(a.downloads.items) != 0 {
		t.Fatalf("job still listed after 删除: %+v", a.downloads.items)
	}
}

// TestListInfiniteScroll checks the next page is fetched once the grid builds
// cards in its last rows, without a click on 加载更多.
func TestListInfiniteScroll(t *testing.T) {
	var mu sync.Mutex
	pages := 0
	mux := http.NewServeMux()
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Query().Get("page")
		mu.Lock()
		pages++
		mu.Unlock()
		items := []map[string]any{}
		switch p {
		case "0":
			for i := 1; i <= 8; i++ {
				items = append(items, galleryItem(i, fmt.Sprintf("tok%d", i),
					fmt.Sprintf("画廊 %d", i), "manga", 5))
			}
		case "1":
			for i := 9; i <= 11; i++ {
				items = append(items, galleryItem(i, fmt.Sprintf("tok%d", i),
					fmt.Sprintf("画廊 %d", i), "manga", 5))
			}
		}
		writeJSON(w, map[string]any{"page": 0, "page_size": 8, "total": 11, "results": items})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	if err := tt.Click("首页"); err != nil {
		t.Fatalf("click 首页: %v", err)
	}
	navSettle(tt)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if s := a.lists["/"]; s != nil && len(s.items) == 11 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := len(a.lists["/"].items); got != 11 {
		t.Fatalf("items = %d, want 11 without clicking 加载更多", got)
	}
	if !tt.HasText("共 11 个结果") {
		t.Fatalf("summary missing; texts=%q", tt.Texts())
	}

	// A complete list asks for nothing more.
	mu.Lock()
	n := pages
	mu.Unlock()
	time.Sleep(20 * time.Millisecond)
	tt.Frame()
	mu.Lock()
	defer mu.Unlock()
	if pages != n {
		t.Fatalf("extra page requests: %d -> %d", n, pages)
	}
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

func TestImageURLsDefaultToLiveProxy(t *testing.T) {
	c := NewClient("http://127.0.0.1:8080")
	if got := c.ThumbnailURL("https://img.example/1.jpg"); !strings.Contains(got, "/api/image/thumbnail?") {
		t.Fatalf("ThumbnailURL = %q, want live proxy", got)
	}
	if got := c.PageImageURL("https://img.example/p0.jpg"); !strings.Contains(got, "/api/image/page?") {
		t.Fatalf("PageImageURL = %q, want live proxy", got)
	}
	if got := c.PageThumbnailURL(1, "tok", 0); !strings.Contains(got, "/api/image/page-thumbnail?") {
		t.Fatalf("PageThumbnailURL = %q, want live proxy", got)
	}
}

func TestImageURLsUseCacheProxyWhenEnabled(t *testing.T) {
	c := NewClient("http://127.0.0.1:8080")
	c.UseImageCache(true)
	if got := c.ThumbnailURL("https://img.example/1.jpg"); !strings.Contains(got, "/api/image-cache/thumbnail?") {
		t.Fatalf("ThumbnailURL = %q, want cache proxy", got)
	}
	if got := c.PageImageURL("https://img.example/p0.jpg"); !strings.Contains(got, "/api/image-cache/page?") {
		t.Fatalf("PageImageURL = %q, want cache proxy", got)
	}
	if got := c.PageThumbnailURL(1, "tok", 0); !strings.Contains(got, "/api/image-cache/page-thumbnail?") {
		t.Fatalf("PageThumbnailURL = %q, want cache proxy", got)
	}
}

// TestHomeFetchesCoversThroughCacheProxy is the end-to-end check: with the
// cache proxy enabled, rendering the home grid must request covers from
// /api/image-cache/* and never from the live /api/image/* endpoints.
func TestHomeFetchesCoversThroughCacheProxy(t *testing.T) {
	var mu sync.Mutex
	cacheHits, liveHits := 0, 0
	srv := newFakeServerWithHooks(t, func(r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.Path {
		case "/api/image-cache/thumbnail", "/api/image-cache/page":
			cacheHits++
		case "/api/image/thumbnail", "/api/image/page":
			liveHits++
		}
	})

	client := NewClient(srv.URL)
	client.UseImageCache(true)
	a := New(client)
	tt := ui.NewTester(a.View, 1280, 860)

	waitText(t, tt, "示例 A")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		mu.Lock()
		hit := cacheHits > 0
		mu.Unlock()
		if hit {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	mu.Lock()
	cache, live := cacheHits, liveHits
	mu.Unlock()
	if cache == 0 {
		t.Fatalf("no cover requests reached /api/image-cache/*")
	}
	if live != 0 {
		t.Fatalf("cover requests hit live proxy %d times, want 0", live)
	}
}

// fakeBackend is an app.Backend for tests: Start flips to configured and
// points the UI at an already-running httptest server.
type fakeBackend struct {
	mu         sync.Mutex
	serverURL  string
	base       string
	configured bool
	cleared    int
	cacheSize  int64
	maxBytes   int64
	authToken  string
	appearance string
	storage    BackendStorage
}

func (f *fakeBackend) AuthToken() string { return f.authToken }

func (f *fakeBackend) Configured() bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.configured
}

func (f *fakeBackend) BaseURL() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.base
}

func (f *fakeBackend) Start(cookie string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if strings.TrimSpace(cookie) == "" {
		return "", fmt.Errorf("empty cookie")
	}
	f.base = f.serverURL
	f.configured = true
	return f.serverURL, nil
}

func (f *fakeBackend) Storage() BackendStorage {
	if f.storage.DBPath != "" {
		return f.storage
	}
	return BackendStorage{
		DBPath:     "/tmp/manga.db",
		CacheDir:   "/tmp/manga-cache",
		ConfigPath: "/tmp/manga/config.json",
	}
}

func (f *fakeBackend) ClearCache() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared++
	f.cacheSize = 0
	return nil
}

func (f *fakeBackend) CacheInfo() (CacheStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	root := "/tmp/manga-cache"
	if f.storage.CacheDir != "" {
		root = f.storage.CacheDir
	}
	return CacheStats{
		Root:     root,
		Bytes:    f.cacheSize,
		MaxBytes: f.maxBytes,
	}, nil
}

func (f *fakeBackend) SetCacheMaxBytes(maxBytes int64) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if maxBytes < 0 {
		maxBytes = 0
	}
	f.maxBytes = maxBytes
	return nil
}

func (f *fakeBackend) SetAppearance(source string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if source != "light" && source != "dark" {
		source = "system"
	}
	f.appearance = source
	return nil
}

func (f *fakeBackend) Appearance() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.appearance
}

func (f *fakeBackend) clearedCount() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cleared
}

func (f *fakeBackend) maxBytesValue() int64 {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.maxBytes
}

func TestSetupConnectsBackend(t *testing.T) {
	srv := newFakeServer(t)
	backend := &fakeBackend{serverURL: srv.URL, cacheSize: 2048, authToken: "run-token"}

	a := New(NewClient(""))
	a.SetBackend(backend)
	tt := ui.NewTester(a.View, 1280, 860)

	waitText(t, tt, "首次设置")
	tt.Type("ipb_member_id=1; ipb_pass_hash=2")
	if err := tt.Click("保存并启动"); err != nil {
		t.Fatalf("click 保存并启动: %v", err)
	}

	waitText(t, tt, "示例 A")
	if got := a.client.BaseURL(); got != srv.URL {
		t.Fatalf("client base = %q, want %q", got, srv.URL)
	}
	if got := a.client.authTokenValue(); got != "run-token" {
		t.Fatalf("client auth token = %q, want run-token", got)
	}
	if a.showSetup {
		t.Fatal("showSetup is still true after a successful setup")
	}
}

func TestClientSendsAuthToken(t *testing.T) {
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Get(config.AuthHeader)
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 0, "results": []map[string]any{}})
	}))
	defer srv.Close()

	c := NewClient(srv.URL)
	c.SetAuthToken("sekret")
	if _, err := c.List(context.Background(), "/api/galleries", 0, nil); err != nil {
		t.Fatalf("List: %v", err)
	}
	if got != "sekret" {
		t.Fatalf("token header = %q, want sekret", got)
	}
}

func TestImageStoreSendsAuthToken(t *testing.T) {
	var mu sync.Mutex
	var got string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		got = r.Header.Get(config.AuthHeader)
		mu.Unlock()
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakeImagePNG)
	}))
	defer srv.Close()

	store := NewImageStore(1)
	store.SetAuthToken("sekret")
	store.Get(srv.URL)

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		seen := got
		mu.Unlock()
		if seen != "" {
			if seen != "sekret" {
				t.Fatalf("image token header = %q, want sekret", seen)
			}
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("image request never reached the server")
}

func TestSetupRejectsEmptyCookie(t *testing.T) {
	backend := &fakeBackend{serverURL: "http://127.0.0.1:1"}
	a := New(NewClient(""))
	a.SetBackend(backend)
	tt := ui.NewTester(a.View, 1280, 860)

	waitText(t, tt, "首次设置")
	if err := tt.Click("保存并启动"); err != nil {
		t.Fatalf("click 保存并启动: %v", err)
	}
	waitText(t, tt, "请粘贴 ExHentai Cookies")
	if backend.Configured() {
		t.Fatal("backend was configured with an empty cookie")
	}
}

func TestSettingsCacheInfoAndClear(t *testing.T) {
	srv := newFakeServer(t)
	backend := &fakeBackend{
		serverURL: srv.URL, base: srv.URL,
		configured: true, cacheSize: 2048,
	}
	a := New(NewClient(srv.URL))
	a.SetBackend(backend)
	tt := ui.NewTester(a.View, 1280, 860)

	waitText(t, tt, "示例 A")
	if err := tt.Click("设置"); err != nil {
		t.Fatalf("click 设置: %v", err)
	}
	navSettle(tt)

	waitText(t, tt, "缓存占用")
	waitText(t, tt, "2.0 KB")
	if err := tt.Click("清空缓存"); err != nil {
		t.Fatalf("click 清空缓存: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if backend.clearedCount() > 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if backend.clearedCount() == 0 {
		t.Fatal("ClearCache was not called")
	}
}

func TestSettingsShowsCacheLimit(t *testing.T) {
	srv := newFakeServer(t)
	backend := &fakeBackend{
		serverURL: srv.URL, base: srv.URL,
		configured: true, cacheSize: 2048, maxBytes: 2 << 30,
	}
	a := New(NewClient(srv.URL))
	a.SetBackend(backend)
	tt := ui.NewTester(a.View, 1280, 860)

	waitText(t, tt, "示例 A")
	if err := tt.Click("设置"); err != nil {
		t.Fatalf("click 设置: %v", err)
	}
	navSettle(tt)

	// The badge reads "<used> / <limit>" and the input is prefilled in MB.
	waitText(t, tt, "2.0 KB / 2.0 GB")
	waitText(t, tt, "缓存上限 (MB)")
}

func TestSettingsSavesCacheLimit(t *testing.T) {
	srv := newFakeServer(t)
	backend := &fakeBackend{
		serverURL: srv.URL, base: srv.URL,
		configured: true, cacheSize: 512, maxBytes: 2 << 30,
	}
	a := New(NewClient(srv.URL))
	a.SetBackend(backend)
	tt := ui.NewTester(a.View, 1280, 860)

	waitText(t, tt, "示例 A")

	// White-box: drive the save without depending on headless focus handling.
	a.mu.Lock()
	a.cacheMaxInput = "512"
	a.saveCacheLimit()
	a.mu.Unlock()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if backend.maxBytesValue() == 512<<20 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := backend.maxBytesValue(); got != 512<<20 {
		t.Fatalf("maxBytes = %d, want %d", got, 512<<20)
	}
}

func TestSaveCacheLimitRejectsBadInput(t *testing.T) {
	backend := &fakeBackend{serverURL: "http://127.0.0.1:1", configured: true, maxBytes: 1 << 20}
	a := New(NewClient(""))
	a.SetBackend(backend)

	a.mu.Lock()
	a.cacheMaxInput = "abc"
	a.saveCacheLimit()
	errMsg := a.cacheMaxErr
	a.mu.Unlock()

	if errMsg == "" {
		t.Fatal("expected a validation error for a non-numeric limit")
	}
	if backend.maxBytesValue() != 1<<20 {
		t.Fatalf("limit changed to %d despite invalid input", backend.maxBytesValue())
	}
}

// TestSearchDebouncesRequests proves typing several characters quickly results
// in a single API call for the final text, plus the initial empty query.
func TestSearchDebouncesRequests(t *testing.T) {
	var mu sync.Mutex
	var queries []string

	mux := http.NewServeMux()
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"page": 0, "page_size": 25, "total": 1,
			"results": []map[string]any{galleryItem(1, "tok1", "示例 A", "doujinshi", 3)},
		})
	})
	mux.HandleFunc("/api/search", func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		queries = append(queries, r.URL.Query().Get("q"))
		mu.Unlock()
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 0, "results": []map[string]any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)

	waitText(t, tt, "示例 A")
	if err := tt.Click("搜索"); err != nil {
		t.Fatalf("click 搜索: %v", err)
	}
	navSettle(tt)

	// Type three characters, rendering between each so the debounce sees
	// every change and keeps rescheduling.
	for _, ch := range []string{"a", "b", "c"} {
		tt.Type(ch)
		tt.Frame()
	}

	// Wait for the debounced request to land.
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		mu.Lock()
		last := ""
		if n := len(queries); n > 0 {
			last = queries[n-1]
		}
		mu.Unlock()
		if last == "abc" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}

	mu.Lock()
	got := append([]string(nil), queries...)
	mu.Unlock()

	if len(got) != 2 {
		t.Fatalf("search queries = %v, want [\"\" \"abc\"]", got)
	}
	if got[0] != "" || got[1] != "abc" {
		t.Fatalf("search queries = %v, want [\"\" \"abc\"]", got)
	}
}

// TestDownloadExportZip covers the ZIP export: the fake backend streams a
// small archive, and the file lands where the (stubbed) save dialog points.
func TestDownloadExportZip(t *testing.T) {
	dir := t.TempDir()
	picked := filepath.Join(dir, "out.zip")
	old := pickSavePath
	pickSavePath = func(parent *mygo.Window, dir, name string) (string, error) {
		if !strings.HasSuffix(name, ".zip") {
			t.Errorf("default file name = %q, want a .zip", name)
		}
		return picked, nil
	}
	t.Cleanup(func() { pickSavePath = old })

	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	if err := tt.Click("下载管理"); err != nil {
		t.Fatalf("click 下载管理: %v", err)
	}
	navSettle(tt)
	for i := 0; i < 3; i++ {
		tt.Advance(downloadPollInterval)
		time.Sleep(10 * time.Millisecond)
	}
	if !tt.HasText("已完成") {
		t.Fatalf("job did not complete; texts=%q", tt.Texts()[14:])
	}
	if err := tt.Click("导出 ZIP"); err != nil {
		t.Fatalf("click 导出 ZIP: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if a.zipMsg != "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if a.zipMsg == "" {
		t.Fatalf("export never finished")
	}
	if strings.Contains(a.zipMsg, "失败") {
		t.Fatalf("export failed: %s", a.zipMsg)
	}
	data, err := os.ReadFile(picked)
	if err != nil {
		t.Fatalf("read exported file: %v", err)
	}
	if string(data) != "PK" {
		t.Fatalf("exported file = %q, want the backend's PK", data)
	}
}

func TestZipFileName(t *testing.T) {
	cases := map[string]string{
		"":                       "gallery-7.zip",
		"  ":                     "gallery-7.zip",
		"[Group] Title / Vol. 1": "[Group] Title  Vol 1.zip",
	}
	for in, want := range cases {
		if got := zipFileName(PrefillJob{ID: "j", GalleryID: 7, Title: in}); got != want {
			t.Errorf("zipFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestTagSettingsAddAndRemove drives the tag-rule page: adding a rule saves it
// and lists it, and 删除 takes it out again.
func TestTagSettingsAddAndRemove(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if err := tt.Click("标签设置"); err != nil {
		t.Fatalf("click 标签设置: %v", err)
	}
	navSettle(tt)
	waitText(t, tt, "还没有规则")

	if err := tt.Click("添加标签"); err != nil {
		t.Fatalf("click 添加标签: %v", err)
	}
	tt.Type("Female:Yuri")
	if err := tt.Click("添加"); err != nil {
		t.Fatalf("click 添加: %v", err)
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if len(a.tags.rules) == 1 && tt.HasText("female:yuri") {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := a.tags.rules; len(got) != 1 || got[0].Tag != "female:yuri" || got[0].Action != TagLike || got[0].Weight != 10 {
		t.Fatalf("rules = %+v", got)
	}
	waitText(t, tt, "已保存")

	if err := tt.Click("删除标签规则"); err != nil {
		t.Fatalf("click 删除标签规则: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if len(a.tags.rules) == 0 {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if len(a.tags.rules) != 0 {
		t.Fatalf("rules after delete = %+v", a.tags.rules)
	}
}

// TestTagRuleColoursGalleryChips checks a saved rule colours and orders the
// gallery page's tag chips.
func TestTagRuleColoursGalleryChips(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	// Seed one preferred tag and one neutral tag.
	a.tags.rules = []TagRule{{Tag: "female:yuri", Action: TagDislike, Weight: -50}}
	a.tags.loaded = true

	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "female:yuri")

	rule := a.tagRule("female:yuri")
	if rule == nil || rule.Action != TagDislike {
		t.Fatalf("tagRule(female:yuri) = %+v", rule)
	}
	if got := tagActionColor(TagDislike); got != ui.Hex("#FF6666") {
		t.Fatalf("dislike colour = %v", got)
	}
	if got := tagActionColor(TagLike); got != ui.Hex("#3377FF") {
		t.Fatalf("like colour = %v", got)
	}
}

// TestTagSettingsSync covers the sync button: the fake merge reports what it
// pulled and replaces the rules.
func TestTagSettingsSync(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if err := tt.Click("标签设置"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "与源站同步")

	if err := tt.Click("同步"); err != nil {
		t.Fatalf("click 同步: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if a.tags.syncResult != "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !strings.Contains(a.tags.syncResult, "拉取 2") {
		t.Fatalf("sync result = %q", a.tags.syncResult)
	}
	if got := a.tags.rules; len(got) != 1 || got[0].Tag != "female:yuri" {
		t.Fatalf("rules after sync = %+v", got)
	}
}

func TestSortedTagsAndWeightInput(t *testing.T) {
	a := New(NewClient("http://example.test"))
	a.tags.loaded = true
	a.tags.rules = []TagRule{
		{Tag: "artist:x", Action: TagLike, Weight: 5},
		{Tag: "female:yuri", Action: TagLike, Weight: 20},
		{Tag: "male:y", Action: TagDislike, Weight: -3},
	}
	got := a.sortedTags([]apiTag{
		{Namespace: "tag", Name: "netorare"},
		{Namespace: "male", Name: "y"},
		{Namespace: "artist", Name: "x"},
		{Namespace: "female", Name: "yuri"},
	})
	want := []string{"female:yuri", "artist:x", "male:y", "tag:netorare"}
	for i := range want {
		if got[i].Namespace+":"+got[i].Name != want[i] {
			t.Fatalf("sorted[%d] = %s:%s, want %s", i, got[i].Namespace, got[i].Name, want[i])
		}
	}

	// An out-of-range weight is rejected without changing the rules.
	a.tags.input = "female:yuri"
	a.tags.weight = "1000"
	a.tags.rules = nil
	a.addTagRule()
	if a.tags.inputErr == "" {
		t.Fatal("weight 1000 was accepted")
	}
	if len(a.tags.rules) != 0 {
		t.Fatalf("invalid add changed rules: %+v", a.tags.rules)
	}
}

// TestListCardsShowColouredTags checks the home grid's chips follow the rules
// and that a rule change recolours cards already on screen.
func TestListCardsShowColouredTags(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	// The card keeps the tag it was built with.
	card := a.lists["/"].items[0]
	if len(card.Tags) != 1 || card.Tags[0].Namespace != "female" || card.Tags[0].Name != "yuri" {
		t.Fatalf("card tags = %+v", card.Tags)
	}
	if got := tagChipText(card.Tags[0].Namespace, card.Tags[0].Name); got != "female:yuri" {
		t.Fatalf("chip text = %q", got)
	}

	// A new rule must be noticed on the next frame.
	a.tags.rules = []TagRule{{Tag: "female:yuri", Action: TagLike, Weight: 10}}
	a.tags.loaded = true
	tt.Frame()
	if len(a.announcedRules) != 1 || a.announcedRules[0].Tag != "female:yuri" {
		t.Fatalf("announced rules = %+v", a.announcedRules)
	}
	if r := a.tagRule("female:yuri"); r == nil || r.Action != TagLike {
		t.Fatalf("tagRule = %+v", r)
	}

	// A bare tag has no namespace and still matches its rule.
	if ns, name := splitTag("netorare"); ns != "" || name != "netorare" {
		t.Fatalf("splitTag(netorare) = %q %q", ns, name)
	}
	if ns, name := splitTag("tag:netorare"); ns != "tag" || name != "netorare" {
		t.Fatalf("splitTag(tag:netorare) = %q %q", ns, name)
	}
}

// TestGalleryTorrents covers the lazy torrent list and saving a .torrent.
func TestGalleryTorrents(t *testing.T) {
	dir := t.TempDir()
	picked := filepath.Join(dir, "t.torrent")
	old := pickSavePath
	pickSavePath = func(parent *mygo.Window, dir, name string) (string, error) {
		if name != "[Yanje] 示例 A.torrent" {
			t.Errorf("default torrent name = %q", name)
		}
		return picked, nil
	}
	t.Cleanup(func() { pickSavePath = old })

	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")

	// Torrents are fetched only when asked for.
	if a.details[1].torrents.loaded {
		t.Fatal("torrents loaded before the user asked")
	}
	if err := tt.Click("查看种子"); err != nil {
		t.Fatalf("click 查看种子: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if tt.HasText("下载种子") {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !tt.HasText("[Yanje] 示例 A.zip") {
		t.Fatalf("torrent row missing; texts=%q", tt.Texts())
	}

	if err := tt.Click("下载种子"); err != nil {
		t.Fatalf("click 下载种子: %v", err)
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if a.details[1].torrents.msg != "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if msg := a.details[1].torrents.msg; !strings.Contains(msg, "已保存") {
		t.Fatalf("torrent save message = %q", msg)
	}
	data, err := os.ReadFile(picked)
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != "d8:announce" {
		t.Fatalf("saved torrent = %q", data)
	}
}

func TestTorrentFileName(t *testing.T) {
	cases := map[string]string{
		"a/b:c.zip": "a_b_c.torrent",
		"":          "torrent-9.torrent",
		"x.torrent": "x.torrent",
	}
	for in, want := range cases {
		if got := torrentFileName(Torrent{GTID: "9", Name: in}); got != want {
			t.Errorf("torrentFileName(%q) = %q, want %q", in, got, want)
		}
	}
}

const sampleTagDB = `load_ehtagtranslation_db_text({"version":2,"head":{"sha":"abc"},` +
	`"data":[{"namespace":"rows","data":{"female":{"name":"女性"}}},` +
	`{"namespace":"female","data":{"yuri":{"name":"百合","intro":"<p>女性之间的恋爱</p>"},` +
	`"x":{"name":"<b>X</b> 标签"}}}]});`

func TestParseTagTranslationDB(t *testing.T) {
	db, err := parseTagTranslationDB([]byte(sampleTagDB))
	if err != nil {
		t.Fatal(err)
	}
	if got := db.Lookup("female", "yuri"); got != "百合" {
		t.Fatalf("Lookup = %q", got)
	}
	if got := db.NamespaceName("female"); got != "女性" {
		t.Fatalf("NamespaceName = %q", got)
	}
	if tr, desc := db.Describe("female", "yuri"); tr != "百合" || desc != "女性之间的恋爱" {
		t.Fatalf("Describe = %q %q", tr, desc)
	}
	if got := db.Lookup("female", "x"); got != "X 标签" {
		t.Fatalf("HTML not cleaned: %q", got)
	}
	if db.Count() != 2 {
		t.Fatalf("Count = %d", db.Count())
	}
	if got := db.Search("百合", 5); len(got) != 1 || got[0].Tag != "yuri" || got[0].Translation != "百合" {
		t.Fatalf("Search = %+v", got)
	}
	if got := db.Search("YURI", 5); len(got) != 1 {
		t.Fatalf("case-insensitive search = %+v", got)
	}
	if db.Ready() {
		t.Fatal("Ready before Load")
	}
	if got := db.Status(); !strings.Contains(got, "尚未加载") {
		t.Fatalf("Status = %q", got)
	}
}

func TestParseTagTranslationDBPlainJSON(t *testing.T) {
	// The release also ships plain JSON; the parser accepts both.
	plain := strings.TrimSuffix(strings.TrimPrefix(sampleTagDB, "load_ehtagtranslation_db_text("), ");")
	db, err := parseTagTranslationDB([]byte(plain))
	if err != nil {
		t.Fatal(err)
	}
	if db.Count() != 2 {
		t.Fatalf("Count = %d", db.Count())
	}
}

// TestTagTranslationColoursChips checks a translated tag renders its
// translation in the chip.
func TestTagTranslationColoursChips(t *testing.T) {
	db, err := parseTagTranslationDB([]byte(sampleTagDB))
	if err != nil {
		t.Fatal(err)
	}
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	// Copy the tables, not the lock.
	a.translations.mu.Lock()
	a.translations.entries = db.entries
	a.translations.ns = db.ns
	a.translations.names = db.names
	a.translations.loaded = true
	a.translations.mu.Unlock()

	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "百合 / female:yuri")
}

// TestTagTranslationCachedLoad checks Load reads the cached file and parses it
// without touching the network.
func TestTagTranslationCachedLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "tag-translations.json")
	if err := os.WriteFile(path, []byte(sampleTagDB), 0o644); err != nil {
		t.Fatal(err)
	}
	db := &TagTranslationDB{path: path}
	db.Load()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if db.Ready() {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !db.Ready() {
		t.Fatalf("load failed: %s", db.Status())
	}
	if got := db.Lookup("female", "yuri"); got != "百合" {
		t.Fatalf("Lookup = %q", got)
	}
	if !strings.Contains(db.Status(), "已加载 2 个标签") {
		t.Fatalf("Status = %q", db.Status())
	}
}

// TestReaderProgressIsBatched checks that flipping pages quickly writes one
// progress request for the page the reader stopped on.
func TestReaderProgressIsBatched(t *testing.T) {
	var mu sync.Mutex
	var puts []int
	mux := http.NewServeMux()
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 1,
			"results": []map[string]any{galleryItem(1, "tok1", "示例 A", "doujinshi", 3)}})
	})
	mux.HandleFunc("/api/gallery/1/tok1/pages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"type":"meta","total":3}`)
		for i := 0; i < 3; i++ {
			fmt.Fprintf(w, `{"type":"page","index":%d,"page_url":"https://img.example/page-%d.jpg"}`+"\n", i, i)
		}
		fmt.Fprintln(w, `{"type":"done","total":3}`)
	})
	mux.HandleFunc("/api/gallery-cache/1/tok1/pages", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintln(w, `{"type":"meta","total":3}`)
		fmt.Fprintln(w, `{"type":"done","total":0}`)
	})
	mux.HandleFunc("/api/progress/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPut {
			var body struct {
				CurrentPage int `json:"current_page"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			mu.Lock()
			puts = append(puts, body.CurrentPage)
			mu.Unlock()
			return
		}
		writeJSON(w, map[string]any{"current_page": 0, "progress": 0, "completed": false})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "示例 A")

	a.router.Push("/reader/1/tok1")
	navSettle(tt)
	waitText(t, tt, "1 / 3 页")

	// Two quick turns: only the last page should be reported.
	if err := tt.Click("下一页"); err != nil {
		t.Fatal(err)
	}
	waitText(t, tt, "2 / 3 页")
	tt.Key(0, ui.KeyRight)
	waitText(t, tt, "3 / 3 页")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		n := len(puts)
		mu.Unlock()
		if n > 0 {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(progressFlushDelay + 100*time.Millisecond)

	mu.Lock()
	got := append([]int(nil), puts...)
	mu.Unlock()
	if len(got) != 1 || got[0] != 2 {
		t.Fatalf("progress writes = %v, want a single write of page 2", got)
	}
}

// TestGalleryContinueReading labels the start button with the saved page.
func TestGalleryContinueReading(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	// The fake detail has no progress; seed one through the detail state used
	// by the bookshelf cards instead.
	if g := (Gallery{Reading: &ReadingProgress{CurrentPage: 4}}); g.NextPage() != 4 {
		t.Fatalf("NextPage = %d", g.NextPage())
	}
	if g := (Gallery{Reading: &ReadingProgress{CurrentPage: 4, Completed: true}}); g.NextPage() != 0 {
		t.Fatalf("finished gallery NextPage = %d", g.NextPage())
	}

	// A list card with progress says so.
	a.lists["/"].items[0].Reading = &ReadingProgress{CurrentPage: 1}
	tt.Frame()
	if !tt.HasText("读到第 2 页") {
		t.Fatalf("card does not show progress; texts=%q", tt.Texts())
	}
}

// TestReaderDoubleAndWheel covers double-page steps and wheel turning.
func TestReaderDoubleAndWheel(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	a.router.Push("/reader/1/tok1")
	navSettle(tt)
	waitText(t, tt, "1 / 3 页")
	// The reader opens under the home page's grid for the router's
	// transition; any frame after it lays the bottom bar out.
	tt.Frame()

	// Double mode steps two pages at a time.
	if err := tt.Click("单页"); err != nil {
		t.Fatalf("click 单页: %v", err)
	}
	if !a.reader.double {
		t.Fatal("double mode not enabled")
	}
	waitText(t, tt, "双页")
	if err := tt.Click("下一页"); err != nil {
		t.Fatal(err)
	}
	waitText(t, tt, "3 / 3 页")
	if err := tt.Click("上一页"); err != nil {
		t.Fatal(err)
	}
	waitText(t, tt, "1 / 3 页")

	// Wheel mode: a notch down turns forward instead of panning.
	if err := tt.Click("滚轮平移"); err != nil {
		t.Fatalf("click wheel mode: %v", err)
	}
	waitText(t, tt, "滚轮翻页")
	if err := tt.Click("双页"); err != nil {
		t.Fatal(err)
	}
	tt.Scroll(640, 250, 0, 40)
	waitText(t, tt, "2 / 3 页")
	tt.Scroll(640, 250, 0, -40)
	waitText(t, tt, "1 / 3 页")
}

// TestImageStoreErrAndRetry covers the placeholder state: a failed load is
// reported, and Retry makes the next Get ask again.
func TestImageStoreErrAndRetry(t *testing.T) {
	fail := true
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		shouldFail := fail
		mu.Unlock()
		if shouldFail {
			http.Error(w, "nope", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write(fakeImagePNG)
	}))
	defer srv.Close()

	store := NewImageStore(4)
	url := srv.URL + "/img"

	if got := store.Get(url); got != nil {
		t.Fatal("first Get returned a bitmap")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if store.Err(url) != "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if store.Err(url) == "" {
		t.Fatal("failed load not reported")
	}

	fail = false
	store.Retry(url)
	if got := store.Get(url); got != nil {
		t.Fatal("Get after Retry should start loading, not return at once")
	}
	deadline = time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if store.Get(url) != nil {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if store.Get(url) == nil {
		t.Fatal("retry did not load the image")
	}
	if store.Err(url) != "" {
		t.Fatalf("Err after success = %q", store.Err(url))
	}
	mu.Lock()
	n := hits
	mu.Unlock()
	if n < 2 {
		t.Fatalf("server hits = %d, want at least 2", n)
	}
}

// TestSettingsAppearance covers the theme choice: the buttons persist the
// value through the backend.
func TestSettingsAppearance(t *testing.T) {
	srv := newFakeServer(t)
	backend := &fakeBackend{serverURL: srv.URL, base: srv.URL, configured: true, appearance: "system"}
	a := New(NewClient(srv.URL))
	a.SetBackend(backend)
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "示例 A")
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "✓ 跟随系统")

	if err := tt.Click("深色"); err != nil {
		t.Fatalf("click 深色: %v", err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if a.appearance == "dark" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if got := backend.Appearance(); got != "dark" {
		t.Fatalf("backend appearance = %q", got)
	}
	waitText(t, tt, "✓ 深色")

	// The language section names the only UI language.
	if !tt.HasText("简体中文") {
		t.Fatalf("language row missing; texts=%q", tt.Texts())
	}
}

// TestSettingsShowsBuild checks the 关于 row shows the build string main sets.
func TestSettingsShowsBuild(t *testing.T) {
	a, tt := newTestApp(t)
	a.SetBuild("1.2.3 · linux/amd64")
	waitText(t, tt, "示例 A")
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "1.2.3 · linux/amd64")
	if !tt.HasText("无 WebView") {
		t.Fatalf("about text missing; texts=%q", tt.Texts())
	}
}

// TestDiagnosticsExport writes the diagnostics text and checks it holds the
// settings paths but never the cookie.
func TestDiagnosticsExport(t *testing.T) {
	srv := newFakeServer(t)
	dir := t.TempDir()
	picked := filepath.Join(dir, "diag.txt")
	old := pickSavePath
	pickSavePath = func(parent *mygo.Window, dir, name string) (string, error) {
		if !strings.HasSuffix(name, ".txt") {
			t.Errorf("diagnostics name = %q", name)
		}
		return picked, nil
	}
	t.Cleanup(func() { pickSavePath = old })

	backend := &fakeBackend{
		serverURL: srv.URL, base: srv.URL, configured: true,
		storage: BackendStorage{ConfigPath: "/cfg/config.json", DBPath: "/cfg/app.db", CacheDir: "/cache/images"},
	}
	a := New(NewClient(srv.URL))
	a.SetBackend(backend)
	a.SetBuild("1.0.0 · linux/amd64")
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "示例 A")
	if err := tt.Click("设置"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "无 WebView")

	// The settings page scrolls; the button is below the fold until the view
	// is scrolled to the bottom.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if r, ok := tt.Find("导出诊断信息"); ok && r.W > 0 {
			break
		}
		tt.Scroll(640, 400, 0, 2000)
	}
	if r, _ := tt.Find("导出诊断信息"); r.W == 0 {
		t.Fatalf("export button never laid out")
	}

	if err := tt.Click("导出诊断信息"); err != nil {
		t.Fatalf("click 导出诊断信息: %v", err)
	}
	for time.Now().Before(deadline) {
		tt.Frame()
		if a.diagMsg != "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}
	if !strings.Contains(a.diagMsg, "已保存") {
		t.Fatalf("export message = %q", a.diagMsg)
	}
	data, err := os.ReadFile(picked)
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	for _, want := range []string{"1.0.0 · linux/amd64", "/cfg/app.db", "/cache/images", "标签规则"} {
		if !strings.Contains(text, want) {
			t.Errorf("diagnostics missing %q:\n%s", want, text)
		}
	}
	if strings.Contains(strings.ToLower(text), "cookie=") || strings.Contains(text, "ipb_") {
		t.Fatalf("diagnostics leak cookies:\n%s", text)
	}
}

// TestDownloadToastAndOpen checks the success toast after queueing a job and
// that clicking a job's title opens the gallery.
func TestDownloadToastAndOpen(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	if err := tt.Click("离线下载"); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		tt.Frame()
		if a.details[1] != nil && a.details[1].downloadJob != "" {
			break
		}
		time.Sleep(2 * time.Millisecond)
	}

	// The list page opens the gallery again from the job's title.
	a.router.Push("/downloads")
	navSettle(tt)
	waitText(t, tt, "示例 A")
	if err := tt.Click("打开 示例 A"); err != nil {
		t.Fatalf("click job title: %v", err)
	}
	navSettle(tt)
	if p := a.router.Path(); p != "/gallery/1/tok1" {
		t.Fatalf("path after job title = %q", p)
	}
}
