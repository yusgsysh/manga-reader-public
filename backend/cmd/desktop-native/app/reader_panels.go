package app

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// The menu's marks, drawn as line icons so they take the menu's ink like the
// labels beside them.
var (
	readerIconPages    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="3.5" width="7" height="7" rx="1.5"/><rect x="3.5" y="13.5" width="7" height="7" rx="1.5"/><rect x="13.5" y="13.5" width="7" height="7" rx="1.5"/></svg>`))
	readerIconFavorite = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M12 20s-7.5-4.7-7.5-10A4.5 4.5 0 0 1 12 6.7 4.5 4.5 0 0 1 19.5 10c0 5.3-7.5 10-7.5 10z"/></svg>`))
	readerIconKeyboard = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="2.5" y="6" width="19" height="12" rx="2"/><path d="M6 10h.01M9.5 10h.01M13 10h.01M16.5 10h.01M6 13.5h.01M9.5 13.5h.01M13 13.5h.01M16.5 13.5h.01M7.5 16.5h9"/></svg>`))
	readerIconShare    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3.5v11"/><path d="m8 7.5 4-4 4 4"/><path d="M5 13.5v5a2 2 0 0 0 2 2h10a2 2 0 0 0 2-2v-5"/></svg>`))
	readerIconAbout    = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="8.5"/><path d="M12 11v5.5"/><path d="M12 7.6h.01"/></svg>`))
	readerIconExit     = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M9.5 4.5H5.5v15h4"/><path d="M14 8.5l4 3.5-4 3.5"/><path d="M18 12H9"/></svg>`))
)

// The reader's panels, after comimi's menu and settings sheet: the menu holds
// the page list, the kept pages, the shortcuts and the gallery's own details,
// and the settings hold how the pages move.

// readerMenu draws the menu panel, with the view of the panel that is open.
func (a *App) readerMenu(c *ui.Context, r *Reader) {
	// The sheet starts below the top bar and lets presses above it through,
	// so the bar's own buttons still take them.
	ui.Column(c).FillWidth().FillHeight().Absolute().Top(0).Left(0).Padding(56, 18, 24, 18).
		AlignItems(ui.Center).PassThrough().Children(func() {
		ui.Column(c).Width(560).FillHeight().Background(readerOverlaySurface(0.96)).
			Radius(14).Border(1, ui.RGBA(255, 255, 255, 0.08)).
			Padding(14, 18).Gap(10).Children(func() {
			// The head of the menu: the view's name and the way back.
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if r.panel != panelMenu {
					if ui.Button(c, "←").Label(i18n.T("Back")).Width(36).Clicked() {
						a.openPanel(r, panelMenu)
					}
				}
				ui.Text(c, a.panelTitle(r)).Bold().FontSize(15).
					TextColor(readerOverlayInk()).Grow(1)
			})
			switch r.panel {
			case panelMenu:
				a.readerMenuHome(c, r)
			case panelPages:
				a.readerPageList(c, r)
			case panelFavorites:
				a.readerFavorites(c, r)
			case panelShortcuts:
				a.readerShortcuts(c, r)
			case panelShare:
				a.readerShare(c, r)
			case panelAbout:
				a.readerAbout(c, r)
			}
		})
	})
}

// panelTitle names the open view.
func (a *App) panelTitle(r *Reader) string {
	switch r.panel {
	case panelPages:
		return i18n.T("Pages")
	case panelFavorites:
		return i18n.T("Favorites")
	case panelShortcuts:
		return i18n.T("Keyboard shortcuts")
	case panelShare:
		return i18n.T("Share")
	case panelAbout:
		return i18n.T("About")
	default:
		return i18n.T("Menu")
	}
}

// menuEntry is a row of the menu's home: its label, what it says, and what it
// opens.
func (a *App) menuEntry(c *ui.Context, r *Reader, mark *ui.SVG, label, hint string, p readerPanel) {
	ui.Box(c).FillWidth().Padding(9, 10).Radius(10).
		Background(ui.RGBA(255, 255, 255, 0.06)).
		HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputPointerDown {
				return false
			}
			a.markActivity()
			if p == panelNone {
				a.leaveReader(r)
				return true
			}
			a.openPanel(r, p)
			return true
		}).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Box(c).Size(32, 32).Radius(9).Center().Shrink(0).
				Background(ui.RGBA(255, 255, 255, 0.08)).Children(func() {
				ui.Icon(c, mark).Size(17, 17).TextColor(readerOverlayInk())
			})
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, label).FontSize(13).TextColor(readerOverlayInk())
				ui.Text(c, hint).FontSize(11).TextColor(readerOverlayMuted())
			})
			ui.Text(c, "›").FontSize(14).TextColor(readerOverlayMuted())
		})
	})
}

