package app

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// Reader is the state of the reader page: the page list, where it is in a
// gallery, and how the page is zoomed and panned. One reader is reused across
// galleries, so opening another gallery starts over.
type Reader struct {
	id    int64
	token string
	title string

	pages   []string
	total   int
	page    int
	loading bool
	loaded  bool
	err     string
	loadID  int
	// partial is how many pages have streamed in so far, and complete tells
	// that the backend finished the list. A large gallery streams for tens
	// of seconds: the reader shows the pages it has and says how many more
	// are expected.
	partial  int
	complete bool
	// started is set once a load was launched for id/token, so a frame does
	// not launch it again after a failure.
	started bool
	// offline reads the downloaded copy: the reader asks the cache first
	// instead of waiting on upstream to fail.
	offline bool
	// startPage is the page the reader was opened at (a thumbnail click or a
	// jump), -1 when the saved progress decides. It outlives the moment the
	// target is applied, so a late load result cannot override it with the
	// saved page.
	startPage int

	zoom  float32
	panX  float32
	panY  float32
	drag  bool
	lastX float32
	lastY float32
	// fit is how the page fills the area: ui.Contain shows the whole page,
	// ui.Cover fills the width (and crops the overflow, which pan shows).
	fit ui.Fit
	// paging holds the direction, layout and wheel behaviour, which outlive
	// one gallery (see readerPaging).
	paging readerPaging
	// fitHeight scales a page to the reader's height instead of using the
	// Image fit mode, which cannot express "as tall as the area".
	fitHeight bool
	// preloaded is the page URLs the last frame asked the image store for,
	// so the pages leaving that window can be released.
	preloaded map[string]bool
	// downX/downY and moved tell a click from a drag, so a tap can turn the
	// page while dragging still pans.
	downX, downY float32
	moved        bool

	lastSaved int
	// saveSeq invalidates a pending progress write when the page moves again.
	saveSeq int
}

// flushProgressNow writes the current page immediately, for leaving the reader
// where waiting for the debounce would lose the turn.
func (a *App) flushProgressNow(r *Reader) {
	if !r.loaded || r.page == r.lastSaved {
		return
	}
	r.saveSeq++ // a pending write is superseded
	a.flushProgress(r, r.page)
}

func (r *Reader) resetView() {
	r.zoom = 1
	r.panX, r.panY = 0, 0
	r.drag = false
}

func (r *Reader) zoomBy(d float32) {
	r.zoom = clampFloat(r.zoom+d, 0.5, 6)
}

// readerPaging is how a page turns and what a turn shows. It is a property of
// the reader's taste, not of one gallery, so opening the next gallery keeps
// it: turning 从右到左 on once should not have to be done again.
type readerPaging struct {
	rtl        bool
	double     bool
	wheelTurns bool
}

// ensureReader starts loading the reader's page list unless it is already
// loading or showing this gallery. offline selects the downloaded copy, so
// a change of mode starts the load again.
func (a *App) ensureReader(id int64, token string, offline bool) {
	r := &a.reader
	// started covers the failure case: after a failed load, loading, loaded
	// and the page list are all empty, so a frame that ran again would start
	// the load anew forever. The reader then shows the error until Retry.
	if r.id == id && r.token == token && r.offline == offline &&
		(r.loading || r.loaded || r.started) {
		return
	}
	r.id, r.token, r.offline = id, token, offline
	// The direction, layout and wheel mode are the reader's, not this
	// gallery's: the next gallery opens with the same ones.
	r.paging = a.paging
	r.started = false
	r.page = 0
	r.startPage = -1
	r.pages = nil
	r.total = 0
	r.loaded = false
	r.err = ""
	r.partial = 0
	r.complete = false
	r.lastSaved = -1
	// The previous gallery's pages keep whatever has loaded; only its
	// window bookkeeping is stale.
	r.preloaded = nil
	r.resetView()
	r.title = i18n.T("Reader")
	if d := a.details[id]; d != nil && d.loaded && d.g.Title != "" {
		r.title = d.g.Title
	}
	r.loadID++
	a.startReaderLoad(r)
}

