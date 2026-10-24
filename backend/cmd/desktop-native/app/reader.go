package app

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// The reader follows comimi, yui540's manga viewer: the page fills the
// window, and the interface over it is an overlay that appears when asked for
// and goes away by itself. A tap in the outer thirds turns the page, a tap in
// the middle shows the overlay, a drag is a swipe that turns the page with it,
// and the overlay holds the seek bar, the page mode and the settings.
//
// The reader is the state of that: the page list, where it is in a gallery,
// the overlay and the panels, and the zoom and pan of the shown pages. One
// reader is reused across galleries, so opening another gallery starts over,
// with the reader's own settings (see readerSettings) kept.

// readerPanel is which overlay panel is open, as comimi's ViewerPanel: the
// menu and its sub-views, the settings sheet, or none.
type readerPanel int

const (
	panelNone readerPanel = iota
	panelMenu
	panelSettings
	panelPages
	panelFavorites
	panelShortcuts
	panelShare
	panelAbout
)

// isMenu reports whether the panel is the menu or one of its views, which all
// share the menu's frame.
func (p readerPanel) isMenu() bool {
	switch p {
	case panelMenu, panelPages, panelFavorites, panelShortcuts, panelShare, panelAbout:
		return true
	}
	return false
}

// Reader is the state of the reader page.
type Reader struct {
	id    int64
	token string
	title string
	// total is the gallery's page count as its detail said, which is what the
	// reader lays its slots out from (see ensureSlots): it does not move as the
	// stream arrives, so the page list and the thumbnails keep their shape.
	total int

	pages []string
	page  int
	// saved is the page the backend reported when the list loaded; it decides
	// where the reader opens, and is applied once the page exists (the
	// gallery's count can far outrun a stream that is still arriving).
	saved    int
	savedSet bool
	// settled is set once the stream has ended, and slots are only cut down to
	// the pages that arrived after that, never while it is still running.
	settled bool

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

	// The read, as comimi keeps it: an overlay over the page, a panel inside
	// it, and the zoom and pan of the shown pages.
	settings  readerSettings
	overlay   bool
	panel     readerPanel
	guide     bool
	auto      bool
	zoom      float32
	panX      float32
	panY      float32
	drag      float32
	fav       map[int]bool
	toast     string
	toastKind toastTone
	toastAt   time.Time
	// narrow is set when the window is too narrow for a spread, which shows
	// single pages whatever the setting says.
	narrow bool
	// jump keeps the page-number field's text, in the pages view.
	jump string
	// seekPreview is the page the seek bar hovers over, -1 for none, and
	// pagesScroll keeps the page list where it was scrolled to.
	seekPreview int
	pagesScroll ui.ScrollState

	// The gesture in progress: where the pointer went down, what pan it
	// started from, and whether it moved far enough to be a swipe rather
	// than a tap.
	downX, downY float32
	startPanX    float32
	startPanY    float32
	pressed      bool
	moved        bool
	// longPress is when the press on a page began, for the long-press
	// favorite; zero when no press is pending.
	longPress              time.Time
	longPressX, longPressY float32
	longPressPage          int

	// The turn animation: the group drawn sliding out, the time it started,
	// and the page it lands on when it finishes.
	turnDir  pageGroupPlacement
	turnFrom pageGroup
	turnAt   time.Time

	// autoSince is when the page up under auto page turn was shown.
	autoSince time.Time

	// preloaded is the page URLs the last frame asked the image store for,
	// so the pages leaving that window can be released.
	preloaded map[string]bool

	// lastSent is the page of the last write this reader sent. A turn only
	// writes when it differs, so turning back to the page already saved does
	// not write it again.
	lastSent int
	// lastSaved is the page the backend accepted last, which the debounced
	// write checks itself against.
	lastSaved int
	// saveSeq invalidates a pending progress write when the page moves again.
	saveSeq int
}

// resetView puts the zoom and pan back to the whole page.
func (r *Reader) resetView() {
	r.zoom = 1
	r.panX, r.panY = 0, 0
}

// shown reports the page count the reader can read now.
func (r *Reader) shown() int { return len(r.pages) }

// maxZoom is the largest zoom the reader allows, as comimi's default of 4.
const maxZoom float32 = 4

// minZoom is the smallest, the whole page.
const minZoom float32 = 1

// zoomStep is one notch of the zoom: a wheel notch multiplies by at most
// 1+zoomStep, as comimi's zoom.step of .25 does.
const zoomStep float32 = 0.25