// readerMenuHome lists what the menu offers.
func (a *App) readerMenuHome(c *ui.Context, r *Reader) {
	a.menuEntry(c, r, readerIconPages, i18n.T("Page list"),
		i18n.Sprintf(i18n.T("%d pages"), len(r.pages)), panelPages)
	a.menuEntry(c, r, readerIconFavorite, i18n.T("Favorites"),
		i18n.Sprintf(i18n.T("%d kept"), len(r.favoritePages())), panelFavorites)
	a.menuEntry(c, r, readerIconKeyboard, i18n.T("Keyboard shortcuts"),
		i18n.T("Every key the reader takes"), panelShortcuts)
	a.menuEntry(c, r, readerIconShare, i18n.T("Share"),
		i18n.T("Copy the gallery's address"), panelShare)
	a.menuEntry(c, r, readerIconAbout, i18n.T("About"),
		i18n.T("About this reader"), panelAbout)
	a.menuEntry(c, r, readerIconExit, i18n.T("Back to the gallery"),
		i18n.T("Leave the reader where it is"), panelNone)
}

// readerPageList is the page list: every page as a thumbnail, the one shown
// marked, and a field to jump to a page by its number.
func (a *App) readerPageList(c *ui.Context, r *Reader) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.TextInput(c, &r.jump).Width(90).Label(i18n.T("Page number")).
			Placeholder(i18n.T("Page number"))
		if ui.Button(c, i18n.T("Go")).Clicked() {
			a.jumpTo(r)
		}
		ui.Text(c, i18n.Sprintf(i18n.T("%s / %d"), pageGroupText(a.shownGroup()), len(r.pages))).
			FontSize(11).TextColor(readerOverlayMuted())
	})
	if r.pagesScrollTo >= 0 {
		const pageListCols = 5
		const pageListRow = 136 + 6
		row := r.pagesScrollTo / pageListCols
		r.pagesScroll.Y = float32(row) * pageListRow
		r.pagesScrollTo = -1
	}
	ui.Scroll(c).Fill().TrackScroll(&r.pagesScroll).Children(func() {
		ui.Grid(c).Columns(5).Gap(6).Padding(0, 0, 8, 0).Children(func() {
			for page := range r.pages {
				a.pageListCell(c, r, page)
			}
		})
	})
}

// pageListCell is one page's thumbnail in the page list.
func (a *App) pageListCell(c *ui.Context, r *Reader, page int) {
	shown := page >= pageGroupFirst(a.shownGroup()) && page <= pageGroupLast(a.shownGroup())
	// The shown page stands out from the rest, as comimi's page list marks
	// the page being read.
	bg := ui.RGBA(255, 255, 255, 0.06)
	if shown {
		bg = ui.RGBA(110, 160, 255, 0.35)
	}
	cell := ui.Box(c).Height(136).Radius(6).Clip().Background(bg)
	if shown {
		cell.Border(2, ui.RGB(110, 160, 255))
	}
	cell.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputPointerDown {
			return false
		}
		a.markActivity()
		a.goToPage(r, page)
		return true
	})
	cell.Children(func() {
		if r.hasPage(page) {
			bitmap := a.pageThumbs.Get(a.readerThumbURL(page))
			ui.Image(c, bitmap).Fill().Fit(ui.Cover).Absolute()
		}
		if r.fav[page] {
			ui.Box(c).Padding(2, 4).Radius(4).Background(ui.RGBA(0, 0, 0, 0.6)).
				Absolute().Top(3).Left(3).Children(func() {
				ui.Text(c, "♥").FontSize(10).TextColor(ui.RGB(255, 120, 150))
			})
		}
		ui.Box(c).Padding(1, 4).Radius(4).Background(ui.RGBA(0, 0, 0, 0.62)).
			Absolute().Bottom(3).Left(3).Children(func() {
			ui.Text(c, itoa(page+1)).FontSize(10).TextColor(ui.RGB(255, 255, 255))
		})
	})
}

// readerFavorites lists the pages the reader kept, as comimi's favorites view
// does, with a way to let each go.
func (a *App) readerFavorites(c *ui.Context, r *Reader) {
	pages := r.favoritePages()
	if len(pages) == 0 {
		ui.Text(c, i18n.T("Hold a page for a moment to keep it here")).FontSize(12).
			TextColor(readerOverlayMuted())
		return
	}
	ui.Scroll(c).Fill().Children(func() {
		ui.Grid(c).Columns(5).Gap(8).Children(func() {
			for _, page := range pages {
				a.favoriteCell(c, r, page)
			}
		})
	})
}

