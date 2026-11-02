package app

import (
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// The overlay is comimi's controls: floating glass cards over the page rather
// than a bar across it. The pill at the top left holds the title and opens the
// menu, chevrons at the sides turn the page, and the dock at the bottom carries
// the seek bar, auto page turn, the page mode, the view and the settings. A
// tap on the page shows or hides all of it.

// readerOverlay draws the overlay over the page.
func (a *App) readerOverlay(c *ui.Context, r *Reader) {
	look := readerLookOf(r)
	// The chrome comes in from its edge and leaves the same way, as comimi's
	// dock and chevrons do: transform and opacity are worked out from the
	// clock, so a card is where the pointer sees it while it moves, without
	// rebuilding what is inside it. While it goes, it takes no press, so a
	// tap lands on the page underneath it.
	slide, alpha := chromeMotion(r, time.Now())
	frame := ui.Box(c).Absolute().Left(0).Top(0).Right(0).Bottom(0).
		PassThrough().Key("reader-overlay").IgnoreInput(!r.overlay)
	frame.Children(func() {
		// The menu and the settings are cards of their own; the pill stays for
		// the settings so the title and the way to the menu never move.
		if !r.panel.isMenu() {
			a.readerPill(c, r, slide, alpha)
		}
		a.readerSideArrows(c, r, look, slide, alpha)
		if r.panel == panelNone {
			a.readerCenterHint(c, r, alpha)
		}
		a.readerDock(c, r, slide, alpha)
		// Panels come last so they cover the chevrons they reach, as comimi's
		// settings card does.
		if r.panel.isMenu() {
			a.readerMenu(c, r, slide, alpha)
		}
		if r.panel == panelSettings {
			a.readerSettingsPanel(c, r, slide, alpha)
		}
	})
}

// chromeMotion is where the overlay's cards are in their entrance or exit:
// slide is how far toward their edge they still are, 1 at the start of the
// entrance and 1 at the end of the exit, and alpha is their opacity.
func chromeMotion(r *Reader, now time.Time) (slide, alpha float32) {
	if r.overlay {
		p := clampFloat(float32(now.Sub(r.overlayAt))/float32(chromeEnter), 0, 1)
		a := clampFloat(float32(now.Sub(r.overlayAt))/float32(chromeFade), 0, 1)
		return 1 - chromeEase(p), a
	}
	p := clampFloat(float32(now.Sub(r.overlayClosedAt))/float32(chromeExit), 0, 1)
	a := clampFloat(float32(now.Sub(r.overlayClosedAt))/float32(chromeFade), 0, 1)
	return chromeEase(p), 1 - a
}

// readerPill is the floating title card at the top left: the menu button, the
// gallery's title, and a press anywhere on it opens the menu.
func (a *App) readerPill(c *ui.Context, r *Reader, slide, alpha float32) {
	look := readerLookOf(r)
	w, _ := c.Size()
	inset := readerInset(w)
	width := minFloat(500, w-2*inset)
	if width < 240 {
		width = maxFloat(w-2*inset, 200)
	}
	pill := readerGlassCard(c, look, 16).Width(width).Height(62).
		Absolute().Top(inset).Left(inset).Clip().
		Translate(0, -slide*chromeSlide).Opacity(alpha)
	pill.Children(func() {
		ui.Row(c).Fill().Padding(0, 13).Gap(13).AlignItems(ui.Center).Children(func() {
			a.readerIconButton(c, look, readerIconMenu, i18n.T("Reader menu"),
				34, 18, look.muted, func() {
					a.openPanel(r, panelMenu)
				})
			title := ui.Box(c).Grow(1).Height(34).Focusable().
				Label(i18n.T("Reader menu")).Tooltip(i18n.T("Reader menu"))
			title.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind != ui.InputPointerDown {
					return false
				}
				a.markActivity()
				a.openPanel(r, panelMenu)
				return true
			})
			title.Children(func() {
				ui.Text(c, r.title).FontSize(15).FontWeight(700).TextColor(look.fg).
					SingleLine().Grow(1)
			})
			if r.offline {
				ui.Box(c).Padding(2, 8).Radius(999).Shrink(0).
					Background(look.surface2).Children(func() {
					ui.Text(c, i18n.T("Offline")).FontSize(11).FontWeight(700).
						TextColor(look.muted)
				})
			}
			a.readerIconButton(c, look, readerIconBack, i18n.T("Back to the gallery"),
				34, 18, look.muted, func() {
					a.leaveReader(r)
				})
		})
	})
}

