//go:build snapshot

package app

import (
	"fmt"
	"image/png"
	"os"
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// savePNG writes a tester's frame to /tmp/opencode for visual checks. The
// snapshots are a development aid; the assertions live in app_test.go.
func savePNG(t *testing.T, tt *ui.Tester, name string) {
	t.Helper()
	f, err := os.Create("/tmp/opencode/" + name + ".png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := png.Encode(f, tt.Image()); err != nil {
		t.Fatal(err)
	}
}

func TestSnapshots(t *testing.T) {
	a, tt := newTestApp(t)

	waitText(t, tt, "示例 A")
	savePNG(t, tt, "home")

	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	savePNG(t, tt, "gallery")

	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "第 1 / 3 页")
	// The page alone first, then the overlay with its controls, then the
	// settings sheet and the menu over it.
	a.update(func() { a.closeOverlay(&a.reader) })
	tt.Frame()
	savePNG(t, tt, "reader")
	showReaderOverlay(t, a, tt)
	savePNG(t, tt, "reader-overlay")
	showReaderPanel(t, a, tt, panelSettings)
	savePNG(t, tt, "reader-settings")
	showReaderPanel(t, a, tt, panelMenu)
	savePNG(t, tt, "reader-menu")
	showReaderPanel(t, a, tt, panelPages)
	savePNG(t, tt, "reader-pages")
	showReaderPanel(t, a, tt, panelMenu)

	// Back to the app for the search snapshot: the reader's menu holds the
	// way out, and the app's own search is outside the reader.
	if err := tt.Click("返回画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	if err := tt.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	tt.Type("B")
	waitText(t, tt, "共 1 个结果")
	savePNG(t, tt, "search")
}

// TestSetupAndSettingsSnapshots captures the first-run screen and the settings
// page (with cache stats) for visual checks.
func TestSetupAndSettingsSnapshots(t *testing.T) {
	srv := newFakeServer(t)

	{
		backend := &fakeBackend{serverURL: srv.URL, cacheSize: 2 << 20}
		a := New(NewClient(""))
		a.SetBackend(backend)
		tt := ui.NewTester(a.View, 1280, 860)
		waitText(t, tt, "首次设置")
		savePNG(t, tt, "setup")
	}

	{
		backend := &fakeBackend{
			serverURL: srv.URL, base: srv.URL,
			configured: true, cacheSize: 2 << 20,
		}
		a := New(NewClient(srv.URL))
		a.SetBackend(backend)
		tt := ui.NewTester(a.View, 1280, 860)
		waitText(t, tt, "示例 A")
		if err := tt.Click("设置"); err != nil {
			t.Fatal(err)
		}
		navSettle(tt)
		waitText(t, tt, "缓存占用")
		waitText(t, tt, "2.0 MB")
		savePNG(t, tt, "settings")
	}
}

// TestTagSettingsSnapshot captures the tag-rule page with a rule and the sync
// controls.
func TestTagSettingsSnapshot(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	a.tags.loaded = true
	a.tags.rules = []TagRule{
		{Tag: "female:yuri", Action: TagLike, Weight: 10},
		{Tag: "tag:netorare", Action: TagDislike, Weight: -10},
	}
	if err := tt.Click("标签设置"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "female:yuri")
	savePNG(t, tt, "tags")
}

// TestDarkSnapshots captures the same pages in the dark appearance, where the
// muted greys and cover shadows have to keep their contrast.
func TestDarkSnapshots(t *testing.T) {
	a, tt := newTestApp(t)
	tt.SetDark(true)
	waitText(t, tt, "示例 A")
	savePNG(t, tt, "home-dark")

	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	savePNG(t, tt, "gallery-dark")

	_ = a
}

// TestNarrowHomeSnapshot shows a full grid at the smallest window: the rows
// must not overlap when a narrow window makes each cover taller.
func TestNarrowHomeSnapshot(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	a.update(func() {
		s := a.lists["/"]
		base := append([]Gallery(nil), s.items...)
		for i := 0; i < 8; i++ {
			g := base[i%len(base)]
			g.ID, g.Title = int64(100+i), fmt.Sprintf("画廊 %d", i+1)
			s.items = append(s.items, g)
		}
	})
	tt.SetSize(960, 700)
	tt.Frame()
	savePNG(t, tt, "home-narrow")
}

// TestDownloadsSnapshot shows the download manager with a running job.
func TestDownloadsSnapshot(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	if err := tt.Click("下载管理"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "取消")
	savePNG(t, tt, "downloads")
	_ = a
}

// TestReaderSpreadSnapshot shows a two-page spread on the widest common
// window, where the page shadow doubles as the gutter between the pages.
func TestReaderSpreadSnapshot(t *testing.T) {
	a, tt := newTestApp(t)
	a.router.Push("/reader/1/tok1")
	navSettle(tt)
	waitText(t, tt, "第 1 / 3 页")
	a.update(func() {
		a.reader.settings.double = true
		a.reader.settings.hasCover = true
		a.reader.page = 1
		a.reader.resetView()
		a.closeOverlay(&a.reader)
	})
	tt.Frame()
	savePNG(t, tt, "reader-spread")
}

// TestReaderZoomSnapshot shows a zoomed page with the badge the hidden
// overlay leaves for getting back out.
func TestReaderZoomSnapshot(t *testing.T) {
	a, tt := newTestApp(t)
	a.router.Push("/reader/1/tok1")
	navSettle(tt)
	waitText(t, tt, "第 1 / 3 页")
	a.update(func() {
		a.reader.zoom = 2
		a.closeOverlay(&a.reader)
	})
	tt.Frame()
	savePNG(t, tt, "reader-zoom")
}

// TestReaderDarkSnapshot shows the black paper with the page shadow and the
// hint pill, which have to stay readable without a dark app theme.
func TestReaderDarkSnapshot(t *testing.T) {
	a, tt := newTestApp(t)
	a.router.Push("/reader/1/tok1")
	navSettle(tt)
	waitText(t, tt, "第 1 / 3 页")
	a.update(func() {
		a.reader.settings.dark = true
		a.closeOverlay(&a.reader)
	})
	tt.Frame()
	savePNG(t, tt, "reader-dark")
}

// TestReaderNarrowSnapshot shows the dock at the smallest window, where its
// controls have the least room.
func TestReaderNarrowSnapshot(t *testing.T) {
	a, tt := newTestApp(t)
	a.router.Push("/reader/1/tok1")
	navSettle(tt)
	waitText(t, tt, "第 1 / 3 页")
	tt.SetSize(960, 700)
	showReaderOverlay(t, a, tt)
	savePNG(t, tt, "reader-narrow")
}

// TestReaderFavoritesSnapshot shows kept pages in the menu, where the cells
// share the page list's grid and must not stretch across it.
func TestReaderFavoritesSnapshot(t *testing.T) {
	a, tt := newTestApp(t)
	a.router.Push("/reader/1/tok1")
	navSettle(tt)
	waitText(t, tt, "第 1 / 3 页")
	a.update(func() {
		a.reader.fav = map[int]bool{0: true, 2: true}
	})
	// The page list asks for the thumbnails the favorites view reuses.
	showReaderPanel(t, a, tt, panelPages)
	tt.Frame()
	time.Sleep(150 * time.Millisecond)
	tt.Frame()
	showReaderPanel(t, a, tt, panelFavorites)
	savePNG(t, tt, "reader-favorites")
}
