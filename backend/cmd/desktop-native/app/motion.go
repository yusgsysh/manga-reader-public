package app

import (
	"time"

	"github.com/egoist/mygo/ui"
)

// Motion here is short and stays in place. A page fades in rather than sliding
// across: the layout is final in the first frame, so a control never moves out
// from under the pointer, and there is no second page to build while an offset
// travels. Hover lift and press scale are poses, which the renderer plays
// without rebuilding the view (see ui.Element.AnimatePose); only the frame the
// pointer moved in builds, the rest are paint-only. A navigation tint fades
// between its states instead of switching in one frame.
//
// The desktop's Reduce Motion preference still wins: mygo completes poses,
// transitions and colours at once, and keeps the fades in place.
var (
	// pageFade is how a page appears after the router switches. It is short
	// enough that a click on the new page never waits for it, and fades
	// instead of sliding so the page is at its final position throughout.
	pageFade = ui.Animation{Duration: 140 * time.Millisecond, Ease: ui.EaseOut}
	// tintFade colours a navigation item as it becomes chosen or hovered.
	tintFade = ui.Animation{Duration: 120 * time.Millisecond, Ease: ui.EaseOut}
	// hoverLift settles a card's cover after the pointer moves onto it.
	hoverLift = ui.Snappy
)

const (
	// overlayFade is how the reader's floating chrome appears over the page.
	overlayFade = 150 * time.Millisecond
)

// overlayEnter is where the reader's overlay comes from: transparent in its
// own place, so the controls are where they will stay while they fade in.
var overlayEnter = ui.Motion{Opacity: 0}

// overlayTransition fades the reader's chrome in place. It animates colours
// only on later changes, so opening a panel does not move the chrome around.
func overlayTransition() ui.ElementTransition {
	return ui.ElementTransition{
		Duration: overlayFade,
		Ease:     ui.EaseOut,
		Colors:   true,
		Enter:    &overlayEnter,
	}
}
