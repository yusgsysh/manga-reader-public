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
	"strings"
	"sync"
	"testing"
	"time"

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
	mux.HandleFunc("/api/image/thumbnail", image)
	mux.HandleFunc("/api/image/page", image)
	mux.HandleFunc("/api/image-cache/thumbnail", image)
	mux.HandleFunc("/api/image-cache/page", image)

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
	return CacheStats{
		Root:     "/tmp/manga-cache",
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
