package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"golang.org/x/text/language"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// App is the window's state. The view is a function of it, built again for
// every frame. Loads run in the background and update the state under mu, so
// the view never waits on the network.
type App struct {
	mu sync.Mutex

	router   *ui.Router
	selected string // the sidebar item shown
	lastList string // the list a gallery or the reader came from

	win        *mygo.Window
	client     *Client
	covers     *CoverCache
	images     *ImageStore
	pageImages *ImageStore
	pageThumbs *ImageStore
	pageCache  *PageCache
	// inView is set while View builds a frame (see pageImageStore), so the
	// pages a frame asks for are the ones it is about to draw.
	inView bool

	// backend is the in-process API lifecycle; nil in tests and when the app
	// talks to an external server.
	backend Backend
	storage BackendStorage
	// build is the version and platform line shown under 关于.
	build string
	// diagBusy and diagMsg track exporting the diagnostics file.
	diagBusy bool
	diagMsg  string
	diagSeq  int

	// setup state: the first-run cookie screen.
	showSetup   bool
	setupCookie string
	setupErr    string
	setupBusy   bool

	// toasts are the ones asked for off the main thread, which View adds
	// while it builds the next frame.
	toasts []ui.Toast

	// appearance is the theme choice shown on the settings page; main applies
	// it. appearanceBusy while saving, appearanceMsg the last result.
	appearance     string
	appearanceBusy bool
	appearanceMsg  string

	// language is the interface language tag saved in config.json, and
	// languageMsg the last result of changing it.
	language    string
	languageMsg string

	// sync is the settings page's data-sync section: the form fields, the
	// loaded config and the engine's live state.
	sync syncSection

	// cache stats shown on the settings page.
	cacheBytes   int64
	cacheMax     int64 // 0 means unlimited
	cacheLoading bool
	cacheLoaded  bool
	cacheBusy    bool
	cacheErr     string

	// cache limit editing.
	cacheMaxInput string
	cacheMaxInit  bool
	cacheMaxBusy  bool
	cacheMaxErr   string

	lists     map[string]*listState
	details   map[int64]*detailState
	downloads Downloads
	// listForCard is the list whose grid is being built, with the window
	// height, so a card can tell whether it is near the visible end.
	listForCard *listState
	cardWindowH float32
	// tags is the tag-rule page state; rules colour and order tags anywhere.
	tags TagSettings
	// translations is the EhTagTranslation database, shared by every page.
	translations TagTranslationDB
	// announcedRules is the rule list last copied into lists and details
	// that show tag chips, so a rule change recolours them.
	announcedRules []TagRule
	// transBusy/transMsg report the tag translation import/export the tag
	// page started; transExportInFlight is set while the dialog is up.
	transBusy bool
	transMsg  string

	// zipBusy is the jobs whose ZIP is being saved; zipMsg reports the last
	// save's result on the downloads page.
	zipBusy map[string]bool
	zipMsg  string

	searchQuery string
	// searchApplied is the text the debounce timer has already been armed
	// for, and searchSeq invalidates timers superseded by newer keystrokes.
	searchApplied string
	searchSeq     int
	reader        Reader
	// readerSettings is the reader's last chosen direction, page mode,
	// cover, auto page turn interval and background, carried into the next
	// gallery opened (see readerSettings).
	readerSettings readerSettings
	// readerStart is the page the next reader open should show, set when a
	// detail-page thumbnail is clicked; -1 means "resume saved progress".
	readerStart int
	// overlayActivity is when the reader's overlay last saw activity, which
	// its auto-hide counts from.
	overlayActivity time.Time
	// readings is the reading progress this app itself saved, per gallery, so
	// lists and detail pages show the page just read instead of the one the
	// backend's list rows still carry (see mergeListReading).
	readings map[int64]readingAt
}

// syncSection is the data-sync part of the settings page. The form holds
// what the user edits; the rest mirrors the backend's config and engine
// state.
type syncSection struct {
	form         syncForm
	config       SyncConfig
	loaded       bool
	loading      bool
	saveBusy     bool
	msg          string
	err          string
	status       SyncStatus
	statusLoaded bool
	statusBusy   bool
	// accepting is this device's own peer switch, applied through the
	// backend rather than the engine's config.
	accepting     bool
	acceptingInit bool
}

// syncForm is the editable part of the sync section. tokenInit stops the
// masked stored token from being written over the user's typing on the
// first load.
type syncForm struct {
	enabled   bool
	serverURL string
	token     string
	tokenInit bool
}

type listKind int

const (
	kindHome listKind = iota
	kindWatched
	kindPopular
	kindSearch
	kindBookshelf
	kindRecent
)

// browsable reports whether the list can be refreshed on a timer without a
// user's query: the browse and shelf lists can, while search waits for what is
// typed and must not reload on its own.
func (k listKind) browsable() bool {
	return k != kindSearch
}

// listState is the scroll, selection and page data of one gallery list.
type listState struct {
	kind     listKind
	items    []Gallery
	grid     *ui.GridState
	selected int

	page     int
	pageSize int
	// atEnd is set while a frame builds a card in the grid's last rows.
	atEnd   bool
	lastLen int
	total   int
	loading bool
	loaded  bool

	err    string
	query  string
	loadID int
	// refreshed is when the last automatic refresh of a browsable list began;
	// zero until the first. refreshing guards against a second round while one
	// is in flight.
	refreshed  time.Time
	refreshing bool
}

// detailState is one gallery's metadata, fetched first from the cache endpoint
// and then from upstream, plus its bookshelf state.
type detailState struct {
	id    int64
	token string

	g                      Gallery
	tags                   []apiTag
	uploader, posted, size string
	loading, loaded        bool
	err                    string
	loadID                 int
	// loadedAt is when the live metadata last arrived, which decides
	// whether visiting the page again fetches it (ensureDetail).
	loadedAt time.Time

	inBookshelf bool
	shelfKnown  bool
	busy        bool

	// thumbGrid scrolls the detail page's page-thumbnail grid; thumbSel is
	// the cell the keyboard has moved to.
	thumbGrid *ui.GridState
	thumbSel  int

	// reading is the saved page, loaded with the bookshelf status, which
	// the grid marks as where reading stopped.
	reading      int
	readingKnown bool
	// readingSeq numbers the progress requests, so a late answer to an
	// older one does not overwrite a newer page.
	readingSeq int

	// offline download started from this page.
	downloadBusy bool
	downloadErr  string
	downloadJob  string

	// torrents, loaded the first time the user asks for them.
	torrents     torrentsState
	showTorrents bool
}

// newRouter builds the window's router with no page transition. Every page is
// a list, a gallery or the reader, all a sidebar click or one gallery apart,
// and both of mygo's defaults read as motion for its own sake: pages across
// (every sidebar click, and coming back from a gallery to the reader) faded in
// whole for 150 ms, and going deeper slid 250 ms, rebuilding the page left
// behind for the slide. A page appears at once instead, as a native app's
// window replaces its content. Changes inside a page (a load finishing, list
// rows settling, the reader's progress bar) are instant too, through
// c.DisableAnimations in View; loading indicators are untouched, as they play
// timelines of their own rather than animating towards a target.
func newRouter() *ui.Router {
	r := ui.NewRouter("/")
	r.Transition = ui.TransitionNone
	return r
}

func New(client *Client) *App {
	a := &App{
		router:      newRouter(),
		selected:    "/",
		lastList:    "/",
		client:      client,
		covers:      NewCoverCache(),
		images:      NewImageStore(128),
		pageImages:  NewImageStore(6),
		pageThumbs:  NewImageStore(96),
		pageCache:   NewPageCache(4),
		lists:       make(map[string]*listState),
		details:     make(map[int64]*detailState),
		readings:    make(map[int64]readingAt),
		readerStart: -1,

		readerSettings: defaultReaderSettings(),
	}
	a.images.SetAuth(client.user, client.password)
	a.pageImages.SetAuth(client.user, client.password)
	a.pageThumbs.SetAuth(client.user, client.password)
	a.translations.hooked = func() { a.update(func() {}) }
	return a
}

// pageImageStore is the store the reader asks for a picture through. While
// the view builds a frame, what it asks for is what it will draw within the
// refresh, so the store keeps those until the next frame starts
// (ImageStore.BeginFrame) and fetches their downloads first.
//
// Only the view's asks count: the preload window asks on the main thread
// between frames, for pages nobody is looking at yet, and asking for those
// again every frame would keep the whole window from ever being evicted.
func (a *App) pageImageStore() interface {
	Get(string) *ui.Bitmap
} {
	if !a.inView {
		return shownStore{nil, a.pageImages}
	}
	return shownStore{a.pageImages, a.pageImages}
}

// shownStore is the image store seen from the view: asking it for a picture
// marks the picture as one the frame being built shows. A nil marker is the
// main thread's view between frames, which shows nothing yet.
type shownStore struct{ marker, store *ImageStore }

func (s shownStore) Get(url string) *ui.Bitmap {
	if s.marker != nil {
		s.marker.Showed(url)
	}
	return s.store.Get(url)
}

// SetBuild records the version and platform shown in settings (set by main).
func (a *App) SetBuild(description string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.build = description
}

// Attach gives the app the window, which background loads use to redraw.
func (a *App) Attach(w *mygo.Window) {
	a.win = w
	a.images.SetWindow(w)
	a.pageImages.SetWindow(w)
	a.pageThumbs.SetWindow(w)
	a.pageCache.SetWindow(w)
}

// SetBackend installs the in-process backend lifecycle. When the backend is
// not configured yet, the app shows the first-run cookie screen instead of
// the normal UI.
func (a *App) SetBackend(b Backend) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.backend = b
	if b == nil {
		return
	}
	a.setAuthTokenLocked(b.AuthToken())
	a.storage = b.Storage()
	a.translations.path = tagTranslationCachePath(a.storage.CacheDir)
	a.translations.customPath = customTranslationsPath(a.storage.CacheDir)
	a.appearance = b.Appearance()
	a.language = b.Language()
	if b.Configured() {
		a.adoptBackendLocked(b.BaseURL())
		return
	}
	a.showSetup = true
}

// setAuthTokenLocked points the API client and image stores at the backend's
// per-run token.
func (a *App) setAuthTokenLocked(token string) {
	a.client.SetAuthToken(token)
	a.images.SetAuthToken(token)
	a.pageImages.SetAuthToken(token)
	a.pageThumbs.SetAuthToken(token)
}

// adoptBackendLocked points the API client at the backend and drops cached
// data that belonged to the previous address.
func (a *App) adoptBackendLocked(baseURL string) {
	if baseURL != "" {
		a.client.SetBaseURL(baseURL)
	}
	a.lists = make(map[string]*listState)
	a.details = make(map[int64]*detailState)
	a.downloads = Downloads{}
	a.tags = TagSettings{}
	a.cacheLoaded = false
	a.cacheBytes = 0
	a.cacheMax = 0
	a.cacheMaxInit = false
	a.cacheErr = ""
}