func (a *App) startReaderLoad(r *Reader) {
	r.loading = true
	r.started = true
	r.err = ""
	// A target set before the load began decides the page, not the saved
	// progress that arrives with the list.
	if a.readerStart >= 0 {
		r.startPage = a.readerStart
	}
	// partial is how many pages the view already shows. Until the backend
	// sends the first one, the reader waits; after that it stays readable
	// while the rest arrives.
	r.partial = 0
	r.complete = false
	lid := r.loadID
	id, token := r.id, r.token

	// onPage hears each page as it is scraped, from the stream's goroutine:
	// it copies what arrived under update, without touching ui.
	onPage := func(index int, pageURL string, total int) {
		a.update(func() {
			if r.loadID != lid || index != len(r.pages) {
				return
			}
			r.pages = append(r.pages, pageURL)
			if total > 0 {
				r.total = total
			}
			r.partial = len(r.pages)
			r.loaded = true
			// The first page makes the reader usable: drop the spinner so
			// the toolbar and the page show while the rest streams in.
			r.loading = false
			// Apply a target as soon as its page arrives, rather than when
			// the whole list does: on a slow gallery that can be a minute.
			if r.startPage >= 0 && r.startPage < len(r.pages) {
				r.page = r.startPage
				r.resetView()
			}
		})
	}

	go func() {
		ctx := context.Background()
		var progress apiReading
		if p, err := a.client.Progress(ctx, id, token); err == nil {
			progress = p
		}
		var total int
		var urls []string
		var err error
		if r.offline {
			// Offline reading asks the cached copy first and never waits on
			// upstream, which would take a timeout to fail when disconnected.
			total, urls, err = a.client.CachedPagesStreaming(ctx, id, token, onPage)
		} else {
			total, urls, err = a.client.PagesStreaming(ctx, id, token, onPage)
			if err != nil {
				total, urls, err = a.client.CachedPagesStreaming(ctx, id, token, onPage)
			}
		}
		a.update(func() {
			if r.loadID != lid {
				return
			}
			r.loading = false
			if err != nil {
				// Pages already streamed stay readable; the error is only
				// fatal when nothing arrived.
				if len(r.pages) == 0 {
					r.err = err.Error()
					r.loaded = false
				}
				return
			}
			r.pages = urls
			r.total = total
			r.partial = len(urls)
			r.complete = true
			r.loaded = true
			// The saved progress resumes reading, unless the page list came
			// with a target: a thumbnail click or a jump must not be undone
			// here. The stream marks the reader loaded on its first page, so
			// this runs after readerPage may already have applied one.
			if progress.CurrentPage > 0 && progress.CurrentPage < len(urls) &&
				a.readerStart < 0 && r.startPage < 0 {
				r.page = progress.CurrentPage
			}
		})
	}()
}

func (a *App) retryReader(r *Reader) {
	r.loading = false
	r.loaded = false
	r.err = ""
	r.pages = nil
	r.total = 0
	r.partial = 0
	r.complete = false
	r.loadID++
	a.startReaderLoad(r)
}

// progressFlushDelay is the pause before a page turn is written, so a burst
// of turns becomes one request. It also bounds how long a page the reader
// left is unsaved.
const progressFlushDelay = 600 * time.Millisecond

// fitLabelFor names the current fit mode on the toolbar.
func fitLabelFor(r *Reader) string {
	switch {
	case r.fitHeight:
		return i18n.T("Fit height")
	case r.fit == ui.Cover:
		return i18n.T("Fit width")
	case r.fit == ui.NaturalSize:
		return i18n.T("Original size")
	default:
		return i18n.T("Fit page")
	}
}

// maxFloat is the larger of a and b.
func maxFloat(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
}

