package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// readerPage is the reader's page: the colour the pages are read on, the
// stage with the pages, and the overlay over it.
func (a *App) readerPage(c *ui.Context, id int64, token string, offline bool) {
	a.ensureReader(id, token, offline)
	r := &a.reader
	// The stream has ended: cut the page list down to the pages that arrived,
	// so the seek bar and the previews count what can be read.
	r.trimSlots()
	// A spread needs the width two pages deserve; a narrow window shows one
	// at a time, as comimi does on a phone-width viewport.
	w, _ := c.Size()
	r.narrow = w < readerSpreadMinWidth

	// A thumbnail click on the detail page asks for a particular page; apply
	// it as soon as that page has streamed in, overriding the saved progress.
	// Remembering it keeps the stream's later result from restoring the saved
	// page on top.
	if a.readerStart >= 0 {
		r.startPage = a.readerStart
		a.readerStart = -1
		if r.hasPage(r.startPage) {
			r.page = r.startPage
			r.resetView()
			a.saveProgress(r)
		}
	}

	a.readerKeys(c, r)

	if r.loaded {
		a.preloadPages(r)
	}
	// Everything that moves on its own asks for the frames it needs: a turn
	// sliding out, auto page turn, the overlay's auto-hide, a toast.
	if a.readerTick(r) {
		a.keepFrames()
	}

	bg := readerPaper(r)
	ui.Box(c).Fill().Background(bg).Clip().Children(func() {
		switch {
		case r.loading && !r.loaded:
			ui.Column(c).Fill().Center().Gap(10).Children(func() {
				pageLoading(c, i18n.T("Loading pages…"))
				ui.Text(c, i18n.T("Loading pages…")).FontSize(12).TextColor(readerMuted(r))
			})
		case r.err != "" && !r.loaded:
			a.errorBlock(c, r.err, func() { a.retryReader(r) })
		case !r.loaded || len(r.pages) == 0:
			a.emptyPage(c, i18n.T("No pages"), i18n.T("This gallery has no readable pages"), "")
		default:
			a.readerStageView(c, r)
			if r.overlay {
				a.readerOverlay(c, r)
			} else {
				a.readerHint(c, r)
			}
		}
		if r.toast != "" {
			a.readerToast(c, r)
		}
	})
}

// readerPaper is the colour the pages sit on: the reader's own white or
// black, which follows the setting rather than the app's theme, as comimi's
// backgroundColor does.
func readerPaper(r *Reader) ui.Color {
	if r.settings.dark {
		return ui.RGB(16, 16, 18)
	}
	return ui.RGB(246, 246, 247)
}

// readerInk is text on the reader's paper.
func readerInk(r *Reader) ui.Color {
	if r.settings.dark {
		return ui.RGB(236, 236, 238)
	}
	return ui.RGB(28, 28, 30)
}

// readerMuted is secondary text on the reader's paper.
func readerMuted(r *Reader) ui.Color {
	if r.settings.dark {
		return ui.RGB(150, 150, 156)
	}
	return ui.RGB(120, 120, 126)
}

// readerOverlaySurface is the glass the overlay's panels are made of, which
// the reader draws itself: a dark pane over the page in either paper, so the
// interface reads over a white or a black page alike.
func readerOverlaySurface(alpha float32) ui.Color {
	return ui.RGBA(18, 18, 20, alpha)
}

// readerStageView draws the pages and takes the gestures that turn them.
func (a *App) readerStageView(c *ui.Context, r *Reader) {
	st := a.stageOf(c)
	stage := ui.Box(c).Fill().Clip().Focusable()
	stage.HandleInput(func(ev ui.InputEvent) bool {
		return a.readerInput(r, ev, st.width, st.height)
	})
	stage.Children(func() {
		// One screen per group, placed by the group's offset. The shown
		// group alone is zoomed, with the neighbours waiting off screen.
		for _, spec := range st.groups {
			offset := float32(a.groupOffsetX(spec)) * st.width
			if st.offsets != nil {
				if x, ok := st.offsets[spec.placement]; ok {
					offset = x
				}
			}
			offset += st.dragX
			zoom, panX, panY := float32(1), float32(0), float32(0)
			shown := spec.placement == groupCurrent
			if st.turn && sameGroup(spec.pages, st.turnIn) {
				shown = true
			}
			if shown {
				zoom, panX, panY = st.zoom, st.panX, st.panY
			}
			box := ui.Box(c).Absolute().Width(st.width).Height(st.height).
				Translate(offset, 0)
			box.Children(func() {
				a.readerGroupView(c, spec.pages, zoom, panX, panY)
			})
		}
	})
}

