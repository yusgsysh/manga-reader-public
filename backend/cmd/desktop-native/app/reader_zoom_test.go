package app

import (
	"testing"

	"github.com/egoist/mygo/ui"
)

// TestReaderZoomControls covers the dock's zoom buttons, the percentage that
// resets them, and the badge the hidden overlay leaves on a zoomed page.
func TestReaderZoomControls(t *testing.T) {
	a, tt := newTestApp(t)
	a.router.Push("/reader/1/tok1")
	navSettle(tt)
	waitReaderPage(t, tt, a, 0)
	showReaderOverlay(t, a, tt)

	if err := tt.Click("放大"); err != nil {
		t.Fatalf("click zoom in: %v", err)
	}
	if a.reader.zoom != 1+zoomStep {
		t.Fatalf("zoom = %v, want %v", a.reader.zoom, 1+zoomStep)
	}
	if !tt.HasText("125%") {
		t.Fatalf("dock does not show the zoom; texts=%q", tt.Texts())
	}

	if err := tt.Click("125%"); err != nil {
		t.Fatalf("click the percentage: %v", err)
	}
	if a.reader.zoom != 1 {
		t.Fatalf("zoom after reset = %v, want 1", a.reader.zoom)
	}

	// The keyboard takes the same steps, and 0 goes back to the page.
	tt.Key(0, ui.KeyEqual)
	tt.Frame()
	if a.reader.zoom != 1+zoomStep {
		t.Fatalf("zoom after = = %v, want %v", a.reader.zoom, 1+zoomStep)
	}
	tt.Key(0, ui.Key0)
	tt.Frame()
	if a.reader.zoom != 1 {
		t.Fatalf("zoom after 0 = %v, want 1", a.reader.zoom)
	}

	// With the overlay away the percentage stays on the page, and resets it.
	tt.Key(0, ui.KeyEqual)
	tt.Frame()
	a.update(func() { a.closeOverlay(&a.reader) })
	tt.Frame()
	// The chrome slides away first; the badge is what is left once it has.
	waitFor(t, tt, "the reader's chrome to finish closing", func() bool {
		return a.reader.overlayClosedAt.IsZero()
	})
	if !tt.HasText("125%") {
		t.Fatalf("hidden overlay leaves no zoom badge; texts=%q", tt.Texts())
	}
	if err := tt.Click("125%"); err != nil {
		t.Fatalf("click the badge: %v", err)
	}
	if a.reader.zoom != 1 {
		t.Fatalf("zoom after the badge = %v, want 1", a.reader.zoom)
	}
}
