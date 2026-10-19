package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// Benchmarks for the v1 targets in MYGO_MIGRATION.md §3.4:
//
//	a 1000-page gallery scrolls at 60fps, resident memory stays under
//	300MB, and a cold start under 1s (the backend's first fetch aside).
//
// The Tester renders every frame on the CPU, as the software renderer of
// a machine without a GPU does, so a frame here is slower than one the GPU
// renderers present; -benchtime=20x and comparing runs is what the numbers
// are for, not an absolute fps. Run them with:
//
//	cd backend
//	go test ./cmd/desktop-native/app -run '^$' -bench Scroll -benchtime=50x
//	go test ./cmd/desktop-native/app -run '^$' -bench Evict
//	go test ./cmd/desktop-native/app -run '^$' -bench Memory -benchtime=1x

// bigGalleryServer serves lists of many galleries and galleries of many
// pages, so the benchmarks scroll a list as long as a real one.
func bigGalleryServer(tb testing.TB, galleries, pages int) *httptest.Server {
	tb.Helper()
	mux := http.NewServeMux()
	all := make([]map[string]any, galleries)
	for i := range all {
		all[i] = galleryItem(i+1, fmt.Sprintf("tok%d", i+1),
			fmt.Sprintf("示例画廊 %d", i+1), "manga", pages)
	}
	list := func(w http.ResponseWriter, r *http.Request) {
		// One page of results at a time, as the real API pages them.
		page := 0
		start := page * 100
		end := start + 100
		if start > len(all) {
			start = len(all)
		}
		if end > len(all) {
			end = len(all)
		}
		writeJSON(w, map[string]any{
			"page": page, "page_size": 100, "total": len(all),
			"results": all[start:end],
		})
	}
	for _, route := range []string{"/api/galleries", "/api/watched", "/api/popular"} {
		mux.HandleFunc(route, list)
	}
	mux.HandleFunc("/api/search", list)
	mux.HandleFunc("/api/bookshelf", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 0, "results": []any{}})
	})
	mux.HandleFunc("/api/recently-read", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 0, "results": []any{}})
	})
	mux.HandleFunc("/api/gallery/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{
			"id": 1, "token": "tok1", "title": "示例画廊 1", "category": "manga",
			"thumbnail": "https://img.example/1.jpg", "page_count": pages,
			"tags": []map[string]string{{"namespace": "female", "name": "yuri"}},
		})
	})
	pagesHandler := func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")
		fmt.Fprintf(w, `{"type":"meta","total":%d}`+"\n", pages)
		for i := 0; i < pages; i++ {
			fmt.Fprintf(w, `{"type":"page","index":%d,"page_url":"https://img.example/p%d.jpg"}`+"\n", i, i)
		}
		fmt.Fprintf(w, `{"type":"done","total":%d}`+"\n", pages)
	}
	mux.HandleFunc("/api/gallery/1/tok1/pages", pagesHandler)
	mux.HandleFunc("/api/gallery-cache/1/tok1/pages", pagesHandler)
	mux.HandleFunc("/api/progress/1/tok1", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"current_page": 0})
	})
	mux.HandleFunc("/api/bookshelf/1/tok1/status", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"in_bookshelf": false})
	})
	return httptest.NewServer(mux)
}

// BenchmarkScrollHome renders the frames of scrolling the home list, which
// the grid builds lazily: only the visible cards are built and painted.
func BenchmarkScrollHome(b *testing.B) {
	srv := bigGalleryServer(b, 1000, 1000)
	defer srv.Close()
	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitTextB(b, tt, "示例画廊 1")

	b.ResetTimer()
	for b.Loop() {
		tt.Scroll(640, 400, 0, 60)
	}
}

// BenchmarkScrollThumbnails renders the frames of scrolling a 1000-page
// thumbnail grid on the gallery page, the density the v1 target names.
func BenchmarkScrollThumbnails(b *testing.B) {
	srv := bigGalleryServer(b, 10, 1000)
	defer srv.Close()
	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitTextB(b, tt, "示例画廊 1")
	if err := tt.Click("示例画廊 1"); err != nil {
		b.Fatal(err)
	}
	navSettle(tt)
	waitTextB(b, tt, "开始阅读")

	b.ResetTimer()
	for b.Loop() {
		tt.Scroll(640, 400, 0, 60)
	}
}

// BenchmarkEvict measures page eviction with a cache as large as the reader
// keeps (6 pages), over a gallery long enough that the access list wraps
// many times. The old FIFO scan was O(n) per drop; this catches a return to
// that.
func BenchmarkEvict(b *testing.B) {
	keys := make([]string, 64)
	for i := range keys {
		keys[i] = fmt.Sprintf("p%d", i)
	}
	b.ReportAllocs()
	for b.Loop() {
		s := NewImageStore(6)
		for _, k := range keys {
			e := &imageEntry{url: k}
			s.entries[k] = e
			s.touchLocked(e)
		}
		s.evictLocked()
	}
}

// BenchmarkTouchHit measures what a cache hit costs: the Get path moves the
// entry to the tail. A reader asks for every visible page each frame, so
// this runs many times per frame.
func BenchmarkTouchHit(b *testing.B) {
	s := NewImageStore(6)
	var es []*imageEntry
	for i := 0; i < 6; i++ {
		e := &imageEntry{url: fmt.Sprintf("p%d", i)}
		s.entries[e.url] = e
		s.touchLocked(e)
		es = append(es, e)
	}
	b.ResetTimer()
	b.ReportAllocs()
	for b.Loop() {
		for _, e := range es {
			s.mu.Lock()
			s.touchLocked(e)
			s.mu.Unlock()
		}
	}
}