// update runs fn on the main thread with the app lock held. Background loads
// use it so they never race the frame being built.
// toast asks for a toast from any goroutine. View adds it while it builds
// the next frame: ui.Context.AddToast touches state the frame owns, so
// calling it from a load goroutine races the renderer.
func (a *App) toast(t ui.Toast) {
	a.mu.Lock()
	a.toasts = append(a.toasts, t)
	win := a.win
	a.mu.Unlock()
	if win != nil {
		win.Update(func() {})
	}
}

// toastLocked is toast for a caller holding a.mu, as update's callback does.
func (a *App) toastLocked(t ui.Toast) {
	a.toasts = append(a.toasts, t)
	if a.win != nil {
		a.win.Update(func() {})
	}
}

// flushToasts adds the toasts asked for since the last frame. The caller
// holds a.mu, as View does.
func (a *App) flushToasts(c *ui.Context) {
	if len(a.toasts) == 0 {
		return
	}
	pending := a.toasts
	a.toasts = nil
	for _, t := range pending {
		c.AddToast(t)
	}
}

func (a *App) update(fn func()) {
	run := func() {
		a.mu.Lock()
		defer a.mu.Unlock()
		fn()
	}
	if a.win != nil {
		a.win.Update(run)
		return
	}
	run()
}

// keepFrames asks the window for another frame while something in it moves on
// its own: a page turning, auto page turn, an overlay about to hide. Without a
// window (tests) the frames come from the tester, so it does nothing.
func (a *App) keepFrames() {
	if a.win != nil {
		a.win.Invalidate()
	}
}

// toggleFullscreen flips the main window between full screen and windowed.
// Without a window (tests) it does nothing.
func (a *App) toggleFullscreen() {
	if a.win == nil {
		return
	}
	a.win.ToggleFullScreen()
}

// isFullscreen reports the window's full-screen state, false without a window.
func (a *App) isFullscreen() bool {
	return a.win != nil && a.win.IsFullScreen()
}

// exitFullscreen leaves full screen, if the window is in it. Leaving the reader
// calls it, so a full-screen read never leaves the rest of the app full screen.
// Without a window (tests) it does nothing.
func (a *App) exitFullscreen() {
	if a.win == nil || !a.win.IsFullScreen() {
		return
	}
	a.win.SetFullScreen(false)
}

// View builds the whole window.
func (a *App) View(c *ui.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	// The frame starts here: what the image store keeps is what this frame
	// asks for (see ImageStore.BeginFrame). Without this the store would keep
	// everything every frame ever asked for, and a page the reader once
	// ordered would never be released.
	a.pageImages.BeginFrame()
	a.inView = true
	defer func() { a.inView = false }()
	a.syncTagRulesLocked()
	// Toasts asked for off the main thread (a download started in a
	// goroutine) are shown here, where the frame, not the goroutine, adds
	// them: mygo's toasts are only safe to touch while a view builds.
	a.flushToasts(c)
	// The view is a function of the state, so what changes between frames is
	// data arriving, not a person moving: a page's loader giving way to its
	// content, the reader growing a toolbar, a list row appearing, the
	// progress bar stepping. Those show at once rather than sliding or fading
	// on their own, and a load finishing never blurs the page it lands on.
	// Navigating is instant already (newRouter); the desktop's Reduce Motion
	// preference still applies to anything that does animate.
	c.DisableAnimations()

	if a.backend != nil && a.showSetup {
		a.setupView(c)
		return
	}

	t := c.Theme()
	a.syncSelection()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		// The reader is read, not browsed: it takes the whole window, without
		// the sidebar its content column would otherwise sit right of.
		if !a.readerActive() {
			a.sidebar(c, t)
		}
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			a.router.View(c, func(r *ui.Route) { a.route(c, r) })
		})
	})
}

// readerActive reports whether the reader page is the one shown, which is what
// decides the sidebar and the reading area's width.
func (a *App) readerActive() bool {
	return strings.HasPrefix(a.router.Path(), "/reader/")
}

func (a *App) syncSelection() {
	p := a.router.Path()
	if p == "" {
		p = "/"
	}
	switch {
	case isListPath(p):
		a.lastList, a.selected = p, p
	case strings.HasPrefix(p, "/gallery/") || strings.HasPrefix(p, "/reader/"):
		a.selected = a.lastList
	default:
		a.selected = p
	}
}

func isListPath(p string) bool {
	switch p {
	case "/", "/watched", "/popular", "/search", "/bookshelf",
		"/recently-read", "/downloads", "/tag-settings", "/settings":
		return true
	}
	return false
}

func (a *App) sidebar(c *ui.Context, t *ui.Theme) {
	nav := a.selected
	ui.Column(c).Width(220).Shrink(0).Background(t.Surface).Children(func() {
		ui.Row(c).Padding(14, 16, 8, 16).AlignItems(ui.Center).Children(func() {
			ui.Text(c, "Manga Reader").Bold().FontSize(15).Grow(1)
			ui.Text(c, i18n.T("Native")).FontSize(10).TextColor(t.TextMuted)
		})
		ui.Sidebar(c, &nav, func() {
			ui.SidebarSection(c, i18n.T("Browse"), nil, func() {
				ui.SidebarItem(c, "/", nil, i18n.T("Home"))
				ui.SidebarItem(c, "/watched", nil, i18n.T("Watched"))
				ui.SidebarItem(c, "/popular", nil, i18n.T("Popular"))
				ui.SidebarItem(c, "/search", nil, i18n.T("Search"))
			})
			ui.SidebarSection(c, i18n.T("My"), nil, func() {
				ui.SidebarItem(c, "/bookshelf", nil, i18n.T("Bookshelf"))
				ui.SidebarItem(c, "/recently-read", nil, i18n.T("Recently read"))
				ui.SidebarItem(c, "/downloads", nil, i18n.T("Downloads"))
			})
			ui.SidebarSection(c, i18n.T("Manage"), nil, func() {
				ui.SidebarItem(c, "/tag-settings", nil, i18n.T("Tag settings"))
				ui.SidebarItem(c, "/settings", nil, i18n.T("Settings"))
			})
		}).Grow(1).OnChange(func() {
			a.router.Push(nav)
		})
	})
}

func (a *App) route(c *ui.Context, r *ui.Route) {
	switch {
	case r.Match("/"):
		r.Title(i18n.T("Home"))
		a.listPage(c, "/", i18n.T("Home"), i18n.T("Latest galleries"), kindHome)
	case r.Match("/watched"):
		r.Title(i18n.T("Watched"))
		a.listPage(c, "/watched", i18n.T("Watched"), i18n.T("Watched galleries"), kindWatched)
	case r.Match("/popular"):
		r.Title(i18n.T("Popular"))
		a.listPage(c, "/popular", i18n.T("Popular"), i18n.T("Sorted by popularity"), kindPopular)
	case r.Match("/search"):
		r.Title(i18n.T("Search"))
		a.searchPage(c)
	case r.Match("/bookshelf"):
		r.Title(i18n.T("Bookshelf"))
		a.listPage(c, "/bookshelf", i18n.T("Bookshelf"), i18n.T("Saved galleries"), kindBookshelf)
	case r.Match("/recently-read"):
		r.Title(i18n.T("Recently read"))
		a.listPage(c, "/recently-read", i18n.T("Recently read"), i18n.T("Pick up where you left off"), kindRecent)
	case r.Match("/downloads"):
		r.Title(i18n.T("Downloads"))
		a.downloadsPage(c)
	case r.Match("/tag-settings"):
		r.Title(i18n.T("Tag settings"))
		a.tagSettingsPage(c)
	case r.Match("/settings"):
		r.Title(i18n.T("Settings"))
		a.settingsPage(c)
	case r.Match("/gallery/{id}/{token}"):
		r.Title(i18n.T("Gallery"))
		id, _ := strconv.ParseInt(r.Param("id"), 10, 64)
		a.galleryPage(c, id, r.Param("token"))
	case r.Match("/reader/{id}/{token}"):
		r.Title(i18n.T("Reader"))
		id, _ := strconv.ParseInt(r.Param("id"), 10, 64)
		a.readerPage(c, id, r.Param("token"), r.Query("offline") == "1")
	default:
		r.Title(i18n.T("Not found"))
		a.emptyPage(c, i18n.T("Not found"), i18n.T("This page does not exist"), "")
	}
}

// --- Lists ---

func (a *App) listFor(path string, kind listKind) *listState {
	s := a.lists[path]
	if s == nil {
		// -1: a grid of covers has no chosen row; the theme would otherwise
		// paint the first item as selected on the first frame.
		s = &listState{kind: kind, selected: -1}
		s.grid = &ui.GridState{
			Selected: &s.selected,
			Key:      func(i int) any { return s.items[i].ID },
			Label:    func(i int) string { return s.items[i].Title },
		}
		a.lists[path] = s
	}
	s.kind = kind
	return s
}

func (a *App) ensureList(s *listState) {
	if s.loading || s.loaded {
		return
	}
	a.startListLoad(s)
}

func (a *App) startListLoad(s *listState) {
	if s.loading {
		return
	}
	s.loading = true
	s.err = ""
	s.atEnd = false
	s.loadID++
	lid := s.loadID
	page, kind, query := s.page, s.kind, s.query

	go func() {
		items, total, pageSize, err := a.fetchList(kind, page, query)
		a.update(func() {
			if s.loadID != lid {
				return
			}
			s.loading = false
			if err != nil {
				// A list left open keeps what it shows; one that has nothing
				// yet (a retry, or a first load) shows the failure with its
				// retry rather than an empty grid.
				if len(s.items) == 0 {
					s.err = err.Error()
				}
				return
			}
			if page == 0 {
				s.items = items
			} else {
				s.items = append(s.items, items...)
			}
			a.mergeListReading(s)
			s.page = page
			s.total = total
			s.pageSize = pageSize
			s.lastLen = len(items)
			s.loaded = true
		})
	}()
}

// makeCard builds a gallery as a list card shows it: the list row, and the page
// this app already knows the gallery was read to. The backend answers a list
// from its own rows, which can lag behind a page the reader here just saved;
// the card (and the detail page it opens) then still says "read to page N" and
// opens there, as main keeps the just-saved progress in its list caches.
func (a *App) makeCard(g Gallery) Gallery {
	if known, ok := a.knownReading(g.ID); ok {
		g.Reading = newerReading(g.Reading, known)
	}
	return g
}