// readerSideArrows puts comimi's chevrons at the middle of either edge. They
// are hidden while auto page turn runs, as the library hides them.
func (a *App) readerSideArrows(c *ui.Context, r *Reader, look readerLook, slide, alpha float32) {
	if r.auto || !r.loaded || len(r.pages) == 0 {
		return
	}
	w, h := c.Size()
	inset := readerInset(w)
	size := float32(46)
	top := (h - size) / 2
	draw := func(left bool, mark *ui.SVG, label string, enabled bool, run func()) {
		x := inset
		away := -slide * chromeSlide
		if !left {
			x = w - inset - size
			away = slide * chromeSlide
		}
		bg := look.glass
		color := look.muted
		if !enabled {
			bg = look.glass
			color = look.soft
		}
		btn := ui.Box(c).Size(size, size).Radius(16).Center().Background(bg).
			Shadow(0, 4, 18, 0, look.shadow).
			Absolute().Top(top).Left(x).Translate(away, 0).Opacity(alpha).
			Focusable().Label(label).Tooltip(label)
		scale := float32(1)
		if enabled {
			btn.HandleInput(func(ev ui.InputEvent) bool {
				if ev.Kind != ui.InputPointerDown {
					return false
				}
				a.markActivity()
				run()
				return true
			})
			if btn.Hovered() {
				color = look.fg
				scale = 1.08
			}
		}
		btn.AnimatePose(ui.Pose{Scale: scale}, ui.Snappy)
		btn.Children(func() {
			ui.Icon(c, mark).Size(20, 20).TextColor(color)
		})
	}
	labelFor := func(dir pageGroupPlacement) string {
		if dir == groupNext {
			return i18n.T("Next page")
		}
		return i18n.T("Previous page")
	}
	leftDir := a.sideDirection(sideLeft)
	rightDir := a.sideDirection(sideRight)
	draw(true, readerIconPrev, labelFor(leftDir),
		a.readerCanTurn(leftDir), func() {
			a.moveFromSide(r, sideLeft)
		})
	draw(false, readerIconNext, labelFor(rightDir),
		a.readerCanTurn(rightDir), func() {
			a.moveFromSide(r, sideRight)
		})
}

// sideDirection is which way a turn from one edge of the page goes: the left
// side is back in a left-to-right read and on in a right-to-left one.
func (a *App) sideDirection(side pageGroupSide) pageGroupPlacement {
	if (side == sideRight) != a.reader.settings.rtl {
		return groupNext
	}
	return groupPrevious
}

// readerCenterHint is the card in the middle of the page: the direction guide
// while the reader is new, then the line that says how to put the controls
// away. It takes no press, so a tap on it is a tap on the page.
func (a *App) readerCenterHint(c *ui.Context, r *Reader, alpha float32) {
	look := readerLookOf(r)
	ui.Column(c).Fill().Absolute().Justify(ui.Center).AlignItems(ui.Center).Gap(10).
		Opacity(alpha).PassThrough().Children(func() {
		if r.guide {
			arrow, direction := readerIconNext, i18n.T("Pages move to the left")
			if r.settings.rtl {
				arrow, direction = readerIconPrev, i18n.T("Pages move to the right")
			}
			ui.Box(c).Size(96, 96).Radius(16).Background(look.glass).
				Shadow(0, 4, 18, 0, look.shadow).PassThrough().Children(func() {
				ui.Column(c).Fill().Center().Gap(8).PassThrough().Children(func() {
					ui.Icon(c, arrow).Size(30, 30).TextColor(look.muted).PassThrough()
					ui.Text(c, i18n.T("Page direction")).FontSize(12).
						TextColor(look.muted).PassThrough()
				})
			})
			ui.Text(c, direction).FontSize(12).TextColor(look.soft).PassThrough()
			return
		}
		ui.Box(c).Padding(6, 12).Radius(8).Background(look.glass).
			Shadow(0, 4, 18, 0, look.shadow).PassThrough().Children(func() {
			ui.Text(c, i18n.T("Click the middle of the page to hide this")).FontSize(12).
				TextColor(look.muted).PassThrough()
		})
	})
}