// TestMemoryUnderTarget renders a 1000-page thumbnail grid and reports the
// heap, to hold resident memory to the v1 target. It is a test rather than a
// benchmark because what it guards is a limit, not a speed.
func TestMemoryUnderTarget(t *testing.T) {
	if testing.Short() {
		t.Skip("builds a 1000-page grid")
	}
	srv := bigGalleryServer(t, 10, 1000)
	defer srv.Close()
	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "示例画廊 1")
	if err := tt.Click("示例画廊 1"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	for i := 0; i < 20; i++ {
		tt.Scroll(640, 400, 0, 60)
	}

	var m runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&m)
	heapMB := float64(m.HeapAlloc) / (1 << 20)
	t.Logf("a 1000-page thumbnail grid holds %.1f MB of heap (target: under 300MB resident)", heapMB)
	if heapMB > 300 {
		t.Errorf("heap = %.1f MB, over the 300MB target", heapMB)
	}
}

// TestScrollingStaysUnderBudget fails when a frame of scrolling a long list
// takes more than a 60fps budget on the CPU renderer, with margin for CI
// machines. It guards against a frame cost that grows with the list, which
// is what breaks a 1000-page gallery.
func TestScrollingStaysUnderBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("renders a long list")
	}
	if raceEnabled {
		t.Skip("the race detector slows frames many times over")
	}
	srv := bigGalleryServer(t, 1000, 1000)
	defer srv.Close()
	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "示例画廊 1")

	// Warm up: the first frames lay out text and build glyph atlases.
	for i := 0; i < 5; i++ {
		tt.Scroll(640, 400, 0, 60)
	}
	const frames = 30
	budget := time.Second / 60 // a 60fps frame
	start := time.Now()
	for i := 0; i < frames; i++ {
		tt.Scroll(640, 400, 0, 60)
	}
	avg := time.Since(start) / frames
	t.Logf("average scroll frame: %.2f ms on the CPU renderer", float64(avg.Microseconds())/1000)
	if avg > 4*budget {
		t.Errorf("average scroll frame %v is over 4x the 60fps budget (%v); "+
			"frames should not grow with the list", avg, budget)
	}
}

// TestScrollCostIndependentOfListLength guards the grid's virtualization: a
// frame of scrolling builds only the visible cells, so the frame cost must
// not grow with the list. A 50x longer list must stay within a small factor
// of the first, which catches a grid that builds every card.
func TestScrollCostIndependentOfListLength(t *testing.T) {
	if testing.Short() {
		t.Skip("renders two long lists")
	}
	if raceEnabled {
		t.Skip("the race detector's overhead is not uniform across lengths")
	}
	measure := func(galleries int) time.Duration {
		srv := bigGalleryServer(t, galleries, 1000)
		defer srv.Close()
		a := New(NewClient(srv.URL))
		tt := ui.NewTester(a.View, 1280, 860)
		waitText(t, tt, "示例画廊 1")
		for i := 0; i < 5; i++ { // warm up: text layouts, glyph atlases
			tt.Scroll(640, 400, 0, 60)
		}
		const frames = 20
		start := time.Now()
		for i := 0; i < frames; i++ {
			tt.Scroll(640, 400, 0, 60)
		}
		return time.Since(start) / frames
	}

	small, large := measure(100), measure(5000)
	t.Logf("frame: 100 galleries %.2f ms, 5000 galleries %.2f ms",
		float64(small.Microseconds())/1000, float64(large.Microseconds())/1000)
	if large > 2*small {
		t.Errorf("a 5000-gallery list scrolls at %v a frame against %v for 100: "+
			"the cost grows with the list, so the grid is not virtualized", large, small)
	}
}

// TestColdStartUnderBudget measures the app's start, the v1 target's last
// number: building the first frame with no window, backend fetch aside.
// Cover placeholders are painted up front, so this includes them.
func TestColdStartUnderBudget(t *testing.T) {
	if testing.Short() {
		t.Skip("builds the app")
	}
	if raceEnabled {
		t.Skip("the race detector slows the start")
	}
	srv := bigGalleryServer(t, 25, 10)
	defer srv.Close()

	start := time.Now()
	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	firstFrame := time.Since(start)
	t.Logf("first frame after %v (target: under 1s)", firstFrame.Round(time.Millisecond))

	// The first frame with data, once the list arrives.
	waitText(t, tt, "示例画廊 1")
	loaded := time.Since(start)
	t.Logf("list shown after %v", loaded.Round(time.Millisecond))

	if firstFrame > time.Second {
		t.Errorf("the first frame took %v, over the 1s cold-start target", firstFrame)
	}
	_ = tt
}

func waitTextB(b *testing.B, tt *ui.Tester, s string) {
	b.Helper()
	for i := 0; i < 2000; i++ {
		tt.Frame()
		if tt.HasText(s) {
			return
		}
	}
	b.Fatalf("timed out waiting for %q; texts=%q", s, strings.Join(tt.Texts(), ","))
}
