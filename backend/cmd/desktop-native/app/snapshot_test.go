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
	_, tt := newTestApp(t)

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
	waitText(t, tt, "1 / 3 页")
	savePNG(t, tt, "reader")

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
