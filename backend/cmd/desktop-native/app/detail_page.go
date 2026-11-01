package app

import (
	"fmt"

	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// The gallery page follows main's GalleryDetailPage: a quiet way back, the
// cover beside the gallery's facts and actions, the tags grouped by
// namespace, and the pages twenty at a time under the gallery. The
// measurements below are the ones main's kumo theme uses at a desktop width.

const (
	detailPagesPerGrid = 20

	detailCoverWidth  float32 = 220
	detailCoverRadius float32 = 16

	// main's primary action is kumo's emphasis blue, a shade lighter than the
	// navigation rail's accent.
	detailPrimaryColor = "#3671ee"
)

// detailIcon is the line art the gallery page's controls carry, as main's
// phosphor icons do.
type detailIcon int

const (
	iconArrowLeft detailIcon = iota
	iconBookOpen
	iconBookmark
	iconBookmarkFilled
	iconDownload
	iconMagnet
	iconCloudSlash
	iconCaretLeft
	iconCaretRight
)

var detailIconSVG = map[detailIcon]*ui.SVG{
	iconArrowLeft:      ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M19 12H5"/><path d="m11 18-6-6 6-6"/></svg>`)),
	iconBookOpen:       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="M12 4.6c-1.9-1.4-4.5-2-7.2-2H3.5A1.5 1.5 0 0 0 2 4.1v13.2a1.5 1.5 0 0 0 1.5 1.5h1.6c2.6 0 5 .7 6.9 2 1.9-1.3 4.3-2 6.9-2h1.6a1.5 1.5 0 0 0 1.5-1.5V4.1a1.5 1.5 0 0 0-1.5-1.5h-1.3c-2.7 0-5.3.6-7.2 2Zm-1 13.5c-1.6-1-3.6-1.5-5.7-1.5H4V4.6h.8c2.3 0 4.5.5 6.2 1.7v11.8Zm2-11.8c1.7-1.2 3.9-1.7 6.2-1.7h.8v12h-1.3c-2.1 0-4.1.5-5.7 1.5V6.3Z"/></svg>`)),
	iconBookmark:       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M6 3.5h12v17l-6-4.2-6 4.2v-17z"/></svg>`)),
	iconBookmarkFilled: ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="currentColor" stroke="none"><path d="M6 3.5h12v17l-6-4.2-6 4.2v-17z"/></svg>`)),
	iconDownload:       ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M12 3.5v11"/><path d="m7.5 10 4.5 4.5 4.5-4.5"/><path d="M4.5 19.5h15"/></svg>`)),
	iconMagnet:         ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M6 4v7a6 6 0 0 0 12 0V4"/><path d="M6 8h4M14 8h4"/><path d="M6 4h4M14 4h4"/></svg>`)),
	iconCloudSlash:     ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M4 4l16 16"/><path d="M8 8.5A5 5 0 0 0 7.5 18H17a4 4 0 0 0 2.3-.7"/><path d="M18.5 14.5A5 5 0 0 0 15 6a6 6 0 0 0-4.3 1.6"/></svg>`)),
	iconCaretLeft:      ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m15 5-7 7 7 7"/></svg>`)),
	iconCaretRight:     ui.MustParseSVG([]byte(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="m9 5 7 7-7 7"/></svg>`)),
}

// galleryPage is main's gallery detail page.
func (a *App) galleryPage(c *ui.Context, id int64, token string) {
	s := a.ensureDetail(id, token)
	// Rules colour the tags and the translation database names them, so load
	// both with the page.
	a.ensureTagSettings()
	a.ensureTranslations()
	t := c.Theme()

	if s.loading && !s.loaded {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			pageLoading(c, i18n.T("Loading gallery…"))
			ui.Text(c, i18n.T("Loading gallery…")).FontSize(12).TextColor(t.TextMuted)
		})
		return
	}
	if s.err != "" && !s.loaded {
		a.errorBlock(c, s.err, func() { a.retryDetail(s) })
		return
	}

	g := s.g
	cover := a.images.Get(g.CoverURL)
	if cover == nil {
		cover = a.covers.Get(int(g.ID))
	}

	// The page the reader would open at: the saved progress, and the page
	// this app saved in the gallery, which the backend's progress row can
	// lag behind.
	page, finished := s.reading, finishedReading(g.Reading)
	if known, ok := a.knownReading(g.ID); ok && known.CurrentPage > page {
		page, finished = known.CurrentPage, known.Completed
	}

	w, _ := c.Size()
	content := w - a.railWidth() - 2*pagePadX
	if content < 200 {
		content = 200
	}
	stacked := content < 640

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(pagePadY, pagePadX).Gap(32).Children(func() {
			a.detailBack(c)

			ui.Column(c).Gap(24).Children(func() {
				info := func() {
					ui.Column(c).Gap(20).Children(func() {
						a.detailTitle(c, g)
						a.detailSummary(c, s, g)
						a.detailActions(c, s, g, page, finished)
						a.detailMetadata(c, s, stacked)
					})
				}
				if stacked {
					ui.Column(c).Gap(24).Children(func() {
						ui.Row(c).Justify(ui.Center).Children(func() {
							a.detailCover(c, g, cover)
						})
						ui.Column(c).Children(info)
					})
				} else {
					ui.Row(c).Gap(24).AlignItems(ui.Start).Children(func() {
						a.detailCover(c, g, cover)
						ui.Column(c).Grow(1).Children(info)
					})
				}

				if len(s.tags) > 0 {
					a.detailTags(c, s.tags)
				}
			})

			a.detailPagesSection(c, s, g, content)
		})
	})

	a.torrentsDialog(c, s)
}

// detailBack is the quiet way out of the page, as main's ghost button: 12-point
// text in a 26-point-high row with no face until hovered.
func (a *App) detailBack(c *ui.Context) {
	t := c.Theme()
	btn := ui.Button(c, "").Height(26).Padding(0, 8).Radius(6).Gap(8).
		Background(ui.Transparent).BorderWidth(0).AlignSelf(ui.Start).Shrink(0).FontSize(12)
	if btn.Hovered() {
		btn.Background(t.SurfaceHover)
	}
	btn.Children(func() {
		ui.Icon(c, detailIconSVG[iconArrowLeft]).Size(16, 16)
		ui.Text(c, i18n.T("Back")).FontSize(12).FontWeight(500).FixedLineHeight(16).SingleLine()
	})
	if btn.Clicked() {
		a.router.Back()
	}
}

// detailCover is the cover beside the facts: 220 wide, 3:4, rounded 16 with a
// hairline ring and main's lifted shadow.
func (a *App) detailCover(c *ui.Context, g Gallery, cover *ui.Bitmap) {
	t := c.Theme()
	box := ui.Box(c).Size(detailCoverWidth, detailCoverWidth*4.0/3.0).
		Radius(detailCoverRadius).Clip().Shrink(0).
		Background(t.SurfaceHover).Border(1, t.Border).
		Shadow(0, 10, 15, 0, ui.RGBA(0, 0, 0, 0.10))
	box.Children(func() {
		if cover != nil {
			ui.Image(c, cover).Fill().Fit(ui.Cover).Absolute()
		}
		if a.images.Loading(g.CoverURL) {
			ui.Column(c).Fill().Center().Absolute().Children(func() {
				imageLoadingCenter(c, i18n.T("Loading cover…"))
			})
		}
	})
}

// detailTitle is the gallery's name and its Japanese name. Main steps the
// size with the window: 20, 24 from 640 wide, 30 from 1024.
func (a *App) detailTitle(c *ui.Context, g Gallery) {
	w, _ := c.Size()
	size := float32(20)
	switch {
	case w >= 1024:
		size = 30
	case w >= 640:
		size = 24
	}
	t := c.Theme()
	ui.Column(c).Gap(6).Children(func() {
		ui.Text(c, displayTitle(g)).FontSize(size).FontWeight(600).
			FixedLineHeight(size * 1.375).LetterSpacing(-size * 0.025)
		if g.TitleJPN != "" && g.TitleJPN != g.Title {
			ui.Text(c, g.TitleJPN).FontSize(14).FixedLineHeight(21).TextColor(t.TextMuted)
		}
	})
}

// detailSummary is the line under the title: the offline badge, the category,
// the score with its votes, and the page count.
func (a *App) detailSummary(c *ui.Context, s *detailState, g Gallery) {
	t := c.Theme()
	ui.Row(c).Gap(10).Wrap().AlignItems(ui.Center).Children(func() {
		if s.offline {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Padding(2, 8).Radius(999).
				Border(1, t.Border).Shrink(0).Children(func() {
				ui.Icon(c, detailIconSVG[iconCloudSlash]).Size(14, 14).TextColor(t.TextMuted)
				ui.Text(c, i18n.T("Offline data")).FontSize(12).FontWeight(500).
					FixedLineHeight(16).TextColor(t.TextMuted).SingleLine()
			})
		}
		categoryChip(c, g)
		if g.Rating > 0 {
			ui.Row(c).Gap(4).AlignItems(ui.Center).Shrink(0).Children(func() {
				ui.Icon(c, starSVG).Size(16, 16).TextColor(t.Warning)
				ui.Textf(c, "%.2f", g.Rating).FontSize(13).FontWeight(500).FixedLineHeight(16)
				if s.ratingCount > 0 {
					ui.Textf(c, "(%d)", s.ratingCount).FontSize(12).TextColor(t.TextMuted).FixedLineHeight(16)
				}
			})
		}
		ui.Textf(c, i18n.T("%d pages total"), g.Pages).FontSize(13).
			FixedLineHeight(16).TextColor(t.TextMuted).SingleLine()
	})
}

// detailActions is main's row of buttons: read under way, the shelf, the
// offline copy and the torrents.
func (a *App) detailActions(c *ui.Context, s *detailState, g Gallery, page int, finished bool) {
	t := c.Theme()

	readLabel := i18n.T("Start reading")
	switch {
	case finished || (g.Pages > 0 && page >= g.Pages-1):
		readLabel = i18n.T("Read again")
	case page > 0:
		readLabel = i18n.Tf("Continue (page %d)", page+1)
	}

	ui.Row(c).Gap(10).Wrap().AlignItems(ui.Center).Children(func() {
		read := ui.PrimaryButton(c, "").Height(36).Padding(0, 12).Radius(8).Gap(12).
			Background(ui.Hex(detailPrimaryColor))
		read.Children(func() {
			ui.Icon(c, detailIconSVG[iconBookOpen]).Size(16, 16)
			ui.Text(c, readLabel).FontSize(14).FontWeight(500).FixedLineHeight(21).SingleLine()
		})
		if read.Clicked() {
			a.readerStart = -1
			a.router.Push(fmt.Sprintf("/reader/%d/%s", g.ID, g.Token))
		}

		shelfLabel := i18n.T("Add to bookshelf")
		icon := iconBookmark
		if s.inBookshelf {
			shelfLabel = i18n.T("Remove from bookshelf")
			icon = iconBookmarkFilled
		}
		shelf := detailSecondaryButton(c, shelfLabel, detailIconSVG[icon], s.inBookshelf, s.busy)
		if shelf.Clicked() {
			a.toggleBookshelf(s)
		}

		if g.Pages > 0 {
			downloadLabel := i18n.T("Download")
			download := ui.Button(c, "").Height(36).Padding(0, 12).Radius(8).Gap(12).
				Background(ui.Transparent).Border(1, t.Border).Disabled(s.downloadBusy)
			if s.downloadBusy {
				download.Opacity(0.5)
			} else if download.Hovered() {
				download.Background(t.SurfaceHover)
			}
			download.Children(func() {
				if s.downloadBusy {
					ui.Spinner(c).Size(14, 14)
				} else {
					ui.Icon(c, detailIconSVG[iconDownload]).Size(16, 16)
				}
				ui.Text(c, downloadLabel).FontSize(14).FontWeight(500).FixedLineHeight(21).SingleLine()
			})
			if download.Clicked() {
				a.startDownload(c, s)
			}
		}

		torrentLabel := i18n.T("Torrent downloads")
		if s.torrentCountKnown {
			torrentLabel = i18n.Tf("Torrent downloads (%d)", s.torrentCount)
		}
		torrents := detailSecondaryButton(c, torrentLabel, detailIconSVG[iconMagnet], false, false)
		if torrents.Clicked() {
			a.openTorrents(s)
		}
	})
	if s.downloadErr != "" {
		ui.Text(c, s.downloadErr).FontSize(11).TextColor(t.Danger)
	}
}

// detailSecondaryButton is main's outline button: a transparent 36-point row
// with a hairline ring, the icon and the label 12 points apart.
func detailSecondaryButton(c *ui.Context, label string, mark *ui.SVG, filled, disabled bool) ui.Element {
	t := c.Theme()
	btn := ui.Button(c, "").Height(36).Padding(0, 12).Radius(8).Gap(12).
		Background(ui.Transparent).Border(1, t.Border).Disabled(disabled)
	if disabled {
		btn.Opacity(0.5)
	} else if filled {
		btn.Background(t.SurfaceHover)
	} else if btn.Hovered() {
		btn.Background(t.SurfaceHover)
	}
	btn.Children(func() {
		ui.Icon(c, mark).Size(16, 16)
		ui.Text(c, label).FontSize(14).FontWeight(500).FixedLineHeight(21).SingleLine()
	})
	return btn
}

// detailMetadata is main's two-column definition list: 13-point text, labels
// 64 wide and muted, values medium, under a hairline.
func (a *App) detailMetadata(c *ui.Context, s *detailState, stacked bool) {
	type entry struct {
		label string
		build func()
	}
	value := func(text string) func() {
		return func() {
			ui.Text(c, text).FontSize(13).FontWeight(500).FixedLineHeight(16)
		}
	}
	var entries []entry
	if s.uploader != "" {
		entries = append(entries, entry{i18n.T("Uploader"), value(s.uploader)})
	}
	if s.posted != "" {
		entries = append(entries, entry{i18n.T("Posted"), value(postedText(s.posted))})
	}
	if s.language != "" {
		entries = append(entries, entry{i18n.T("Language"), func() {
			t := c.Theme()
			ui.Row(c).Gap(4).AlignItems(ui.Center).Children(func() {
				ui.Text(c, s.language).FontSize(13).FontWeight(500).FixedLineHeight(16)
				if s.translated {
					ui.Text(c, i18n.T("(translated)")).FontSize(12).TextColor(t.Success)
				}
			})
		}})
	}
	if s.size != "" {
		entries = append(entries, entry{i18n.T("File size"), value(s.size)})
	}
	if s.favorited > 0 {
		entries = append(entries, entry{i18n.T("Favorites count"), value(fmt.Sprintf("%d", s.favorited))})
	}
	if len(entries) == 0 {
		return
	}

	t := c.Theme()
	ui.Column(c).Gap(16).Children(func() {
		ui.Divider(c)
		ui.Column(c).Gap(10).Children(func() {
			cols := 2
			if stacked {
				cols = 1
			}
			rows := (len(entries) + cols - 1) / cols
			for r := 0; r < rows; r++ {
				ui.Row(c).Gap(32).AlignItems(ui.Start).Children(func() {
					for k := 0; k < cols; k++ {
						i := r*cols + k
						if i >= len(entries) {
							ui.Box(c).Grow(1)
							continue
						}
						e := entries[i]
						ui.Row(c).Grow(1).AlignItems(ui.Start).Gap(12).Children(func() {
							ui.Text(c, e.label).Width(64).Shrink(0).FontSize(13).
								FixedLineHeight(20).TextColor(t.TextMuted)
							e.build()
						})
					}
				})
			}
		})
	})
}

// detailTags is main's TagGroups: one small heading per namespace, in the
// order the namespaces first appear, with the tags under it sorted by the
// user's rules.
func (a *App) detailTags(c *ui.Context, tags []apiTag) {
	t := c.Theme()
	var order []string
	groups := make(map[string][]apiTag)
	for _, tag := range tags {
		ns := tag.Namespace
		if ns == "" {
			ns = "other"
		}
		if _, ok := groups[ns]; !ok {
			order = append(order, ns)
		}
		groups[ns] = append(groups[ns], tag)
	}

	ui.Column(c).Gap(16).Children(func() {
		for _, ns := range order {
			ui.Column(c).Gap(6).Children(func() {
				name := a.translations.NamespaceName(ns)
				if name == "" {
					name = ns
				}
				ui.Text(c, name).FontSize(12).FontWeight(500).FixedLineHeight(16).
					TextColor(t.TextMuted).SingleLine()
				ui.Row(c).Gap(6).Wrap().Children(func() {
					for _, tag := range a.sortedTags(groups[ns]) {
						a.detailTagChip(c, tag)
					}
				})
			})
		}
	})
}

// detailTagChip is one tag as main's TagBadge shows it here: the translated
// name alone, since the namespace is the group's heading, in the neutral chip
// or the rule's solid blue or red.
func (a *App) detailTagChip(c *ui.Context, tag apiTag) {
	t := c.Theme()
	label := tag.Name
	if tr := a.translations.Lookup(tag.Namespace, tag.Name); tr != "" && tr != tag.Name {
		label = tr
	}
	bg, fg := t.SurfaceHover, t.TextMuted
	if rule := a.tagRule(tagChipText(tag.Namespace, tag.Name)); rule != nil {
		bg, fg = tagActionColor(rule.Action), ui.Hex("#f1f1f1")
	}
	ui.Box(c).Padding(2, 8).Radius(999).Background(bg).Shrink(0).Children(func() {
		ui.Text(c, label).FontSize(12).FontWeight(400).FixedLineHeight(16).TextColor(fg).SingleLine()
	})
}

// detailPagesSection is main's "页面" section: the count under the heading,
// then twenty thumbnails and the pager that moves between them.
func (a *App) detailPagesSection(c *ui.Context, s *detailState, g Gallery, content float32) {
	t := c.Theme()
	ui.Column(c).Gap(12).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, i18n.T("Page thumbnails")).FontSize(13).FontWeight(600).
				FixedLineHeight(16).SingleLine()
			if g.Pages > 0 {
				ui.Textf(c, i18n.T("%d pages"), g.Pages).FontSize(12).
					FixedLineHeight(16).TextColor(t.TextMuted).SingleLine()
			}
		})
		if g.Pages <= 0 {
			ui.Text(c, i18n.T("No page data yet")).FontSize(13).TextColor(t.TextMuted)
			return
		}
		a.detailPageGrid(c, s, g, content)
	})
}

// detailPageGrid lays out the current twenty pages in main's auto-fill grid:
// cells at least 140 wide with 12 between them on a desktop window, 96 and 8
// under 640.
func (a *App) detailPageGrid(c *ui.Context, s *detailState, g Gallery, content float32) {
	gap := float32(12)
	minWidth := float32(140)
	if content < 640 {
		gap = 8
		minWidth = 96
	}
	cols := int((content + gap) / (minWidth + gap))
	if cols < 1 {
		cols = 1
	}
	if cols > detailPagesPerGrid {
		cols = detailPagesPerGrid
	}
	cellW := (content - gap*float32(cols-1)) / float32(cols)

	totalPages := (g.Pages + detailPagesPerGrid - 1) / detailPagesPerGrid
	page := s.thumbPage
	if page >= totalPages {
		page = totalPages - 1
	}
	if page < 0 {
		page = 0
	}
	s.thumbPage = page

	start := page * detailPagesPerGrid
	end := min(start+detailPagesPerGrid, g.Pages)
	rows := (end - start + cols - 1) / cols

	ui.Column(c).Gap(gap).Children(func() {
		for r := 0; r < rows; r++ {
			ui.Row(c).Gap(gap).Children(func() {
				for k := 0; k < cols; k++ {
					i := start + r*cols + k
					if i >= end {
						ui.Box(c).Width(cellW)
						continue
					}
					a.detailPageCell(c, g, i, cellW)
				}
			})
		}

		if totalPages > 1 {
			ui.Row(c).FillWidth().Center().Gap(8).Padding(24, 0).Children(func() {
				prev := detailPagerButton(c, i18n.T("Previous page"), detailIconSVG[iconCaretLeft], page > 0)
				if prev.Clicked() {
					s.thumbPage--
				}
				ui.Textf(c, "%d / %d", page+1, totalPages).FontSize(13).
					Padding(0, 16).TextColor(c.Theme().TextMuted).SingleLine()
				next := detailPagerButton(c, i18n.T("Next page"), detailIconSVG[iconCaretRight], page < totalPages-1)
				if next.Clicked() {
					s.thumbPage++
				}
			})
		}
	})
}

// detailPageCell is one page's thumbnail: the picture with its number in a
// dark pill at the lower right, as main's .page-thumb-item.
func (a *App) detailPageCell(c *ui.Context, g Gallery, index int, width float32) {
	t := c.Theme()
	label := i18n.Tf("Page %d", index+1)
	url := a.client.PageThumbnailURL(g.ID, g.Token, index)
	thumb := a.pageThumbs.Get(url)
	errText := a.pageThumbs.Err(url)

	var frame ui.Element
	cell := ui.Column(c).Width(width).Focusable().Label(label).Cursor(ui.CursorPointer)
	cell.Children(func() {
		box := ui.Box(c).Size(width, width*141.0/100.0).Radius(8).Clip().
			Border(1, t.Border).Background(t.SurfaceHover)
		if cell.Hovered() {
			box.Shadow(0, 6, 16, 0, ui.RGBA(0, 0, 0, 0.18)).Translate(0, -2)
		}
		if errText != "" {
			box.Label(i18n.Tf("Thumbnail for page %d failed to load, click to retry", index+1))
		}
		frame = box
		box.Children(func() {
			switch {
			case thumb != nil:
				ui.Image(c, thumb).Fill().Fit(ui.Cover).Absolute()
			case errText != "":
				// A failed thumbnail offers its retry in place, as the page
				// did before main's look: the tile is still the page's.
				box.Center()
				ui.Text(c, i18n.T("Failed to load")).FontSize(11).TextColor(t.Danger)
			default:
				box.Center()
				imageLoadingCenter(c, i18n.Tf("Loading thumbnail for page %d…", index+1))
			}
			ui.Box(c).Absolute().Right(4).Bottom(4).Padding(1, 6).Radius(999).
				Background(ui.RGBA(0, 0, 0, 0.65)).Children(func() {
				ui.Textf(c, "%d", index+1).FontSize(11).FontWeight(600).
					FixedLineHeight(18).TextColor(ui.Hex("#ffffff")).SingleLine()
			})
		})
	})
	if errText != "" {
		if frame.Clicked() {
			a.pageThumbs.Retry(url)
		}
		return
	}
	if cell.Clicked() {
		a.openReaderAt(g.ID, g.Token, index)
	}
}

// detailPagerButton is main's small pagination button: 26 high, 12-point
// medium text, 8 between the arrow and the label.
func detailPagerButton(c *ui.Context, label string, mark *ui.SVG, enabled bool) ui.Element {
	t := c.Theme()
	btn := ui.Button(c, "").Height(26).Padding(0, 8).Radius(6).Gap(8).
		Background(t.Background).Border(1, t.Border).Disabled(!enabled)
	if !enabled {
		btn.Opacity(0.5)
	} else if btn.Hovered() {
		btn.Background(t.SurfaceHover)
	}
	btn.Children(func() {
		ui.Icon(c, mark).Size(16, 16)
		ui.Text(c, label).FontSize(12).FontWeight(500).FixedLineHeight(16).SingleLine()
	})
	return btn
}
