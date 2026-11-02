package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestCardHoverPaintsWithoutRebuilding pins the cost of the hover lift: the
// pointer moving onto a card rebuilds the frame that reads Hovered, and the
// spring that follows is a pose, which the renderer plays in paint-only
// frames. A grid crossing of dozens of cards therefore does not rebuild the
// whole page for every frame of every spring.
func TestCardHoverPaintsWithoutRebuilding(t *testing.T) {
	srv := newFakeServer(t)
	a := New(NewClient(srv.URL))
	builds := 0
	tt := ui.NewTester(func(c *ui.Context) {
		builds++
		a.View(c)
	}, 1280, 860)
	waitText(t, tt, "示例 A")

	at, ok := tt.Find("示例 A")
	if !ok {
		t.Fatalf("the first card is missing; texts=%q", tt.Texts())
	}
	tt.Move(at.X+at.W/2, at.Y+at.H/2)
	tt.Frame()
	if !tt.FramePending() {
		t.Fatal("the hover lift did not ask for frames to settle")
	}

	// The frames of the spring between here and its end are painted, not
	// built: the view is asked for a frame, not rebuilt for each one.
	built := builds
	tt.Advance(250 * time.Millisecond)
	if builds > built {
		t.Fatalf("the hover spring rebuilt the view %d times", builds-built)
	}
}
