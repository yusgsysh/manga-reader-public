//go:build snapshot

package app

import (
	"image/png"
	"os"
	"testing"

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
