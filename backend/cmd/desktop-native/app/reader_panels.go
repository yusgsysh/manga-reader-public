package app

import (
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// The reader's panels, after comimi's menu and settings sheet: the menu is the
// card at the top left that the title pill grows into, and the settings are a
// card above the dock at the right. Both are made of the same glass as the
// rest of the overlay.

// readerMenu draws the menu card, with the view of the panel that is open. It
// drops in from under the pill when it opens and goes back up when it closes;
// while it closes it still draws the view it is leaving.
func (a *App) readerMenu(c *ui.Context, r *Reader, slide, alpha float32) {
	look := readerLookOf(r)
	w, h := c.Size()
	inset := readerInset(w)
	width := minFloat(500, w-2*inset)
	if width < 240 {
		width = maxFloat(w-2*inset, 200)
	}
	shown := r.panel
	closing := false
	if !shown.isMenu() {
		shown, closing = r.menuPanel, true
	}
	drop, menuAlpha := menuMotion(r, time.Now())
	panel := ui.Column(c).Width(width).Absolute().Top(inset).Left(inset).
		Radius(16).Background(look.glass).Shadow(0, 6, 24, 0, look.shadow).Clip().
		Translate(0, -slide*chromeSlide-drop).Opacity(alpha * menuAlpha).
		IgnoreInput(closing)
	panel.Children(func() {
		a.readerMenuHead(c, r, look, shown)
		ui.Box(c).FillWidth().Height(1).Background(look.line)
		switch shown {
		case panelMenu:
			a.readerMenuHome(c, r, look)
		case panelPages:
			a.readerMenuBody(c, look, h, func() { a.readerPageList(c, r, look) })
		case panelFavorites:
			a.readerMenuBody(c, look, h, func() { a.readerFavorites(c, r, look) })
		case panelShortcuts:
			a.readerMenuBody(c, look, h, func() { a.readerShortcuts(c, r, look) })
		case panelShare:
			a.readerMenuBody(c, look, h, func() { a.readerShare(c, r, look) })
		case panelAbout:
			a.readerMenuBody(c, look, h, func() { a.readerAbout(c, r, look) })
		}
	})
}

// readerMenuHead is the card's first row: the button that collapses the menu
// (or goes back to it), the gallery's title, or the name of the view.
func (a *App) readerMenuHead(c *ui.Context, r *Reader, look readerLook, shown readerPanel) {
	head := ui.Box(c).FillWidth().Height(62).
		Focusable().Label(i18n.T("Reader menu")).Tooltip(i18n.T("Reader menu"))
	head.HandleInput(func(ev ui.InputEvent) bool {
		if ev.Kind != ui.InputPointerDown {
			return false
		}
		a.markActivity()
		if shown == panelMenu {
			a.openPanel(r, panelNone)
			return true
		}
		a.openPanel(r, panelMenu)
		return true
	})
	if head.Hovered() {
		head.Background(look.surface)
	}
	head.Children(func() {
		ui.Row(c).Fill().Padding(0, 13).Gap(13).AlignItems(ui.Center).Children(func() {
			mark := readerIconClose
			if shown != panelMenu {
				mark = readerIconBack
			}
			ui.Icon(c, mark).Size(18, 18).TextColor(look.muted).Shrink(0)
			title := r.title
			if shown != panelMenu {
				title = panelTitle(shown)
			}
			ui.Text(c, title).FontSize(15).FontWeight(700).TextColor(look.fg).
				SingleLine().Grow(1)
		})
	})
}

// readerMenuBody wraps a sub-view in the card: its content scrolls inside the
// window instead of running past the card's edge.
func (a *App) readerMenuBody(c *ui.Context, look readerLook, h float32, fn func()) {
	maxH := h - 2*readerInset(h) - 63
	if maxH < 160 {
		maxH = 160
	}
	ui.Scroll(c).FillWidth().MaxHeight(maxH).Children(func() {
		ui.Column(c).FillWidth().Padding(16, 16).Gap(12).Children(fn)
	})
}

// panelTitle names the open view.
func panelTitle(p readerPanel) string {
	switch p {
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

// readerMenuHome lists what the menu offers, as comimi's plain rows: the name
// of the view and a chevron, no second line of explanation.
func (a *App) readerMenuHome(c *ui.Context, r *Reader, look readerLook) {
	ui.Column(c).FillWidth().Padding(6, 0).Children(func() {
		a.menuEntry(c, r, look, i18n.T("Page list"), panelPages)
		a.menuEntry(c, r, look, i18n.T("Favorites"), panelFavorites)
		a.menuEntry(c, r, look, i18n.T("Keyboard shortcuts"), panelShortcuts)
		a.menuEntry(c, r, look, i18n.T("Share"), panelShare)
		a.menuEntry(c, r, look, i18n.T("About"), panelAbout)
		a.menuEntry(c, r, look, i18n.T("Back to the gallery"), panelNone)
	})
}

// menuEntry is a row of the menu's home: its label and what it opens.
func (a *App) menuEntry(c *ui.Context, r *Reader, look readerLook, label string, p readerPanel) {
	row := ui.Box(c).FillWidth().Height(44).Padding(0, 16).
		Focusable().Label(label).Tooltip(label)
	row.HandleInput(func(ev ui.InputEvent) bool {
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
	})
	if row.Hovered() {
		row.Background(look.surface)
	}
	row.Children(func() {
		ui.Row(c).Fill().AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, label).FontSize(14).FontWeight(700).TextColor(look.fg).
				Grow(1).SingleLine()
			ui.Icon(c, readerIconNext).Size(16, 16).TextColor(look.soft)
		})
	})
}

