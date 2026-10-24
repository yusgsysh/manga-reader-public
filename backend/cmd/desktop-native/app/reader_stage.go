package app

import (
	"time"

	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// The stage draws the reader's pages, after comimi's PageStage: the shown
// group of pages fills the window, and the groups on either side of it sit one
// screen away, waiting for a swipe to bring them in. The zoom and pan apply to
// the shown group alone; the neighbours slide in at their own scale.
//
// Everything is laid out in DIPs over the window's size, which the reader's
// frame measures, so a group is a screen wide and its pages share it.

// readerStage is what one frame draws: the groups, where the drag has moved
// them, and the zoom of the shown one.
type readerStage struct {
	width, height float32
	groups        []pageGroupSpec
	// dragX is how far the groups have been dragged across, in DIPs.
	dragX float32
	// zoom and pan apply to the shown group.
	zoom float32
	panX float32
	panY float32
	// turn, when set, is the group sliding out and how far it has gone.
	turn     bool
	turnX    float32
	turnSide pageGroupSide
	// turnFrom is the group being turned away from, and turnIn the group
	// arriving behind it.
	turnFrom pageGroup
	turnIn   pageGroup
	// offsets overrides groupOffsetX while a turn runs, in DIPs.
	offsets map[pageGroupPlacement]float32
}

// pageGroupSide is the side a turn leaves toward.
type pageGroupSide int

const (
	sideLeft pageGroupSide = iota
	sideRight
)

// stageOf measures a frame into what the stage draws.
func (a *App) stageOf(c *ui.Context) readerStage {
	w, h := c.Size()
	r := &a.reader
	st := readerStage{
		width:  w,
		height: h,
		groups: a.pageGroupsToLayOut(),
		dragX:  r.drag,
		zoom:   r.zoom,
		panX:   r.panX,
		panY:   r.panY,
	}
	if r.turnDir != groupCurrent {
		// A turn in flight: the group the turn is leaving slides a whole
		// screen the way the gesture went, over turnAnimation, and the group
		// arriving follows it in from the other edge.
		progress := float32(time.Since(r.turnAt)) / float32(turnAnimation)
		if progress > 1 {
			progress = 1
		}
		st.turn = true
		st.turnFrom = r.turnFrom
		st.turnSide = a.turnSideOf(r.turnDir)
		st.turnIn = a.adjacentGroup(r.turnDir)
		to := w
		if st.turnSide == sideRight {
			to = -w
		}
		st.turnX = to * easeOutCubic(progress)
		// The arriving group starts a screen off in the direction the
		// leaving one is going and slides to rest with it.
		arrive := -to + st.turnX
		st.offsets = map[pageGroupPlacement]float32{
			r.turnDir:           st.turnX,
			opposite(r.turnDir): arrive,
		}
	}
	return st
}

// opposite is the placement a turn in direction comes from.
func opposite(direction pageGroupPlacement) pageGroupPlacement {
	if direction == groupNext {
		return groupPrevious
	}
	return groupNext
}

// turnSideOf is the side a turn in direction leaves toward: the next group is
// where the reading direction continues from, so the page is pushed the other
// way.
func (a *App) turnSideOf(direction pageGroupPlacement) pageGroupSide {
	nextRight := !a.reader.settings.rtl
	if direction == groupNext {
		if nextRight {
			return sideLeft
		}
		return sideRight
	}
	if nextRight {
		return sideRight
	}
	return sideLeft
}

// easeOutCubic decelerates a slide as it ends.
func easeOutCubic(t float32) float32 {
	u := 1 - t
	return 1 - u*u*u
}

// pageImage draws one page of a group filling its share of the group's width.
// A page without its URL yet says so; one whose picture is still arriving
// shows its stand-in with the loading mark over it.
func (a *App) pageImage(c *ui.Context, page int, grow int, zoom, panX, panY float32) {
	r := &a.reader
	if !r.hasPage(page) {
		a.readerPageMissing(c, page)
		return
	}
	src := r.pages[page]
	url := a.client.PageImageURL(src)
	bitmap := a.pageImageStore().Get(url)
	failed := a.pageImages.Err(url) != ""
	placeholder := false
	if bitmap == nil && !failed {
		bitmap = a.pageCache.Get(int(r.id), page)
		// The gradient stands in only until the page arrives; the indicator
		// over it says the real page is still downloading.
		placeholder = bitmap != nil
	}
	if bitmap == nil {
		a.readerPagePlaceholder(c, page, url)
		return
	}
	if failed {
		// The page is painted by the placeholder while its download runs,
		// which a failed download would leave on screen: a page that never
		// loads would stay a blank box with no way to retry it. Draw the
		// failure over the placeholder instead, and ask for the page once
		// per frame so that a failure the store dropped to make room starts
		// the page again.
		a.pageImageStore().Get(url)
		a.readerPageFailure(c, page, url, bitmap, grow > 0)
		return
	}
	draw := func() {
		img := ui.Image(c, bitmap).Fill().Fit(ui.Contain).
			TransformOrigin(0.5, 0.5).
			Translate(panX, panY).
			Scale(zoom, zoom)
		if grow > 0 {
			img.Grow(1)
		}
	}
	if placeholder {
		// Centre the indicator over the stand-in gradient. It is a sibling
		// above the image so neither the image's zoom nor pan moves it.
		box := ui.Box(c).Fill().Clip()
		if grow > 0 {
			box.Grow(1)
		}
		box.Children(func() {
			ui.Box(c).Fill().Absolute().Children(func() { draw() })
			ui.Column(c).Fill().Center().Absolute().Children(func() {
				imageLoadingCenter(c, i18n.Tf("Loading page %d…", page+1))
			})
		})
		return
	}
	draw()
}

// readerPagePlaceholder fills a page that is still loading, instead of leaving
// the area blank with no explanation. A failed page takes its retry buttons
// here too, when it has no placeholder to draw them over.
func (a *App) readerPagePlaceholder(c *ui.Context, page int, url string) {
	t := c.Theme()
	col := ui.Column(c).Fill().Center().Gap(8)
	col.Children(func() {
		if a.pageImages.Err(url) != "" {
			a.readerPageButtons(c, page, url)
			return
		}
		pageLoading(c, i18n.Tf("Loading page %d…", page+1))
		ui.Text(c, i18n.Tf("Loading page %d…", page+1)).
			FontSize(12).TextColor(t.TextMuted)
	})
}

// readerPageMissing draws a page slot that has no URL: the stream has not
// reached it yet, or it ended without it (upstream listed fewer pages than the
// gallery announced). A slot still coming says so and clears by itself; once
// the stream has ended it offers the retry the reader has for a page that
// failed, so a gallery short of its announced count is readable past the gap
// rather than stuck.
func (a *App) readerPageMissing(c *ui.Context, page int) {
	r := &a.reader
	t := c.Theme()
	col := ui.Column(c).Fill().Center().Gap(8)
	col.Children(func() {
		if r.settled {
			ui.Text(c, i18n.Tf("Page %d could not be loaded from the source", page+1)).
				FontSize(12).TextColor(t.Danger)
			if ui.Button(c, i18n.T("Reload pages")).Clicked() {
				a.retryReader(r)
			}
			return
		}
		pageLoading(c, i18n.Tf("Loading page %d…", page+1))
		ui.Text(c, i18n.Tf("Loading page %d…", page+1)).
			FontSize(12).TextColor(t.TextMuted)
	})
}

// readerPageFailure draws a failed page over the placeholder it is showing:
// the image is what the reader sees while the download runs, and the failure
// says why it never arrived, with its retries over the dimmed page.
func (a *App) readerPageFailure(c *ui.Context, page int, url string, bitmap *ui.Bitmap, grow bool) {
	img := ui.Image(c, bitmap).Fill().Fit(ui.Contain)
	if grow {
		img.Grow(1)
	}
	ui.Column(c).Fill().Center().Gap(8).Children(func() {
		a.readerPageButtons(c, page, url)
	})
}

// readerPageButtons offers the retries of a page that failed to load.
func (a *App) readerPageButtons(c *ui.Context, page int, url string) {
	r := &a.reader
	ui.Text(c, i18n.Tf("Page %d failed to load", page+1)).FontSize(12).TextColor(c.Theme().Danger)
	if ui.Button(c, i18n.T("Retry")).Clicked() {
		a.pageImages.Retry(url)
	}
	// A second failure means the cache path itself is the problem (a full
	// or unwritable cache): offer the live endpoint, which skips the local
	// cache entirely. Only shown when cache mode is on — with it off, url is
	// already the live endpoint.
	if a.client.UsingImageCache() {
		if ui.Button(c, i18n.T("Retry without cache")).Label(i18n.T("Retry this page without the cache")).Clicked() {
			a.pageImages.Retry(a.client.LivePageImageURL(r.pages[page]))
		}
	}
}

// preloadPages asks the page-image store for the pages around the shown group.
// Get is cheap once a page is queued or loaded; for a page that is not, it
// starts the download in the background. The shown pages are asked for first
// so they are not queued behind their neighbours.
//
// The window is readerPreloadImages pages either side of what the shown group
// covers, as comimi's page stage preloads around the displayed pages. Pages
// further out are released, which ends their downloads: a gallery turned
// through quickly would otherwise keep every page it passed downloading behind
// the page on screen. Release leaves loaded pages and failures alone, so
// turning back to a page finds it, or its retry.
func (a *App) preloadPages(r *Reader) {
	urls := make([]string, len(r.pages))
	for i, src := range r.pages {
		if src == "" {
			continue // a slot the stream has not filled yet
		}
		urls[i] = a.client.PageImageURL(src)
	}
	shown := a.shownGroup()
	keep := make(map[string]bool, 2*readerPreloadImages+len(shown.indexes))
	warm := func(i int) {
		if i < 0 || i >= len(urls) || urls[i] == "" || keep[urls[i]] {
			return
		}
		keep[urls[i]] = true
		a.pageImages.Get(urls[i])
	}
	// The shown pages first, then outwards from them.
	for _, i := range shown.indexes {
		warm(i)
	}
	if first, last := pageGroupFirst(shown), pageGroupLast(shown); first >= 0 {
		for i := 1; i <= readerPreloadImages; i++ {
			warm(last + i)
			warm(first - i)
		}
	}
	for url := range r.preloaded {
		if !keep[url] {
			a.pageImages.Release(url)
		}
	}
	r.preloaded = keep
}

// readerThumbURL is the page's thumbnail, through the cache proxy the app is
// set to use.
func (a *App) readerThumbURL(page int) string {
	r := &a.reader
	if !r.hasPage(page) {
		return ""
	}
	return a.client.PageThumbnailURL(r.id, r.token, page)
}
