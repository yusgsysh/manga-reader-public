package app

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
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

	// backend is the in-process API lifecycle; nil in tests and when the app
	// talks to an external server.
	backend Backend
	storage BackendStorage

	// setup state: the first-run cookie screen.
	showSetup   bool
	setupCookie string
	setupErr    string
	setupBusy   bool

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
	// readerStart is the page the next reader open should show, set when a
	// detail-page thumbnail is clicked; -1 means "resume saved progress".
	readerStart int
	// readerJump is the page-number field under the reader.
	readerJump string
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

// listState is the scroll, selection and page data of one gallery list.
type listState struct {
	kind     listKind
	items    []Gallery
	grid     *ui.GridState
	selected int

	page     int
	pageSize int
	lastLen  int
	total    int
	loading  bool
	loaded   bool
	err      string
	query    string
	loadID   int
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

	inBookshelf bool
	shelfKnown  bool
	busy        bool

	// thumbGrid scrolls the detail page's page-thumbnail grid; thumbSel is
	// the chosen item.
	thumbGrid *ui.GridState
	thumbSel  int

	// offline download started from this page.
	downloadBusy bool
	downloadErr  string
	downloadJob  string
}

func New(client *Client) *App {
	a := &App{
		router:      ui.NewRouter("/"),
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
		readerStart: -1,
	}
	a.images.SetAuth(client.user, client.password)
	a.pageImages.SetAuth(client.user, client.password)
	a.pageThumbs.SetAuth(client.user, client.password)
	return a
}

// Attach gives the app the window, which background loads use to redraw.
func (a *App) Attach(w *mygo.Window) {
	a.win = w
	a.images.SetWindow(w)
	a.pageImages.SetWindow(w)
	a.pageThumbs.SetWindow(w)
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
	a.cacheLoaded = false
	a.cacheBytes = 0
	a.cacheMax = 0
	a.cacheMaxInit = false
	a.cacheErr = ""
}

// update runs fn on the main thread with the app lock held. Background loads
// use it so they never race the frame being built.
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

// View builds the whole window.
func (a *App) View(c *ui.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

	if a.backend != nil && a.showSetup {
		a.setupView(c)
		return
	}

	t := c.Theme()
	a.syncSelection()
	ui.Row(c).Fill().AlignItems(ui.Stretch).Background(t.Background).Children(func() {
		a.sidebar(c, t)
		ui.Column(c).Grow(1).MinWidth(0).Children(func() {
			a.router.View(c, func(r *ui.Route) { a.route(c, r) })
		})
	})
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
			ui.Text(c, "原生").FontSize(10).TextColor(t.TextMuted)
		})
		ui.Sidebar(c, &nav, func() {
			ui.SidebarSection(c, "浏览", nil, func() {
				ui.SidebarItem(c, "/", nil, "首页")
				ui.SidebarItem(c, "/watched", nil, "订阅")
				ui.SidebarItem(c, "/popular", nil, "热门")
				ui.SidebarItem(c, "/search", nil, "搜索")
			})
			ui.SidebarSection(c, "我的", nil, func() {
				ui.SidebarItem(c, "/bookshelf", nil, "书架")
				ui.SidebarItem(c, "/recently-read", nil, "最近阅读")
				ui.SidebarItem(c, "/downloads", nil, "下载管理")
			})
			ui.SidebarSection(c, "管理", nil, func() {
				ui.SidebarItem(c, "/tag-settings", nil, "标签设置")
				ui.SidebarItem(c, "/settings", nil, "设置")
			})
		}).Grow(1).OnChange(func() {
			a.router.Push(nav)
		})
	})
}

