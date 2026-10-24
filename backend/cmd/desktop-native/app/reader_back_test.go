package app

import (
	"testing"
)

// TestReaderBackLeavesTheGallery covers the reader's back going backwards in
// the history rather than pushing the gallery it came from. A push added an
// entry per visit, so Back from the reader landed on the gallery, Continue
// pushed the reader again, and leaving the two never ended: every Back found
// another gallery (or reader) the last visit had pushed.
func TestReaderBackLeavesTheGallery(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")

	// Out of the gallery into the reader and back twice: the history must stay
	// list → gallery → reader each time, not grow a pair per visit.
	for i := 0; i < 2; i++ {
		if err := tt.Click("开始阅读"); err != nil {
			t.Fatal(err)
		}
		navSettle(tt)
		if got := a.router.Path(); got != "/reader/1/tok1" {
			t.Fatalf("visit %d: path after 开始阅读 = %q, want /reader/1/tok1", i, got)
		}
		waitText(t, tt, "第 1 / 3 页")

		// The reader's overlay opens with the page; its back leaves the
		// reader. The overlay hides itself after a few idle seconds, so a
		// test puts it back up before reaching for it.
		showReaderOverlay(t, a, tt)
		if err := tt.Click("返回画廊"); err != nil {
			t.Fatal(err)
		}
		navSettle(tt)
		if got := a.router.Path(); got != "/gallery/1/tok1" {
			t.Fatalf("visit %d: path after the reader's Back = %q, want the gallery", i, got)
		}
	}

	// The gallery's Back then leaves for the list the visit started at: with
	// the reader pushing its gallery, this was another pushed gallery.
	if err := tt.Click("← 返回"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	if got := a.router.Path(); got != "/" {
		t.Fatalf("path after the gallery's Back = %q, want /", got)
	}
	if a.router.CanGoBack() {
		t.Errorf("history still has entries to go back to after leaving the gallery")
	}
}