// reloadList starts the list over from its first page, keeping the items on
// screen until the new ones arrive: a list the reader comes back to shows what
// it showed, and a reload of the same query is not a navigation that flashes an
// empty grid. Only a list with nothing on it goes back to its loading state.
func (a *App) reloadList(s *listState) {
	s.loadID++
	s.page = 0
	s.total = 0
	s.lastLen = 0
	s.loading = false
	s.atEnd = false
	s.err = ""
	// loaded stays as it was: a list with items is still loaded while the
	// replacement is on its way, so no page shows an empty state in between.
	s.loaded = len(s.items) > 0
	a.startListLoad(s)
}

func (a *App) resetList(s *listState, query string) {
	if s.query == query && (s.loaded || s.loading) {
		return
	}
	s.query = query
	a.reloadList(s)
}

// readingAt is one gallery's reading progress as this app knows it: what a list
// or the detail page reported, or what the reader saved itself.
type readingAt struct {
	progress ReadingProgress
}

// mergeListReading folds the reading progress this app already knows into the
// items a list has just fetched.
//
// Reading a gallery writes the page to the backend right away, but the lists
// that show it are fetched separately, and the backend answers a list from its
// own rows: coming back from the reader can show the page the gallery had
// before the visit, and a list left open never catches up at all. The reader's
// saves are therefore remembered here (rememberReading) and re-applied to
// the fetched items, where they are newer than the row's own reading (see
// newerReading). This is the desktop counterpart of main's
// applyReadingProgressToCaches, which writes the saved page into the
// recently-read and bookshelf caches so the correct order shows before the
// background refetch confirms it.
//
// Only the items the list has are touched; it never inserts a gallery the list
// does not show.
func (a *App) mergeListReading(s *listState) {
	if len(a.readings) == 0 {
		return
	}
	for i := range s.items {
		if r, ok := a.readings[s.items[i].ID]; ok {
			s.items[i].Reading = newerReading(s.items[i].Reading, r.progress)
		}
	}
}

// newerReading picks the progress to show: r unless current is later. A save
// this app made beats a row from the list when the row is at most as new, and
// loses to one the backend changed since (another device, say).
func newerReading(current *ReadingProgress, r ReadingProgress) *ReadingProgress {
	if current == nil || r.UpdatedAt == "" || current.UpdatedAt == "" {
		return &r
	}
	rt, rerr := time.Parse(time.RFC3339Nano, r.UpdatedAt)
	ct, cerr := time.Parse(time.RFC3339Nano, current.UpdatedAt)
	if rerr != nil || cerr != nil || !ct.After(rt) {
		return &r
	}
	return current
}

// rememberReading records the page the reader saved for a gallery, so the
// lists and detail pages show it before the backend's own rows catch up.
func (a *App) rememberReading(id int64, r ReadingProgress) {
	if a.readings == nil {
		a.readings = make(map[int64]readingAt)
	}
	a.readings[id] = readingAt{progress: r}
}

// knownReading is the progress this app remembers for a gallery, if any.
func (a *App) knownReading(id int64) (ReadingProgress, bool) {
	r, ok := a.readings[id]
	return r.progress, ok
}

// autoRefreshEvery is how long a browsable list trusts what it fetched before
// asking for its first page again, so a page left open shows galleries that
// appeared upstream without a Refresh button. Coming back to a list within
// the stale time shows it at once without a request, as main's GALLERY_LIST_
// and BOOKSHELF_STALE_TIME do in the web frontend: the old refresh reloaded on
// every visit, which both flashed the grid and spent a request per return.
const autoRefreshEvery = 2 * time.Minute

// searchStale is the same for a search: its results depend on a query that
// changes more slowly than a browse list, so revisiting waits longer.
const searchStale = 5 * time.Minute

// listStale is how long s's first page is trusted before asking again.
func (s *listState) listStale() time.Duration {
	if s.kind == kindSearch {
		return searchStale
	}
	return autoRefreshEvery
}

// refreshList reloads the first page without blanking what is on screen: a
// refresh replaces the items in place, and a failure during it keeps the
// current items rather than turning the list into an error page. It is the
// automatic counterpart of reloadList, which a deliberate retry uses.
//
// The new page is merged with the reading progress this app already knows (see
// mergeListReading), so a gallery just read does not go back to the page it
// showed before: the backend's bookshelf/recently-read rows are not
// re-read here, and waiting on them would show the old page on every return.
func (a *App) refreshList(s *listState) {
	if s.loading || s.refreshing {
		return
	}
	s.refreshing = true
	s.refreshed = time.Now()
	s.loadID++
	lid := s.loadID
	kind, query := s.kind, s.query
	go func() {
		items, total, pageSize, err := a.fetchList(kind, 0, query)
		a.update(func() {
			if s.loadID != lid {
				return
			}
			s.refreshing = false
			if err != nil {
				// Keep the stale items: a background refresh should not
				// replace a good page with an error.
				return
			}
			s.items = items
			a.mergeListReading(s)
			s.page = 0
			s.total = total
			s.pageSize = pageSize
			s.lastLen = len(items)
			s.loaded = true
			s.err = ""
		})
	}()
}

// scheduleRefresh asks for the frame that runs the next automatic refresh. It
// is called from a list page each frame, and returns the wait until it is due.
//
// A list being shown for the first time after a visit elsewhere is reloaded
// only when its first page has gone stale; until then what it shows (and its
// scrolling position) is what it showed. A list that failed leaves its error
// to the Retry button instead of asking again every frame.
func (a *App) scheduleRefresh(c *ui.Context, s *listState) {
	now := c.Now()
	stale := s.listStale()
	switch {
	case !s.loaded:
		// Nothing fetched yet, loading, or failed: the page's own state
		// (a loader, an error and its Retry) says what happens next.
		c.After(stale)
		return
	case s.refreshed.IsZero():
		// Loaded while it was not the page on screen: the wait runs from the
		// moment it is looked at.
		s.refreshed = now
	}
	due := s.refreshed.Add(stale)
	if !s.loading && !s.refreshing && !now.Before(due) {
		a.refreshList(s)
		due = now.Add(stale)
	}
	if s.loading || s.refreshing {
		due = now.Add(stale)
	}
	c.After(due.Sub(now))
}

// searchDebounce is how long the search box waits after the last keystroke
// before hitting the API, so typing "gallery" is one request, not seven.
const searchDebounce = 300 * time.Millisecond

// scheduleSearch arms the debounce timer when the search text changed since
// the last time we armed it. It runs from the view (the app lock is held); the
// timer applies the query later through a.update.
func (a *App) scheduleSearch(s *listState) {
	if a.searchQuery == a.searchApplied {
		return
	}
	a.searchApplied = a.searchQuery
	a.searchSeq++
	seq := a.searchSeq
	time.AfterFunc(searchDebounce, func() {
		a.update(func() {
			if a.searchSeq != seq {
				return
			}
			if strings.TrimSpace(a.searchApplied) == "" {
				// An empty box searches nothing: drop any results and stay
				// on the prompt, so the page never browses all galleries.
				a.clearList(s)
				return
			}
			a.resetList(s, a.searchApplied)
		})
	})
}

// clearList empties a list and its state without fetching, so a page can show a
// prompt instead of results until there is something to ask for.
func (a *App) clearList(s *listState) {
	s.loadID++
	s.items = nil
	s.page = 0
	s.total = 0
	s.lastLen = 0
	s.loaded = false
	s.loading = false
	s.atEnd = false
	s.err = ""
	s.query = ""
}

func (a *App) loadMore(s *listState) {
	if s.loading || !a.hasMore(s) {
		return
	}
	s.page++
	a.startListLoad(s)
}

func (a *App) hasMore(s *listState) bool {
	if !s.loaded {
		return false
	}
	if s.total > 0 {
		return len(s.items) < s.total
	}
	return s.pageSize > 0 && s.lastLen >= s.pageSize
}

func (a *App) fetchList(kind listKind, page int, query string) ([]Gallery, int, int, error) {
	ctx := context.Background()
	switch kind {
	case kindHome:
		r, err := a.client.List(ctx, "/api/galleries", page, nil)
		return a.mapItems(r.Results), r.Total, r.PageSize, err
	case kindWatched:
		r, err := a.client.List(ctx, "/api/watched", page, nil)
		return a.mapItems(r.Results), r.Total, r.PageSize, err
	case kindPopular:
		r, err := a.client.List(ctx, "/api/popular", page, nil)
		return a.mapItems(r.Results), r.Total, r.PageSize, err
	case kindSearch:
		r, err := a.client.List(ctx, "/api/search", page, url.Values{"q": []string{query}})
		return a.mapItems(r.Results), r.Total, r.PageSize, err
	case kindBookshelf:
		r, err := a.client.Bookshelf(ctx, page)
		return a.mapShelf(r.Results), r.Total, r.PageSize, err
	case kindRecent:
		r, err := a.client.RecentlyRead(ctx, page)
		return a.mapShelf(r.Results), r.Total, r.PageSize, err
	default:
		return nil, 0, 0, errors.New(i18n.T("Unknown list kind"))
	}
}

func (a *App) mapItems(items []apiGalleryItem) []Gallery {
	out := make([]Gallery, 0, len(items))
	for _, it := range items {
		cover := it.Cover
		if cover == "" {
			cover = it.Thumbnail
		}
		pages := it.Pages
		if pages == 0 {
			pages = it.PageCount
		}
		tags := make([]apiTag, 0, len(it.Tags))
		for _, tag := range it.Tags {
			if tag == "" {
				continue
			}
			ns, name := splitTag(tag)
			tags = append(tags, apiTag{Namespace: ns, Name: name})
		}
		out = append(out, Gallery{
			ID:       it.ID,
			Token:    it.Token,
			Title:    it.Title,
			TitleJPN: it.TitleJPN,
			Category: categoryLabel(it.Category),
			Pages:    pages,
			Rating:   it.Rating,
			Tags:     tags,
			CoverURL: a.client.ThumbnailURL(cover),
		})
	}
	for i := range out {
		out[i] = a.makeCard(out[i])
	}
	return out
}

func (a *App) mapShelf(items []apiShelfItem) []Gallery {
	out := make([]Gallery, 0, len(items))
	for _, it := range items {
		g := Gallery{
			ID:       it.ID,
			Token:    it.Token,
			Title:    it.Title,
			TitleJPN: it.TitleJPN,
			Category: categoryLabel(it.Category),
			Pages:    it.Pages,
			CoverURL: a.client.ThumbnailURL(it.Thumbnail),
		}
		if it.Reading != nil {
			g.Reading = &ReadingProgress{
				CurrentPage: it.Reading.CurrentPage,
				Progress:    it.Reading.Progress,
				Completed:   it.Reading.Completed,
				UpdatedAt:   it.Reading.UpdatedAt,
			}
		}
		out = append(out, g)
	}
	return out
}

func listSummary(subtitle string, s *listState) string {
	switch {
	case s.total > 0:
		return fmt.Sprintf(i18n.T("%s · %d results"), subtitle, s.total)
	case len(s.items) > 0:
		return fmt.Sprintf(i18n.T("%s · %d loaded"), subtitle, len(s.items))
	default:
		return subtitle
	}
}