// sameGroup reports whether two groups show the same pages.
func sameGroup(x, y pageGroup) bool {
	if len(x.indexes) == 0 || len(x.indexes) != len(y.indexes) {
		return false
	}
	for i := range x.indexes {
		if x.indexes[i] != y.indexes[i] {
			return false
		}
	}
	return true
}

// readerGroupView draws a group's pages side by side, as they pair up.
func (a *App) readerGroupView(c *ui.Context, g pageGroup, zoom, panX, panY float32) {
	ui.Row(c).Fill().Children(func() {
		for _, page := range g.indexes {
			grow := 0
			if len(g.indexes) > 1 {
				grow = 1
			}
			a.pageImage(c, page, grow, zoom, panX, panY)
		}
	})
}

// readerHint is the one line left on the page while the overlay is away: where
// the reader is, faint at the bottom, as a viewer that keeps out of the way
// does. While the page list still streams it says how much has arrived, so a
// big gallery looks like it is loading rather than stuck.
func (a *App) readerHint(c *ui.Context, r *Reader) {
	if !r.loaded || len(r.pages) == 0 {
		return
	}
	// The line takes no press, and no room: it lies over the pages rather
	// than beside them, so the stage keeps the whole window and a tap at the
	// bottom of the page is still a tap on the page.
	ui.Column(c).Fill().Absolute().Justify(ui.End).AlignItems(ui.Center).PassThrough().
		Padding(0, 0, 8, 0).Children(func() {
		ui.Text(c, a.readerCountText(r, true)).FontSize(11).TextColor(readerMuted(r)).PassThrough()
	})
}

// readerCountText is the page count the hints and the dock show: a spread's
// range, and how much of a streamed list has arrived (see readerStreamText).
func (a *App) readerCountText(r *Reader, withStream bool) string {
	if withStream && !r.complete {
		return a.readerStreamText(r)
	}
	g := a.shownGroup()
	if a.reader.narrow || !r.settings.double {
		return i18n.Sprintf(i18n.T("Page %d / %d"), pageGroupFirst(g)+1, len(r.pages))
	}
	return i18n.Sprintf(i18n.T("Pages %s / %d"), pageGroupText(g), len(r.pages))
}

// readerStreamText says how many pages of a still-streaming list have arrived,
// so a big gallery does not look stuck while upstream scrapes it.
func (a *App) readerStreamText(r *Reader) string {
	if r.total > 0 && r.partial < r.total {
		return i18n.Sprintf(i18n.T("%d / %d pages"), r.partial, r.total)
	}
	return a.readerCountText(r, false)
}

// readerKeys takes the reader's keyboard, as comimi's shortcuts do: arrows and
// space move by side and follow the reading direction, letters toggle the
// view, and escape backs out one step at a time.
func (a *App) readerKeys(c *ui.Context, r *Reader) {
	// Escape closes the panel, then the overlay, then the reader itself:
	// each press gives back one layer, so a reader never lands somewhere
	// they did not mean to go.
	if c.Shortcut(0, ui.KeyEscape) {
		switch {
		case r.panel != panelNone:
			a.closeOverlay(r)
		case r.overlay:
			a.closeOverlay(r)
		default:
			a.leaveReader(r)
		}
	}
	if !r.loaded {
		if c.Shortcut(0, ui.KeyF) {
			a.toggleFullscreen()
		}
		return
	}
	// ← and space move left, → and shift+space right; right-to-left swaps
	// which way the pages go.
	if c.Shortcut(0, ui.KeyLeft) || c.Shortcut(0, ui.KeySpace) {
		a.moveFromSide(r, sideLeft)
	}
	if c.Shortcut(0, ui.KeyRight) || c.Shortcut(ui.Shift, ui.KeySpace) {
		a.moveFromSide(r, sideRight)
	}
	if c.Shortcut(0, ui.KeyA) {
		a.setAuto(r, !r.auto)
	}
	if c.Shortcut(0, ui.KeyP) {
		a.setSpread(r, !r.settings.double)
	}
	if c.Shortcut(0, ui.KeyC) {
		a.setCover(r, !r.settings.hasCover)
	}
	if c.Shortcut(0, ui.KeyF) {
		a.toggleFullscreen()
	}
	// Escape went above; N and W return to the plain window view, as
	// comimi's layout modes do.
	if c.Shortcut(0, ui.KeyN) {
		a.exitFullscreen()
	}
	if c.Shortcut(0, ui.KeyM) {
		a.togglePanel(r, panelMenu)
	}
	if c.Shortcut(0, ui.KeyO) {
		if r.overlay {
			a.closeOverlay(r)
		} else {
			a.markActivity()
			r.overlay = true
			r.guide = false
		}
	}
	if c.Shortcut(0, ui.KeyS) {
		a.togglePanel(r, panelSettings)
	}
}