// readerDock is the floating bar along the bottom: the page count and the seek
// bar, then auto page turn, the zoom, the page mode, the view and settings.
func (a *App) readerDock(c *ui.Context, r *Reader, slide, alpha float32) {
	look := readerLookOf(r)
	w, _ := c.Size()
	inset := readerInset(w)
	padX := float32(24)
	if w < 720 {
		padX = 14
	}
	dock := ui.Column(c).Width(w-2*inset).Absolute().Bottom(inset).Left(inset).
		Padding(18, padX).Radius(20).Gap(12).Background(look.glass).
		Shadow(0, 6, 24, 0, look.shadow).
		Translate(0, slide*chromeSlide).Opacity(alpha)
	dock.Children(func() {
		if r.auto {
			a.autoProgressLine(c, r, look)
		}
		a.readerSeek(c, r, look)
		ui.Row(c).FillWidth().Gap(12).AlignItems(ui.Center).Children(func() {
			a.readerAutoButton(c, r, look)
			ui.Box(c).Grow(1)
			if w >= 620 {
				a.readerZoomControls(c, r, look)
			}
			a.readerPageModeButtons(c, r, look)
			a.readerViewSwitch(c, r, look)
			a.readerIconButton(c, look, readerIconGear, i18n.T("Settings"),
				24, 18, look.muted, func() {
					a.togglePanel(r, panelSettings)
				})
		})
	})
}

// readerAutoButton toggles auto page turn.
func (a *App) readerAutoButton(c *ui.Context, r *Reader, look readerLook) {
	label, mark := i18n.T("Auto page turn"), readerIconPlay
	if r.auto {
		label, mark = i18n.T("Stop auto page turn"), readerIconPause
	}
	a.readerIconButton(c, look, mark, label, 24, 20, look.muted, func() {
		a.setAuto(r, !r.auto)
	})
}

// autoProgressLine is how long the page up under auto page turn has left: a
// line across the top of the dock, drawn from the frame clock so it keeps up
// without a timer of its own.
func (a *App) autoProgressLine(c *ui.Context, r *Reader, look readerLook) {
	if r.autoSince.IsZero() {
		return
	}
	w := a.dockWidth(c)
	interval := time.Duration(r.settings.autoInterval()) * time.Millisecond
	progress := clampFloat(float32(time.Since(r.autoSince))/float32(interval), 0, 1)
	track := ui.Box(c).FillWidth().Height(4).Radius(2).Background(look.line)
	track.Children(func() {
		ui.Box(c).Height(4).Radius(2).Width(w * progress).Background(look.muted)
	})
}

// dockWidth is the width the dock's content spans inside its padding.
func (a *App) dockWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	inset := readerInset(w)
	padX := float32(24)
	if w < 720 {
		padX = 14
	}
	return w - 2*inset - 2*padX
}

// seekHover moves the seek bar's preview to the page under x.
func (a *App) seekHover(r *Reader, x, w float32) {
	total := len(r.pages)
	if w <= 0 || total == 0 {
		return
	}
	ratio := clampFloat(x/w, 0, 1)
	if r.settings.rtl {
		ratio = 1 - ratio
	}
	page := int(ratio * float32(total))
	if page >= total {
		page = total - 1
	}
	r.seekPreview = page
}

// seekTo goes to the page under x on the seek bar.
func (a *App) seekTo(r *Reader, x, w float32) {
	total := len(r.pages)
	if w <= 0 || total == 0 {
		return
	}
	ratio := clampFloat(x/w, 0, 1)
	if r.settings.rtl {
		ratio = 1 - ratio
	}
	a.goToPage(r, int(ratio*float32(total-1)+0.5))
}