func (a *App) listPage(c *ui.Context, path, title, subtitle string, kind listKind) {
	t := c.Theme()
	s := a.listFor(path, kind)
	a.ensureList(s)
	// A page left open keeps itself current: refresh the first page in the
	// background on a timer, never showing a Refresh button.
	if kind.browsable() {
		a.scheduleRefresh(c, s)
	}

	ui.Column(c).Fill().Padding(20, 24).Gap(14).Children(func() {
		ui.Column(c).Gap(2).Children(func() {
			ui.Text(c, title).FontSize(26).Bold()
			ui.Text(c, listSummary(subtitle, s)).FontSize(12).TextColor(t.TextMuted)
		})

		switch {
		case s.loading && len(s.items) == 0:
			ui.Column(c).Fill().Center().Gap(10).Children(func() {
				pageLoading(c, i18n.T("Loading…"))
				ui.Text(c, i18n.T("Loading…")).FontSize(12).TextColor(t.TextMuted)
			})
		case s.err != "" && len(s.items) == 0:
			a.errorBlock(c, s.err, func() { a.reloadList(s) })
		case s.loaded && len(s.items) == 0:
			a.emptyPage(c, i18n.T("No results"), i18n.T("Try another keyword or try again later"), "")
		default:
			a.listForCard = s
			_, a.cardWindowH = c.Size()
			ui.GridView(c, s.grid, len(s.items), 190, 300, func(i int) {
				a.galleryCard(c, s.items[i])
			}).Grow(1).Clip()
			a.listForCard = nil
			if s.atEnd {
				a.loadMore(s)
			}
			a.listFooter(c, s)
		}
	})
}

// listFooter is the row under a list: a spinner while the next page loads,
// a retry when it failed, or nothing once the list is complete.
func (a *App) listFooter(c *ui.Context, s *listState) {
	t := c.Theme()
	switch {
	case s.loading:
		ui.Row(c).Center().Gap(8).AlignItems(ui.Center).Children(func() {
			inlineLoading(c, i18n.T("Loading…"))
			ui.Text(c, i18n.T("Loading…")).FontSize(11).TextColor(t.TextMuted)
		})
	case s.err != "":
		ui.Row(c).Center().Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Text(c, i18n.T("Failed to load more")).FontSize(11).TextColor(t.Danger)
			if ui.Button(c, i18n.T("Retry")).Clicked() {
				a.startListLoad(s)
			}
		})
	case a.hasMore(s):
		// The grid asks for the next page as the user nears the end; the
		// button stays for a deliberate click.
		ui.Row(c).Center().Children(func() {
			if ui.Button(c, i18n.T("Load more")).Clicked() {
				a.loadMore(s)
			}
		})
	}
}

// galleryCard draws one gallery. Cards are built lazily by the grid, so this
// doubles as the "is the list near its end" probe: the scroll offset derived
// from the cell gives the item the grid is building, and the tail marks the
// last page it shows.
func (a *App) galleryCard(c *ui.Context, g Gallery) {
	t := c.Theme()
	cover := a.images.Get(g.CoverURL)
	if cover == nil {
		cover = a.covers.Get(int(g.ID))
	}

	cell := ui.Column(c).Gap(6).Focusable()
	// A cell laid out past the bottom of the grid scrolled view is the last
	// item the grid built: the visible range ends there, so a cell in the last
	// few rows means the list is at its end.
	if s := a.listForCard; s != nil && a.cardWindowH > 0 {
		if b := cell.Bounds(); b.H > 0 && b.Y > a.cardWindowH-2*300 {
			s.atEnd = true
		}
	}
	cell.Children(func() {
		loading := a.images.Loading(g.CoverURL)
		switch {
		case cover != nil && !loading:
			ui.Image(c, cover).AspectRatio(3.0 / 4.0).Fit(ui.Cover).Radius(10)
		case cover != nil:
			// A gradient stands in while the real cover downloads; the
			// indicator turns over it so the card says it is still coming.
			box := ui.Box(c).AspectRatio(3.0 / 4.0).Radius(10).Clip()
			box.Children(func() {
				ui.Image(c, cover).Fill().Fit(ui.Cover).Absolute()
				ui.Column(c).Fill().Center().Absolute().Children(func() {
					imageLoadingCenter(c, i18n.T("Loading cover…"))
				})
			})
		case a.images.Err(g.CoverURL) != "":
			// A failed cover retries on click instead of staying blank.
			box := ui.Box(c).AspectRatio(3.0 / 4.0).Radius(10).Center().
				Background(t.Surface).Label(i18n.T("Cover failed to load, click to retry"))
			box.Children(func() {
				ui.Text(c, i18n.T("Cover failed to load")).FontSize(11).TextColor(t.TextMuted)
			})
			if box.Clicked() {
				a.images.Retry(g.CoverURL)
			}
		default:
			box := ui.Box(c).AspectRatio(3.0 / 4.0).Radius(10).Center().Background(t.SurfaceHover)
			box.Children(func() {
				imageLoadingCenter(c, i18n.T("Loading cover…"))
			})
		}
		ui.Text(c, displayTitle(g)).FontSize(13).SingleLine()
		if !g.HasMetadata() {
			// The reading record (or the shelf entry) is there but its
			// metadata never arrived: say so, once, in place of the
			// category/page line an empty card would leave as " · 0 pages".
			ui.Text(c, i18n.T("Details unavailable")).FontSize(11).TextColor(t.TextMuted).SingleLine()
			return
		}
		var meta string
		switch {
		case g.Reading != nil && g.Reading.Completed:
			// 读完了 stays visible: the shelf should say so, not silently
			// look like page 1 of a fresh gallery.
			meta = fmt.Sprintf(i18n.T("%s · Finished · %d pages"), g.Category, g.Pages)
		case g.Reading != nil && g.Reading.CurrentPage > 0:
			meta = fmt.Sprintf(i18n.T("%s · Page %d"), g.Category, g.Reading.CurrentPage+1)
		default:
			meta = fmt.Sprintf(i18n.T("%s · %d pages"), g.Category, g.Pages)
		}
		ui.Text(c, meta).FontSize(11).TextColor(t.TextMuted).SingleLine()
		if len(g.Tags) > 0 {
			// Two chips at most: the card is small, and the colours are what
			// matter at a glance.
			ui.Row(c).Gap(4).Wrap().Children(func() {
				for i, tag := range a.sortedTags(g.Tags) {
					if i == 2 {
						break
					}
					a.tagChip(c, tag.Namespace, tag.Name)
				}
			})
		}
	})
	if cell.Clicked() {
		a.router.Push(fmt.Sprintf("/gallery/%d/%s", g.ID, g.Token))
	}
}

// HasMetadata reports whether the gallery can describe itself: a title, a
// category or a page count. The bookshelf and the history refer to galleries
// whose upstream entry is gone, and the backend fetches such an entry the
// first time a list needs it; until that read succeeds the card has only the
// record, and says so instead of showing blanks.
func (g Gallery) HasMetadata() bool {
	return g.Title != "" || g.TitleJPN != "" || g.Pages > 0 || g.CoverURL != ""
}

// finishedReading reports whether progress says the gallery was read to its
// end. A gallery that was never opened has no progress (or page 0, not
// completed), which is not the same as one finished on its first page.
func finishedReading(r *ReadingProgress) bool {
	return r != nil && r.Completed
}

func displayTitle(g Gallery) string {
	if g.Title != "" {
		return g.Title
	}
	if g.TitleJPN != "" {
		return g.TitleJPN
	}
	return fmt.Sprintf(i18n.T("Gallery %d"), g.ID)
}

func (a *App) searchPage(c *ui.Context) {
	t := c.Theme()
	s := a.listFor("/search", kindSearch)
	a.scheduleSearch(s)
	// Nothing to search yet: the page starts on a prompt, and only asks the
	// API once there is a query.
	hasQuery := strings.TrimSpace(s.query) != ""
	if hasQuery {
		a.ensureList(s)
	}

	ui.Column(c).Fill().Padding(20, 24).Gap(14).Children(func() {
		ui.Text(c, i18n.T("Search")).FontSize(26).Bold()
		ui.TextInput(c, &a.searchQuery).Width(480).Label(i18n.T("Search field")).
			Placeholder(i18n.T("Search titles, categories or tags…")).AutoFocus()
		ui.Text(c, listSummary(i18n.T("Search results"), s)).FontSize(12).TextColor(t.TextMuted)

		switch {
		case !hasQuery:
			a.emptyPage(c, i18n.T("Search"), i18n.T("Type a keyword to search titles, categories or tags"), "")
		case s.loading && len(s.items) == 0:
			ui.Column(c).Fill().Center().Gap(10).Children(func() {
				pageLoading(c, i18n.T("Searching…"))
				ui.Text(c, i18n.T("Searching…")).FontSize(12).TextColor(t.TextMuted)
			})
		case s.err != "" && len(s.items) == 0:
			a.errorBlock(c, s.err, func() { a.reloadList(s) })
		case s.loaded && len(s.items) == 0:
			a.emptyPage(c, i18n.T("No results"), i18n.T("Try a different keyword"), "")
		default:
			a.listForCard = s
			_, a.cardWindowH = c.Size()
			ui.GridView(c, s.grid, len(s.items), 190, 300, func(i int) {
				a.galleryCard(c, s.items[i])
			}).Grow(1).Clip()
			a.listForCard = nil
			if s.atEnd {
				a.loadMore(s)
			}
			a.listFooter(c, s)
		}
	})
}

// --- Gallery detail ---

// detailStale is how long a gallery's metadata is trusted before visiting it
// again asks the backend (main's GALLERY_DETAIL_STALE_TIME): a gallery's
// metadata changes slowly, and re-fetching it on every visit put a spinner in
// front of a page that was already known. Reading progress is different and is
// asked for on every visit: main refetches it too.
const detailStale = 5 * time.Minute

// ensureDetail returns the gallery's page state, fetching what is missing or
// stale. A page that is already known is shown as it is while the rest loads:
// main's detail page shows its cached copy while the live scrape runs, and a
// visit back from the reader should not blank the page it returns to.
func (a *App) ensureDetail(id int64, token string) *detailState {
	s := a.details[id]
	if s == nil {
		s = &detailState{id: id, token: token}
		a.details[id] = s
	}
	if s.loading {
		return s
	}
	needMeta := !s.loaded || s.loadedAt.IsZero() || time.Since(s.loadedAt) >= detailStale
	// Reading progress is asked for on every visit, as main's detail page
	// does: one cheap request which the button and the reader's start page
	// depend on. It is a request of its own, leaving an already loaded page
	// as it is until the answer lands (startDetailLoad).
	if needMeta {
		a.startDetailLoad(s, true)
	} else {
		a.refreshDetailReading(s)
	}
	return s
}