// gesture thresholds, after comimi's renderer.
const (
	// swipeThreshold is how far a drag travels, across, before it is a turn.
	swipeThreshold float32 = 40
	// dragSlop is how far a press travels before it is a drag rather than a
	// click.
	dragSlop float32 = 6
	// tapZoneEdge is where the turn zones end and the overlay zone begins:
	// the outer 30% either side turns the page.
	tapZoneEdge float32 = 0.3
	// turnAnimation is how long a swipe's turn slides a group out.
	turnAnimation = 180 * time.Millisecond
	// overlayAutoHide is how long the overlay stays with no activity.
	overlayAutoHide = 3 * time.Second
	// longPressDelay is how long a press on a page takes to favorite it.
	longPressDelay = 500 * time.Millisecond
	// longPressSlop is how far a press may wander and still count.
	longPressSlop float32 = 10
	// toastDuration is how long a notification stays.
	toastDuration = 1500 * time.Millisecond
)

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
	// The settings are the reader's, not this gallery's: the next gallery
	// opens with the same ones.
	r.settings = a.readerSettings
	r.started = false
	r.page = 0
	r.startPage = -1
	r.pages = nil
	// The gallery's own count is what the reader plans with, as main's
	// authoritative total is: the detail page answers from cache first, and
	// the count says how many slots to expect before any page arrives.
	r.total = 0
	if d := a.details[id]; d != nil && d.loaded {
		r.total = d.g.Pages
	}
	r.loaded = false
	r.err = ""
	r.partial = 0
	r.complete = false
	r.settled = false
	// This app's own last save opens the reader ahead of the backend's row,
	// which can lag behind a visit that just ended (see applyStart).
	r.saved, r.savedSet = 0, false
	if known, ok := a.knownReading(id); ok && known.CurrentPage > 0 {
		r.saved, r.savedSet = known.CurrentPage, true
	}
	r.lastSaved = -1
	r.lastSent = -1
	// The previous gallery's pages keep whatever has loaded; only its
	// window bookkeeping is stale.
	r.preloaded = nil
	r.resetView()
	r.drag = 0
	// The overlay opens by itself on a new gallery, with the reading
	// direction guide, as comimi's splash gives way to it (see startGuide).
	// Its auto-hide clock starts now: an overlay that opened without activity
	// would be counted as idle since the clock's zero and hide at once.
	r.overlay = true
	a.markActivity()
	r.panel = panelNone
	r.auto = false
	r.fav = make(map[int]bool)
	r.toast = ""
	r.turnDir = groupCurrent
	r.jump = ""
	r.seekPreview = -1
	r.title = i18n.T("Reader")
	if d := a.details[id]; d != nil && d.loaded && d.g.Title != "" {
		r.title = d.g.Title
	}
	r.loadID++
	// The sidebar gives way to the page, and the window follows the reader
	// into full screen as it does when a manga viewer opens.
	a.startGuide(r)
	a.startReaderLoad(r)
}

// startGuide arms the reading direction guide the overlay opens with: it is
// shown first, then gives way to the message that tells what a tap does, so a
// first-time reader learns the one gesture the reader lives on.
func (a *App) startGuide(r *Reader) {
	// No gallery to read yet: the guide waits for the pages.
	r.guide = true
	time.AfterFunc(2*time.Second, func() {
		a.update(func() {
			if &a.reader != r {
				return
			}
			r.guide = false
		})
	})
}

