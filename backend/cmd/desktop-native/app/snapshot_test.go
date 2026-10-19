//go:build snapshot

package app

import (
	"image/png"
	"os"
	"testing"
)

// TestSnapshots writes the main pages to /tmp/opencode for visual checks.
// It is a development aid; the assertions live in app_test.go.
func TestSnapshots(t *testing.T) {
	_, tt := newTestApp(t)
	save := func(name string) {
		f, err := os.Create("/tmp/opencode/" + name + ".png")
		if err != nil {
			t.Fatal(err)
		}
		defer f.Close()
		if err := png.Encode(f, tt.Image()); err != nil {
			t.Fatal(err)
		}
	}

	waitText(t, tt, "示例 A")
	save("home")

	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")
	save("gallery")

	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "1 / 3 页")
	save("reader")

	if err := tt.Click("搜索"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	tt.Type("B")
	waitText(t, tt, "共 1 个结果")
	save("search")
}