func (a *App) route(c *ui.Context, r *ui.Route) {
	switch {
	case r.Match("/"):
		r.Title("首页")
		a.listPage(c, "/", "首页", "最新画廊", kindHome)
	case r.Match("/watched"):
		r.Title("订阅")
		a.listPage(c, "/watched", "订阅", "已关注的画廊", kindWatched)
	case r.Match("/popular"):
		r.Title("热门")
		a.listPage(c, "/popular", "热门", "按热度排序", kindPopular)
	case r.Match("/search"):
		r.Title("搜索")
		a.searchPage(c)
	case r.Match("/bookshelf"):
		r.Title("书架")
		a.listPage(c, "/bookshelf", "书架", "收藏的画廊", kindBookshelf)
	case r.Match("/recently-read"):
		r.Title("最近阅读")
		a.listPage(c, "/recently-read", "最近阅读", "继续上次的进度", kindRecent)
	case r.Match("/downloads"):
		r.Title("下载管理")
		a.downloadsPage(c)
	case r.Match("/tag-settings"):
		r.Title("标签设置")
		a.emptyPage(c, "标签设置", "标签翻译与偏好", "接入后端后在这里管理")
	case r.Match("/settings"):
		r.Title("设置")
		a.settingsPage(c)
	case r.Match("/gallery/{id}/{token}"):
		r.Title("画廊详情")
		id, _ := strconv.ParseInt(r.Param("id"), 10, 64)
		a.galleryPage(c, id, r.Param("token"))
	case r.Match("/reader/{id}/{token}"):
		r.Title("阅读器")
		id, _ := strconv.ParseInt(r.Param("id"), 10, 64)
		a.readerPage(c, id, r.Param("token"))
	default:
		r.Title("未找到")
		a.emptyPage(c, "未找到", "这个页面不存在", "")
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
				s.err = err.Error()
				return
			}
			if page == 0 {
				s.items = items
			} else {
				s.items = append(s.items, items...)
			}
			s.page = page
			s.total = total
			s.pageSize = pageSize
			s.lastLen = len(items)
			s.loaded = true
		})
	}()
}

func (a *App) reloadList(s *listState) {
	s.loadID++
	s.items = nil
	s.page = 0
	s.total = 0
	s.lastLen = 0
	s.loaded = false
	s.loading = false
	s.err = ""
	a.startListLoad(s)
}

func (a *App) resetList(s *listState, query string) {
	if s.query == query && (s.loaded || s.loading) {
		return
	}
	s.query = query
	a.reloadList(s)
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
			a.resetList(s, a.searchApplied)
		})
	})
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
		return nil, 0, 0, fmt.Errorf("未知的列表类型")
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
		out = append(out, Gallery{
			ID:       it.ID,
			Token:    it.Token,
			Title:    it.Title,
			TitleJPN: it.TitleJPN,
			Category: categoryLabel(it.Category),
			Pages:    pages,
			Rating:   it.Rating,
			Tags:     it.Tags,
			CoverURL: a.client.ThumbnailURL(cover),
		})
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
			}
		}
		out = append(out, g)
	}
	return out
}

func listSummary(subtitle string, s *listState) string {
	switch {
	case s.total > 0:
		return fmt.Sprintf("%s · 共 %d 个结果", subtitle, s.total)
	case len(s.items) > 0:
		return fmt.Sprintf("%s · 已加载 %d 个", subtitle, len(s.items))
	default:
		return subtitle
	}
}

func (a *App) listPage(c *ui.Context, path, title, subtitle string, kind listKind) {
	t := c.Theme()
	s := a.listFor(path, kind)
	a.ensureList(s)

	ui.Column(c).Fill().Padding(20, 24).Gap(14).Children(func() {
		ui.Row(c).AlignItems(ui.End).Gap(12).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, title).FontSize(26).Bold()
				ui.Text(c, listSummary(subtitle, s)).FontSize(12).TextColor(t.TextMuted)
			})
			if ui.Button(c, "刷新").Clicked() {
				a.reloadList(s)
			}
		})

		switch {
		case s.loading && len(s.items) == 0:
			ui.Column(c).Fill().Center().Gap(10).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在加载…").FontSize(12).TextColor(t.TextMuted)
			})
		case s.err != "" && len(s.items) == 0:
			a.errorBlock(c, s.err, func() { a.reloadList(s) })
		case s.loaded && len(s.items) == 0:
			a.emptyPage(c, "没有结果", "换个关键词或稍后重试", "")
		default:
			ui.GridView(c, s.grid, len(s.items), 190, 300, func(i int) {
				a.galleryCard(c, s.items[i])
			}).Grow(1).Clip()
			if s.loading || a.hasMore(s) {
				ui.Row(c).Center().Gap(8).Children(func() {
					if s.loading {
						ui.Spinner(c)
					} else if ui.Button(c, "加载更多").Clicked() {
						a.loadMore(s)
					}
				})
			}
		}
	})
}

