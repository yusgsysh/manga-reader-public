package app

import (
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// listFilters is the home page's advanced-search state, as main keeps it in
// the URL. Empty categories means every category, as the endpoint treats a
// missing list.
type listFilters struct {
	categories []string
	minPages   float64
	maxPages   float64
	minRating  float64
	hasTorrent bool
}

// allCategories is the category codes the endpoint accepts, in main's order.
var allCategories = []string{
	"doujinshi", "manga", "artistcg", "gamecg", "western", "image-set",
	"cosplay", "asianporn", "non-h", "misc", "other",
}

// values turns the filters and the browse position into the query the list
// endpoints take, named as main's advancedToParams/navToParams name them.
func (f listFilters) values(nav listNav) url.Values {
	v := url.Values{}
	if len(f.categories) > 0 {
		v.Set("categories", strings.Join(f.categories, ","))
	}
	if f.minPages > 0 {
		v.Set("min_pages", itoa(int(f.minPages)))
	}
	if f.maxPages > 0 {
		v.Set("max_pages", itoa(int(f.maxPages)))
	}
	if f.minRating > 0 {
		v.Set("min_rating", trimFloat(f.minRating))
	}
	if f.hasTorrent {
		v.Set("has_torrent", "true")
	}
	if nav.seek != "" {
		v.Set("seek", nav.seek)
	}
	if nav.jump != "" {
		v.Set("jump", nav.jump)
	}
	return v
}

// active counts the filters that narrow the list: the header button's pressed
// look and the count in its corner badge.
func (f listFilters) active() int {
	n := 0
	if len(f.categories) > 0 && len(f.categories) < len(allCategories) {
		n += len(allCategories) - len(f.categories)
	}
	if f.minPages > 0 || f.maxPages > 0 {
		n++
	}
	if f.minRating > 0 {
		n++
	}
	if f.hasTorrent {
		n++
	}
	return n
}

// included reports whether code is one of the chosen categories: none chosen
// means every category.
func (f listFilters) included(code string) bool {
	if len(f.categories) == 0 {
		return true
	}
	for _, cat := range f.categories {
		if cat == code {
			return true
		}
	}
	return false
}

// toggleCategory adds or removes one category from the chosen set, folding
// "every category" back into an empty list.
func (f *listFilters) toggleCategory(code string) {
	chosen := make([]string, 0, len(allCategories))
	for _, cat := range allCategories {
		on := f.included(cat)
		if cat == code {
			on = !on
		}
		if on {
			chosen = append(chosen, cat)
		}
	}
	if len(chosen) == len(allCategories) {
		f.categories = nil
		return
	}
	f.categories = chosen
}

// listNav is the browse position: a chosen date (seek) or a quick jump
// relative to the newest galleries (jump), as main's listingParams keep them.
type listNav struct {
	seek string
	jump string
}

// active reports whether the position narrows the list at all.
func (n listNav) active() bool {
	return n.seek != "" || n.jump != ""
}

// homePage is main's HomePage: the continue-reading row on top (whenever
// there is one), then the latest galleries under their own header.
func (a *App) homePage(c *ui.Context, s *listState) {
	// The row and the reading-history page show the same list, fetched once:
	// reading a gallery in the reader updates both through mergeListReading.
	recent := a.listFor("/recently-read", kindRecent)
	a.ensureList(recent)
	a.scheduleRefresh(c, recent)

	ui.Column(c).Fill().Padding(pagePadY, pagePadX).Gap(32).Children(func() {
		if items := recentItems(recent, 12); len(items) > 0 {
			ui.Column(c).Gap(12).Children(func() {
				sectionHeader(c, i18n.T("Continue reading"), i18n.T("View all"), func() {
					a.router.Push("/recently-read")
				})
				a.continueRow(c, items)
			})
		}
		ui.Column(c).Gap(20).Grow(1).MinHeight(0).Children(func() {
			a.latestHeader(c, s)
			a.listBody(c, s)
		})
	})
}

// recentItems is the first n galleries of the reading history.
func recentItems(s *listState, n int) []Gallery {
	if len(s.items) < n {
		return s.items
	}
	return s.items[:n]
}

// continueRow is main's strip of recently read galleries: 144-point cards in
// a horizontal scroller, 16 apart.
func (a *App) continueRow(c *ui.Context, items []Gallery) {
	ui.ScrollHorizontal(c).Shrink(0).Children(func() {
		ui.Row(c).Gap(16).PaddingX(4).Padding(0, 0, 8, 0).Children(func() {
			for _, g := range items {
				cell := ui.Column(c).Width(144).Shrink(0).Gap(0).Focusable()
				hovered := cell.Hovered()
				cell.Children(func() {
					a.coverBox(c, g, hovered)
					ui.Column(c).Gap(6).Padding(8, 0, 0, 0).Children(func() {
						ui.Text(c, displayTitle(g)).FontSize(13).FontWeight(500).
							FixedLineHeight(20).MaxLines(2).MinHeight(40)
						a.cardMeta(c, g)
					})
				})
				if cell.Clicked() {
					a.router.Push(fmt.Sprintf("/gallery/%d/%s", g.ID, g.Token))
				}
			}
		})
	})
}

// latestHeader is the "最新" PageHeader with its two quiet actions, as main
// has them: the advanced-search funnel and the browse-position calendar.
func (a *App) latestHeader(c *ui.Context, s *listState) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(12).Shrink(0).Children(func() {
		ui.Box(c).Size(36, 36).Radius(12).Center().Shrink(0).
			Background(t.Accent.Alpha(0.12)).Children(func() {
			ui.Icon(c, sparkleSVG).Size(20, 20).TextColor(t.Accent)
		})
		ui.Text(c, i18n.T("Latest")).FontSize(16).FontWeight(600).SingleLine()
		ui.Box(c).Grow(1)
		ui.Row(c).Gap(8).Shrink(0).AlignItems(ui.Center).Children(func() {
			a.filterButton(c, s)
			a.jumpButton(c, s)
		})
	})
}