// startDetailLoad asks for what the page does not have yet: the reading
// progress (always, unless asked for metadata only) and, when withMeta, the
// gallery's metadata: the cached copy first, as main's useOnlineThenCached
// does (useCacheAsPlaceholder), then the live answer when it arrives. The two
// run apart, so "Continue" and the reader's page do not wait on a live scrape
// that takes seconds, and a page already on screen keeps showing it.
func (a *App) startDetailLoad(s *detailState, withMeta bool) {
	s.loading = true
	s.err = ""
	s.loadID++
	lid := s.loadID
	id, token := s.id, s.token

	a.refreshDetailReading(s)

	go func() {
		ctx := context.Background()
		if withMeta {
			if d, err := a.client.CachedGallery(ctx, id, token); err == nil && d.PageCount > 0 {
				a.update(func() {
					if s.loadID != lid {
						return
					}
					a.applyDetail(s, d)
					s.loaded = true
				})
			}
		}
		if !withMeta {
			a.update(func() {
				if s.loadID == lid {
					s.loading = false
				}
			})
			return
		}
		d, err := a.client.Gallery(ctx, id, token)
		if err != nil {
			// The cache may have answered above; only a page with nothing
			// on it shows this error.
			a.update(func() {
				if s.loadID != lid {
					return
				}
				s.loading = false
				if !s.loaded {
					s.err = err.Error()
				}
			})
			return
		}
		inShelf, shelfErr := a.client.BookshelfStatus(ctx, id, token)
		a.update(func() {
			if s.loadID != lid {
				return
			}
			s.loading = false
			a.applyDetail(s, d)
			s.loaded = true
			s.loadedAt = time.Now()
			if shelfErr == nil {
				s.inBookshelf, s.shelfKnown = inShelf, true
			}
		})
	}()
}

func (a *App) retryDetail(s *detailState) {
	s.loaded = false
	s.loading = false
	s.err = ""
	a.startDetailLoad(s, true)
}

// refreshDetailReading asks for where the gallery was read to, without
// touching the metadata: the detail page loads like main's (the progress query
// runs on its own), so a page already on screen is never blanked for it, and a
// visit to the reader shows up as soon as the answer lands.
func (a *App) refreshDetailReading(s *detailState) {
	s.readingSeq++
	seq := s.readingSeq
	id, token := s.id, s.token
	go func() {
		reading, err := a.client.Progress(context.Background(), id, token)
		if err != nil {
			return
		}
		a.update(func() {
			if s.readingSeq == seq {
				a.applyDetailReading(s, reading)
			}
		})
	}()
}

// applyDetail copies a gallery's metadata into the detail page. The reading
// progress comes from Progress, not from the metadata, and is left alone.
func (a *App) applyDetail(s *detailState, d apiGalleryDetail) {
	reading, readingKnown := s.reading, s.readingKnown
	g := a.galleryFromDetail(d)
	if readingKnown {
		g.Reading = &ReadingProgress{CurrentPage: reading}
	}
	s.g = g
	s.tags = d.Tags
	s.uploader = d.Uploader
	s.posted = d.PostedAt
	s.size = d.FileSize
	s.reading, s.readingKnown = reading, readingKnown
}

// applyDetailReading records where the gallery was read to: the backend's
// progress row, and the page this app saved in it when that is further on (the
// row can be read before a visit's write lands, or be a list row the backend
// had not updated yet). The detail page's button and the reader both take their
// page from it.
func (a *App) applyDetailReading(s *detailState, r apiReading) {
	progress := ReadingProgress{
		CurrentPage: r.CurrentPage,
		Progress:    r.Progress,
		Completed:   r.Completed,
		UpdatedAt:   r.UpdatedAt,
	}
	if known, ok := a.knownReading(s.id); ok && known.CurrentPage > progress.CurrentPage {
		progress = known
		progress.UpdatedAt = r.UpdatedAt
	}
	s.reading, s.readingKnown = progress.CurrentPage, true
	s.g.Reading = &progress
}

func (a *App) galleryFromDetail(d apiGalleryDetail) Gallery {
	cover := d.Thumbnail
	if cover == "" {
		cover = d.Cover
	}
	return Gallery{
		ID:       d.ID,
		Token:    d.Token,
		Title:    d.Title,
		TitleJPN: d.TitleJPN,
		Category: categoryLabel(d.Category),
		Pages:    d.PageCount,
		Rating:   d.Rating,
		Tags:     d.Tags,
		CoverURL: a.client.ThumbnailURL(cover),
	}
}

func (a *App) galleryPage(c *ui.Context, id int64, token string) {
	s := a.ensureDetail(id, token)
	// Rules colour the tags, so load them with the page.
	a.ensureTagSettings()
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

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(20, 24).Gap(18).MaxWidth(1100).Children(func() {
			ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
				if ui.Button(c, i18n.T("← Back")).Clicked() {
					a.router.Back()
				}
				ui.Text(c, displayTitle(g)).FontSize(22).Bold().Grow(1)
				// NextPage is 0 both for a gallery never opened and for one
				// read to its end: the saved page tells them apart. The page
				// this app saved in the gallery counts too, so a gallery just
				// read continues where it stopped even before the backend's
				// progress row says so.
				page, finished := s.reading, finishedReading(g.Reading)
				if known, ok := a.knownReading(g.ID); ok && known.CurrentPage > page {
					page, finished = known.CurrentPage, known.Completed
				}
				label := i18n.T("Start reading")
				if g.Pages > 0 && page >= g.Pages-1 {
					label = i18n.T("Read again")
				} else if finished {
					label = i18n.T("Read again")
				} else if page > 0 {
					label = fmt.Sprintf(i18n.T("Continue (page %d)"), page+1)
				}
				if ui.PrimaryButton(c, label).Clicked() {
					a.readerStart = -1
					a.router.Push(fmt.Sprintf("/reader/%d/%s", g.ID, g.Token))
				}
			})

			ui.Row(c).Gap(20).AlignItems(ui.Start).Children(func() {
				if a.images.Loading(g.CoverURL) {
					box := ui.Box(c).Size(240, 320).Radius(12).Clip()
					box.Children(func() {
						ui.Image(c, cover).Fill().Fit(ui.Cover).Absolute()
						ui.Column(c).Fill().Center().Absolute().Children(func() {
							imageLoadingCenter(c, i18n.T("Loading cover…"))
						})
					})
				} else {
					ui.Image(c, cover).Size(240, 320).Fit(ui.Cover).Radius(12)
				}
				ui.Column(c).Gap(10).Grow(1).Children(func() {
					if g.TitleJPN != "" && g.TitleJPN != g.Title {
						ui.Text(c, g.TitleJPN).FontSize(13).TextColor(t.TextMuted)
					}
					ui.Textf(c, i18n.T("Category: %s"), g.Category).FontSize(13)
					ui.Textf(c, i18n.T("Pages: %d"), g.Pages).FontSize(13)
					ui.Textf(c, i18n.T("Rating: %.1f"), g.Rating).FontSize(13)
					if s.uploader != "" {
						ui.Textf(c, i18n.T("Uploader: %s"), s.uploader).FontSize(13)
					}
					if s.posted != "" {
						ui.Textf(c, i18n.T("Posted: %s"), s.posted).FontSize(13)
					}
					if s.size != "" {
						ui.Textf(c, i18n.T("Size: %s"), s.size).FontSize(13)
					}

					ui.Text(c, i18n.T("Tags")).FontSize(13).TextColor(t.TextMuted)
					ui.Row(c).Gap(6).Wrap().Children(func() {
						for _, tag := range a.sortedTags(s.tags) {
							a.tagChip(c, tag.Namespace, tag.Name)
						}
					})

					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						label := i18n.T("Add to bookshelf")
						if s.inBookshelf {
							label = i18n.T("Remove from bookshelf")
						}
						if ui.Button(c, label).Disabled(s.busy).Clicked() {
							a.toggleBookshelf(s)
						}
					})
					a.detailDownloadsBlock(c, s)
					if !s.shelfKnown {
						ui.Text(c, i18n.T("Bookshelf state is unavailable")).FontSize(11).TextColor(t.TextMuted)
					}
				})
			})

			ui.Divider(c)
			a.torrentsBlock(c, s)
			ui.Divider(c)
			a.pageThumbGrid(c, g, s)
		})
	})
}

// pageThumbGrid draws the detail page's page thumbnails, one per page, in a
// virtualized grid that fetches only the cells on screen. A click opens the
// reader at that page.
func (a *App) pageThumbGrid(c *ui.Context, g Gallery, s *detailState) {
	t := c.Theme()
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Text(c, i18n.T("Page thumbnails")).FontSize(16).Bold()
		if g.Pages > 0 {
			ui.Textf(c, i18n.T("%d pages · click to jump"), g.Pages).FontSize(12).TextColor(t.TextMuted)
		}
	})
	if g.Pages <= 0 {
		ui.Text(c, i18n.T("Page count unknown; open the reader to see the pages.")).FontSize(12).TextColor(t.TextMuted)
		return
	}

	// The page the reader would open at, marked in the grid: the saved
	// progress, so the grid says where reading stopped.
	marked := -1
	if s.readingKnown && s.reading >= 0 && s.reading < g.Pages {
		marked = s.reading
	}
	if s.thumbGrid == nil {
		s.thumbSel = -1
		s.thumbGrid = &ui.GridState{
			Selected: &s.thumbSel,
			Key:      func(i int) any { return i },
			Label:    func(i int) string { return fmt.Sprintf(i18n.T("Page %d"), i+1) },
		}
	}
	ui.GridView(c, s.thumbGrid, g.Pages, 120, 190, func(i int) {
		cell := ui.Column(c).Gap(4).Focusable().Label(fmt.Sprintf(i18n.T("Page %d"), i+1))
		if i == marked {
			cell.Radius(8).Border(2, t.Accent)
		}
		cell.Children(func() {
			url := a.client.PageThumbnailURL(g.ID, g.Token, i)
			thumb := a.pageThumbs.Get(url)
			switch {
			case thumb != nil:
				ui.Image(c, thumb).AspectRatio(200.0 / 282.0).Fit(ui.Cover).Radius(8)
			case a.pageThumbs.Err(url) != "":
				// A failed thumbnail offers its retry in place.
				box := ui.Box(c).AspectRatio(200.0 / 282.0).Radius(8).Center().
					Background(t.SurfaceHover).Label(i18n.Tf("Thumbnail for page %d failed to load, click to retry", i+1))
				box.Children(func() {
					ui.Text(c, i18n.T("Failed to load")).FontSize(11).TextColor(t.Danger)
				})
				if box.Clicked() {
					a.pageThumbs.Retry(url)
				}
				return
			default:
				box := ui.Box(c).AspectRatio(200.0 / 282.0).Radius(8).Center().Background(t.SurfaceHover)
				box.Children(func() {
					imageLoadingCenter(c, i18n.Tf("Loading thumbnail for page %d…", i+1))
				})
			}
			ui.Textf(c, "%d", i+1).FontSize(11).TextColor(t.TextMuted)
		})
		if cell.Clicked() {
			a.openReaderAt(g.ID, g.Token, i)
		}
	}).Height(420).Clip()
}