// readerPageList is the page list: every page as a thumbnail, the one shown
// marked, and a field to jump to a page by its number.
func (a *App) readerPageList(c *ui.Context, r *Reader, look readerLook) {
	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		ui.TextInput(c, &r.jump).Width(90).Label(i18n.T("Page number")).
			Placeholder(i18n.T("Page number"))
		if ui.Button(c, i18n.T("Go")).Clicked() {
			a.jumpTo(r)
		}
	})
	if r.pagesScrollTo >= 0 {
		const pageListCols = 4
		const pageListRow = 141 + 12
		row := r.pagesScrollTo / pageListCols
		r.pagesScroll.Y = float32(row) * pageListRow
		r.pagesScrollTo = -1
	}
	ui.Scroll(c).FillWidth().MaxHeight(420).TrackScroll(&r.pagesScroll).Children(func() {
		ui.Grid(c).Columns(4).Gap(12).Padding(0, 0, 8, 0).Children(func() {
			for page := range r.pages {
				a.pageListCell(c, r, look, page)
			}
		})
	})
}

// pageListCell is one page's thumbnail in the page list.
func (a *App) pageListCell(c *ui.Context, r *Reader, look readerLook, page int) {
	shown := page >= pageGroupFirst(a.shownGroup()) && page <= pageGroupLast(a.shownGroup())
	cell := ui.Box(c).AspectRatio(100.0/141.0).Radius(6).Clip().
		Background(look.surface2).Border(2, look.line)
	if shown {
		cell.Border(2, look.muted)
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
			ui.Box(c).Padding(2, 5).Radius(4).Background(look.glass).
				Absolute().Top(4).Right(4).Children(func() {
				ui.Text(c, "♥").FontSize(10).TextColor(ui.RGB(255, 120, 150))
			})
		}
		ui.Box(c).Padding(2, 6).Radius(999).Background(look.glass).
			Shadow(0, 1, 4, 0, look.shadow).Absolute().Top(4).Left(4).
			Children(func() {
				ui.Text(c, itoa(page+1)).FontSize(10).FontWeight(700).
					TextColor(look.muted)
			})
	})
}

// readerFavorites lists the pages the reader kept, as comimi's favorites view
// does, with a way to let each go.
func (a *App) readerFavorites(c *ui.Context, r *Reader, look readerLook) {
	pages := r.favoritePages()
	if len(pages) == 0 {
		ui.Text(c, i18n.T("Hold a page for a moment to keep it here")).FontSize(12).
			TextColor(look.muted)
		return
	}
	ui.Scroll(c).FillWidth().MaxHeight(420).Children(func() {
		ui.Grid(c).Columns(4).Gap(12).Children(func() {
			for _, page := range pages {
				a.favoriteCell(c, r, look, page)
			}
		})
	})
}

// favoriteCell is one kept page: its thumbnail, the number, and the way to
// let it go.
func (a *App) favoriteCell(c *ui.Context, r *Reader, look readerLook, page int) {
	box := ui.Box(c).AspectRatio(100.0/141.0).Radius(6).Clip().
		Background(look.surface2).Border(2, look.line)
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
		remove := ui.Box(c).Size(24, 24).Radius(999).Center().Background(look.glassStrong).
			Shadow(0, 2, 8, 0, look.shadow).Absolute().Top(4).Right(4).
			Focusable().Label(i18n.T("Remove from favorites")).
			Tooltip(i18n.T("Remove from favorites"))
		remove.HandleInput(func(ev ui.InputEvent) bool {
			if ev.Kind != ui.InputPointerDown {
				return false
			}
			a.markActivity()
			a.toggleFavorite(r, page)
			return true
		})
		remove.Children(func() {
			ui.Icon(c, readerIconClose).Size(12, 12).TextColor(look.muted)
		})
		ui.Box(c).Padding(2, 6).Radius(999).Background(look.glass).
			Absolute().Bottom(4).Left(4).Children(func() {
			ui.Text(c, itoa(page+1)).FontSize(10).FontWeight(700).
				TextColor(look.muted)
		})
	})
}

// readerShortcuts is the keyboard's own page.
func (a *App) readerShortcuts(c *ui.Context, r *Reader, look readerLook) {
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
			ui.Text(c, row[0]).FontSize(12).FontWeight(700).TextColor(look.fg).Width(130)
			ui.Text(c, row[1]).FontSize(12).TextColor(look.muted).Grow(1)
		})
	}
	ui.Text(c, i18n.T("Drag across the page to turn it, pinch or ctrl-wheel to zoom, drag a zoomed page to move it, and hold a page to keep it")).
		FontSize(11).TextColor(look.muted).MaxWidth(460)
}