// nextFit cycles the fit modes in toolbar order: whole page, fill width,
// fill height (a flag, not a Fit), natural size, then whole page again.
func nextFit(r *Reader) {
	if r.fitHeight {
		// fit still names the last Image mode; the flag takes precedence.
		r.fit, r.fitHeight = ui.NaturalSize, false
		return
	}
	switch {
	case r.fit == ui.Contain:
		r.fit, r.fitHeight = ui.Cover, false
	case r.fit == ui.Cover:
		r.fitHeight = true
	default:
		r.fit, r.fitHeight = ui.Contain, false
	}
}

// readerStep is how far forward a turn moves: two pages in double mode.
func (r *Reader) readerStep() int {
	if r.paging.double {
		return 2
	}
	return 1
}

func (a *App) readerNext(r *Reader) {
	if !r.loaded || r.page+r.readerStep() >= len(r.pages) {
		return
	}
	r.page += r.readerStep()
	r.resetView()
	a.saveProgress(r)
}

func (a *App) readerPrev(r *Reader) {
	if !r.loaded || r.page == 0 {
		return
	}
	r.page -= r.readerStep()
	if r.page < 0 {
		r.page = 0
	}
	r.resetView()
	a.saveProgress(r)
}

// saveProgress records the page without blocking the frame. Page turns are
// batched: the write happens after a short pause, so flipping quickly sends
// one request for the page the reader stopped on.
func (a *App) saveProgress(r *Reader) {
	if !r.loaded || len(r.pages) == 0 || r.page == r.lastSaved {
		return
	}
	r.saveSeq++
	seq := r.saveSeq
	id, token, page := r.id, r.token, r.page
	time.AfterFunc(progressFlushDelay, func() {
		a.update(func() {
			if r.saveSeq != seq || r.id != id || r.token != token {
				return
			}
			a.flushProgress(r, page)
		})
	})
}

// flushProgress sends the page unless it was already saved.
func (a *App) flushProgress(r *Reader, page int) {
	if len(r.pages) == 0 || page == r.lastSaved {
		return
	}
	r.lastSaved = page
	id, token := r.id, r.token
	total := len(r.pages)
	progress := float64(page+1) / float64(total)
	completed := page == total-1
	go func() {
		_ = a.client.UpdateProgress(context.Background(), id, token, page, progress, completed)
	}()
}

