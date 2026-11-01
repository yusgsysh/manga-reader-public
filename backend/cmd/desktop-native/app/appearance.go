package app

import (
	"github.com/egoist/mygo/ui"
)

// This file holds the look the pages share: the measurements they use, the
// marks in the sidebar and the small pieces (page headers, section titles,
// raised surfaces) that make one page read like the next. The reader has its
// own look in reader_*.go; it is a full-window stage, not a page.

const (
	// pagePadX/pagePadY frame a page's content against the window edge, as
	// main's app-container does at desktop widths (px-8, py-8).
	pagePadX float32 = 32
	pagePadY float32 = 32

	// cardRadius rounds a panel on the page; coverRadius rounds a picture.
	cardRadius  float32 = 14
	coverRadius float32 = 12

	// listCellHeight is the least room a card in a list grid gets: cover plus
	// its text (title, meta, posted). The grid lays rows out with a fixed
	// height, so a narrow window (wider cells, taller 3:4 covers) gets more
	// than this (see listCellHeightFor).
	listCellHeight float32 = 336

	// sidebarWidth matches the column the sidebar takes: main's expanded
	// 15rem rail.
	sidebarWidth float32 = 240
	// listCardText is the room under a card's cover, as main's GalleryCard
	// stacks it: 8 above the two-line title, 40 for the title, 6, the meta
	// line, 6, the posted line.
	listCardText float32 = 96
	// listGap is the space between cards in the grid (main's 20px at desktop
	// widths). The grid view spaces cells by the theme's own gap, so the
	// list draws the rest as padding around each cell (see listGridInset).
	listGap float32 = 20
	// listViewGap is GridView's own cell gap at the default theme spacing.
	listViewGap float32 = 8
)

// navIcon is the mark beside a sidebar item, drawn as a line icon so it takes
// the colour of the item (and its text, when chosen).
type navIcon int

const (
	iconHome navIcon = iota
	iconWatched
	iconPopular
	iconSearch
	iconBookshelf
	iconRecent
	iconDownloads
	iconTags
	iconSettings
)

// iconSVG is the artwork for each mark, 24×24 line drawings. Parsed once at
// start-up rather than per frame.
var iconSVG = map[navIcon]*ui.SVG{
	iconHome:      ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3 10.5 12 3l9 7.5"/><path d="M5.5 9.5V20h13V9.5"/><path d="M9.5 20v-5.5h5V20"/></svg>`)),
	iconWatched:   ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="2.6"/></svg>`)),
	iconPopular:   ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3s5.5 4.3 5.5 9a5.5 5.5 0 0 1-11 0c0-1.7.6-3 1.5-4.1.2 1.8 1 2.9 2.3 3.3C9.6 8.4 10.7 5.6 12 3z"/></svg>`)),
	iconSearch:    ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="10.5" cy="10.5" r="6.5"/><path d="m15.5 15.5 5 5"/></svg>`)),
	iconBookshelf: ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M6 3.5h12v17l-6-4.2-6 4.2v-17z"/></svg>`)),
	iconRecent:    ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><circle cx="12" cy="12" r="8.5"/><path d="M12 7.5V12l3.2 2"/></svg>`)),
	iconDownloads: ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3.5v11"/><path d="m7.5 10 4.5 4.5 4.5-4.5"/><path d="M4.5 19.5h15"/></svg>`)),
	iconTags:      ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 11.5V4.5h7l9.5 9.5-7 7-9.5-9.5z"/><circle cx="7.5" cy="7.5" r="1.1"/></svg>`)),
	iconSettings:  ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M4 7h9"/><circle cx="16.5" cy="7" r="2.2"/><path d="M4 12h3"/><circle cx="10" cy="12" r="2.2"/><path d="M4 17h11"/><circle cx="18" cy="17" r="2.2"/></svg>`)),
}

// listColumns is main's .gallery-grid breakpoints, counted on the window:
// 2 columns under 480, 3 under 768, 4 under 1024, 5 under 1440, 6 under 1920,
// 7 from there up. Counting on the window (not the content) keeps the native
// grid changing at the same sizes as the web frontend.
func listColumns(windowWidth float32) int {
	switch {
	case windowWidth < 480:
		return 2
	case windowWidth < 768:
		return 3
	case windowWidth < 1024:
		return 4
	case windowWidth < 1440:
		return 5
	case windowWidth < 1920:
		return 6
	default:
		return 7
	}
}

// listGridInset is the horizontal room a list cell leaves around its card.
// GridView spaces its cells by the theme's gap (8 at default spacing), and
// main spaces cards by 20; the cell draws the difference as padding on each
// side, while the grid itself hangs 6 past the page's padding so the covers
// still start at the page edge. The cell's own 2-point border takes the last
// 4 of the 12 inset, leaving the cover at main's column width.
const listGridInset float32 = (listGap - listViewGap) / 2

// listCellWidth is the cover's width at this window size, as main's 1fr
// columns work it out from the content width and the grid's gap.
func listCellWidth(windowWidth, rail float32) float32 {
	cols := float32(listColumns(windowWidth))
	content := windowWidth - rail - 2*pagePadX
	return (content - listGap*(cols-1)) / cols
}

