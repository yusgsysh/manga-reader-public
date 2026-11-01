package app

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestHomeGridColumnsMatchMain checks the breakpoints the native grid picks:
// the same counts main's .gallery-grid uses at the same window widths, so a
// resized window keeps the web frontend's column rhythm.
func TestHomeGridColumnsMatchMain(t *testing.T) {
	cases := []struct {
		width float32
		want  int
	}{
		{400, 2}, {479, 2}, {480, 3}, {767, 3}, {768, 4},
		{1023, 4}, {1024, 5}, {1439, 5}, {1440, 6}, {1919, 6}, {1920, 7},
	}
	for _, c := range cases {
		if got := listColumns(c.width); got != c.want {
			t.Errorf("listColumns(%v) = %d, want %d", c.width, got, c.want)
		}
	}

	// At 1280 the content is 976 points, five columns of 179.2 with 20 gaps.
	if got := listCellWidth(1280, sidebarWidth); got < 179.1 || got > 179.3 {
		t.Errorf("card width at 1280 = %v, want 179.2", got)
	}
	// Folded, the rail gives the cards the extra room main gives them.
	if got := listCellWidth(1280, sidebarWidthCollapsed); got <= 179.2 {
		t.Errorf("folded card width = %v, want wider than the open rail's", got)
	}
}

// TestHomeFilterPanelApplies opens the advanced-search panel, drops one
// category and applies it: the list narrows at once.
func TestHomeFilterPanelApplies(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	if err := tt.Click("高级搜索"); err != nil {
		t.Fatalf("open the filter panel: %v", err)
	}
	tt.Frame()
	waitText(t, tt, "分类")
	if err := tt.Click("非 H"); err != nil {
		t.Fatalf("drop a category: %v", err)
	}
	tt.Frame()
	if err := tt.Click("应用"); err != nil {
		t.Fatalf("apply the filters: %v", err)
	}
	tt.Frame()

	a.mu.Lock()
	filters := a.lists["/"].filters
	a.mu.Unlock()
	if filters.included("non-h") {
		t.Fatalf("non-h is still included: %+v", filters.categories)
	}
	if got := filters.active(); got != 1 {
		t.Fatalf("active filters = %d, want 1", got)
	}
	if a.filterOpen {
		t.Fatal("the panel stayed open after Apply")
	}
}

// TestHomeJumpPanelSeeks opens the browse-position panel and takes a quick
// jump: the list reloads at that position.
func TestHomeJumpPanelSeeks(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	if err := tt.Click("浏览定位"); err != nil {
		t.Fatalf("open the jump panel: %v", err)
	}
	tt.Frame()
	waitText(t, tt, "快捷")
	if err := tt.Click("1 周"); err != nil {
		t.Fatalf("choose a quick jump: %v", err)
	}
	tt.Frame()

	a.mu.Lock()
	nav := a.lists["/"].nav
	open := a.jumpOpen
	a.mu.Unlock()
	if nav.jump != "1w" {
		t.Fatalf("jump = %q, want 1w", nav.jump)
	}
	if open {
		t.Fatal("the panel stayed open after the jump")
	}
}

// TestSidebarFoldsToIcons checks the rail folds and unfolds, and that the
// pages stay reachable while it is folded.
func TestSidebarFoldsToIcons(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	if err := tt.Click("折叠侧边栏"); err != nil {
		t.Fatalf("fold the rail: %v", err)
	}
	tt.Frame()
	a.mu.Lock()
	folded := a.sidebarCollapsed
	a.mu.Unlock()
	if !folded {
		t.Fatal("the rail did not fold")
	}
	// The names are hidden, but the items keep their labels and stay
	// reachable by a click on the mark.
	if err := tt.Click("设置"); err != nil {
		t.Fatalf("click a folded item: %v", err)
	}
	navSettle(tt)
	if got := a.router.Path(); got != "/settings" {
		t.Fatalf("path = %q, want /settings", got)
	}
	if err := tt.Click("展开侧边栏"); err != nil {
		t.Fatalf("unfold the rail: %v", err)
	}
	tt.Frame()
	a.mu.Lock()
	folded = a.sidebarCollapsed
	a.mu.Unlock()
	if folded {
		t.Fatal("the rail did not unfold")
	}
}

// TestSidebarThemeSwitch picks the dark appearance from the rail's switch.
func TestSidebarThemeSwitch(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if err := tt.Click("深色"); err != nil {
		t.Fatalf("pick dark: %v", err)
	}
	tt.Frame()
	a.mu.Lock()
	appearance := a.appearance
	a.mu.Unlock()
	if appearance != "dark" {
		t.Fatalf("appearance = %q, want dark", appearance)
	}
	_ = ui.Center
}