func (a *App) readerPage(c *ui.Context, id int64, token string, offline bool) {
	a.ensureReader(id, token, offline)
	r := &a.reader
	t := c.Theme()

	// A thumbnail click on the detail page asks for a particular page; apply
	// it once the list is here, overriding the saved progress.
	if r.loaded && a.readerStart >= 0 {
		if a.readerStart < len(r.pages) {
			r.page = a.readerStart
			r.resetView()
			a.saveProgress(r)
		}
		// Remember it: the page list may still be streaming, and its last
		// result would otherwise restore the saved progress on top.
		r.startPage = a.readerStart
		a.readerStart = -1
	}

	if r.loaded {
		if c.Shortcut(0, ui.KeyDown) || c.Shortcut(0, ui.KeySpace) {
			a.readerNext(r)
		}
		if c.Shortcut(0, ui.KeyUp) {
			a.readerPrev(r)
		}
		if c.Shortcut(0, ui.KeyRight) {
			if r.paging.rtl {
				a.readerPrev(r)
			} else {
				a.readerNext(r)
			}
		}
		if c.Shortcut(0, ui.KeyLeft) {
			if r.paging.rtl {
				a.readerNext(r)
			} else {
				a.readerPrev(r)
			}
		}
		if c.Shortcut(0, ui.KeyEqual) || c.Shortcut(ui.Shift, ui.KeyEqual) {
			r.zoomBy(0.2)
		}
		if c.Shortcut(0, ui.KeyMinus) {
			r.zoomBy(-0.2)
		}
		if c.Shortcut(0, ui.Key0) {
			r.resetView()
		}
	}
	// Full screen works whether or not the pages have arrived yet.
	if c.Shortcut(0, ui.KeyF) {
		a.toggleFullscreen()
	}

	title := r.title
	if d := a.details[id]; d != nil && d.loaded && d.g.Title != "" {
		title = d.g.Title
	}

	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Padding(10, 16).Gap(10).AlignItems(ui.Center).Background(t.Surface).Children(func() {
			if ui.Button(c, i18n.T("← Back")).Clicked() {
				a.router.Push(fmt.Sprintf("/gallery/%d/%s", r.id, r.token))
			}
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, title).Bold().FontSize(14).SingleLine()
				if r.loaded {
					// While the backend still scrapes, say how much has
					// arrived so a big gallery does not look stuck.
					if !r.complete && r.total > 0 && len(r.pages) < r.total {
						ui.Textf(c, i18n.T("%d / %d pages"), len(r.pages), r.total).
							FontSize(11).TextColor(t.TextMuted)
						return
					}
					var line string
					if r.offline {
						line = i18n.Sprintf(i18n.T("%d / %d pages · %d%% · offline"), r.page+1, len(r.pages), int(r.zoom*100))
					} else {
						line = i18n.Sprintf(i18n.T("%d / %d pages · %d%%"), r.page+1, len(r.pages), int(r.zoom*100))
					}
					ui.Text(c, line).FontSize(11).TextColor(t.TextMuted)
				} else {
					ui.Text(c, i18n.T("Loading…")).FontSize(11).TextColor(t.TextMuted)
				}
			})
			if ui.Button(c, "−").Width(36).Label(i18n.T("Zoom out")).Disabled(!r.loaded).Clicked() {
				r.zoomBy(-0.2)
			}
			// Cycle: whole page → fill width → 100% pixels → whole page.
			if ui.Button(c, fitLabelFor(r)).Label(i18n.T("Switch fit mode")).Disabled(!r.loaded).Clicked() {
				nextFit(r)
				r.resetView()
			}
			if ui.Button(c, "+").Width(36).Label(i18n.T("Zoom in")).Disabled(!r.loaded).Clicked() {
				r.zoomBy(0.2)
			}
			if ui.Button(c, i18n.T("Previous page")).Disabled(!r.loaded || r.page == 0).Clicked() {
				a.readerPrev(r)
			}
			if ui.PrimaryButton(c, i18n.T("Next page")).Disabled(!r.loaded || r.page+r.readerStep() >= len(r.pages)).Clicked() {
				a.readerNext(r)
			}
			fullscreen := i18n.T("Full screen")
			if a.isFullscreen() {
				fullscreen = i18n.T("Exit full screen")
			}
			if ui.Button(c, fullscreen).Label(i18n.T("Toggle full screen")).Clicked() {
				a.toggleFullscreen()
			}
		})

		switch {
		case r.loading && !r.loaded:
			ui.Column(c).Fill().Center().Gap(10).Children(func() {
				pageLoading(c, i18n.T("Loading pages…"))
				ui.Text(c, i18n.T("Loading pages…")).FontSize(12).TextColor(t.TextMuted)
			})
		case r.err != "" && !r.loaded:
			a.errorBlock(c, r.err, func() { a.retryReader(r) })
		case !r.loaded || len(r.pages) == 0:
			a.emptyPage(c, i18n.T("No pages"), i18n.T("This gallery has no readable pages"), "")
		default:
			// Start the neighbouring pages downloading before the frame is
			// painted, so the visible page and its successors arrive together.
			a.preloadPages(r)

			// The area spans the content column, right of the 220px sidebar;
			// a tap in its outer thirds turns the page (panning stays).
			windowW, _ := c.Size()
			areaW := windowW - 220

			area := ui.Box(c).Fill().Focusable().Clip()
			area.HandleInput(func(ev ui.InputEvent) bool {
				switch ev.Kind {
				case ui.InputScroll:
					mods := ev.Mods&ui.Ctrl != 0 || ev.Mods&ui.Cmd != 0
					switch {
					case mods:
						if ev.DY > 0 {
							r.zoomBy(-0.1)
						} else {
							r.zoomBy(0.1)
						}
					case r.paging.wheelTurns:
						// Wheel mode: a notch turns a page, as viewers do.
						if ev.DY > 0 {
							a.readerNext(r)
						} else if ev.DY < 0 {
							a.readerPrev(r)
						}
					case ev.DY > 0:
						// Scrolled to the bottom of the zoomed-in page: turn.
						if r.zoom > 1 {
							r.panY -= ev.DY
							if r.panY > 0 {
								a.readerNext(r)
							}
						} else {
							a.readerNext(r)
						}
					case ev.DY < 0:
						if r.zoom > 1 {
							r.panY -= ev.DY
						} else {
							a.readerPrev(r)
						}
					default:
						r.panX -= ev.DX
					}
					return true
				case ui.InputPointerDown:
					r.drag = true
					r.lastX, r.lastY = ev.X, ev.Y
					r.downX, r.downY = ev.X, ev.Y
					r.moved = false
					return true
				case ui.InputPointerMove:
					if r.drag {
						r.panX += ev.X - r.lastX
						r.panY += ev.Y - r.lastY
						r.lastX, r.lastY = ev.X, ev.Y
						if absFloat(ev.X-r.downX)+absFloat(ev.Y-r.downY) > 6 {
							r.moved = true
						}
					}
					return true
				case ui.InputPointerUp:
					r.drag = false
					if !r.moved {
						a.readerTap(r, ev.X, areaW)
					}
					return true
				}
				return false
			})
			area.Children(func() {
				// Two pages side by side, left then right. In right-to-left
				// mode the spread swaps: the later page sits on the left,
				// as in Japanese manga.
				first, second := r.page, -1
				if r.paging.double && r.page+1 < len(r.pages) {
					second = r.page + 1
				}
				left, right := swapSpread(r.paging.rtl, first, second)
				ui.Row(c).Fill().Children(func() {
					a.readerPageImage(c, r, left)
					if right >= 0 {
						a.readerPageImage(c, r, right)
					}
				})
			})

			ui.Progress(c, float64(r.page+1)/float64(len(r.pages))).FillWidth().Height(3)
			ui.Row(c).Padding(6, 16).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Text(c, i18n.T("← → pages · Ctrl + wheel zoom · drag to pan · 0 reset · F fullscreen")).
					FontSize(11).TextColor(t.TextMuted).Grow(1)
			})
			ui.Row(c).Padding(2, 16, 6, 16).Gap(8).AlignItems(ui.Center).Children(func() {
				dir := i18n.T("Left to right")
				if r.paging.rtl {
					dir = i18n.T("Right to left")
				}
				if ui.Button(c, dir).Label(i18n.T("Switch reading direction")).Clicked() {
					r.paging.rtl = !r.paging.rtl
					a.paging = r.paging
				}
				layout := i18n.T("Single page")
				if r.paging.double {
					layout = i18n.T("Double page")
				}
				if ui.Button(c, layout).Label(i18n.T("Toggle single/double page")).Clicked() {
					r.paging.double = !r.paging.double
					// Keep the spread aligned to even page indices.
					if r.paging.double && r.page%2 == 1 {
						r.page--
					}
					a.paging = r.paging
				}
				wheel := i18n.T("Wheel pans")
				if r.paging.wheelTurns {
					wheel = i18n.T("Wheel turns")
				}
				if ui.Button(c, wheel).Label(i18n.T("Toggle wheel behaviour")).Clicked() {
					r.paging.wheelTurns = !r.paging.wheelTurns
					a.paging = r.paging
				}
				ui.TextInput(c, &a.readerJump).Width(64).Label(i18n.T("Jump to page")).Placeholder(i18n.T("Page number"))
				if ui.Button(c, i18n.T("Jump")).Clicked() {
					a.readerJumpTo(r)
				}
			})
		}
	})
}