// startReaderLoad streams the reader's page list in. The pages are laid out as
// slots for the gallery's whole count up front (ensureSlots), so the list does
// not rebuild as pages arrive: main's reader pre-allocates page slots from the
// gallery total for the same reason, and keeps its page-list key constant.
func (a *App) startReaderLoad(r *Reader) {
	r.loading = true
	r.started = true
	r.err = ""
	// The page this app last saved can open the reader, ahead of the backend's
	// row, which can lag behind a visit that just ended.
	r.saved, r.savedSet = 0, false
	if known, ok := a.knownReading(r.id); ok && known.CurrentPage > 0 {
		r.saved, r.savedSet = known.CurrentPage, true
	}
	// A target set before the load began decides the page, not the saved
	// progress that arrives with the list.
	if a.readerStart >= 0 {
		r.startPage = a.readerStart
		r.saved, r.savedSet = 0, false
	}
	// partial is how many pages the view already shows. Until the backend
	// sends the first one, the reader waits; after that it stays readable
	// while the rest arrives.
	r.partial = 0
	r.complete = false
	r.settled = false
	r.ensureSlots()
	lid := r.loadID
	id, token := r.id, r.token

	// onPage hears each page as it is scraped, from the stream's goroutine:
	// it copies what arrived under update, without touching ui.
	onPage := func(index int, pageURL string, total int) {
		a.update(func() {
			// The list was padded with slots for the whole gallery
			// (ensureSlots), so a page fills its slot rather than being
			// appended: the indices must still line up.
			if r.loadID != lid || index < 0 || index >= len(r.pages) {
				return
			}
			r.pages[index] = pageURL
			if total > r.total {
				r.total = total
			}
			r.partial = index + 1
			r.ensureSlots()
			r.loaded = true
			// The first page makes the reader usable: drop the loader so
			// the page and the overlay show while the rest streams in.
			r.loading = false
			r.applyStart()
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
				if len(urls) == 0 && !r.loaded {
					r.err = err.Error()
					r.loaded = false
				}
				return
			}
			r.pages = urls
			if total < len(urls) {
				total = len(urls)
			}
			r.total = total
			r.partial = len(urls)
			r.complete = true
			r.settled = true
			r.ensureSlots()
			r.loaded = true
			// The saved page resumes reading unless a target was asked
			// for: a thumbnail click or a jump must not be undone here.
			// The stream marks the reader loaded on its first page, so
			// this runs after readerPage may already have applied one.
			if progress.CurrentPage > 0 && r.startPage < 0 {
				r.saved, r.savedSet = progress.CurrentPage, true
			}
			r.applyStart()
		})
	}()
}

// applyStart opens the reader at the page it was asked to: the target of a
// thumbnail click or a jump first, then the saved page (what this app last
// wrote, else the backend's row), as soon as that page has arrived. A saved
// page past what the gallery turned out to have opens the last one.
func (r *Reader) applyStart() {
	n := len(r.pages)
	if n == 0 {
		return
	}
	want := -1
	switch {
	case r.startPage >= 0:
		want = r.startPage
	case r.savedSet && r.saved > 0:
		want = r.saved
	case r.savedSet && r.settled:
		// Saved on page 0 (or finished): open at the start.
		want = 0
	}
	if want < 0 {
		return
	}
	if !r.hasPage(want) {
		if !r.settled {
			return // the page has not streamed in yet
		}
		// Ended short of it: open the last page that is here.
		if want > n {
			want = n
		}
		for want > 0 && !r.hasPage(want-1) {
			want--
		}
		want--
	}
	if want != r.page {
		r.page = want
		r.resetView()
	}
}

// ensureSlots pads the page list with empty slots up to the gallery's count,
// so the reader's list is as long as the gallery from the first frame instead
// of growing with every page that streams in. A slot that never fills is
// drawn as a page the source did not list (readerPageMissing).
//
// Slots are only cut back once the stream has ended, to the pages that
// arrived: a stream that ends short is reported by its slots.
func (r *Reader) ensureSlots() {
	if !r.complete && len(r.pages) < r.total {
		r.pages = append(r.pages, make([]string, r.total-len(r.pages))...)
	}
}

// trimSlots drops the empty slots past the last page that arrived, once the
// stream has ended: a gallery whose upstream list is shorter than its count is
// read, and counted by the toolbar and the bar, as the pages it has.
func (r *Reader) trimSlots() {
	if !r.settled {
		return
	}
	last := -1
	for i, src := range r.pages {
		if src != "" {
			last = i
		}
	}
	if last+1 < len(r.pages) {
		r.pages = r.pages[:last+1]
	}
	if r.total > len(r.pages) {
		r.total = len(r.pages)
	}
	if r.page >= len(r.pages) && len(r.pages) > 0 {
		r.page = len(r.pages) - 1
	}
}

// hasPage reports whether page i has its real URL yet.
func (r *Reader) hasPage(i int) bool {
	return i >= 0 && i < len(r.pages) && r.pages[i] != ""
}

func (a *App) retryReader(r *Reader) {
	r.loading = false
	r.loaded = false
	r.err = ""
	r.pages = nil
	r.total = 0
	r.partial = 0
	r.complete = false
	r.settled = false
	r.loadID++
	a.startReaderLoad(r)
}

// progressFlushDelay is the pause before a page turn is written, so a burst
// of turns becomes one request. It also bounds how long a page the reader
// left is unsaved.
const progressFlushDelay = 600 * time.Millisecond