func (a *App) galleryCard(c *ui.Context, g Gallery) {
	t := c.Theme()
	cover := a.images.Get(g.CoverURL)
	if cover == nil {
		cover = a.covers.Get(int(g.ID))
	}

	cell := ui.Column(c).Gap(6).Focusable()
	cell.Children(func() {
		ui.Image(c, cover).AspectRatio(3.0 / 4.0).Fit(ui.Cover).Radius(10)
		ui.Text(c, displayTitle(g)).FontSize(13).SingleLine()
		meta := fmt.Sprintf("%s · %d 页", g.Category, g.Pages)
		if g.Reading != nil && g.Reading.CurrentPage > 0 {
			meta = fmt.Sprintf("%s · 读到第 %d 页", g.Category, g.Reading.CurrentPage+1)
		}
		ui.Text(c, meta).FontSize(11).TextColor(t.TextMuted).SingleLine()
	})
	if cell.Clicked() {
		a.router.Push(fmt.Sprintf("/gallery/%d/%s", g.ID, g.Token))
	}
}

func displayTitle(g Gallery) string {
	if g.Title != "" {
		return g.Title
	}
	if g.TitleJPN != "" {
		return g.TitleJPN
	}
	return fmt.Sprintf("画廊 %d", g.ID)
}

func (a *App) searchPage(c *ui.Context) {
	t := c.Theme()
	s := a.listFor("/search", kindSearch)
	a.scheduleSearch(s)
	a.ensureList(s)

	ui.Column(c).Fill().Padding(20, 24).Gap(14).Children(func() {
		ui.Text(c, "搜索").FontSize(26).Bold()
		ui.TextInput(c, &a.searchQuery).Width(480).Label("搜索框").
			Placeholder("搜索标题、分类或标签…").AutoFocus()
		ui.Text(c, listSummary("搜索结果", s)).FontSize(12).TextColor(t.TextMuted)

		switch {
		case s.loading && len(s.items) == 0:
			ui.Column(c).Fill().Center().Gap(10).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在搜索…").FontSize(12).TextColor(t.TextMuted)
			})
		case s.err != "" && len(s.items) == 0:
			a.errorBlock(c, s.err, func() { a.reloadList(s) })
		case s.loaded && len(s.items) == 0:
			a.emptyPage(c, "没有结果", "换个关键词试试", "")
		default:
			ui.GridView(c, s.grid, len(s.items), 190, 300, func(i int) {
				a.galleryCard(c, s.items[i])
			}).Grow(1).Clip()
			if s.loading || a.hasMore(s) {
				ui.Row(c).Center().Gap(8).Children(func() {
					if s.loading {
						ui.Spinner(c)
					} else if ui.Button(c, "加载更多").Clicked() {
						a.loadMore(s)
					}
				})
			}
		}
	})
}

// --- Gallery detail ---

func (a *App) ensureDetail(id int64, token string) *detailState {
	s := a.details[id]
	if s == nil {
		s = &detailState{id: id, token: token}
		a.details[id] = s
	}
	if s.loaded || s.loading {
		return s
	}
	a.startDetailLoad(s)
	return s
}

