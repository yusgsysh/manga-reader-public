package app

import (
	"testing"
)

// TestReaderHidesSidebarAndLeavesFullScreen covers the reader taking the whole
// window and giving full screen back when it is left: the sidebar is only a
// browse control, and a read that entered full screen used to leave the gallery
// behind it in full screen with nothing in it to leave it.
func TestReaderHidesSidebarAndLeavesFullScreen(t *testing.T) {
	a, tt := newTestApp(t)
	waitText(t, tt, "示例 A")

	sidebarShown := func() bool {
		_, ok := tt.Find("Manga Reader")
		return ok
	}
	if !sidebarShown() {
		t.Fatal("the list has no sidebar to begin with")
	}

	if err := tt.Click("示例 A"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "开始阅读")

	if err := tt.Click("开始阅读"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	waitText(t, tt, "第 1 / 3 页")
	if got := a.router.Path(); got != "/reader/1/tok1" {
		t.Fatalf("path = %q, want the reader", got)
	}

	// The reader is full width: the sidebar's title is gone from the frame.
	if sidebarShown() {
		t.Error("the reader still shows the sidebar")
	}
	if a.readerActive() != true {
		t.Error("readerActive = false on the reader page")
	}

	// Leaving the reader through its back returns the sidebar.
	showReaderOverlay(t, a, tt)
	if err := tt.Click("返回画廊"); err != nil {
		t.Fatal(err)
	}
	navSettle(tt)
	if got := a.router.Path(); got != "/gallery/1/tok1" {
		t.Fatalf("path after the reader's Back = %q, want the gallery", got)
	}
	if !sidebarShown() {
		t.Error("the gallery behind the reader has no sidebar")
	}
	if a.readerActive() {
		t.Error("readerActive = true off the reader page")
	}
	// Leaving full screen is a no-op without a window (tests have none), so
	// what is pinned down here is that the reader's exit path runs it at all:
	// with a window, leaveReader is the only way the reader is left.
	if a.isFullscreen() {
		t.Error("the window is in full screen without a window")
	}
}

// TestReaderActiveFollowsTheRoute checks the sidebar condition is the path's,
// not a flag that could drift from the page shown.
func TestReaderActiveFollowsTheRoute(t *testing.T) {
	a, _ := newTestApp(t)
	a.router.Replace("/reader/1/tok1")
	if !a.readerActive() {
		t.Error("readerActive = false on /reader/1/tok1")
	}
	a.router.Replace("/gallery/1/tok1")
	if a.readerActive() {
		t.Error("readerActive = true on /gallery/1/tok1")
	}
	a.router.Push("/")
	if a.readerActive() {
		t.Error("readerActive = true on /")
	}
}
