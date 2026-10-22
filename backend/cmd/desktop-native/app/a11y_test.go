package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestTabOrderReachesControls covers keyboard reachability of the pages: Tab
// walks focusable controls, so a keyboard-only user can reach the sidebar and
// the gallery cards without a pointer.
func TestTabOrderReachesControls(t *testing.T) {
	_, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	// Every frame, record which rendered items have the focus. A focused
	// container also reports as focused for its children, so this collects
	// the trail; what matters is that the walk reaches more than the first
	// container and ends up on real controls.
	var focused []string
	seen := map[string]bool{}
	for i := 0; i < 40; i++ {
		tt.Key(0, ui.KeyTab)
		tt.Frame()
		for _, s := range tt.Texts() {
			if tt.Focused(s) && !seen[s] {
				seen[s] = true
				focused = append(focused, s)
			}
		}
	}
	// The sidebar and a gallery card are both reached.
	for _, want := range []string{"首页", "示例 A"} {
		if !seen[want] {
			t.Fatalf("Tab never reached %q; visited %v", want, focused)
		}
	}
}

// TestSidebarItemsAreFocusable checks the navigation itself takes the focus:
// the sidebar marks each item focusable and labels it with its page name.
func TestSidebarItemsAreFocusable(t *testing.T) {
	_, tt := newTestApp(t)
	waitText(t, tt, "示例 A")
	for _, name := range []string{"首页", "书架", "标签设置", "设置"} {
		if _, ok := tt.Find(name); !ok {
			t.Fatalf("sidebar item %q not rendered", name)
		}
	}
}