// favoriteCell is one kept page: its thumbnail, the number, and the way to
// let it go. It is a box rather than a column so the grid stretches it to its
// cell, as the page list's cells do.
func (a *App) favoriteCell(c *ui.Context, r *Reader, page int) {
	box := ui.Box(c).Height(150).Radius(6).Clip().
		Background(ui.RGBA(255, 255, 255, 0.06))
	box.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputPointerDown {
			return false
		}
		a.markActivity()
		a.goToPage(r, page)
		return true
	})
	box.Children(func() {
		if r.hasPage(page) {
			bitmap := a.pageThumbs.Get(a.readerThumbURL(page))
			ui.Image(c, bitmap).Fill().Fit(ui.Cover).Absolute()
		}
		if ui.Button(c, "✕").Label(i18n.T("Remove from favorites")).Width(28).
			Absolute().Top(4).Right(4).Clicked() {
			a.toggleFavorite(r, page)
		}
		ui.Box(c).Padding(1, 4).Radius(4).Background(ui.RGBA(0, 0, 0, 0.62)).
			Absolute().Bottom(3).Left(3).Children(func() {
			ui.Text(c, itoa(page+1)).FontSize(10).TextColor(ui.RGB(255, 255, 255))
		})
	})
}

// readerShortcuts is the keyboard's own page.
func (a *App) readerShortcuts(c *ui.Context, r *Reader) {
	rows := [][2]string{
		{"← / Space", i18n.T("Move to the left")},
		{"→ / Shift + Space", i18n.T("Move to the right")},
		{"A", i18n.T("Auto page turn")},
		{"P", i18n.T("One page or a spread")},
		{"C", i18n.T("The cover page")},
		{"N", i18n.T("The window")},
		{"F", i18n.T("Full screen")},
		{"M", i18n.T("The menu")},
		{"O", i18n.T("This overlay")},
		{"S", i18n.T("The settings")},
		{"- / =", i18n.T("Zoom out or in")},
		{"0", i18n.T("Reset the zoom")},
		{"Esc", i18n.T("Back one step")},
	}
	for _, row := range rows {
		ui.Row(c).Gap(12).AlignItems(ui.Center).Children(func() {
			ui.Text(c, row[0]).FontSize(12).TextColor(readerOverlayInk()).Width(150)
			ui.Text(c, row[1]).FontSize(12).TextColor(readerOverlayMuted()).Grow(1)
		})
	}
	ui.Text(c, i18n.T("Drag across the page to turn it, pinch or ctrl-wheel to zoom, drag a zoomed page to move it, and hold a page to keep it")).
		FontSize(11).TextColor(readerOverlayMuted()).MaxWidth(560)
}

// readerShare is the gallery's address, to copy, with the option of starting
// at the page the reader is on.
func (a *App) readerShare(c *ui.Context, r *Reader) {
	url := galleryURL(r.id, r.token)
	ui.Text(c, url).FontSize(12).TextColor(readerOverlayInk()).MaxWidth(560)
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c, i18n.T("Copy")).Clicked() {
			a.copyToClipboard(url)
			a.notify(r, i18n.T("Copied"), toneSuccess)
		}
	})
	ui.Text(c, i18n.T("The gallery opens at its cover, whatever page the address came from")).
		FontSize(11).TextColor(readerOverlayMuted()).MaxWidth(560)
}

// readerAbout says what this reader is.
func (a *App) readerAbout(c *ui.Context, r *Reader) {
	ui.Text(c, i18n.T("A native reader built after comimi's manga viewer")).FontSize(12).
		TextColor(readerOverlayInk()).MaxWidth(560)
	ui.Text(c, "github.com/yui540/comimi").FontSize(11).
		TextColor(readerOverlayMuted())
	ui.Text(c, "e-hentai.org/g/"+itoa(int(r.id))+"/"+r.token+"/").FontSize(11).
		TextColor(readerOverlayMuted())
}

