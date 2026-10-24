package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// openReaderForTest walks from the list into a gallery and its reader, the way
// a visitor does, and returns with the first page shown.
func openReaderForTest(t *testing.T) (*App, *ui.Tester) {
	t.Helper()
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
	waitText(t, tt, "第 1 / 3 页")
	return a, tt
}

// TestReaderOverlayAutoHides covers the overlay's clock: it stays while it is
// used and puts itself away after three seconds without a press, as comimi's
// auto-hide does.
func TestReaderOverlayAutoHides(t *testing.T) {
	a, tt := openReaderForTest(t)
	showReaderOverlay(t, a, tt)
	if !a.reader.overlay {
		t.Fatal("the overlay is not up")
	}

	// A press over the controls restarts the clock; the overlay stays.
	tt.ClickAt(640, 400)
	if a.reader.overlay {
		t.Fatal("a tap in the middle left the overlay up")
	}

	// The clock is read before the frame changes anything, so an overlay idle
	// for longer than the wait goes without another press.
	a.update(func() {
		a.reader.overlay = true
		a.reader.panel = panelNone
		a.overlayActivity = time.Now().Add(-overlayAutoHide - 100*time.Millisecond)
	})
	tt.Frame()
	if a.reader.overlay {
		t.Fatal("the overlay stayed after the idle wait")
	}
}

// TestReaderAutoTurnAdvances covers auto page turn: it moves on when the
// interval has run, and stops itself at the last page, as comimi's timer does.
func TestReaderAutoTurnAdvances(t *testing.T) {
	a, tt := openReaderForTest(t)
	a.update(func() {
		a.reader.overlay = false
		a.reader.settings.autoIntervalMS = 3000
		a.setAuto(&a.reader, true)
	})
	if !a.reader.auto {
		t.Fatal("auto page turn did not start")
	}
	waitText(t, tt, "已开始自动播放")

	// The interval has run: the next frame turns the page.
	a.update(func() { a.reader.autoSince = time.Now().Add(-4 * time.Second) })
	tt.Frame()
	if a.reader.page != 1 {
		t.Fatalf("page = %d after the interval, want 1", a.reader.page)
	}

	// The last page has nowhere to go: auto page turn stops by itself.
	a.update(func() {
		a.goToPage(&a.reader, 2)
		a.setAuto(&a.reader, true)
		a.reader.autoSince = time.Now().Add(-4 * time.Second)
	})
	tt.Frame()
	if a.reader.page != 2 {
		t.Fatalf("page = %d, want to stay on 2", a.reader.page)
	}
	if a.reader.auto {
		t.Fatal("auto page turn ran on past the last page")
	}
}

// TestReaderTurnSlidesBeforeItLands covers the turn animation: a swipe leaves
// the page it was on until the slide has run, then lands on the next.
func TestReaderTurnSlidesBeforeItLands(t *testing.T) {
	a, tt := openReaderForTest(t)
	a.update(func() { a.reader.overlay = false })

	tt.Press(400, 400)
	tt.Move(560, 402)
	tt.Move(700, 405)
	tt.Release(700, 405)
	if a.reader.turnDir == groupCurrent {
		t.Fatal("a swipe did not start a turn")
	}
	if a.reader.page != 0 {
		t.Fatalf("page = %d in flight, want to still be 0", a.reader.page)
	}

	// The slide has run: the next frame lands the turn.
	a.update(func() { a.reader.turnAt = time.Now().Add(-turnAnimation - 20*time.Millisecond) })
	tt.Frame()
	if a.reader.turnDir != groupCurrent {
		t.Fatal("the turn did not land")
	}
	if a.reader.page != 1 {
		t.Fatalf("page = %d after the turn, want 1", a.reader.page)
	}
}