// readerSeek is the seek bar: the page count, comimi's rounded track with the
// reader's place filled in, and the preview of the page a pointer hovers over.
func (a *App) readerSeek(c *ui.Context, r *Reader, look readerLook) {
	total := len(r.pages)
	first := pageGroupFirst(a.shownGroup())
	frac := float32(0)
	if total > 1 && first > 0 {
		frac = float32(first) / float32(total-1)
	}
	w := a.dockWidth(c)
	ui.Row(c).FillWidth().Gap(10).AlignItems(ui.Center).Children(func() {
		ui.Text(c, a.readerCountText(r)).FontSize(12).FontWeight(700).
			TextColor(look.fg).Shrink(0).SingleLine()
		track := ui.Box(c).Height(18).Grow(1).Focusable().
			Label(i18n.T("Seek")).Tooltip(i18n.T("Seek"))
		// The bar is painted from the box it is given, so its fill and knob
		// follow the exact width the layout settled on. The width it painted
		// last frame is also what a press measures against.
		barW := ui.Local(track, "seekWidth", func() float32 { return 0 })
		width := func() float32 {
			if *barW > 0 {
				return *barW
			}
			// Before the bar has painted once: the dock's content less the
			// room the page count takes.
			return maxFloat(w-80, 80)
		}
		// Reading the pointer in the frame is what gives the bar a frame for
		// every move over it: an element that only takes pointer events is told
		// of none while the button is not held.
		px, _, over := trackPointer(c, track)
		if over {
			a.seekHover(r, px, width())
		}
		track.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputPointerDown {
				return false
			}
			a.markActivity()
			a.seekTo(r, ev.X, width())
			return true
		})
		// The frame is built under the app's lock, but mygo paints this box
		// after View returns, while the page stream still writes what it has
		// arrived at. Snapshot the stream's state for the paint callback so it
		// reads one consistent frame instead of racing the load goroutine.
		streaming := !r.complete && r.total > 0
		streamPartial, streamTotal := r.partial, r.total
		rtl := r.settings.rtl
		track.Draw(func(p *ui.Painter, box ui.Rect) {
			*barW = box.W
			if box.W <= 0 {
				return
			}
			mid := box.Y + box.H/2
			bar := ui.Rect{X: box.X, Y: mid - 4, W: box.W, H: 8}
			p.Fill(bar, look.line, 999)
			// A page list still streaming shows how much of the bar is real.
			if streaming {
				stream := clampFloat(float32(streamPartial)/float32(streamTotal), 0, 1)
				sr := ui.Rect{X: box.X, Y: mid - 4, W: box.W * stream, H: 8}
				if rtl {
					sr.X = box.X + box.W - sr.W
				}
				p.Fill(sr, look.surface2, 999)
			}
			fill := ui.Rect{X: box.X, Y: mid - 4, W: box.W * frac, H: 8}
			if rtl {
				fill.X = box.X + box.W - fill.W
			}
			if fill.W > 0 {
				p.Fill(fill, look.muted, 999)
			}
			knobFrac := frac
			if rtl {
				knobFrac = 1 - frac
			}
			// The knob grows a little under the pointer, as comimi's seek
			// thumb does.
			knobSize := float32(18)
			if over {
				knobSize = 20.7
			}
			kx := box.X + knobFrac*box.W - knobSize/2
			kx = clampFloat(kx, box.X, box.X+box.W-knobSize)
			knob := ui.Rect{X: kx, Y: mid - knobSize/2, W: knobSize, H: knobSize}
			p.Shadow(knob, 999, 0, 1, 4, 0, look.shadow)
			p.Fill(knob, look.muted, 999)
		})
		track.Children(func() {
			if r.seekPreview >= 0 && r.seekPreview < total {
				a.readerSeekPreview(c, r, look, width())
			}
		})
	})
}

// readerSeekPreview draws the page the seek bar hovers over: its picture and
// the number the turn would land on, above the bar and the pointer. The
// preview shows a single page whatever the page mode, so hovering a spread
// does not stack two thumbnails. Its bottom clears the bar's own height so it
// never reaches the dock's button row.
func (a *App) readerSeekPreview(c *ui.Context, r *Reader, look readerLook, w float32) {
	total := len(r.pages)
	page := r.seekPreview
	if page < 0 || page >= total {
		return
	}
	x := float32(page) / float32(maxInt(total-1, 1))
	if r.settings.rtl {
		x = 1 - x
	}
	cardW := float32(80)
	left := clampFloat(x*w-cardW/2, 0, maxFloat(w-cardW, 0))
	ui.Column(c).Width(cardW).Gap(6).AlignItems(ui.Center).
		Absolute().Left(left).Bottom(32).Children(func() {
		ui.Box(c).Width(80).Height(113).Radius(6).Clip().
			Background(look.surface2).
			Shadow(0, 4, 12, 0, look.shadow).Children(func() {
			a.seekThumb(c, page, false)
		})
		ui.Box(c).Padding(4, 10).Radius(8).Background(look.glass).
			Shadow(0, 4, 12, 0, look.shadow).Children(func() {
			ui.Text(c, itoa(page+1)).FontSize(12).FontWeight(600).
				TextColor(look.fg)
		})
	})
}