// openReaderAt opens the reader showing page (0-based), whatever page the
// saved progress would have resumed.
func (a *App) openReaderAt(id int64, token string, page int) {
	a.readerStart = page
	a.router.Push(fmt.Sprintf("/reader/%d/%s", id, token))
}

func (a *App) toggleBookshelf(s *detailState) {
	if s.busy {
		return
	}
	add := !s.inBookshelf
	s.busy = true
	id, token := s.id, s.token

	go func() {
		ctx := context.Background()
		var err error
		if add {
			err = a.client.AddBookshelf(ctx, id, token)
		} else {
			err = a.client.RemoveBookshelf(ctx, id, token)
		}
		a.update(func() {
			s.busy = false
			if err == nil {
				s.inBookshelf, s.shelfKnown = add, true
			}
		})
	}()
}

// --- Setup ---

// setupView is the first-run cookie screen. It replaces the whole window
// until the embedded backend starts successfully.
func (a *App) setupView(c *ui.Context) {
	t := c.Theme()
	ui.Row(c).Fill().Background(t.Background).Center().Children(func() {
		ui.Column(c).Width(620).Gap(14).Children(func() {
			ui.Text(c, i18n.T("First run")).FontSize(26).Bold()
			ui.Text(c, i18n.T("Paste your ExHentai cookie; the app starts the built-in service on this machine and saves the setting.")).
				FontSize(13).TextColor(t.TextMuted)
			if a.backend != nil {
				if msg := a.backend.StartError(); msg != "" {
					ui.Text(c, msg).FontSize(12).TextColor(t.Danger).MaxWidth(560)
				}
			}
			ui.TextArea(c, &a.setupCookie).
				Label("ExHentai Cookies").
				Placeholder("ipb_member_id=...; ipb_pass_hash=...; igneous=...; sk=...").
				Height(90).
				AutoFocus()
			ui.Text(c, i18n.T("Sign in to exhentai.org in a browser, then copy the Cookie from the request headers in the developer tools.")).
				FontSize(11).TextColor(t.TextMuted).MaxWidth(560)
			if a.setupErr != "" {
				ui.Text(c, a.setupErr).FontSize(12).TextColor(t.Danger).MaxWidth(560)
			}
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				if ui.PrimaryButton(c, i18n.T("Save and start")).Disabled(a.setupBusy).Clicked() {
					a.beginSetup()
				}
				if a.setupBusy {
					inlineLoading(c, i18n.T("Starting the built-in service…"))
					ui.Text(c, i18n.T("Starting the built-in service…")).FontSize(12).TextColor(t.TextMuted)
				}
			})
		})
	})
}

// beginSetup starts the backend with the entered cookie. It runs from the
// view, so the app lock is already held; the backend starts in the background
// and the result is applied under the lock.
func (a *App) beginSetup() {
	if a.setupBusy {
		return
	}
	cookie := strings.TrimSpace(a.setupCookie)
	if cookie == "" {
		a.setupErr = i18n.T("Please paste your ExHentai cookies")
		return
	}
	backend := a.backend
	if backend == nil {
		return
	}
	a.setupBusy = true
	a.setupErr = ""

	go func() {
		base, err := backend.Start(cookie)
		a.update(func() {
			a.setupBusy = false
			if err != nil {
				a.setupErr = err.Error()
				return
			}
			a.storage = backend.Storage()
			a.setAuthTokenLocked(backend.AuthToken())
			a.showSetup = false
			a.adoptBackendLocked(base)
		})
	}()
}

// --- Static pages ---

func (a *App) settingsPage(c *ui.Context) {
	t := c.Theme()
	if a.backend != nil {
		a.ensureCacheInfo()
	}

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(20, 24).Gap(14).MaxWidth(760).Children(func() {
			ui.Text(c, i18n.T("Settings")).FontSize(26).Bold()
			if a.backend != nil && !a.backend.Configured() {
				if msg := a.backend.StartError(); msg != "" {
					ui.Text(c, msg).FontSize(12).TextColor(t.Danger).MaxWidth(720)
				}
			}

			if a.backend != nil {
				ui.Text(c, i18n.T("Account")).FontSize(16).Bold()
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, "ExHentai Cookies").Grow(1)
					ui.Badge(c, a.cookieStatus())
				})
				ui.Text(c, i18n.T("Changing the cookies restarts the built-in service.")).FontSize(11).TextColor(t.TextMuted)
				ui.Row(c).Children(func() {
					if ui.Button(c, i18n.T("Reconfigure cookies")).Clicked() {
						a.setupCookie = ""
						a.setupErr = ""
						a.showSetup = true
					}
				})
				ui.Divider(c)
			}

			ui.Text(c, i18n.T("Storage")).FontSize(16).Bold()
			a.settingsRow(c, i18n.T("Backend address"), a.client.BaseURL())
			a.settingsRow(c, i18n.T("Config file"), a.storage.ConfigPath)
			a.configFileRow(c)
			a.settingsRow(c, i18n.T("Local database"), a.storage.DBPath)
			a.settingsRow(c, i18n.T("Image cache directory"), a.storage.CacheDir)
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, i18n.T("Cache size")).Grow(1)
				switch {
				case a.cacheErr != "":
					ui.Badge(c, i18n.T("Read failed"))
				case a.cacheLoading && !a.cacheLoaded:
					ui.Badge(c, i18n.T("Counting…"))
				default:
					ui.Badge(c, humanBytes(a.cacheBytes)+" / "+cacheLimitText(a.cacheMax))
				}
			})
			if a.cacheErr != "" {
				ui.Text(c, a.cacheErr).FontSize(11).TextColor(t.Danger)
			}
			if a.backend != nil {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, i18n.T("Cache limit (MB)")).Grow(1)
					ui.TextInput(c, &a.cacheMaxInput).Width(160)
					if ui.Button(c, i18n.T("Save limit")).Disabled(a.cacheMaxBusy).Clicked() {
						a.saveCacheLimit()
					}
					if a.cacheMaxBusy {
						inlineLoading(c, i18n.T("Saving…"))
					}
				})
				if a.cacheMaxErr != "" {
					ui.Text(c, a.cacheMaxErr).FontSize(11).TextColor(t.Danger)
				}
				ui.Text(c, i18n.T("0 means unlimited; past the limit the least recently used images are deleted.")).
					FontSize(11).TextColor(t.TextMuted)
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					if ui.Button(c, i18n.T("Clear cache")).Disabled(a.cacheBusy).Clicked() {
						a.clearCache()
					}
					if a.cacheBusy {
						inlineLoading(c, i18n.T("Clearing cache…"))
					}
				})
			}
			ui.Text(c, i18n.T("Cleared images download again the next time they are shown.")).
				FontSize(11).TextColor(t.TextMuted)

			if a.backend != nil {
				ui.Divider(c)
				a.syncSettingsSection(c)
			}

			ui.Divider(c)
			ui.Text(c, i18n.T("Appearance")).FontSize(16).Bold()
			a.appearanceRow(c)

			ui.Divider(c)
			ui.Text(c, i18n.T("Language")).FontSize(16).Bold()
			a.languageRow(c)

			ui.Divider(c)
			ui.Text(c, i18n.T("About")).FontSize(16).Bold()
			a.settingsRow(c, i18n.T("Version"), a.build)
			ui.Text(c, i18n.T("Third-party ExHentai client, native Go UI (mygo), no WebView.")).
				FontSize(11).TextColor(t.TextMuted)
			ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
				if ui.Button(c, i18n.T("Export diagnostics")).Disabled(a.diagBusy).Clicked() {
					a.exportDiagnostics()
				}
				ui.Text(c, i18n.T("Includes version, paths, cache size and job counts; no cookies or images.")).
					FontSize(11).TextColor(t.TextMuted)
			})
			if a.diagMsg != "" {
				ui.Text(c, a.diagMsg).FontSize(11).TextColor(t.TextMuted)
			}
		})
	})
}

func (a *App) settingsRow(c *ui.Context, label, value string) {
	if value == "" {
		value = "—"
	}
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Text(c, label).Grow(1)
		ui.Badge(c, value)
	})
}

func (a *App) cookieStatus() string {
	if a.backend != nil && a.backend.Configured() {
		return i18n.T("Configured")
	}
	return i18n.T("Not configured")
}

// ensureCacheInfo kicks off a background size computation once per load.
func (a *App) ensureCacheInfo() {
	// The settings page reads sizes from disk, so it is worth asking even
	// before the backend answered; the error then shows instead of a count.
	if a.backend == nil || a.cacheLoading || a.cacheLoaded {
		return
	}
	a.cacheLoading = true
	backend := a.backend
	go func() {
		stats, err := backend.CacheInfo()
		a.update(func() {
			a.cacheLoading = false
			a.cacheLoaded = true
			if err != nil {
				a.cacheErr = err.Error()
				return
			}
			a.cacheErr = ""
			if stats.Root != "" {
				a.storage.CacheDir = stats.Root
			}
			a.cacheBytes = stats.Bytes
			a.cacheMax = stats.MaxBytes
			if !a.cacheMaxInit {
				a.cacheMaxInput = cacheLimitInput(stats.MaxBytes)
				a.cacheMaxInit = true
			}
		})
	}()
}

// saveCacheLimit parses the MB input and persists the new limit. It runs from
// the view, so the lock is already held.
func (a *App) saveCacheLimit() {
	if a.backend == nil || a.cacheMaxBusy {
		return
	}
	raw := strings.TrimSpace(a.cacheMaxInput)
	var maxBytes int64
	if raw != "" {
		mb, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || mb < 0 {
			a.cacheMaxErr = i18n.T("Enter a whole number of MB, 0 or more")
			return
		}
		maxBytes = mb << 20
	}

	a.cacheMaxBusy = true
	a.cacheMaxErr = ""
	backend := a.backend
	go func() {
		err := backend.SetCacheMaxBytes(maxBytes)
		a.update(func() {
			a.cacheMaxBusy = false
			if err != nil {
				a.cacheMaxErr = err.Error()
				return
			}
			a.cacheMaxErr = ""
			a.cacheMax = maxBytes
			// Recompute the size: a smaller limit prunes in the background.
			a.cacheLoaded = false
			a.ensureCacheInfo()
		})
	}()
}

func (a *App) clearCache() {
	if a.backend == nil || a.cacheBusy {
		return
	}
	a.cacheBusy = true
	a.cacheErr = ""
	backend := a.backend
	go func() {
		err := backend.ClearCache()
		a.update(func() {
			a.cacheBusy = false
			if err != nil {
				a.cacheErr = err.Error()
				return
			}
			a.cacheErr = ""
			a.cacheBytes = 0
			a.cacheLoaded = true
		})
	}()
}