// readerPreloadPages is how many pages past the visible one we warm up, so
// turning the page shows a decoded bitmap instead of a blank frame. One page
// behind is warmed too, making a step back instant.
const readerPreloadPages = 2

// readerKeepPages is how many pages past the window the reader keeps asking
// for, without warming them up first.
const readerKeepPages = 2

// preloadPages asks the page-image store for the pages around r.page. Get is
// cheap once a page is queued or loaded; for a page that is not, it starts the
// download in the background. The visible page is asked for first so it is not
// queued behind its neighbours.
//
// The warm window is one page behind and readerPreloadPages ahead. Pages
// further out are released, which ends their downloads: a gallery turned
// through quickly would otherwise keep every page it passed downloading
// behind the page on screen. Release leaves loaded pages and failures alone,
// so turning back to a page finds it, or its retry.
//
// readerKeepPages beyond the warm window the pages are still asked for, one
// per frame: a hit is cheap, and it keeps the store from evicting a page that
// failed. Once the store drops the failure to make room, the next hit starts
// the page again instead of leaving a blank page nothing asks for.
func (a *App) preloadPages(r *Reader) {
	urls := make([]string, len(r.pages))
	for i, src := range r.pages {
		urls[i] = a.client.PageImageURL(src)
	}
	step := r.readerStep()
	near := map[string]bool{urls[r.page]: true}
	warm := func(i int) {
		if i < 0 || i >= len(r.pages) || near[urls[i]] {
			return
		}
		near[urls[i]] = true
		a.pageImages.Get(urls[i])
	}
	warm(r.page)
	for i := 1; i <= readerPreloadPages; i++ {
		warm(r.page + step*i)
	}
	if r.paging.double {
		// The spread shows the page after the first.
		warm(r.page + 1)
	}
	warm(r.page - 1)
	// The keep window asks for these pages again every frame. A page that
	// failed is asked for once per frame too: that is what starts it again
	// once the store has dropped the failure, so a page outside the warm
	// window cannot stay a blank box with no retry.
	keep := make(map[string]bool, 2*readerKeepPages+len(near))
	for url := range near {
		keep[url] = true
	}
	for i := 1; i <= readerKeepPages; i++ {
		for _, j := range [2]int{r.page + readerPreloadPages + i, r.page - 1 - i} {
			if j < 0 || j >= len(r.pages) || keep[urls[j]] {
				continue
			}
			keep[urls[j]] = true
			a.pageImages.Get(urls[j])
		}
	}

	for url := range r.preloaded {
		if !keep[url] {
			a.pageImages.Release(url)
		}
	}
	r.preloaded = keep
}