// TestIdleHomeDoesNotPageThrough guards the grid's end probe: a home page
// left alone must not walk the whole listing page by page. Only the row the
// reader has scrolled to asks for more.
func TestIdleHomeDoesNotPageThrough(t *testing.T) {
	mux := http.NewServeMux()
	var mu sync.Mutex
	pages := map[int]int{}
	total := 1000
	items := func(start, end int) []map[string]any {
		out := make([]map[string]any, 0, end-start)
		for i := start; i < end; i++ {
			out = append(out, galleryItem(i+1, "t"+itoa(i+1),
				"示例画廊 "+itoa(i+1), "manga", 10))
		}
		return out
	}
	mux.HandleFunc("/api/galleries", func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		mu.Lock()
		pages[page]++
		mu.Unlock()
		start := min(page*100, total)
		end := min(start+100, total)
		writeJSON(w, map[string]any{
			"page": page, "page_size": 100, "total": total, "results": items(start, end),
		})
	})
	mux.HandleFunc("/api/recently-read", func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, map[string]any{"page": 0, "page_size": 25, "total": 0, "results": []any{}})
	})
	srv := httptest.NewServer(mux)
	defer srv.Close()

	a := New(NewClient(srv.URL))
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "示例画廊 1")
	a.mu.Lock()
	loaded := len(a.lists["/"].items)
	a.mu.Unlock()
	// The first grid build may measure the whole page and ask for one more.
	if loaded > 200 {
		t.Fatalf("loaded %d items, want the first page and at most one more", loaded)
	}
	for i := 0; i < 40; i++ {
		tt.Frame()
		time.Sleep(2 * time.Millisecond)
	}
	mu.Lock()
	extra := pages[2] + pages[3]
	mu.Unlock()
	if extra > 0 {
		t.Fatalf("the page fetched %d later pages while idle", extra)
	}
}

// TestContinueRowPagesWithButton checks the strip's page button: it moves the
// row by a screenful, and the scrollbar itself is not shown.
func TestContinueRowPagesWithButton(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	for i := 0; i < 200; i++ {
		tt.Frame()
		if r := a.lists["/recently-read"]; r != nil && r.loaded {
			break
		}
	}
	a.update(func() {
		r := a.lists["/recently-read"]
		base := append([]Gallery(nil), a.lists["/"].items...)
		for i := 0; i < 11; i++ {
			g := base[i%len(base)]
			g.ID, g.Title = int64(400+i), fmt.Sprintf("继续阅读 %d", i+1)
			r.items = append(r.items, g)
		}
	})
	tt.Frame()
	tt.Frame()

	a.mu.Lock()
	before, maxX := a.continueScroll.X, a.continueScroll.MaxX
	a.mu.Unlock()
	if maxX <= 0 {
		t.Fatal("the strip does not overflow, so there is nothing to page")
	}
	if _, ok := tt.Find("下一页"); !ok {
		t.Fatalf("the page button is missing; texts=%q", tt.Texts())
	}
	if err := tt.Click("下一页"); err != nil {
		t.Fatalf("page the strip: %v", err)
	}
	tt.Frame()
	tt.Frame()
	a.mu.Lock()
	after := a.continueScroll.X
	a.mu.Unlock()
	if after <= before {
		t.Fatalf("continue X = %v after the page button, want more than %v", after, before)
	}
}

// TestHomeHeaderScrollsAway checks that the continue row and the latest
// header are part of the page's scroll, not pinned above it.
func TestHomeHeaderScrollsAway(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	a.update(func() {
		s := a.lists["/"]
		base := append([]Gallery(nil), s.items...)
		for i := 0; i < 22; i++ {
			g := base[i%len(base)]
			g.ID, g.Title = int64(500+i), fmt.Sprintf("画廊 %d", i+1)
			s.items = append(s.items, g)
		}
	})
	tt.Frame()
	if _, ok := tt.Find("最新"); !ok {
		t.Fatalf("the latest header is missing; texts=%q", tt.Texts())
	}
	if tt.HasText("加载更多") {
		t.Fatalf("the load-more button is still there; texts=%q", tt.Texts())
	}

	tt.Scroll(640, 400, 0, 520)
	tt.Frame()
	if r, ok := tt.Find("最新"); ok && r.Y+r.H > 0 {
		t.Fatalf("the latest header stayed at y=%v after scrolling", r.Y)
	}
}