// humanBytes formats a byte count for the settings page.
func humanBytes(n int64) string {
	const unit = 1024
	if n < unit {
		return fmt.Sprintf("%d B", n)
	}
	div, exp := int64(unit), 0
	for x := n / unit; x >= unit; x /= unit {
		div *= unit
		exp++
	}
	return fmt.Sprintf("%.1f %cB", float64(n)/float64(div), "KMGTPE"[exp])
}

// cacheLimitText renders a byte limit for display; 0 means unlimited.
func cacheLimitText(maxBytes int64) string {
	if maxBytes <= 0 {
		return i18n.T("Unlimited")
	}
	return humanBytes(maxBytes)
}

// cacheLimitInput renders a byte limit as whole megabytes for the settings
// input; 0 (unlimited) renders as "0".
func cacheLimitInput(maxBytes int64) string {
	if maxBytes <= 0 {
		return "0"
	}
	return strconv.FormatInt(maxBytes>>20, 10)
}

// emptyPage is the one empty state for pages: an icon tile, a title, a line
// of explanation and an optional hint, all centred. Every list, the reader
// and the downloads page use it, so "nothing here" looks the same anywhere.
func (a *App) emptyPage(c *ui.Context, title, subtitle, hint string) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(10).Padding(24).Children(func() {
		stateIcon(c, moodEmpty)
		ui.Text(c, title).FontSize(22).Bold()
		ui.Text(c, subtitle).FontSize(13).TextColor(t.TextMuted).MaxWidth(560)
		if hint != "" {
			ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted).MaxWidth(560)
		}
	})
}

// bookSVG is the app's mark as a line icon: an open book, matching the
// application icon. Parsed once.
var bookSVG = ui.MustParseSVG([]byte(
	`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" ` +
		`stroke="currentColor" stroke-width="1.6" stroke-linecap="round" stroke-linejoin="round">` +
		`<path d="M12 6.5C10.5 5.2 8.5 4.6 5.5 4.5v13c3 .1 5 .7 6.5 2 1.5-1.3 3.5-1.9 6.5-2v-13c-3 .1-5 .7-6.5 2z"/>` +
		`<path d="M12 6.5v13"/></svg>`))

// stateIcon is the rounded tile above an empty or error message: the app's
// book mark drawn in a muted colour, big enough to read as an illustration.
func stateIcon(c *ui.Context, mood stateMood) {
	t := c.Theme()
	tile := ui.Box(c).Size(72, 72).Radius(18).Center()
	switch mood {
	case moodError:
		tile.Background(t.Surface).Border(2, t.Danger)
	default:
		tile.Background(t.SurfaceHover)
	}
	ink := t.TextMuted
	if mood == moodError {
		ink = t.Danger
	}
	tile.Children(func() {
		ui.Icon(c, bookSVG).Size(40, 40).TextColor(ink)
	})
}

// stateMood picks the colour of a state icon.
type stateMood int

const (
	moodEmpty stateMood = iota
	moodError
)

// errorBlock is the one error state for pages: a title, the backend's
// message, and 重试. Backend down, network failure and a bad response all
// come through here, so they look the same wherever they happen.
func (a *App) errorBlock(c *ui.Context, msg string, retry func()) {
	t := c.Theme()
	if msg == "" {
		msg = i18n.T("An unknown error occurred")
	}
	ui.Column(c).Fill().Center().Gap(10).Padding(24).Children(func() {
		stateIcon(c, moodError)
		ui.Badge(c, errorBadge(msg)).Background(t.Danger).TextColor(t.Background)
		ui.Text(c, msg).FontSize(12).TextColor(t.TextMuted).MaxWidth(560)
		if ui.Button(c, i18n.T("Retry")).Clicked() {
			retry()
		}
	})
}

// errorBadge says which kind of failure this is, so the same block can read
// "无法连接" for a dead backend and "请求失败" for a bad response.
func errorBadge(msg string) string {
	if strings.HasPrefix(msg, i18n.T("Failed to reach the backend")) {
		return i18n.T("Cannot connect")
	}
	return i18n.T("Request failed")
}

// appearanceRow is the 跟随系统/浅色/深色 choice. The window is themed by main
// through mygo.Theme, which the backend call wraps.
func (a *App) appearanceRow(c *ui.Context) {
	t := c.Theme()
	if a.appearance == "" {
		a.appearance = "system"
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, i18n.T("Theme")).Grow(1)
		for _, opt := range []struct{ value, label string }{
			{"system", i18n.T("Follow system")},
			{"light", i18n.T("Light")},
			{"dark", i18n.T("Dark")},
		} {
			label := opt.label
			if a.appearance == opt.value {
				label = "✓ " + label
			}
			// The appearance is the window's own, so it applies whether or
			// not an embedded backend is there to remember it (external
			// backend mode keeps it for this run only).
			if ui.Button(c, label).Disabled(a.appearanceBusy).
				Clicked() && a.appearance != opt.value {
				a.saveAppearance(opt.value)
			}
		}
	})
	if a.appearanceMsg != "" {
		ui.Text(c, a.appearanceMsg).FontSize(11).TextColor(t.TextMuted)
	}
}

// saveAppearance persists the choice; main applies it to mygo.Theme through
// the backend hook, so no page reload is needed.
func (a *App) saveAppearance(value string) {
	if a.appearanceBusy {
		return
	}
	a.appearanceBusy = true
	a.appearanceMsg = ""
	// The window follows at once: mygo themes every window from the source,
	// so there is nothing to restart. Applying before the save means a save
	// that fails still shows the choice, and the message says so.
	applyAppearance(value)
	a.appearance = value
	if a.backend == nil {
		a.appearanceBusy = false
		a.requestFrame()
		return
	}
	backend := a.backend
	go func() {
		err := backend.SetAppearance(value)
		a.update(func() {
			a.appearanceBusy = false
			if err != nil {
				a.appearanceMsg = i18n.Tf("Shown, but not saved: %s", err.Error())
				return
			}
			a.appearance = backend.Appearance()
			a.appearanceMsg = i18n.T("Saved")
		})
	}()
}

// applyAppearance gives mygo the appearance source: "system" follows the
// desktop, "light" and "dark" pin it. The windows redraw from it.
//
// It runs in the background: Theme.SetSource marshals to the main thread and
// waits, which would deadlock a caller inside an event handler anyway, and
// would wait forever when no loop is running (headless tests).
func applyAppearance(source string) {
	var value mygo.ThemeSource
	switch source {
	case "light":
		value = mygo.ThemeLight
	case "dark":
		value = mygo.ThemeDark
	default:
		value = mygo.ThemeSystem
	}
	go mygo.Theme.SetSource(value)
}

// languageOptions names the interface languages the UI can be shown in: the
// mygo locale tag each applies, in the order the settings page lists them.
// Traditional Chinese is not on the list: it would change one word of one
// row and nothing else.
type languageOption struct {
	saved  string // what config.json keeps
	locale language.Tag
}

var languageOptions = []languageOption{
	{"zh-CN", language.Chinese},
	{"en", language.English},
}

// languageOptionNames returns the labels of languageOptions in the language
// the interface currently shows.
func languageOptionNames() []string { return i18n.Default.Names() }

// savedLanguageFor is the config.json tag of the language a mygo locale shows,
// so the row's tick follows what is on screen before the file is read.
func savedLanguageFor(t language.Tag) string {
	base, _ := t.Base()
	if base.String() == "en" {
		return "en"
	}
	return "zh-CN"
}

// syncSettingsSection is the 数据同步 part of the settings page: enable the
// engine, point it at another device running this app, and see whether it
// is connected. Both sides must use the same token, and both sides must
// have the peer endpoints on (a sync token in the settings).
func (a *App) syncSettingsSection(c *ui.Context) {
	t := c.Theme()
	a.ensureSyncConfig()
	s := &a.sync

	ui.Text(c, i18n.T("Data sync")).FontSize(16).Bold()
	ui.Text(c, i18n.T("Sync bookshelf, reading progress and offline cache between two devices; both sides enable it and enter each other's address and token.")).
		FontSize(12).TextColor(t.TextMuted)

	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		label := i18n.T("Not enabled")
		if s.form.enabled {
			label = i18n.T("✓ Enabled")
		}
		if ui.Button(c, label).Disabled(s.saveBusy).Label(i18n.T("Toggle sync")).Clicked() {
			s.form.enabled = !s.form.enabled
		}
		ui.Text(c, syncStatusLine(s.status)).FontSize(12).TextColor(t.TextMuted)
		if s.status.Pending > 0 {
			ui.Textf(c, i18n.T("%d changes waiting"), s.status.Pending).FontSize(12).TextColor(t.TextMuted)
		}
		if s.statusBusy {
			inlineLoading(c, i18n.T("Checking sync status…"))
		}
	})

	ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.TextInput(c, &s.form.serverURL).Label(i18n.T("Peer address")).
				Placeholder("http://192.168.1.20:8080")
		})
		ui.Column(c).Gap(2).Children(func() {
			ui.TextInput(c, &s.form.token).Width(220).Label(i18n.T("Sync token")).
				Placeholder(i18n.T("Use the same token on both sides"))
		})
		if ui.PrimaryButton(c, i18n.T("Save sync settings")).Disabled(s.saveBusy).Clicked() {
			a.saveSyncSettings()
		}
		if s.saveBusy {
			inlineLoading(c, i18n.T("Saving sync settings…"))
		}
	})
	if a.backend != nil {
		label := i18n.T("This device does not accept sync")
		if s.accepting {
			label = i18n.T("✓ This device accepts sync")
		}
		if ui.Button(c, label).Disabled(s.saveBusy).Label(i18n.T("Toggle accepting sync on this device")).Clicked() {
			s.accepting = !s.accepting
		}
		ui.Text(c, i18n.T("The other device must be able to connect: enable this too, with the same token.")).
			FontSize(11).TextColor(t.TextMuted)
	}
	ui.Text(c, i18n.T("Both sides must use the same token; empty clears it, and ******** keeps the stored one.")).
		FontSize(11).TextColor(t.TextMuted)

	switch {
	case s.err != "":
		ui.Text(c, s.err).FontSize(12).TextColor(t.Danger)
	case s.msg != "":
		ui.Text(c, s.msg).FontSize(12).TextColor(t.TextMuted)
	}
	if s.status.LastError != "" && s.status.Enabled {
		ui.Textf(c, i18n.T("Last sync error: %s"), s.status.LastError).FontSize(12).TextColor(t.Danger)
	}
	if s.status.LastSyncAt != nil {
		ui.Textf(c, i18n.T("Last synced %s"), s.status.LastSyncAt.Local().Format("2006-01-02 15:04")).
			FontSize(11).TextColor(t.TextMuted)
	}
	if !s.loaded && s.loading {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			inlineLoading(c, i18n.T("Reading sync configuration…"))
			ui.Text(c, i18n.T("Reading sync configuration…")).FontSize(12).TextColor(t.TextMuted)
		})
	}
}