// readerTap turns the page when the user taps the outer third of the reading
// area. Zoomed or panned views ignore taps, where a click is likely meant to
// position the page rather than turn it.
func (a *App) readerTap(r *Reader, x, width float32) {
	if width <= 0 || r.zoom > 1 {
		return
	}
	left, right := a.readerPrev, a.readerNext
	if r.paging.rtl {
		left, right = right, left
	}
	switch {
	case x < width/3:
		left(r)
	case x > width*2/3:
		right(r)
	}
}

// readerJumpTo goes to the page typed in the bottom bar, clamped to the list.
func (a *App) readerJumpTo(r *Reader) {
	if !r.loaded || len(r.pages) == 0 {
		return
	}
	n, err := strconv.Atoi(strings.TrimSpace(a.readerJump))
	if err != nil {
		return
	}
	page := n - 1
	if page < 0 {
		page = 0
	}
	if page >= len(r.pages) {
		page = len(r.pages) - 1
	}
	r.page = page
	r.resetView()
	a.saveProgress(r)
	a.readerJump = ""
}

// swapSpread orders a two-page spread: left-to-right shows the pages in
// order; right-to-left swaps them, so the later page sits on the left as
// Japanese manga do. A single page (second < 0) is unaffected.
func swapSpread(rtl bool, first, second int) (left, right int) {
	if rtl && second >= 0 {
		return second, first
	}
	return first, second
}