// seekThumb draws one page of a preview from its thumbnail.
func (a *App) seekThumb(c *ui.Context, page int, grow bool) {
	r := &a.reader
	if !r.hasPage(page) {
		return
	}
	bitmap := a.pageThumbs.Get(a.readerThumbURL(page))
	img := ui.Image(c, bitmap).Fill().Fit(ui.Contain)
	if grow {
		img.Grow(1)
	}
}

// readerZoomControls are a small segmented group: zoom out, the percentage
// (which resets), and zoom in.
func (a *App) readerZoomControls(c *ui.Context, r *Reader, look readerLook) {
	group := ui.Row(c).Height(36).Radius(12).Background(look.surface2).Padding(3).Gap(1)
	group.Children(func() {
		a.readerIconButton(c, look, readerIconMinus, i18n.T("Zoom out"),
			26, 14, look.muted, func() {
				a.zoomBy(r, 1/(1+zoomStep))
			})
		percent := fmt.Sprintf("%d%%", int(r.zoom*100+0.5))
		label := i18n.T("Reset zoom")
		btn := ui.Box(c).Height(30).MinWidth(46).Center().Focusable().Label(percent).
			Tooltip(label)
		btn.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputPointerDown {
				return false
			}
			a.markActivity()
			a.resetZoom(r)
			return true
		})
		btn.Children(func() {
			ui.Text(c, percent).FontSize(12).FontWeight(700).TextColor(look.fg)
		})
		a.readerIconButton(c, look, readerIconPlus, i18n.T("Zoom in"),
			26, 14, look.muted, func() {
				a.zoomBy(r, 1+zoomStep)
			})
	})
}

// zoomBy zooms about the middle of the page, where a button-driven zoom
// should keep the page.
func (a *App) zoomBy(r *Reader, factor float32) {
	w, h := a.readerSize()
	a.zoomAt(r, w/2, h/2, r.zoom*factor, w, h)
}

// resetZoom goes back to the whole page.
func (a *App) resetZoom(r *Reader) {
	r.zoom, r.panX, r.panY = 1, 0, 0
}

// readerSize is the window the reader fills, which is the stage's size too.
func (a *App) readerSize() (float32, float32) {
	if a.reader.stageW > 0 && a.reader.stageH > 0 {
		return a.reader.stageW, a.reader.stageH
	}
	if a.win == nil {
		return 0, 0
	}
	w, h := a.win.Size()
	return float32(w), float32(h)
}

// readerZoomBadge is the way back from a zoom while the controls are hidden:
// a small pill at the top of the page saying how far in it is, which resets
// the zoom when clicked.
func (a *App) readerZoomBadge(c *ui.Context, r *Reader) {
	if r.zoom <= 1.001 {
		return
	}
	look := readerLookOf(r)
	label := fmt.Sprintf("%d%%", int(r.zoom*100+0.5))
	ui.Row(c).FillWidth().Absolute().Top(14).Left(0).Justify(ui.Center).Children(func() {
		if ui.Button(c, label).Label(i18n.T("Reset zoom")).
			Background(look.glass).TextColor(look.fg).
			Border(1, look.line).Shadow(0, 4, 18, 0, look.shadow).
			Clicked() {
			a.resetZoom(r)
		}
	})
}

// readerPageModeButtons switch between one page and a spread, as comimi's
// two-column page-mode switch does.
func (a *App) readerPageModeButtons(c *ui.Context, r *Reader, look readerLook) {
	selected := 0
	if r.settings.double {
		selected = 1
	}
	readerSegments(c, look, selected, func() {
		a.readerSegmentButton(c, look, 42, 36, readerIconSingle, i18n.T("Single page"),
			!r.settings.double, func() { a.setSpread(r, false) })
		a.readerSegmentButton(c, look, 42, 36, readerIconSpread, i18n.T("Double page"),
			r.settings.double, func() { a.setSpread(r, true) })
	})
}