// listCellHeightFor is the row height a list grid needs at this window size:
// a fixed height cannot follow the 3:4 cover as the cells widen in a narrow
// window, and a card taller than its row would paint over the row below. The
// 12 extra points are the vertical half of the gap that the cell leaves
// (GridView's own row padding accounts for the other 8).
func listCellHeightFor(c *ui.Context, rail float32) float32 {
	w, _ := c.Size()
	cellW := listCellWidth(w, rail)
	return max(listCellHeight, cellW*4.0/3.0+listCardText+listGridInset*2)
}

// listGridMinWidth is the minWidth GridView needs to lay out main's column
// count at this window size. The grid hangs listGridInset past each edge, so
// its rows are 12 points wider than the page content.
func listGridMinWidth(windowWidth, rail float32) float32 {
	cols := float32(listColumns(windowWidth))
	return (windowWidth-rail-2*pagePadX+listGap)/cols - listViewGap
}

// starSVG is the rating mark: a filled five-point star.
var starSVG = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="m12 2.6 2.9 5.9 6.5.9-4.7 4.6 1.1 6.5L12 17.4l-5.8 3.1 1.1-6.5L2.6 9.4l6.5-.9L12 2.6z"/></svg>`))

// filterSVG is the advanced-search mark in a page header.
var filterSVG = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><path d="M3.5 5h17l-6.5 7.5V20l-4-2v-5.5L3.5 5z"/></svg>`))

// calendarSVG is the browse-position mark in a page header.
var calendarSVG = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round"><rect x="3.5" y="5" width="17" height="16" rx="2.5"/><path d="M3.5 9.5h17"/><path d="M8 3v4M16 3v4"/></svg>`))

// sparkleSVG is the latest-list mark main puts beside "最新": a four-point
// star with a smaller one at its shoulder.
var sparkleSVG = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="M10 3.5 11.7 8.3 16.5 10 11.7 11.7 10 16.5 8.3 11.7 3.5 10 8.3 8.3 10 3.5z"/><path d="M17.5 13.5 18.5 16 21 17l-2.5 1-1 2.5-1-2.5L14 17l2.5-1 1-2.5z"/></svg>`))

// chevronRightSVG is the continue row's page button.
var chevronRightSVG = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7"/></svg>`))

// tagSVG is the tag mark on a card's cover button.
var tagSVG = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" fill-rule="evenodd" stroke="none"><path d="M3.5 4.5h7l9.5 9.5-7 7-9.5-9.5zM8.2 9.2a1.5 1.5 0 1 0 0-3 1.5 1.5 0 0 0 0 3z"/></svg>`))

// pageHeader is the title block every page starts with: an accent tile with
// the page's mark, its name, and a muted line saying what the page holds. It
// follows main's PageHeader: a 36-point tile and a 16-point title rather than
// a poster headline, so the gallery covers stay the loudest thing on the page.
func pageHeader(c *ui.Context, title, subtitle string, mark *ui.SVG) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(12).Shrink(0).Children(func() {
		if mark != nil {
			ui.Box(c).Size(36, 36).Radius(12).Center().Shrink(0).
				Background(t.Accent.Alpha(0.12)).Children(func() {
				ui.Icon(c, mark).Size(20, 20).TextColor(t.Accent)
			})
		}
		ui.Column(c).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(16).FontWeight(600).SingleLine()
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(13).TextColor(t.TextMuted).SingleLine()
			}
		})
	})
}

// sectionHeader is main's Section header: a 13-point semibold title and an
// optional accent link at the right, above the section's content.
func sectionHeader(c *ui.Context, title, action string, run func()) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(8).Shrink(0).Children(func() {
		ui.Text(c, title).FontSize(13).FontWeight(600).FixedLineHeight(16).Grow(1).SingleLine()
		if action != "" {
			link := ui.Text(c, action).FontSize(12).FontWeight(500).TextColor(t.Accent)
			link.Cursor(ui.CursorPointer)
			if run != nil && link.Clicked() {
				run()
			}
		}
	})
}

// iconAction is the quiet 32-point icon button main puts at the end of a
// page's header (advanced search, browse position). active gives it main's
// pressed look: an accent-tinted square with the accent mark.
func iconAction(c *ui.Context, mark *ui.SVG, label string, active bool) ui.Element {
	t := c.Theme()
	color := t.TextMuted
	btn := ui.Box(c).Size(32, 32).Radius(8).Center().Shrink(0).
		Background(ui.Transparent).Focusable().Tooltip(label).Label(label)
	switch {
	case active:
		btn.Background(t.Accent.Alpha(0.12))
		color = t.Accent
	case btn.Hovered():
		btn.Background(t.SurfaceHover)
	}
	if btn.Focused() && !active {
		color = t.Text
	}
	btn.Children(func() {
		ui.Icon(c, mark).Size(16, 16).TextColor(color)
	})
	return btn
}