func absFloat(v float32) float32 {
	if v < 0 {
		return -v
	}
	return v
}

func clampFloat(v, lo, hi float32) float32 {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// readerPageImage draws one page of the reader, filling half the area in
// double mode. A missing bitmap asks for a frame once it arrives through the
// image store; until then the page is blank rather than a stretched error.
//
// This is the only place the reader asks the image store for a page: a hit
// moves the page to the end of the store's access list, so asking again for
// the same page in the same frame would evict what the frame just queued.
func (a *App) readerPageImage(c *ui.Context, r *Reader, page int) {
	src := r.pages[page]
	url := a.client.PageImageURL(src)
	bitmap := a.pageImages.Get(url)
	failed := a.pageImages.Err(url) != ""
	placeholder := false
	if bitmap == nil && !failed {
		bitmap = a.pageCache.Get(int(r.id), page)
		// The gradient stands in only until the page arrives; the indicator
		// over it says the real page is still downloading.
		placeholder = bitmap != nil
	}
	if bitmap == nil {
		a.readerPagePlaceholder(c, r, page, url)
		return
	}
	if failed {
		// The page is painted by the placeholder while its download runs,
		// which a failed download would leave on screen: a page that never
		// loads would stay a blank box with no way to retry it. Draw the
		// failure over the placeholder instead, and ask for the page once
		// per frame so that a failure the store dropped to make room starts
		// the page again.
		a.pageImages.Get(url)
		a.readerPageFailure(c, r, page, url, bitmap)
		return
	}
	draw := func() {
		img := ui.Image(c, bitmap).Fill().Fit(r.fit).
			TransformOrigin(0.5, 0.5).
			Translate(r.panX, r.panY).
			Scale(r.zoom, r.zoom)
		if r.fitHeight && bitmap != nil {
			// The area is the reader's content height; the toolbar and bars
			// take about 120 DIPs, and the image keeps its aspect ratio at the
			// scaled-up size, cropped horizontally when it overflows.
			w, h := bitmap.Size()
			_, winH := c.Size()
			scale := (winH - 120) / float32(h)
			if r.paging.double {
				// Two pages side by side share the width, so half the scale.
				scale /= 2
			}
			img.Size(float32(w)*maxFloat(scale, 0.1), float32(h)*maxFloat(scale, 0.1))
		}
		if r.paging.double {
			img.Grow(1)
		}
	}
	if placeholder {
		// Centre the indicator over the stand-in gradient. It is a sibling
		// above the image so neither the image's zoom nor pan moves it.
		box := ui.Box(c).Fill().Clip()
		if r.paging.double {
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
func (a *App) readerPagePlaceholder(c *ui.Context, r *Reader, page int, url string) {
	t := c.Theme()
	col := ui.Column(c).Fill().Center().Gap(8)
	if r.paging.double {
		col.Grow(1)
	}
	col.Children(func() {
		if a.pageImages.Err(url) != "" {
			a.readerPageButtons(c, r, page, url)
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
func (a *App) readerPageFailure(c *ui.Context, r *Reader, page int, url string, bitmap *ui.Bitmap) {
	if r.paging.double {
		ui.Image(c, bitmap).Fill().Fit(r.fit).Grow(1)
	} else {
		ui.Image(c, bitmap).Fill().Fit(r.fit)
	}
	ui.Column(c).Fill().Center().Gap(8).Children(func() {
		a.readerPageButtons(c, r, page, url)
	})
}

// readerPageButtons offers the retries of a page that failed to load.
func (a *App) readerPageButtons(c *ui.Context, r *Reader, page int, url string) {
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