// readerCanTurn reports whether a turn in direction has somewhere to go.
func (a *App) readerCanTurn(direction pageGroupPlacement) bool {
	return len(a.adjacentGroup(direction).indexes) > 0
}

// readerNext turns to the next group, if there is one.
func (a *App) readerNext(r *Reader) {
	a.turnPage(r, groupNext, false)
}

// readerPrev turns to the previous group, if there is one.
func (a *App) readerPrev(r *Reader) {
	a.turnPage(r, groupPrevious, false)
}

// turnPage moves the reader one group in direction. animated slides the
// group out over turnAnimation first, which is what a swipe does; a click or a
// key turns at once, as comimi's buttons do. It reports whether it turned.
func (a *App) turnPage(r *Reader, direction pageGroupPlacement, animated bool) bool {
	if !r.loaded || r.turnDir != groupCurrent {
		return false
	}
	next := a.adjacentGroup(direction)
	if len(next.indexes) == 0 {
		return false
	}
	if animated {
		r.turnDir = direction
		r.turnFrom = a.shownGroup()
		r.turnAt = time.Now()
		return true
	}
	a.commitTurn(r, direction, next)
	return true
}

// commitTurn lands a turn on group next.
func (a *App) commitTurn(r *Reader, direction pageGroupPlacement, next pageGroup) {
	r.turnDir = groupCurrent
	r.drag = 0
	// Land on the first page of the new group, so a spread opened from its
	// second page shows the same pair.
	r.page = pageGroupFirst(next)
	r.resetView()
	a.saveProgress(r)
}

// finishTurn completes an animated turn once its slide has run.
// finishTurn completes a turn once its slide has run. A turn is a slide of
// turnAnimation; the reader asks for the frames that carry it, and it lands
// when its time is up.
func (a *App) finishTurn(r *Reader) {
	if r.turnDir == groupCurrent {
		return
	}
	if time.Since(r.turnAt) < turnAnimation {
		return
	}
	direction := r.turnDir
	next := a.adjacentGroup(direction)
	if len(next.indexes) == 0 {
		r.turnDir = groupCurrent
		r.drag = 0
		return
	}
	a.commitTurn(r, direction, next)
}

// goToPage moves the reader to a page (0-based), clamped, as the seek bar and
// the page list do.
func (a *App) goToPage(r *Reader, page int) {
	if !r.loaded || r.turnDir != groupCurrent {
		return
	}
	last := len(r.pages) - 1
	if last < 0 {
		return
	}
	if page < 0 {
		page = 0
	}
	if page > last {
		page = last
	}
	if page == r.page {
		return
	}
	r.page = page
	r.resetView()
	r.drag = 0
	a.saveProgress(r)
}

