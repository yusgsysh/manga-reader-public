package app

import (
	"testing"
	"time"
)

// TestReaderStillStatesWakeAtTheirDeadline covers what keeps the reader's
// clock going. Motion (a turn sliding, the menu dropping in) needs a frame
// every frame; a still page with a deadline ahead (the overlay hiding, auto
// page turn's interval, a long press) needs one frame at that deadline, not
// one every time the display can show one; and a page with nothing pending
// needs none at all.
func TestReaderStillStatesWakeAtTheirDeadline(t *testing.T) {
	a := &App{}
	r := &a.reader
	r.pages = []string{"a", "b", "c"}
	r.page = 0
	r.loaded = true
	r.settings = defaultReaderSettings()

	// Nothing moving, nothing waiting.
	if a.readerTick(r) {
		t.Fatal("a still page asked for a frame to carry motion")
	}
	if at, ok := a.readerWakeAt(r); ok {
		t.Fatalf("a still page asked to be woken at %v", at)
	}

	// The overlay hides itself after its idle wait: a still overlay asks for
	// one frame at that moment, not a frame every time the display can show
	// one for the three seconds before it.
	r.overlay = true
	a.markActivity()
	if a.readerTick(r) {
		t.Fatal("a still overlay asked for every frame")
	}
	if at, ok := a.readerWakeAt(r); !ok || !at.Equal(a.overlayActivity.Add(overlayAutoHide)) {
		t.Fatalf("overlay wake = %v (%v), want %v", at, ok, a.overlayActivity.Add(overlayAutoHide))
	}

	// The dock's auto-turn line is drawn from the clock, so it does move
	// while auto page turn is on with the overlay up: that case keeps frames
	// going, as it did before.
	r.auto = true
	r.autoSince = time.Now()
	if !a.readerTick(r) {
		t.Fatal("the auto-turn line stopped asking for frames while it shows")
	}

	// With the overlay away nothing moves between turns: the next frame is
	// the one the interval is up at.
	r.overlay = false
	if a.readerTick(r) {
		t.Fatal("auto page turn asked for every frame while it waited")
	}
	if at, ok := a.readerWakeAt(r); !ok || !at.Equal(r.autoSince.Add(time.Duration(r.settings.autoInterval())*time.Millisecond)) {
		t.Fatalf("auto wake = %v (%v), want the interval's end", at, ok)
	}
	r.auto = false

	// The menu drops in over a few frames, then rests: once it has landed,
	// nothing asks to be woken for it.
	r.menuAt = time.Now()
	if !a.readerTick(r) {
		t.Fatal("the menu dropping in asked for no frames")
	}
	r.menuAt = time.Now().Add(-menuEnter)
	if a.readerTick(r) {
		t.Fatal("a settled menu asked for every frame")
	}
	if at, ok := a.readerWakeAt(r); ok {
		t.Fatalf("a settled menu asked to be woken at %v", at)
	}
	// Going back up is motion too.
	r.menuClosedAt = time.Now()
	if !a.readerTick(r) {
		t.Fatal("the menu going away asked for no frames")
	}
	r.menuClosedAt = time.Now().Add(-menuExit)
	if a.readerTick(r) {
		t.Fatal("a closed menu asked for every frame")
	}
	if !r.menuClosedAt.IsZero() {
		t.Fatal("the menu's exit did not finish")
	}

	// A held press fires when its delay is up.
	r.pressed = true
	r.longPress = time.Now()
	if a.readerTick(r) {
		t.Fatal("a held press asked for every frame")
	}
	if at, ok := a.readerWakeAt(r); !ok || !at.Equal(r.longPress.Add(longPressDelay)) {
		t.Fatalf("long press wake = %v (%v), want its delay's end", at, ok)
	}
	r.pressed = false
	r.longPress = time.Time{}

	// A turn in flight moves every frame.
	r.turnDir = groupNext
	r.turnFrom = a.shownGroup()
	r.turnAt = time.Now()
	if !a.readerTick(r) {
		t.Fatal("a turn in flight did not ask for the next frame")
	}
}
