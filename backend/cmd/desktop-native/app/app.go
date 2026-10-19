package app

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"

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
	pageCache  *PageCache

	lists   map[string]*listState
	details map[int64]*detailState

	searchQuery string
	reader      Reader
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
}

func New(client *Client) *App {
	a := &App{
		router:     ui.NewRouter("/"),
		selected:   "/",
		lastList:   "/",
		client:     client,
		covers:     NewCoverCache(),
		images:     NewImageStore(128),
		pageImages: NewImageStore(6),
		pageCache:  NewPageCache(4),
		lists:      make(map[string]*listState),
		details:    make(map[int64]*detailState),
	}
	a.images.SetAuth(client.user, client.password)
	a.pageImages.SetAuth(client.user, client.password)
	return a
}

// Attach gives the app the window, which background loads use to redraw.
func (a *App) Attach(w *mygo.Window) {
	a.win = w
	a.images.SetWindow(w)
	a.pageImages.SetWindow(w)
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

// View builds the whole window.
func (a *App) View(c *ui.Context) {
	a.mu.Lock()
	defer a.mu.Unlock()

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
		a.emptyPage(c, "下载管理", "还没有离线下载任务", "从画廊详情页开始预填充")
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
	a.resetList(s, a.searchQuery)
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

					ui.Row(c).Gap(8).Children(func() {
						label := "加入书架"
						if s.inBookshelf {
							label = "移出书架"
						}
						if ui.Button(c, label).Disabled(s.busy).Clicked() {
							a.toggleBookshelf(s)
						}
						if ui.Button(c, "页面缩略图").Clicked() {
							c.Toast("页面缩略图网格稍后实现")
						}
					})
					if !s.shelfKnown {
						ui.Text(c, "书架状态暂不可用").FontSize(11).TextColor(t.TextMuted)
					}
				})
			})

			ui.Divider(c)
			ui.Text(c, "页面缩略图").FontSize(16).Bold()
			ui.Text(c, "页面缩略图会在这里按分页网格显示。").FontSize(12).TextColor(t.TextMuted)
		})
	})
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

// --- Static pages ---

func (a *App) settingsPage(c *ui.Context) {
	t := c.Theme()
	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(20, 24).Gap(18).MaxWidth(760).Children(func() {
			ui.Text(c, "设置").FontSize(26).Bold()
			ui.Text(c, "这里最终会接上现有的设置、同步与缓存选项。").FontSize(12).TextColor(t.TextMuted)

			ui.Text(c, "外观").FontSize(16).Bold()
			dark := false
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Switch(c, &dark)
				ui.Text(c, "深色模式（占位，待接入系统主题）")
			})

			ui.Divider(c)
			ui.Text(c, "数据").FontSize(16).Bold()
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, "后端地址").Grow(1)
				ui.Badge(c, a.client.BaseURL())
			})
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, "本地数据库").Grow(1)
				ui.Badge(c, "SQLite")
			})
			ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
				ui.Text(c, "图片缓存").Grow(1)
				ui.Badge(c, "内存 LRU")
			})
		})
	})
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
