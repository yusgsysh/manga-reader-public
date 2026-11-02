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
// between its states instead of switching in one frame. The reader's floating
// chrome is the exception that slides: it comes in from its edge and goes back
// out (chromeEnter), as comimi's dock and chevrons do.
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
	// chromeEnter, chromeExit and chromeFade are how the reader's floating
	// cards come in and go out, after comimi's controls dock and chevrons:
	// they slide a short way from their edge over a spring and fade on the
	// way. The exit is quicker, as closing a menu should be.
	chromeEnter = 420 * time.Millisecond
	chromeExit  = 260 * time.Millisecond
	chromeFade  = 200 * time.Millisecond
	// chromeSlide is how far a card starts from its place, in DIPs: comimi
	// moves the dock, the pill and the chevrons 35 points.
	chromeSlide float32 = 35
	// menuEnter and menuExit are how long the menu card takes to drop in from
	// the pill and to go back up, after comimi's menu panel.
	menuEnter = 360 * time.Millisecond
	menuExit  = 240 * time.Millisecond
	// menuSlide is how far above its place the menu card starts, in DIPs.
	menuSlide float32 = 16
)

// chromeEase is comimi's spring, cubic-bezier(.34, 1.56, .64, 1): it settles
// with the small overshoot that makes a card look placed rather than stopped.
func chromeEase(t float32) float32 {
	return cubicBezierEase(t, 0.34, 1.56, 0.64, 1)
}

// stageEase is CSS's ease, cubic-bezier(.25, .1, .25, 1), which comimi's page
// stage uses for a turn and for a drag returning to rest.
func stageEase(t float32) float32 {
	return cubicBezierEase(t, 0.25, 0.1, 0.25, 1)
}

// cubicBezierEase solves a CSS cubic-bezier timing function at x = t and
// returns its y. Newton's method finds the curve's parameter, as a browser
// does; t outside 0..1 is at the ends.
func cubicBezierEase(t, x1, y1, x2, y2 float32) float32 {
	if t <= 0 {
		return 0
	}
	if t >= 1 {
		return 1
	}
	u := t
	for i := 0; i < 6; i++ {
		dx := bezierAt(u, x1, x2) - t
		if absFloat(dx) < 0.0001 {
			break
		}
		slope := bezierSlope(u, x1, x2)
		if slope == 0 {
			break
		}
		u = clampFloat(u-dx/slope, 0, 1)
	}
	return bezierAt(u, y1, y2)
}

// bezierAt is one axis of a cubic Bezier with its ends at 0 and 1.
func bezierAt(u, p1, p2 float32) float32 {
	v := 1 - u
	return 3*v*v*u*p1 + 3*v*u*u*p2 + u*u*u
}

// bezierSlope is the derivative of bezierAt with respect to u.
func bezierSlope(u, p1, p2 float32) float32 {
	v := 1 - u
	return 3*v*v*p1 + 6*v*u*(p2-p1) + 3*u*u*(1-p2)
}