func (a *App) startDetailLoad(s *detailState) {
	s.loading = true
	s.err = ""
	s.loadID++
	lid := s.loadID
	id, token := s.id, s.token

	go func() {
		ctx := context.Background()
		d, err := a.client.Gallery(ctx, id, token)
		if err != nil {
			d, err = a.client.CachedGallery(ctx, id, token)
		}
		inShelf, shelfErr := a.client.BookshelfStatus(ctx, id, token)
		a.update(func() {
			if s.loadID != lid {
				return
			}
			s.loading = false
			if err != nil {
				s.err = err.Error()
				return
			}
			s.g = a.galleryFromDetail(d)
			s.tags = d.Tags
			s.uploader = d.Uploader
			s.posted = d.PostedAt
			s.size = d.FileSize
			s.loaded = true
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
	a.startDetailLoad(s)
}

func (a *App) galleryFromDetail(d apiGalleryDetail) Gallery {
	cover := d.Thumbnail
	if cover == "" {
		cover = d.Cover
	}
	tags := make([]string, 0, len(d.Tags))
	for _, t := range d.Tags {
		tags = append(tags, t.Namespace+":"+t.Name)
	}
	return Gallery{
		ID:       d.ID,
		Token:    d.Token,
		Title:    d.Title,
		TitleJPN: d.TitleJPN,
		Category: categoryLabel(d.Category),
		Pages:    d.PageCount,
		Rating:   d.Rating,
		Tags:     tags,
		CoverURL: a.client.ThumbnailURL(cover),
	}
}

func (a *App) galleryPage(c *ui.Context, id int64, token string) {
	s := a.ensureDetail(id, token)
	t := c.Theme()

	if s.loading && !s.loaded {
		ui.Column(c).Fill().Center().Gap(10).Children(func() {
			ui.Spinner(c)
			ui.Text(c, "正在加载画廊…").FontSize(12).TextColor(t.TextMuted)
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
				if ui.Button(c, "← 返回").Clicked() {
					a.router.Back()
				}
				ui.Text(c, displayTitle(g)).FontSize(22).Bold().Grow(1)
				if ui.PrimaryButton(c, "开始阅读").Clicked() {
					a.readerStart = -1
					a.router.Push(fmt.Sprintf("/reader/%d/%s", g.ID, g.Token))
				}
			})

			ui.Row(c).Gap(20).AlignItems(ui.Start).Children(func() {
				ui.Image(c, cover).Size(240, 320).Fit(ui.Cover).Radius(12)
				ui.Column(c).Gap(10).Grow(1).Children(func() {
					if g.TitleJPN != "" && g.TitleJPN != g.Title {
						ui.Text(c, g.TitleJPN).FontSize(13).TextColor(t.TextMuted)
					}
					ui.Textf(c, "分类：%s", g.Category).FontSize(13)
					ui.Textf(c, "页数：%d", g.Pages).FontSize(13)
					ui.Textf(c, "评分：%.1f", g.Rating).FontSize(13)
					if s.uploader != "" {
						ui.Textf(c, "上传者：%s", s.uploader).FontSize(13)
					}
					if s.posted != "" {
						ui.Textf(c, "发布：%s", s.posted).FontSize(13)
					}
					if s.size != "" {
						ui.Textf(c, "大小：%s", s.size).FontSize(13)
					}

					ui.Text(c, "标签").FontSize(13).TextColor(t.TextMuted)
					ui.Row(c).Gap(6).Wrap().Children(func() {
						for _, tag := range s.tags {
							ui.Badge(c, tag.Namespace+":"+tag.Name)
						}
					})

					ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
						label := "加入书架"
						if s.inBookshelf {
							label = "移出书架"
						}
						if ui.Button(c, label).Disabled(s.busy).Clicked() {
							a.toggleBookshelf(s)
						}
					})
					a.detailDownloadsBlock(c, s)
					if !s.shelfKnown {
						ui.Text(c, "书架状态暂不可用").FontSize(11).TextColor(t.TextMuted)
					}
				})
			})

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
		ui.Text(c, "页面缩略图").FontSize(16).Bold()
		if g.Pages > 0 {
			ui.Textf(c, "共 %d 页 · 点击跳转", g.Pages).FontSize(12).TextColor(t.TextMuted)
		}
	})
	if g.Pages <= 0 {
		ui.Text(c, "页面数量未知，打开阅读器后可见。").FontSize(12).TextColor(t.TextMuted)
		return
	}

	if s.thumbGrid == nil {
		s.thumbSel = -1
		s.thumbGrid = &ui.GridState{
			Selected: &s.thumbSel,
			Key:      func(i int) any { return i },
			Label:    func(i int) string { return fmt.Sprintf("第 %d 页", i+1) },
		}
	}
	ui.GridView(c, s.thumbGrid, g.Pages, 120, 190, func(i int) {
		cell := ui.Column(c).Gap(4).Focusable().Label(fmt.Sprintf("第 %d 页", i+1))
		cell.Children(func() {
			ui.Image(c, a.pageThumbs.Get(a.client.PageThumbnailURL(g.ID, g.Token, i))).
				AspectRatio(200.0 / 282.0).Fit(ui.Cover).Radius(8)
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
			ui.Text(c, "首次设置").FontSize(26).Bold()
			ui.Text(c, "粘贴 ExHentai 的 Cookie，应用会在本机启动内置服务并保存设置。").
				FontSize(13).TextColor(t.TextMuted)
			ui.TextArea(c, &a.setupCookie).
				Label("ExHentai Cookies").
				Placeholder("ipb_member_id=...; ipb_pass_hash=...; igneous=...; sk=...").
				Height(90).
				AutoFocus()
			ui.Text(c, "在浏览器登录 exhentai.org 后，从开发者工具的请求头里复制 Cookie 即可。").
				FontSize(11).TextColor(t.TextMuted).MaxWidth(560)
			if a.setupErr != "" {
				ui.Text(c, a.setupErr).FontSize(12).TextColor(t.Danger).MaxWidth(560)
			}
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				if ui.PrimaryButton(c, "保存并启动").Disabled(a.setupBusy).Clicked() {
					a.beginSetup()
				}
				if a.setupBusy {
					ui.Spinner(c)
					ui.Text(c, "正在启动内置服务…").FontSize(12).TextColor(t.TextMuted)
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
		a.setupErr = "请粘贴 ExHentai Cookies"
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
			ui.Text(c, "设置").FontSize(26).Bold()

			if a.backend != nil {
				ui.Text(c, "账号").FontSize(16).Bold()
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, "ExHentai Cookies").Grow(1)
					ui.Badge(c, a.cookieStatus())
				})
				ui.Text(c, "更改 Cookies 会重启内置服务。").FontSize(11).TextColor(t.TextMuted)
				ui.Row(c).Children(func() {
					if ui.Button(c, "重新配置 Cookies").Clicked() {
						a.setupCookie = ""
						a.setupErr = ""
						a.showSetup = true
					}
				})
				ui.Divider(c)
			}

			ui.Text(c, "存储").FontSize(16).Bold()
			a.settingsRow(c, "后端地址", a.client.BaseURL())
			a.settingsRow(c, "配置文件", a.storage.ConfigPath)
			a.settingsRow(c, "本地数据库", a.storage.DBPath)
			a.settingsRow(c, "图片缓存目录", a.storage.CacheDir)
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, "缓存占用").Grow(1)
				switch {
				case a.cacheErr != "":
					ui.Badge(c, "读取失败")
				case a.cacheLoading && !a.cacheLoaded:
					ui.Badge(c, "统计中…")
				default:
					ui.Badge(c, humanBytes(a.cacheBytes)+" / "+cacheLimitText(a.cacheMax))
				}
			})
			if a.cacheErr != "" {
				ui.Text(c, a.cacheErr).FontSize(11).TextColor(t.Danger)
			}
			if a.backend != nil {
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					ui.Text(c, "缓存上限 (MB)").Grow(1)
					ui.TextInput(c, &a.cacheMaxInput).Width(160)
					if ui.Button(c, "保存上限").Disabled(a.cacheMaxBusy).Clicked() {
						a.saveCacheLimit()
					}
					if a.cacheMaxBusy {
						ui.Spinner(c)
					}
				})
				if a.cacheMaxErr != "" {
					ui.Text(c, a.cacheMaxErr).FontSize(11).TextColor(t.Danger)
				}
				ui.Text(c, "填 0 表示不限制；超过上限时会自动删除最久未使用的图片。").
					FontSize(11).TextColor(t.TextMuted)
				ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
					if ui.Button(c, "清空缓存").Disabled(a.cacheBusy).Clicked() {
						a.clearCache()
					}
					if a.cacheBusy {
						ui.Spinner(c)
					}
				})
			}
			ui.Text(c, "清空后，已缓存的图片会在再次浏览时重新下载。").
				FontSize(11).TextColor(t.TextMuted)

			ui.Divider(c)
			ui.Text(c, "外观").FontSize(16).Bold()
			dark := false
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Switch(c, &dark)
				ui.Text(c, "深色模式（当前跟随系统）").FontSize(13)
			})
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
		return "已配置"
	}
	return "未配置"
}

// ensureCacheInfo kicks off a background size computation once per load.
func (a *App) ensureCacheInfo() {
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
			a.cacheMaxErr = "请输入不小于 0 的整数（MB）"
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
		return "不限"
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

func (a *App) emptyPage(c *ui.Context, title, subtitle, hint string) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(8).Children(func() {
		ui.Text(c, title).FontSize(22).Bold()
		ui.Text(c, subtitle).FontSize(13).TextColor(t.TextMuted)
		if hint != "" {
			ui.Text(c, hint).FontSize(12).TextColor(t.TextMuted)
		}
	})
}

func (a *App) errorBlock(c *ui.Context, msg string, retry func()) {
	t := c.Theme()
	ui.Column(c).Fill().Center().Gap(10).Padding(24).Children(func() {
		ui.Text(c, "请求失败").FontSize(18).Bold()
		ui.Text(c, msg).FontSize(12).TextColor(t.TextMuted).MaxWidth(560)
		if ui.Button(c, "重试").Clicked() {
			retry()
		}
	})
}