// filterButton is the funnel in the header and its advanced-search panel.
func (a *App) filterButton(c *ui.Context, s *listState) {
	active := s.filters.active()
	btn := iconAction(c, filterSVG, i18n.T("Advanced search"), active > 0)
	if active > 0 {
		// The count sits in the button's corner, as main's funnel has it.
		btn.Children(func() {
			t := c.Theme()
			ui.Box(c).Size(14, 14).Radius(999).Center().Absolute().Top(-2).Right(-2).
				Background(t.Accent).Children(func() {
				ui.Text(c, itoa(active)).FontSize(9).FontWeight(600).
					TextColor(t.AccentText).SingleLine()
			})
		})
	}
	if btn.Clicked() {
		a.filterOpen = !a.filterOpen
		if a.filterOpen {
			a.jumpOpen = false
			a.filterDraft = s.filters
		}
	}
	if !a.filterOpen {
		return
	}
	t := c.Theme()
	ui.PopoverBase(c, btn, &a.filterOpen, func(panel ui.Element) {
		panel.AttachTo(btn, ui.AnchorBottomEnd, ui.AnchorTopEnd).
			Margin(8, 0, 0, 0).Padding(16).Width(360).Radius(12).
			Background(t.Background).Border(1, t.Border).
			Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.18))
		panel.Children(func() { a.filterPanel(c, s) })
	})
}

// filterPanel edits a draft of the filters; only Apply commits them to the
// list, as main's popover does.
func (a *App) filterPanel(c *ui.Context, s *listState) {
	t := c.Theme()
	ui.Column(c).Gap(16).Children(func() {
		ui.Text(c, i18n.T("Advanced search")).FontSize(13).FontWeight(600).SingleLine()

		ui.Column(c).Gap(8).Children(func() {
			ui.Text(c, i18n.T("Categories")).FontSize(12).TextColor(t.TextMuted).SingleLine()
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for _, code := range allCategories {
					on := a.filterDraft.included(code)
					bg, fg := t.SurfaceHover, t.TextMuted
					if on {
						bg, fg = categoryColor(code), ui.Hex("#ffffff")
					}
					ch := ui.Box(c).Padding(2, 8).Radius(999).Background(bg).Shrink(0).
						Cursor(ui.CursorPointer).Focusable().Label(categoryLabel(code))
					ch.Children(func() {
						ui.Text(c, categoryLabel(code)).FontSize(12).FontWeight(500).TextColor(fg).SingleLine()
					})
					if ch.Clicked() {
						a.filterDraft.toggleCategory(code)
					}
				}
			})
		})

		ui.Row(c).Gap(12).Children(func() {
			ui.Column(c).Gap(4).Grow(1).Children(func() {
				ui.Text(c, i18n.T("Minimum pages")).FontSize(12).TextColor(t.TextMuted).SingleLine()
				ui.NumberInput(c, &a.filterDraft.minPages, 0, 100000, 1).Label(i18n.T("Minimum pages"))
			})
			ui.Column(c).Gap(4).Grow(1).Children(func() {
				ui.Text(c, i18n.T("Maximum pages")).FontSize(12).TextColor(t.TextMuted).SingleLine()
				ui.NumberInput(c, &a.filterDraft.maxPages, 0, 100000, 1).Label(i18n.T("Maximum pages"))
			})
		})

		ui.Column(c).Gap(4).Children(func() {
			ui.Text(c, i18n.T("Minimum rating")).FontSize(12).TextColor(t.TextMuted).SingleLine()
			ui.NumberInput(c, &a.filterDraft.minRating, 0, 5, 0.1).Label(i18n.T("Minimum rating"))
		})

		ui.Checkbox(c, &a.filterDraft.hasTorrent, i18n.T("Has torrent"))

		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, i18n.T("Reset")).Clicked() {
				a.filterDraft = listFilters{}
			}
			if ui.PrimaryButton(c, i18n.T("Apply")).Clicked() {
				if a.filterDraft.maxPages > 0 && a.filterDraft.minPages > a.filterDraft.maxPages {
					a.filterDraft.minPages, a.filterDraft.maxPages = a.filterDraft.maxPages, a.filterDraft.minPages
				}
				s.filters = a.filterDraft
				a.filterOpen = false
				a.reloadList(s)
			}
		})
	})
}