// moveFromSide turns a page as a move to the left or right side asks.
func (a *App) moveFromSide(r *Reader, side pageGroupSide) {
	goNext := (side == sideRight) != r.settings.rtl
	if goNext {
		a.readerNext(r)
	} else {
		a.readerPrev(r)
	}
}

// markActivity restarts the overlay's auto-hide clock.
func (a *App) markActivity() { a.overlayActivity = time.Now() }

// readerInput takes the pointer over the page, as comimi's renderer does.
func (a *App) readerInput(r *Reader, ev ui.InputEvent, w, h float32) bool {
	switch ev.Kind {
	case ui.InputPointerDown:
		a.markActivity()
		if r.turnDir != groupCurrent {
			return true
		}
		// A press on a page starts the long-press that favorites it.
		a.beginLongPress(r, ev.X, ev.Y)
		r.pressed = true
		r.moved = false
		r.downX, r.downY = ev.X, ev.Y
		r.startPanX, r.startPanY = r.panX, r.panY
		return true
	case ui.InputPointerMove:
		a.markActivity()
		if !r.pressed {
			return false
		}
		if absFloat(ev.X-r.downX) > dragSlop || absFloat(ev.Y-r.downY) > dragSlop {
			r.moved = true
			a.cancelLongPress(r)
		}
		if !r.moved {
			return true
		}
		dx, dy := ev.X-r.downX, ev.Y-r.downY
		if r.zoom > 1 {
			// Zoomed in, a drag pans the page, kept within its bounds.
			r.panX, r.panY = clampPan(r, r.startPanX+dx, r.startPanY+dy, w, h)
			return true
		}
		r.drag = a.constrainDrag(r, dx, dy, w)
		return true
	case ui.InputPointerUp:
		a.markActivity()
		a.cancelLongPress(r)
		if !r.pressed {
			return false
		}
		r.pressed = false
		if !r.moved {
			a.readerTap(r, ev.X, w)
			return true
		}
		a.readerSwipeEnd(r, ev.X-r.downX, ev.Y-r.downY)
		return true
	case ui.InputZoom:
		// A pinch on a touchpad, at the pointer, as comimi's pinch does.
		a.markActivity()
		a.zoomAt(r, ev.X, ev.Y, r.zoom*ev.Scale, w, h)
		return true
	case ui.InputScroll:
		// A wheel or a touchpad zooms only with Ctrl/⌘ held, as comimi does;
		// plain scrolling over a manga does not turn its pages.
		if ev.Mods&ui.Ctrl == 0 && ev.Mods&ui.Cmd == 0 && !(ev.Precise && r.zoom > 1) {
			return false
		}
		a.markActivity()
		factor := float32(1) + ev.DY*-0.002
		if factor > 1+zoomStep {
			factor = 1 + zoomStep
		}
		if factor < 1/(1+zoomStep) {
			factor = 1 / (1 + zoomStep)
		}
		a.zoomAt(r, ev.X, ev.Y, r.zoom*factor, w, h)
		return true
	}
	return false
}

// readerTap handles a click on the page: the outer thirds turn the page while
// the overlay is away, and the middle shows or hides the overlay.
func (a *App) readerTap(r *Reader, x, w float32) {
	if r.turnDir != groupCurrent || w <= 0 {
		return
	}
	ratio := x / w
	if !r.overlay {
		if ratio < tapZoneEdge {
			if r.settings.rtl {
				a.readerNext(r)
			} else {
				a.readerPrev(r)
			}
			return
		}
		if ratio >= 1-tapZoneEdge {
			if r.settings.rtl {
				a.readerPrev(r)
			} else {
				a.readerNext(r)
			}
			return
		}
		// The middle: show the overlay again, with the guide gone.
		r.overlay = true
		r.guide = false
		a.markActivity()
		return
	}
	// The overlay is up and the tap missed its controls: put it away.
	a.closeOverlay(r)
}