// readerShare is the gallery's address, to copy, with the option of starting
// at the page the reader is on.
func (a *App) readerShare(c *ui.Context, r *Reader, look readerLook) {
	url := galleryURL(r.id, r.token)
	ui.Text(c, url).FontSize(12).TextColor(look.fg).MaxWidth(460)
	ui.Row(c).Gap(8).Children(func() {
		if ui.PrimaryButton(c, i18n.T("Copy")).Clicked() {
			a.copyToClipboard(url)
		}
	})
	ui.Text(c, i18n.T("The gallery opens at its cover, whatever page the address came from")).
		FontSize(11).TextColor(look.muted).MaxWidth(460)
}

// readerAbout says what this reader is.
func (a *App) readerAbout(c *ui.Context, r *Reader, look readerLook) {
	ui.Text(c, i18n.T("A native reader built after comimi's manga viewer")).FontSize(12).
		TextColor(look.fg).MaxWidth(460)
	ui.Text(c, "github.com/yui540/comimi").FontSize(11).TextColor(look.muted)
	ui.Text(c, "e-hentai.org/g/"+itoa(int(r.id))+"/"+r.token+"/").FontSize(11).
		TextColor(look.muted)
}

// readerSettingsPanel is the settings card: the reading direction, the page
// mode, the background and how often auto page turn moves on. It floats above
// the dock at the right, as comimi's panel does.
func (a *App) readerSettingsPanel(c *ui.Context, r *Reader, slide, alpha float32) {
	look := readerLookOf(r)
	w, h := c.Size()
	inset := readerInset(w)
	width := float32(300)
	if w < 420 {
		width = maxFloat(w-2*inset, 220)
	}
	maxH := h - 2*inset - 140
	if maxH < 220 {
		maxH = 220
	}
	panel := ui.Column(c).Width(width).MaxHeight(maxH).
		Absolute().Right(inset).Bottom(inset+122).Radius(20).
		Background(look.glassStrong).Shadow(0, 6, 24, 0, look.shadow).Clip().
		Translate(0, slide*chromeSlide*0.5).Opacity(alpha)
	panel.Children(func() {
		ui.Row(c).FillWidth().Padding(18, 20, 8, 20).Gap(12).AlignItems(ui.Center).
			Children(func() {
				ui.Text(c, i18n.T("Settings")).FontSize(16).FontWeight(700).
					TextColor(look.fg).Grow(1)
				a.readerIconButton(c, look, readerIconClose, i18n.T("Close"),
					24, 16, look.muted, func() {
						a.openPanel(r, panelNone)
					})
			})
		ui.Scroll(c).FillWidth().MaxHeight(maxH - 70).Children(func() {
			ui.Column(c).FillWidth().Padding(0, 20, 20, 20).Gap(18).Children(func() {
				ui.Column(c).Gap(8).Children(func() {
					readerLabel(c, look, i18n.T("Reading direction"))
					a.readerChoiceRow(c, look, i18n.T("Right to left"), i18n.T("Left to right"),
						r.settings.rtl,
						func() { a.setDirection(r, true) },
						func() { a.setDirection(r, false) })
				})

				ui.Column(c).Gap(6).Children(func() {
					readerLabel(c, look, i18n.T("Page mode"))
					a.readerCheckRow(c, look, i18n.T("Two pages at a time"), r.settings.double,
						func() { a.setSpread(r, !r.settings.double) })
					a.readerCheckRow(c, look, i18n.T("The first page is a cover"), r.settings.hasCover,
						func() { a.setCover(r, !r.settings.hasCover) })
				})

				ui.Column(c).Gap(8).Children(func() {
					readerLabel(c, look, i18n.T("Background"))
					a.readerChoiceRow(c, look, i18n.T("White"), i18n.T("Black"),
						!r.settings.dark,
						func() { a.setDark(r, false) },
						func() { a.setDark(r, true) })
				})

				ui.Column(c).Gap(8).Children(func() {
					seconds := float64(r.settings.autoInterval()) / 1000
					ui.Row(c).FillWidth().AlignItems(ui.Center).Gap(10).Children(func() {
						readerLabel(c, look, i18n.T("Auto page turn"))
						ui.Box(c).Grow(1)
						ui.Text(c, i18n.Sprintf(i18n.T("%d s"), int(seconds))).
							FontSize(12).FontWeight(700).TextColor(look.fg)
					})
					a.readerSliderRow(c, look, &seconds, autoTurnMin, autoTurnMax, 1, width-40,
						func(v float32) {
							a.setAutoInterval(r, int(v)*1000)
						})
				})
			})
		})
	})
	// The panel edits the reader's settings directly; keep the app's copy in
	// step so the next gallery opens with them.
	a.readerSettings = r.settings
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
}

// setDark sets the paper the pages sit on.
func (a *App) setDark(r *Reader, dark bool) {
	r.settings.dark = dark
	a.readerSettings = r.settings
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
