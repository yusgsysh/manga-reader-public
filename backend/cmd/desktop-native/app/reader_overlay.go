package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// The overlay is comimi's controls: a centre hint, the dock along the bottom
// with the seek bar and the toggles, and whatever panel is open over it.

// readerOverlay draws the overlay over the page.
func (a *App) readerOverlay(c *ui.Context, r *Reader) {
	// A dark pane dims the page under the controls, so the interface reads
	// over a white or a black page alike. A press on it that misses the
	// controls puts the overlay away, as tapping the middle does; so the pane
	// lets the pointer through to the stage under it, which is what tells a
	// press on the middle from one on the controls, and what the wheel
	// reaches to zoom.
	ui.Box(c).Fill().Absolute().PassThrough().Background(readerOverlaySurface(0.34)).
		Children(func() {
			a.readerOverlayTop(c, r)
			switch {
			case r.panel.isMenu():
				a.readerMenu(c, r)
			case r.panel == panelSettings:
				a.readerSettingsPanel(c, r)
			default:
				a.readerCenterHint(c, r)
				a.readerDock(c, r)
			}
		})
}

// readerOverlayTop is the bar the overlay opens with: back to the gallery, the
// gallery's title and where in it, and the menu and settings toggles.
func (a *App) readerOverlayTop(c *ui.Context, r *Reader) {
	ui.Row(c).FillWidth().Absolute().Top(0).Left(0).Padding(10, 14).Gap(8).
		AlignItems(ui.Center).Children(func() {
		if ui.Button(c, "←").Label(i18n.T("Back to the gallery")).Width(36).Clicked() {
			a.leaveReader(r)
		}
		ui.Column(c).Gap(1).Grow(1).Children(func() {
			ui.Text(c, r.title).Bold().FontSize(13).SingleLine().
				TextColor(readerOverlayInk())
			count := a.readerCountText(r, true)
			if r.offline {
				count = i18n.Sprintf(i18n.T("%s · offline"), count)
			}
			ui.Text(c, count).FontSize(11).
				TextColor(readerOverlayMuted())
		})
		if r.panel == panelSettings {
			if ui.Button(c, i18n.T("Close")).Label(i18n.T("Close settings")).Clicked() {
				a.closeOverlay(r)
			}
		} else if ui.Button(c, i18n.T("Settings")).Label(i18n.T("Reader settings")).
			Clicked() {
			a.togglePanel(r, panelSettings)
		}
		if ui.Button(c, i18n.T("Menu")).Label(i18n.T("Reader menu")).
			Disabled(r.panel.isMenu()).Clicked() {
			a.openPanel(r, panelMenu)
		}
	})
}

// readerOverlayMuted is secondary text on the overlay's pane.
func readerOverlayMuted() ui.Color { return ui.RGBA(235, 235, 240, 0.62) }

// dismissOverlaySoon puts the overlay away just after a press that missed its
// controls: the release that press started reaches no control, so the overlay
// goes without waiting for another tap.
func (a *App) dismissOverlaySoon(r *Reader) {
	armed := time.Now()
	time.AfterFunc(50*time.Millisecond, func() {
		a.update(func() {
			if &a.reader != r || !r.overlay || r.panel != panelNone {
				return
			}
			// A press that hit a control marks activity after this one: the
			// dismissal only stands while nothing else has been touched.
			if a.overlayActivity.After(armed) {
				return
			}
			a.closeOverlay(r)
		})
	})
}

// readerOverlayInk is text on the overlay's pane.
func readerOverlayInk() ui.Color { return ui.RGB(242, 242, 246) }