// readerSwipeEnd turns the page when a drag was a swipe: far enough across,
// mostly across, and not zoomed in, where a drag is a pan instead.
func (a *App) readerSwipeEnd(r *Reader, dx, dy float32) {
	r.drag = 0
	if r.zoom > 1 || absFloat(dx) <= swipeThreshold || absFloat(dx) <= absFloat(dy) {
		return
	}
	// Right-to-left: dragging right brings the next page.
	toNext := dx > 0
	if !r.settings.rtl {
		toNext = dx < 0
	}
	dir := groupPrevious
	if toNext {
		dir = groupNext
	}
	// A swipe animates its turn; a key or a click does not, as comimi's
	// gestures and its buttons differ.
	a.turnPage(r, dir, true)
}

// constrainDrag keeps a swipe from pulling the page off into nothing: past the
// end of the gallery, or against the reading direction's edge, the drag gives
// way with a resistance instead of following the pointer.
func (a *App) constrainDrag(r *Reader, dx, dy, w float32) float32 {
	if absFloat(dy) > absFloat(dx) {
		return 0
	}
	toNext := dx > 0
	if !r.settings.rtl {
		toNext = dx < 0
	}
	dir := groupPrevious
	if toNext {
		dir = groupNext
	}
	if len(a.adjacentGroup(dir).indexes) == 0 {
		return edgeResistance(dx, w)
	}
	return dx
}

// edgeResistance is how far a drag goes when there is nothing to turn to: a
// fraction of the pull, tapering off, as comimi's rubber band.
func edgeResistance(dx, w float32) float32 {
	direction := float32(1)
	if dx < 0 {
		direction = -1
	}
	distance := absFloat(dx)
	maxPull := w * 0.22
	if maxPull > 140 {
		maxPull = 140
	}
	if maxPull <= 0 {
		return 0
	}
	// 1-e^(-d/max) approaches one: the pull approaches maxPull.
	resisted := maxPull * (1 - expNeg(distance/maxPull))
	return direction * resisted
}

// expNeg is e^-x for x >= 0, by a series, so no math import is needed for the
// one place the reader uses it.
func expNeg(x float32) float32 {
	const terms = 12
	sum, term := float32(1), float32(1)
	for i := 1; i <= terms; i++ {
		term *= -x / float32(i)
		sum += term
	}
	if sum < 0 {
		return 0
	}
	return sum
}

// clampPan keeps the zoomed page within its own bounds: at zoom z the page is
// z times the area, so it may move half the overflow either way.
func clampPan(r *Reader, x, y, w, h float32) (float32, float32) {
	if r.zoom <= 1 {
		return 0, 0
	}
	maxX := w * (r.zoom - 1) / 2
	maxY := h * (r.zoom - 1) / 2
	return clampFloat(x, -maxX, maxX), clampFloat(y, -maxY, maxY)
}

// zoomAt zooms to scale at the point (x, y), keeping the point under the
// pointer where it is, as comimi's cursor-centered zoom does.
func (a *App) zoomAt(r *Reader, x, y, scale, w, h float32) {
	scale = clampFloat(scale, minZoom, maxZoom)
	if scale == r.zoom {
		return
	}
	ratio := scale / r.zoom
	cx, cy := x-w/2, y-h/2
	r.panX = r.panX*ratio + cx*(1-ratio)
	r.panY = r.panY*ratio + cy*(1-ratio)
	r.zoom = scale
	r.panX, r.panY = clampPan(r, r.panX, r.panY, w, h)
}

// beginLongPress arms the long press that favorites a page, as comimi's
// "ここすき!" gesture does: hold on a page for a moment and it is kept.
func (a *App) beginLongPress(r *Reader, x, y float32) {
	a.cancelLongPress(r)
	r.longPress = time.Now()
	r.longPressX, r.longPressY = x, y
	shown := a.shownGroup()
	r.longPressPage = -1
	if len(shown.indexes) == 1 {
		r.longPressPage = shown.indexes[0]
	}
}

// cancelLongPress drops a pending long press.
func (a *App) cancelLongPress(r *Reader) { r.longPress = time.Time{} }

// longPressDue fires a pending long press once its time has run, if the press
// has not wandered.
func (a *App) longPressDue(r *Reader) bool {
	if r.longPress.IsZero() || !r.pressed {
		return false
	}
	if time.Since(r.longPress) < longPressDelay {
		return false
	}
	page := r.longPressPage
	a.cancelLongPress(r)
	if page < 0 || !r.hasPage(page) {
		return true
	}
	a.toggleFavorite(r, page)
	return true
}