// jumpButton is the calendar in the header and its browse-position panel:
// quick jumps relative to the newest galleries, or a chosen date.
func (a *App) jumpButton(c *ui.Context, s *listState) {
	btn := iconAction(c, calendarSVG, i18n.T("Browse position"), s.nav.active())
	if btn.Clicked() {
		a.jumpOpen = !a.jumpOpen
		if a.jumpOpen {
			a.filterOpen = false
			a.jumpDate = time.Now()
		}
	}
	if !a.jumpOpen {
		return
	}
	t := c.Theme()
	ui.PopoverBase(c, btn, &a.jumpOpen, func(panel ui.Element) {
		panel.AttachTo(btn, ui.AnchorBottomEnd, ui.AnchorTopEnd).
			Margin(8, 0, 0, 0).Padding(16).Width(280).Radius(12).
			Background(t.Background).Border(1, t.Border).
			Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.18))
		panel.Children(func() { a.jumpPanel(c, s) })
	})
}

// jumpPanel applies a browse position at once, as main's JumpSeekPanel does.
func (a *App) jumpPanel(c *ui.Context, s *listState) {
	t := c.Theme()
	type quick struct{ value, label string }
	quicks := []quick{
		{"1d", i18n.T("1 day")},
		{"3d", i18n.T("3 days")},
		{"1w", i18n.T("1 week")},
		{"1m", i18n.T("1 month")},
		{"1y", i18n.T("1 year")},
	}
	apply := func(nav listNav) {
		s.nav = nav
		a.jumpOpen = false
		a.reloadList(s)
	}

	ui.Column(c).Gap(14).Children(func() {
		ui.Text(c, i18n.T("Browse position")).FontSize(13).FontWeight(600).SingleLine()
		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, i18n.T("Quick jump")).FontSize(12).TextColor(t.TextMuted).SingleLine()
			ui.Row(c).Gap(6).Wrap().Children(func() {
				for _, q := range quicks {
					selected := s.nav.jump == q.value
					bg, fg := t.SurfaceHover, t.Text
					if selected {
						bg, fg = t.Accent.Alpha(0.16), t.Accent
					}
					b := ui.Box(c).Padding(4, 10).Radius(8).Background(bg).Shrink(0).
						Cursor(ui.CursorPointer).Focusable().Label(q.label)
					b.Children(func() {
						ui.Text(c, q.label).FontSize(12).FontWeight(500).TextColor(fg).SingleLine()
					})
					if b.Clicked() {
						apply(listNav{jump: q.value})
					}
				}
			})
		})

		ui.Column(c).Gap(6).Children(func() {
			ui.Text(c, i18n.T("Pick a date")).FontSize(12).TextColor(t.TextMuted).SingleLine()
			date := ui.DateInput(c, &a.jumpDate).Label(i18n.T("Pick a date"))
			if date.Changed() {
				apply(listNav{seek: a.jumpDate.Format("2006-01-02")})
			}
		})

		ui.Row(c).Gap(8).Justify(ui.End).Children(func() {
			if ui.Button(c, i18n.T("Back to latest")).Disabled(!s.nav.active()).Clicked() {
				apply(listNav{})
			}
		})
	})
}

// trimFloat prints a rating as main's URL does: 4 rather than 4.0, 4.5 as is.
func trimFloat(v float64) string {
	return strconv.FormatFloat(v, 'f', -1, 64)
}