// chip is a small pill label, as main's Chip and CategoryChip: 12-point
// medium text in 2×8 padding, fully rounded.
func chip(c *ui.Context, label string, bg, fg ui.Color) {
	ui.Box(c).Radius(999).Padding(2, 8).Background(bg).Shrink(0).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(500).FixedLineHeight(16).TextColor(fg).SingleLine()
	})
}

// listIcon picks the page mark for a list kind, so the sidebar and the page
// header agree on what the page is.
func listIcon(kind listKind) *ui.SVG {
	switch kind {
	case kindWatched:
		return iconSVG[iconWatched]
	case kindPopular:
		return iconSVG[iconPopular]
	case kindSearch:
		return iconSVG[iconSearch]
	case kindBookshelf:
		return iconSVG[iconBookshelf]
	case kindRecent:
		return iconSVG[iconRecent]
	default:
		return iconSVG[iconHome]
	}
}

// categoryChip is the category name in the colour the source site gives it,
// so a doujinshi and a manga are told apart before the label is read.
func categoryChip(c *ui.Context, g Gallery) {
	if g.Category == "" {
		return
	}
	chip(c, g.Category, categoryColor(g.CategoryCode), ui.Hex("#ffffff"))
}

// categoryColor is ExHentai's category colour for the API's code, falling
// back to the neutral gray of "other".
func categoryColor(code string) ui.Color {
	switch code {
	case "doujinshi":
		return ui.Hex("#fc4e4e")
	case "manga":
		return ui.Hex("#e78c1a")
	case "artistcg":
		return ui.Hex("#c7bf07")
	case "gamecg":
		return ui.Hex("#1a9317")
	case "western":
		return ui.Hex("#5dc13b")
	case "image-set":
		return ui.Hex("#2756aa")
	case "cosplay":
		return ui.Hex("#8800c3")
	case "asianporn":
		return ui.Hex("#b452a5")
	case "non-h":
		return ui.Hex("#0f9ebd")
	default:
		return ui.Hex("#707070")
	}
}

// sectionTitle heads a part of a page, with an optional muted note after it.
func sectionTitle(c *ui.Context, title, note string) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(10).Shrink(0).Children(func() {
		ui.Text(c, title).FontSize(16).Bold()
		if note != "" {
			ui.Text(c, note).FontSize(12).TextColor(t.TextMuted)
		}
	})
}

// surfaceCard is the raised panel that groups related controls: a rounded,
// very light surface with a hairline border, so the page reads as groups
// rather than one long list of rows.
func surfaceCard(c *ui.Context, fn func()) {
	t := c.Theme()
	ui.Column(c).Gap(14).Padding(18, 20).Radius(cardRadius).
		Background(t.Surface).Border(1, t.Border).Children(fn)
}

// coverRating is the rating in a cover's top corner: the star and the score
// in white text, as main's card overlays them (its text shadow has no native
// counterpart, so the chip is on the artwork alone).
func coverRating(c *ui.Context, g Gallery) {
	if g.Rating <= 0 {
		return
	}
	ui.Box(c).Absolute().Top(0).Right(0).Padding(4, 8).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(2).Children(func() {
			ui.Icon(c, starSVG).Size(12, 12).TextColor(ui.Hex("#fbbf24"))
			ui.Textf(c, "%.1f", g.Rating).FontSize(11).FontWeight(500).TextColor(ui.Hex("#ffffff"))
		})
	})
}

// settingsSection is one part of the settings page: a hairline above it, its
// heading, and the rows under the heading. Flat, as main's Section is, so a
// long settings page scrolls as one list rather than a stack of panels.
func settingsSection(c *ui.Context, title string, fn func()) {
	ui.Divider(c)
	ui.Column(c).Gap(12).Children(func() {
		if title != "" {
			sectionTitle(c, title, "")
		}
		fn()
	})
}

// shadowInk is the colour under a lifted picture: dark but faint by day, and
// a little deeper in the dark theme where the background is already black.
func shadowInk(t *ui.Theme) ui.Color {
	if t.Dark {
		return ui.RGBA(0, 0, 0, 0.55)
	}
	return ui.RGBA(15, 23, 42, 0.16)
}

// readProgress is how much of a gallery has been read, 0 when it has not been
// opened. It is the fraction of the cover's width the progress line fills.
func readProgress(g Gallery) float32 {
	if g.Reading == nil || g.Pages <= 0 {
		return 0
	}
	page := g.Reading.CurrentPage
	if page < 0 {
		return 0
	}
	if page >= g.Pages-1 {
		return 1
	}
	return float32(page+1) / float32(g.Pages)
}

// coverProgress draws the thin line across the bottom of a cover that says
// how far the gallery has been read: main's card puts a solid accent line on
// a translucent accent track. Box must already be clipped so the line
// follows the picture's rounded corners.
func coverProgress(c *ui.Context, g Gallery) {
	if readProgress(g) <= 0 {
		return
	}
	t := c.Theme()
	ui.Box(c).Height(4).FillWidth().Background(t.Accent.Alpha(0.3)).
		Absolute().Left(0).Bottom(0).Children(func() {
		ui.Box(c).Height(4).WidthPercent(readProgress(g) * 100).Background(t.Accent)
	})
}