// readerViewSwitch is the inline/full-screen pair beside the page mode.
func (a *App) readerViewSwitch(c *ui.Context, r *Reader, look readerLook) {
	full := a.isFullscreen()
	selected := 0
	if full {
		selected = 1
	}
	readerSegments(c, look, selected, func() {
		a.readerSegmentButton(c, look, 42, 36, readerIconWindow, i18n.T("Standard view"),
			!full, func() {
				if full {
					a.exitFullscreen()
				}
			})
		a.readerSegmentButton(c, look, 42, 36, readerIconExpand, i18n.T("Full screen"),
			full, func() {
				if !full {
					a.toggleFullscreen()
				}
			})
	})
}

// setSpread turns the page mode, keeping the shown pair together: switching to
// a spread lands on the first page of the pair the reader is on.
func (a *App) setSpread(r *Reader, on bool) {
	if r.settings.double == on {
		return
	}
	r.settings.double = on
	a.readerSettings = r.settings
	r.resetView()
	// Landing on the spread's first page keeps its pages from repeating.
	if on {
		if first := pageGroupFirst(a.pageIndexesFor(r.page, len(r.pages))); first >= 0 && first != r.page {
			r.page = first
			a.saveProgress(r)
		}
	}
}

// setCover has the first page stand alone in a spread, or pair it with the
// second, as comimi's cover setting does.
func (a *App) setCover(r *Reader, on bool) {
	if r.settings.hasCover == on {
		return
	}
	r.settings.hasCover = on
	a.readerSettings = r.settings
	r.resetView()
	if on {
		a.notify(r, i18n.T("A cover page starts alone"), toneInfo)
	} else {
		a.notify(r, i18n.T("No cover page"), toneInfo)
	}
}

// toggleFavorite keeps a page or lets it go, as comimi's long-press favorite
// does, and says so.
func (a *App) toggleFavorite(r *Reader, page int) {
	if r.fav == nil {
		r.fav = make(map[int]bool)
	}
	if r.fav[page] {
		delete(r.fav, page)
		a.notify(r, i18n.T("Removed from favorites"), toneInfo)
		return
	}
	r.fav[page] = true
	a.notify(r, i18n.Sprintf(i18n.T("Page %d added to favorites"), page+1), toneSuccess)
}

// favoritePages lists the kept pages, in page order.
func (r *Reader) favoritePages() []int {
	pages := make([]int, 0, len(r.fav))
	for page := range r.fav {
		if page >= 0 && page < len(r.pages) {
			pages = append(pages, page)
		}
	}
	// Small lists: an insertion sort reads better than a sort import here.
	for i := 1; i < len(pages); i++ {
		for j := i; j > 0 && pages[j] < pages[j-1]; j-- {
			pages[j], pages[j-1] = pages[j-1], pages[j]
		}
	}
	return pages
}

// readerToast is the notification over the page, briefly, as comimi's centred
// pill. It pops in with the same overshoot, and nothing in it takes the
// pointer, so a tap through it still reaches the page.
func (a *App) readerToast(c *ui.Context, r *Reader) {
	look := readerLookOf(r)
	bg := look.muted
	fg := look.white
	switch r.toastKind {
	case toneSuccess:
		bg = ui.RGB(34, 122, 72)
	case toneError:
		bg = ui.RGB(150, 48, 54)
	}
	// The pop: small and low, overshooting a little as it arrives, from the
	// toast's own clock (see toastPop).
	pop := clampFloat(float32(time.Since(r.toastAt))/float32(toastPop), 0, 1)
	eased := chromeEase(pop)
	alpha := pop
	scaleX := 0.2 + 0.8*eased
	scaleY := 0.8 + 0.2*eased
	ui.Box(c).FillWidth().Absolute().Bottom(140).Left(0).Justify(ui.Center).
		PassThrough().Children(func() {
		ui.Box(c).Padding(6, 14).Radius(999).Background(bg).
			Shadow(0, 4, 18, 0, look.shadow).
			Translate(0, 6*(1-eased)).Scale(scaleX, scaleY).Opacity(alpha).
			PassThrough().Children(func() {
			ui.Text(c, r.toast).FontSize(12).FontWeight(700).TextColor(fg).PassThrough()
		})
	})
}

// trackPointer is the pointer's position over el and whether it is over it,
// asking for frames while it moves so a bar can follow it.
func trackPointer(c *ui.Context, el ui.Element) (x, y float32, over bool) {
	return el.PointerPosition()
}