// readerCenterHint is what the overlay says in the middle of the page: first
// the direction the pages move, then what a tap does, as comimi's boot guide
// gives way to the message that closes the overlay.
func (a *App) readerCenterHint(c *ui.Context, r *Reader) {
	// The hint takes no press: it covers the page, and a tap on it is a tap
	// on the page, which is what puts the overlay away.
	ui.Column(c).Fill().Justify(ui.Center).AlignItems(ui.Center).Gap(10).
		PassThrough().Children(func() {
		if r.guide {
			arrow, label := "→", i18n.T("Pages move to the left")
			if r.settings.rtl {
				arrow, label = "←", i18n.T("Pages move to the right")
			}
			ui.Text(c, arrow).FontSize(44).TextColor(readerOverlayInk()).PassThrough()
			ui.Text(c, label).FontSize(12).TextColor(readerOverlayMuted()).PassThrough()
			return
		}
		ui.Text(c, i18n.T("Click the middle of the page to hide this")).FontSize(12).
			TextColor(readerOverlayMuted()).PassThrough()
	})
}

// readerDock is the bar along the bottom of the overlay: the seek bar, the page
// count, auto page turn, the page mode, the view and the settings.
func (a *App) readerDock(c *ui.Context, r *Reader) {
	// Absolute, not only Bottom: an element placed this way is hit where it
	// is drawn, and a column merely pinned to the bottom would take presses
	// at the top of the window instead.
	ui.Column(c).FillWidth().Absolute().Bottom(0).Left(0).Padding(10, 14, 12, 14).Gap(8).
		Children(func() {
			a.readerSeek(c, r)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				a.readerAutoButton(c, r)
				ui.Text(c, a.readerCountText(r, false)).FontSize(12).
					TextColor(readerOverlayMuted())
				ui.Box(c).Grow(1)
				a.readerPageModeButtons(c, r)
				if ui.Button(c, a.viewLabel()).Label(i18n.T("Switch view")).Clicked() {
					a.cycleView()
				}
				if ui.Button(c, i18n.T("Settings")).Label(i18n.T("Reader settings")).
					Clicked() {
					a.togglePanel(r, panelSettings)
				}
			})
		})
}

// readerAutoButton toggles auto page turn, and shows how long the page has left
// as a thin line above the dock while it runs.
func (a *App) readerAutoButton(c *ui.Context, r *Reader) {
	label := i18n.T("Auto page turn")
	mark := "▶"
	if r.auto {
		label = i18n.T("Stop auto page turn")
		mark = "❚❚"
	}
	if r.auto {
		ui.Box(c).Width(120).Height(3).Children(func() {
			a.autoProgressFill(c, r)
		})
	}
	if ui.Button(c, mark+i18n.T(" Auto")).Label(label).Clicked() {
		a.setAuto(r, !r.auto)
	}
}

// autoProgressFill is how long the page up under auto page turn has left,
// drawn from the frame clock so it keeps up without a timer of its own.
func (a *App) autoProgressFill(c *ui.Context, r *Reader) {
	if r.autoSince.IsZero() {
		return
	}
	interval := time.Duration(r.settings.autoInterval()) * time.Millisecond
	progress := clampFloat(float32(time.Since(r.autoSince))/float32(interval), 0, 1)
	ui.Box(c).Width(120 * progress).FillHeight().
		Background(ui.RGBA(235, 235, 240, 0.75))
}

// dockWidth is the width the seek bar spans.
func (a *App) dockWidth(c *ui.Context) float32 {
	w, _ := c.Size()
	return w - 28
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

// readerSeek is the seek bar: the position and the track, with the preview of
// the page a pointer hovers over above it. A press or a drag goes to that page.
func (a *App) readerSeek(c *ui.Context, r *Reader) {
	total := len(r.pages)
	first := pageGroupFirst(a.shownGroup())
	frac := float32(0)
	if total > 1 && first > 0 {
		frac = float32(first) / float32(total-1)
	}
	w := a.dockWidth(c)
	track := ui.Box(c).FillWidth().Height(18)
	// Reading the pointer in the frame is what gives the bar a frame for
	// every move over it: an element that only takes pointer events is told
	// of none while the button is not held.
	px, _, over := trackPointer(c, track)
	if over {
		a.seekHover(r, px, w)
	}
	track.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputPointerDown {
			return false
		}
		a.markActivity()
		a.seekTo(r, ev.X, w)
		return true
	})
	track.Children(func() {
		// The track, and the fill to where the reader is. Left to right the
		// fill grows from the left; right to left from the right, so the far
		// side is always the page further on.
		ui.Box(c).FillWidth().Height(3).Absolute().Top(8).Left(0).
			Background(ui.RGBA(255, 255, 255, 0.22))
		fill := ui.Box(c).Absolute().Top(8).Height(3).Width(w * frac).
			Background(ui.RGBA(255, 255, 255, 0.92))
		if r.settings.rtl {
			fill.Left(w - w*frac)
		} else {
			fill.Left(0)
		}
	})
	if r.seekPreview >= 0 && r.seekPreview < total {
		a.readerSeekPreview(c, r, w)
	}
}