// TestReaderPinchZooms covers a pinch: it zooms about the pointer, and it
// stops at the whole page zooming out, as comimi's pinch does.
func TestReaderPinchZooms(t *testing.T) {
	a, tt := openReaderForTest(t)
	a.update(func() { a.reader.overlay = false })
	tt.Frame()

	tt.Pinch(640, 430, 2) // the middle of the 1280x860 window
	if a.reader.zoom != 2 {
		t.Fatalf("zoom = %v after a pinch of 2, want 2", a.reader.zoom)
	}
	// The point under the pointer stays put: pinching at the middle of the
	// page leaves the page where it was.
	if a.reader.panX != 0 || a.reader.panY != 0 {
		t.Fatalf("pan = (%v, %v) about the centre, want none", a.reader.panX, a.reader.panY)
	}

	// Off centre, the page pans toward the pointer, kept within its bounds.
	tt.Pinch(1000, 700, 2)
	if a.reader.zoom != 4 {
		t.Fatalf("zoom = %v after another pinch of 2, want 4", a.reader.zoom)
	}
	if a.reader.panX >= 0 || a.reader.panY >= 0 {
		t.Fatalf("pan = (%v, %v) after pinching right and low, want it moved that way",
			a.reader.panX, a.reader.panY)
	}
	if maxX := 1280 * (a.reader.zoom - 1) / 2; a.reader.panX < -maxX {
		t.Fatalf("panX = %v, past the page's edge at %v", a.reader.panX, -maxX)
	}

	// Zooming out stops at the whole page.
	tt.Pinch(640, 430, 0.1)
	if a.reader.zoom != 1 {
		t.Fatalf("zoom = %v after pinching out, want 1", a.reader.zoom)
	}
	if a.reader.panX != 0 || a.reader.panY != 0 {
		t.Fatalf("pan = (%v, %v) at the whole page, want none", a.reader.panX, a.reader.panY)
	}
}

// TestReaderLongPressKeepsThePage covers the "keep this page" gesture: a press
// held on a page keeps it, and a press that wanders before its time does not.
func TestReaderLongPressKeepsThePage(t *testing.T) {
	a, tt := openReaderForTest(t)
	a.update(func() { a.reader.overlay = false })
	tt.Frame()

	tt.Press(640, 400)
	a.update(func() { a.reader.longPress = time.Now().Add(-longPressDelay - 100*time.Millisecond) })
	tt.Frame()
	if !a.reader.fav[0] {
		t.Fatalf("page 0 not kept; kept=%v", a.reader.fav)
	}
	tt.Release(640, 400)
	waitText(t, tt, "已收藏第 1 页")

	// A drag drops the press before its time runs: the page is not kept.
	a.update(func() { a.reader.fav = map[int]bool{} })
	tt.Press(640, 400)
	a.update(func() { a.reader.longPress = time.Now().Add(-longPressDelay - 100*time.Millisecond) })
	tt.Move(700, 400)
	tt.Frame()
	if a.reader.fav[0] {
		t.Fatal("a drag kept the page")
	}
	tt.Release(700, 400)
}

// TestReaderEscapeGivesBackOneLayer covers Escape backing out a layer at a
// time: the panel, then the overlay, then the reader.
func TestReaderEscapeGivesBackOneLayer(t *testing.T) {
	a, tt := openReaderForTest(t)
	showReaderPanel(t, a, tt, panelSettings)
	if a.reader.panel != panelSettings {
		t.Fatal("the settings sheet is not open")
	}

	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.reader.panel != panelNone || a.reader.overlay {
		t.Fatalf("after one Escape: panel=%v overlay=%v, want the overlay gone",
			a.reader.panel, a.reader.overlay)
	}
	if got := a.router.Path(); got != "/reader/1/tok1" {
		t.Fatalf("path after the first Escape = %q, want to still be in the reader", got)
	}

	// The overlay is up with no panel: the next Escape puts it away.
	showReaderOverlay(t, a, tt)
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	if a.reader.overlay {
		t.Fatal("the second Escape left the overlay up")
	}

	// Nothing left to give back: the reader itself goes.
	tt.Key(0, ui.KeyEscape)
	tt.Frame()
	waitFor(t, tt, "the reader to close", func() bool {
		return a.router.Path() != "/reader/1/tok1"
	})
}
