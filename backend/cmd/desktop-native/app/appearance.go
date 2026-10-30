package app

import (
	"github.com/egoist/mygo/ui"
)

// This file holds the look the pages share: the measurements they use, the
// marks in the sidebar and the small pieces (page headers, section titles,
// raised surfaces) that make one page read like the next. The reader has its
// own look in reader_*.go; it is a full-window stage, not a page.

const (
	// pagePadX/pagePadY frame a page's content against the window edge.
	pagePadX float32 = 28
	pagePadY float32 = 24

	// cardRadius rounds a panel on the page; coverRadius rounds a picture.
	cardRadius  float32 = 14
	coverRadius float32 = 11

	// listCellHeight is the least room a card in a list grid gets: cover plus
	// its three lines of text. The grid lays rows out with a fixed height, so
	// a narrow window (wider cells, taller 3:4 covers) gets more than this
	// (see listCellHeightFor).
	listCellHeight float32 = 336

	// sidebarWidth matches the column the sidebar takes.
	sidebarWidth float32 = 220
	// listCardText is the room under a card's cover: gap, title, meta, tags.
	listCardText float32 = 82
	// listCardMinWidth is the grid's minWidth, so the column count here
	// matches the one GridView works out from the same width.
	listCardMinWidth float32 = 190
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

// listCellHeightFor is the row height a list grid needs at this window size:
// a fixed height cannot follow the 3:4 cover as the cells widen in a narrow
// window, and a card taller than its row would paint over the row below.
func listCellHeightFor(c *ui.Context) float32 {
	w, _ := c.Size()
	gap := c.Theme().Space(2)
	content := w - sidebarWidth - 2*pagePadX
	cols := max(1, int((content+gap)/(listCardMinWidth+gap)))
	cellW := (content - gap*float32(cols-1)) / float32(cols)
	return max(listCellHeight, cellW*4.0/3.0+listCardText)
}

// starSVG is the rating mark: a filled five-point star.
var starSVG = ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="m12 2.6 2.9 5.9 6.5.9-4.7 4.6 1.1 6.5L12 17.4l-5.8 3.1 1.1-6.5L2.6 9.4l6.5-.9L12 2.6z"/></svg>`))

// sidebarMark is the tile beside the app name: the book mark, in the accent
// colour, sized to sit with the 15 pt title.
func sidebarMark(c *ui.Context) {
	t := c.Theme()
	ui.Box(c).Size(28, 28).Radius(8).Center().Background(t.Accent).Shrink(0).Children(func() {
		ui.Icon(c, bookSVG).Size(17, 17).TextColor(t.AccentText)
	})
}

// pageHeader is the title block every page starts with: an accent tile with
// the page's mark, its name, and a muted line saying what the page holds. It
// follows main's PageHeader: a 20 pt title rather than a poster headline, so
// the gallery covers stay the loudest thing on the page.
func pageHeader(c *ui.Context, title, subtitle string, mark *ui.SVG) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(12).Shrink(0).Children(func() {
		if mark != nil {
			ui.Box(c).Size(38, 38).Radius(11).Center().Shrink(0).
				Background(t.Accent.Alpha(0.12)).Children(func() {
				ui.Icon(c, mark).Size(21, 21).TextColor(t.Accent)
			})
		}
		ui.Column(c).Gap(1).Children(func() {
			ui.Text(c, title).FontSize(20).FontWeight(600).SingleLine()
			if subtitle != "" {
				ui.Text(c, subtitle).FontSize(13).TextColor(t.TextMuted).SingleLine()
			}
		})
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
	ink := ui.Hex("#ffffff")
	ui.Badge(c, g.Category).Background(categoryColor(g.CategoryCode)).TextColor(ink)
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

// coverRating is the rating badge in a cover's top corner: a dark pill that
// stays readable over any artwork, as main's card has.
func coverRating(c *ui.Context, g Gallery) {
	if g.Rating <= 0 {
		return
	}
	ui.Box(c).Absolute().Top(8).Right(8).Radius(999).Padding(3, 7).
		Background(ui.RGBA(0, 0, 0, 0.5)).Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(3).Children(func() {
			ui.Icon(c, starSVG).Size(11, 11).TextColor(ui.Hex("#fbbf24"))
			ui.Textf(c, "%.1f", g.Rating).FontSize(10).FontWeight(600).TextColor(ui.Hex("#ffffff"))
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
// how far the gallery has been read. Box must already be clipped so the line
// follows the picture's rounded corners.
func coverProgress(c *ui.Context, g Gallery) {
	if readProgress(g) <= 0 {
		return
	}
	t := c.Theme()
	ui.Box(c).Height(3).WidthPercent(readProgress(g) * 100).Background(t.Accent).
		Absolute().Left(0).Bottom(0)
}