// readerSeekPreview draws the page the seek bar hovers over: its picture and
// the number the turn would land on, above the pointer.
func (a *App) readerSeekPreview(c *ui.Context, r *Reader, w float32) {
	total := len(r.pages)
	g := a.pageIndexesFor(r.seekPreview, total)
	x := float32(r.seekPreview) / float32(maxInt(total-1, 1))
	if r.settings.rtl {
		x = 1 - x
	}
	left := clampFloat(x*w-48, 0, maxFloat(w-96, 0))
	ui.Column(c).Width(96).Gap(4).AlignItems(ui.Center).
		Absolute().Left(left).Bottom(24).Children(func() {
		ui.Box(c).Width(96).Height(128).Radius(6).Clip().
			Background(ui.RGBA(0, 0, 0, 0.55)).Children(func() {
			ui.Row(c).Fill().Children(func() {
				for _, page := range g.indexes {
					a.seekThumb(c, page, len(g.indexes) > 1)
				}
			})
		})
		ui.Text(c, pageGroupText(g)).FontSize(11).TextColor(ui.RGB(255, 255, 255))
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

// readerPageModeButtons switch between one page and a spread.
func (a *App) readerPageModeButtons(c *ui.Context, r *Reader) {
	if ui.Button(c, i18n.T("Single page")).Label(i18n.T("Show one page")).
		Disabled(!r.settings.double).Clicked() {
		a.setSpread(r, false)
	}
	if ui.Button(c, i18n.T("Double page")).Label(i18n.T("Show two pages")).
		Disabled(r.settings.double).Clicked() {
		a.setSpread(r, true)
	}
	if r.settings.double && !r.narrow {
		cover := i18n.T("Cover")
		if ui.Button(c, cover).Label(i18n.T("Toggle the cover page")).Clicked() {
			a.setCover(r, !r.settings.hasCover)
		}
	}
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
	if on {
		a.notify(r, i18n.T("Double page"), toneInfo)
	} else {
		a.notify(r, i18n.T("Single page"), toneInfo)
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

// viewLabel names the window's view on the dock's switch.
func (a *App) viewLabel() string {
	if a.isFullscreen() {
		return i18n.T("Full screen")
	}
	return i18n.T("Window")
}

// cycleView flips the window between full screen and its window.
func (a *App) cycleView() { a.toggleFullscreen() }

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

// readerToast is the notification over everything else, briefly. Nothing in it
// takes the pointer, so a tap through it still reaches the page.
func (a *App) readerToast(c *ui.Context, r *Reader) {
	bg := ui.RGBA(30, 30, 34, 0.92)
	switch r.toastKind {
	case toneSuccess:
		bg = ui.RGBA(22, 92, 54, 0.94)
	case toneError:
		bg = ui.RGBA(110, 30, 34, 0.94)
	}
	ui.Box(c).FillWidth().Absolute().Top(64).Left(0).Justify(ui.Center).
		Children(func() {
			ui.Box(c).Padding(8, 14).Radius(999).Background(bg).
				HandleInput(func(ui.InputEvent) bool { return true }).Children(func() {
				ui.Text(c, r.toast).FontSize(12).TextColor(ui.RGB(255, 255, 255))
			})
		})
}

// trackPointer is the pointer's position over el and whether it is over it,
// asking for frames while it moves so a bar can follow it.
func trackPointer(c *ui.Context, el ui.Element) (x, y float32, over bool) {
	return el.PointerPosition()
}