// languageRow is the interface language choice. The UI is translated in
// Chinese only, so the row says so and the choice is stored with the rest of
// config.json rather than offered as a control that does nothing.
func (a *App) languageRow(c *ui.Context) {
	t := c.Theme()
	if a.language == "" {
		// The saved choice is config.json's; until it loads, the row marks
		// the language actually shown so the tick never lies.
		a.language = savedLanguageFor(i18n.Default.Language())
	}
	ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
		ui.Text(c, i18n.T("Interface language")).Grow(1)
		names := languageOptionNames()
		for i, opt := range languageOptions {
			label := names[i]
			if opt.saved == a.language {
				label = "✓ " + label
			}
			// Clicking records the choice whether or not it is the one in
			// use, so config.json says what the UI is shown in instead of
			// leaving the default implicit.
			if ui.Button(c, label).Disabled(a.backend == nil).Clicked() {
				a.saveLanguage(opt.saved)
			}
		}
	})
	ui.Text(c, i18n.T("The interface is in the chosen language; dates, numbers and fonts follow the system region. The choice is written to the config file.")).
		FontSize(11).TextColor(t.TextMuted)
	if a.languageMsg != "" {
		ui.Text(c, a.languageMsg).FontSize(11).TextColor(t.TextMuted)
	}
}

// saveLanguage persists the interface language and shows it at once: the
// localizer switches the strings, and mygo re-draws every window when the
// app's locale changes.
func (a *App) saveLanguage(tag string) {
	if a.backend == nil {
		return
	}
	locale := language.Chinese
	if tag == "en" {
		locale = language.English
	}
	i18n.SetLanguage(locale)
	// mygo's own texts (menus, dialogs) follow the app's locale too; a tag
	// it cannot match falls back to the system's language.
	if a.win != nil {
		mygo.App.SetLocale(locale.String())
	}
	backend := a.backend
	go func() {
		err := backend.SetLanguage(tag)
		a.update(func() {
			if err != nil {
				a.languageMsg = err.Error()
				return
			}
			if saved := backend.Language(); saved != "" {
				a.language = saved
			} else {
				a.language = tag
			}
			a.languageMsg = i18n.T("Saved")
		})
	}()
}

// configFileRow says what config.json holds: the preferences set on this page
// live there, and the cookie is written there (as plain text, 0600) rather
// than in some other store.
func (a *App) configFileRow(c *ui.Context) {
	t := c.Theme()
	path := a.storage.ConfigPath
	if a.backend != nil {
		if p := a.backend.SettingsPath(); p != "" {
			path = p
		}
	}
	if path == "" {
		ui.Text(c, i18n.T("Config file location unknown")).FontSize(11).TextColor(t.Danger)
		return
	}
	st, err := os.Stat(path)
	switch {
	case err != nil && errors.Is(err, fs.ErrNotExist):
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Text(c, i18n.T("Config file contents")).Grow(1)
			ui.Badge(c, i18n.T("Not written yet"))
		})
		ui.Text(c, i18n.T("Setting cookies or changing preferences writes ")+path+i18n.T(" (mode 0600).")).
			FontSize(11).TextColor(t.TextMuted)
		return
	case err != nil:
		ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
			ui.Text(c, i18n.T("Config file contents")).Grow(1)
			ui.Badge(c, i18n.T("Cannot read"))
		})
		ui.Text(c, err.Error()).FontSize(11).TextColor(t.Danger)
		return
	}
	// What the file itself says comes first, so the badges above (which
	// follow what the app is doing) and this line never disagree silently.
	var raw diskSettings
	if data, err := os.ReadFile(path); err == nil {
		if json.Unmarshal(data, &raw) != nil {
			ui.Text(c, i18n.T("The config file is not valid JSON; saving settings will overwrite it.")).
				FontSize(11).TextColor(t.Danger)
		}
	}
	var parts []string
	add := func(label, value string) {
		if value != "" {
			parts = append(parts, label+"："+value)
		}
	}
	switch {
	case raw.Appearance != "":
		add(i18n.T("Theme"), appearanceLabel(raw.Appearance))
	case a.appearance != "":
		add(i18n.T("Theme"), appearanceLabel(a.appearance))
	}
	switch {
	case raw.Language != "":
		add(i18n.T("Language"), languageLabel(raw.Language))
	case a.language != "":
		add(i18n.T("Language"), languageLabel(a.language))
	}
	if raw.Cookie != "" {
		add("Cookie", cookieLabel(true))
	}
	if raw.CacheMaxBytes != nil {
		add(i18n.T("Cache limit"), cacheLimitText(*raw.CacheMaxBytes))
	} else if a.cacheMax > 0 {
		add(i18n.T("Cache limit"), cacheLimitText(a.cacheMax))
	}
	if raw.DatabasePath != "" {
		add(i18n.T("Database"), shortenPath(raw.DatabasePath))
	}
	if raw.CacheDir != "" {
		add(i18n.T("Cache directory"), shortenPath(raw.CacheDir))
	}
	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Text(c, i18n.T("Config file contents")).Grow(1)
		ui.Badge(c, i18n.T("Written"))
	})
	if len(parts) == 0 {
		ui.Text(c, i18n.T("The file exists but holds no preferences to show.")).FontSize(11).TextColor(t.TextMuted)
	} else {
		ui.Text(c, strings.Join(parts, " · ")).FontSize(11).TextColor(t.TextMuted)
	}
	ui.Text(c, i18n.T("Last written ")+st.ModTime().Format("2006-01-02 15:04")+i18n.T("; the cookie is stored in plain text (mode 0600).")).
		FontSize(11).TextColor(t.TextMuted)
	_ = st
}

// diskSettings is the part of config.json the settings page describes. It
// mirrors the fields desktop.Settings writes; the page never reads the cookie
// itself.
type diskSettings struct {
	Cookie        string `json:"cookie"`
	DatabasePath  string `json:"database_path"`
	CacheDir      string `json:"cache_dir"`
	CacheMaxBytes *int64 `json:"cache_max_bytes"`
	Appearance    string `json:"appearance"`
	Language      string `json:"language"`
}

// shortenPath keeps the last two path components, enough to recognise a
// path without repeating the storage rows above in full.
func shortenPath(path string) string {
	base := filepath.Base(path)
	dir := filepath.Base(filepath.Dir(path))
	if dir == "." || dir == string(filepath.Separator) || dir == "" {
		return base
	}
	return dir + "/" + base
}

func appearanceLabel(source string) string {
	switch source {
	case "light":
		return i18n.T("Light")
	case "dark":
		return i18n.T("Dark")
	default:
		return i18n.T("Follow system")
	}
}

func languageLabel(tag string) string {
	names := languageOptionNames()
	for i, opt := range languageOptions {
		if opt.saved == tag {
			return names[i]
		}
	}
	return tag
}

// cookieLabel describes the cookie without printing any of it.
func cookieLabel(configured bool) string {
	if configured {
		return i18n.T("Configured")
	}
	return i18n.T("Not configured")
}

// requestFrame asks the window for one more frame, for state changed outside
// update. Without a window (tests) the next Frame covers it.
func (a *App) requestFrame() {
	if a.win != nil {
		a.win.Update(func() {})
	}
}

// --- Data sync settings ---

// ensureSyncConfig loads the sync configuration and engine state once per
// visit to the settings page.
func (a *App) ensureSyncConfig() {
	s := &a.sync
	if s.loading || (s.loaded && s.statusLoaded) {
		return
	}
	if s.loading {
		return
	}
	s.loading = true
	s.err = ""
	client := a.client
	go func() {
		cfg, cfgErr := client.GetSyncConfig(context.Background())
		var st SyncStatus
		stErr := error(nil)
		if cfgErr == nil {
			st, stErr = client.SyncStatus(context.Background())
		}
		a.update(func() {
			s.loading = false
			if cfgErr != nil {
				s.err = cfgErr.Error()
				return
			}
			s.config = cfg
			s.loaded = true
			if !s.form.tokenInit {
				s.form.enabled = cfg.Enabled
				s.form.serverURL = cfg.ServerURL
				// The stored token arrives masked; keep the field showing
				// that, and save the mask back untouched unless edited.
				s.form.token = cfg.Token
				s.form.tokenInit = true
			}
			if stErr == nil {
				s.status = st
				s.statusLoaded = true
			}
			if !s.acceptingInit {
				s.accepting = a.backend != nil && a.backend.SyncToken() != ""
				s.acceptingInit = true
			}
		})
	}()
}

// saveSyncSettings applies the form both ways: the engine's client config
// through the API, and this device's willingness to accept peers through
// the backend (which also persists the token). An empty token clears it;
// the mask keeps the stored one.
func (a *App) saveSyncSettings() {
	s := &a.sync
	if s.saveBusy {
		return
	}
	s.saveBusy = true
	s.msg = ""
	s.err = ""

	enabled := s.form.enabled
	serverURL := strings.TrimSpace(s.form.serverURL)
	token := strings.TrimSpace(s.form.token)
	var tokenPtr *string
	if token != SyncTokenMask {
		tokenPtr = &token
	}
	accepting := s.accepting
	backend := a.backend
	client := a.client
	go func() {
		var cfg SyncConfig
		var err error
		cfg, err = client.SaveSyncConfig(context.Background(), &enabled, &serverURL, tokenPtr)
		if err == nil && backend != nil {
			if accepting {
				// The peer token is its own secret; reuse the engine's
				// token when the user has not set one for this device.
				err = backend.SetSyncToken(token)
			} else {
				err = backend.SetSyncToken("")
			}
		}
		a.update(func() {
			s.saveBusy = false
			if err != nil {
				s.err = err.Error()
				return
			}
			s.config = cfg
			s.form.enabled = cfg.Enabled
			s.form.serverURL = cfg.ServerURL
			s.form.token = cfg.Token
			s.msg = i18n.T("Saved; the sync engine restarted with the new settings")
			// The engine restarts with the new config; refresh its state.
			s.statusLoaded = false
			a.ensureSyncConfig()
		})
	}()
}

// SyncTokenMask is what the backend sends instead of the stored token, and
// what keeps the stored token unchanged when saved back.
const SyncTokenMask = "********"

// syncStatusLine is what the section says about the engine right now.
func syncStatusLine(st SyncStatus) string {
	switch {
	case !st.Enabled:
		return i18n.T("Not enabled")
	case !st.Configured:
		return i18n.T("Missing server address or token")
	case st.SSEConnected:
		return i18n.T("Connected")
	default:
		return i18n.T("Disconnected")
	}
}