// readerSettingsPanel is the settings sheet: the background, the cover, the
// reading direction and how often auto page turn moves on. It is a card over
// the page, set to the right under the settings button as comimi's popover is,
// so its text does not sit loose on the page.
func (a *App) readerSettingsPanel(c *ui.Context, r *Reader) {
	// The layer sits below the top bar, as the menu does: a sheet that ran
	// under it would put its first controls where the bar takes the press.
	ui.Column(c).FillWidth().FillHeight().Absolute().Top(0).Left(0).Padding(56, 18, 0, 18).
		PassThrough().Children(func() {
		ui.Scroll(c).Fill().Children(func() {
			ui.Row(c).FillWidth().Justify(ui.End).Children(func() {
				ui.Column(c).Width(360).Gap(14).Padding(16, 18).Radius(14).
					Background(readerOverlaySurface(0.96)).TextColor(readerOverlayInk()).
					Children(func() {
						ui.Text(c, i18n.T("Reading direction")).FontSize(12).
							TextColor(readerOverlayMuted())
						ui.Row(c).Gap(8).Children(func() {
							a.readerChoice(c, i18n.T("Right to left"), r.settings.rtl, func() {
								a.setDirection(r, true)
							})
							a.readerChoice(c, i18n.T("Left to right"), !r.settings.rtl, func() {
								a.setDirection(r, false)
							})
						})

						ui.Text(c, i18n.T("Pages")).FontSize(12).TextColor(readerOverlayMuted())
						ui.Column(c).Gap(6).Children(func() {
							// The checkbox writes a copy: a page-mode change
							// goes through setSpread/setCover, which land the
							// reader on the pair that holds the page it shows.
							double, cover := r.settings.double, r.settings.hasCover
							ui.Checkbox(c, &double, i18n.T("Two pages at a time"))
							if double != r.settings.double {
								a.setSpread(r, double)
							}
							ui.Checkbox(c, &cover, i18n.T("The first page is a cover"))
							if cover != r.settings.hasCover {
								a.setCover(r, cover)
							}
						})

						ui.Text(c, i18n.T("Background")).FontSize(12).TextColor(readerOverlayMuted())
						ui.Row(c).Gap(8).Children(func() {
							a.readerChoice(c, i18n.T("White"), !r.settings.dark, func() {
								a.setDark(r, false)
							})
							a.readerChoice(c, i18n.T("Black"), r.settings.dark, func() {
								a.setDark(r, true)
							})
						})

						ui.Text(c, i18n.T("Auto page turn")).FontSize(12).
							TextColor(readerOverlayMuted())
						// A range slider from 3 to 30 seconds in steps of one, as
						// comimi's settings panel has.
						seconds := float64(r.settings.autoInterval()) / 1000
						ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
							ui.StepSlider(c, &seconds, autoTurnMin, autoTurnMax, 1).Grow(1).Changed()
							if seconds != float64(r.settings.autoInterval())/1000 {
								a.setAutoInterval(r, int(seconds)*1000)
							}
							ui.Text(c, i18n.Sprintf(i18n.T("%d s"), int(seconds))).
								FontSize(12).TextColor(readerOverlayInk())
						})
					})
			})
		})
	})
	// The panel edits the reader's settings directly; keep the app's copy in
	// step so the next gallery opens with them.
	a.readerSettings = r.settings
}

// readerChoice is one option of a pair in the settings sheet: the chosen one
// in the accent colour, the other a normal button that switches to it on a
// click. A pair of plain buttons left the choice ambiguous, since the chosen
// one could only be told by the other being disabled.
func (a *App) readerChoice(c *ui.Context, label string, chosen bool, choose func()) {
	if chosen {
		ui.PrimaryButton(c, label).Label(label)
		return
	}
	if ui.Button(c, label).Label(label).Clicked() {
		choose()
	}
}

// setDirection turns the reading direction, landing on the spread's first page
// so the pair the reader is on does not repeat.
func (a *App) setDirection(r *Reader, rtl bool) {
	if r.settings.rtl == rtl {
		return
	}
	r.settings.rtl = rtl
	a.readerSettings = r.settings
	r.resetView()
	if first := pageGroupFirst(a.pageIndexesFor(r.page, len(r.pages))); first >= 0 && first != r.page {
		r.page = first
		a.saveProgress(r)
	}
	if rtl {
		a.notify(r, i18n.T("Right to left"), toneInfo)
	} else {
		a.notify(r, i18n.T("Left to right"), toneInfo)
	}
}

// setDark sets the paper the pages sit on.
func (a *App) setDark(r *Reader, dark bool) {
	r.settings.dark = dark
	a.readerSettings = r.settings
	if dark {
		a.notify(r, i18n.T("Black background"), toneInfo)
	} else {
		a.notify(r, i18n.T("White background"), toneInfo)
	}
}

// setAutoInterval sets how long auto page turn leaves a page up.
func (a *App) setAutoInterval(r *Reader, ms int) {
	r.settings.autoIntervalMS = ms
	a.readerSettings = r.settings
	r.autoSince = time.Now()
}

// copyToClipboard puts text on the system clipboard, if the app has one.
func (a *App) copyToClipboard(text string) {
	if text == "" {
		return
	}
	mygo.Clipboard.WriteText(text)
}