// saveProgress records the page without blocking the frame. Page turns are
// batched: the write happens after a short pause, so flipping quickly sends
// one request for the page the reader stopped on.
//
// The page is remembered as soon as it is chosen, before the write: the lists
// behind the reader show where reading got to without waiting for a round
// trip (see mergeListReading), as main's applyReadingProgressToCaches writes
// the progress into its list caches first and lets the request confirm it.
func (a *App) saveProgress(r *Reader) {
	if r.turnDir != groupCurrent {
		// A turn in flight lands on the page the reader will end on; the
		// commit saves that one.
		return
	}
	if !r.loaded || r.total == 0 || r.page == r.lastSent {
		return
	}
	progress, completed := readingProgressAt(r.page, r.total)
	a.rememberReading(r.id, ReadingProgress{
		CurrentPage: r.page,
		Progress:    progress,
		Completed:   completed,
	})
	r.lastSent = r.page
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

// readingProgressAt is the fraction read and whether it is the end of the
// gallery, as main's calculateProgress: the last page is the whole gallery.
func readingProgressAt(page, total int) (float64, bool) {
	if total <= 0 {
		return 0, false
	}
	if page >= total-1 {
		return 1, true
	}
	return float64(page+1) / float64(total), false
}

// flushProgress sends the page unless it was already saved.
func (a *App) flushProgress(r *Reader, page int) {
	if r.total == 0 || page == r.lastSaved {
		return
	}
	if page >= r.total {
		page = r.total - 1
	}
	r.lastSaved = page
	id, token := r.id, r.token
	progress, completed := readingProgressAt(page, r.total)
	go func() {
		p, c := progress, completed
		_ = a.client.UpdateProgress(context.Background(), id, token, page, p, c)
	}()
}

// flushProgressNow writes the current page immediately, for leaving the reader
// where waiting for the debounce would lose the turn.
func (a *App) flushProgressNow(r *Reader) {
	if !r.loaded || len(r.pages) == 0 || r.page == r.lastSaved {
		return
	}
	r.saveSeq++ // a pending write is superseded
	a.flushProgress(r, r.page)
}

// readerTick advances everything that moves on its own: the turn animation,
// auto page turn, the overlay's auto-hide and the toast. It reports whether
// another frame is needed, so the reader can keep its clock going only while
// something is pending.
func (a *App) readerTick(r *Reader) bool {
	// The overlay's clock is read before anything below can restart it: the
	// auto-hide counts what came before this frame.
	autoHide := time.Since(a.overlayActivity) >= overlayAutoHide
	pending := false
	if r.turnDir != groupCurrent {
		a.finishTurn(r)
		pending = r.turnDir != groupCurrent
	}
	if r.auto && !r.overlay && r.panel == panelNone && r.loaded && r.turnDir == groupCurrent {
		interval := time.Duration(r.settings.autoInterval()) * time.Millisecond
		if !r.autoSince.IsZero() && time.Since(r.autoSince) >= interval {
			if a.turnPage(r, groupNext, false) {
				r.autoSince = time.Now()
			} else {
				// The last page: auto page turn stops by itself, as comimi's
				// timer does. setAuto already starts the clock again.
				a.setAuto(r, false)
			}
		}
		if r.auto {
			pending = true
		}
	}
	if r.overlay && r.panel == panelNone {
		if autoHide {
			a.closeOverlay(r)
		} else {
			pending = true
		}
	}
	// A toast that appeared this frame is shown at least once, then timed.
	if r.toast != "" && time.Since(r.toastAt) >= toastDuration {
		r.toast = ""
	}
	if r.toast != "" {
		pending = true
	}
	if r.guide {
		pending = true
	}
	// A press held on a page becomes a favorite when its time is up.
	if !r.longPress.IsZero() && r.pressed {
		a.longPressDue(r)
		if !r.longPress.IsZero() {
			pending = true
		}
	}
	return pending
}

// setAuto starts or stops auto page turn.
func (a *App) setAuto(r *Reader, on bool) {
	if r.auto == on {
		return
	}
	r.auto = on
	r.autoSince = time.Now()
	if on {
		a.notify(r, i18n.T("Auto page turn on"), toneInfo)
	} else {
		a.notify(r, i18n.T("Auto page turn off"), toneInfo)
	}
}

// closeOverlay hides the overlay and the panel in it, as tapping the middle
// of the page does.
func (a *App) closeOverlay(r *Reader) {
	r.overlay = false
	r.panel = panelNone
}

// openPanel shows the overlay with a panel in it.
func (a *App) openPanel(r *Reader, p readerPanel) {
	r.overlay = true
	r.panel = p
	a.overlayActivity = time.Now()
}

// togglePanel opens panel unless it is the one already open.
func (a *App) togglePanel(r *Reader, p readerPanel) {
	if r.panel == p {
		a.closeOverlay(r)
		return
	}
	a.openPanel(r, p)
}

// toastKind is how a toast reads: neutral, good, or bad.
type toastTone int

const (
	toneInfo toastTone = iota
	toneSuccess
	toneError
)

// notify shows a toast over the reader for a moment.
func (a *App) notify(r *Reader, message string, tone toastTone) {
	r.toast = message
	r.toastKind = tone
	r.toastAt = time.Now()
}

// leaveReader leaves the reader for the page before it. Leaving saves the page
// first, as waiting for the debounced write would lose the turn, and leaves
// full screen: the screen belongs to the read, and the gallery or list behind
// the reader is browsed windowed, with nothing left showing how to get out.
func (a *App) leaveReader(r *Reader) {
	a.flushProgressNow(r)
	a.exitFullscreen()
	a.router.Back()
}

// jumpTo goes to the page typed in the pages view, clamped to the list.
func (a *App) jumpTo(r *Reader) {
	n, err := strconv.Atoi(strings.TrimSpace(r.jump))
	if err != nil {
		return
	}
	a.goToPage(r, n-1)
	r.jump = ""
}

// galleryURL is the gallery's page on the site, which the share view copies.
func galleryURL(id int64, token string) string {
	if token == "" {
		return ""
	}
	return "https://e-hentai.org/g/" + strconv.FormatInt(id, 10) + "/" + token + "/"
}

// maxFloat is the larger of a and b.
func maxFloat(a, b float32) float32 {
	if a > b {
		return a
	}
	return b
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
