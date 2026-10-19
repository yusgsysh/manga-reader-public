package app

import (
	"context"
	"fmt"

	"github.com/egoist/mygo/ui"
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

	zoom  float32
	panX  float32
	panY  float32
	drag  bool
	lastX float32
	lastY float32

	lastSaved int
}

func (r *Reader) resetView() {
	r.zoom = 1
	r.panX, r.panY = 0, 0
	r.drag = false
}

func (r *Reader) zoomBy(d float32) {
	r.zoom = clampFloat(r.zoom+d, 0.5, 6)
}

func (a *App) ensureReader(id int64, token string) {
	r := &a.reader
	if r.id == id && r.token == token && (r.loading || r.loaded) {
		return
	}
	r.id, r.token = id, token
	r.page = 0
	r.pages = nil
	r.total = 0
	r.loaded = false
	r.err = ""
	r.lastSaved = -1
	r.resetView()
	r.title = "阅读器"
	if d := a.details[id]; d != nil && d.loaded && d.g.Title != "" {
		r.title = d.g.Title
	}
	r.loadID++
	a.startReaderLoad(r)
}

func (a *App) startReaderLoad(r *Reader) {
	r.loading = true
	r.err = ""
	lid := r.loadID
	id, token := r.id, r.token

	go func() {
		ctx := context.Background()
		var progress apiReading
		if p, err := a.client.Progress(ctx, id, token); err == nil {
			progress = p
		}
		total, urls, err := a.client.Pages(ctx, id, token)
		if err != nil {
			total, urls, err = a.client.CachedPages(ctx, id, token)
		}
		a.update(func() {
			if r.loadID != lid {
				return
			}
			r.loading = false
			if err != nil {
				r.err = err.Error()
				return
			}
			r.pages = urls
			r.total = total
			r.loaded = true
			if progress.CurrentPage > 0 && progress.CurrentPage < len(urls) {
				r.page = progress.CurrentPage
			}
		})
	}()
}

func (a *App) retryReader(r *Reader) {
	r.loading = false
	r.loaded = false
	r.err = ""
	r.loadID++
	a.startReaderLoad(r)
}

func (a *App) readerNext(r *Reader) {
	if !r.loaded || r.page+1 >= len(r.pages) {
		return
	}
	r.page++
	r.resetView()
	a.saveProgress(r)
}

func (a *App) readerPrev(r *Reader) {
	if !r.loaded || r.page == 0 {
		return
	}
	r.page--
	r.resetView()
	a.saveProgress(r)
}

// saveProgress records the page without blocking the frame. One request per
// page turn, and none when the page did not change.
func (a *App) saveProgress(r *Reader) {
	if !r.loaded || len(r.pages) == 0 || r.page == r.lastSaved {
		return
	}
	r.lastSaved = r.page
	id, token, page := r.id, r.token, r.page
	total := len(r.pages)
	progress := float64(page+1) / float64(total)
	completed := page == total-1
	go func() {
		_ = a.client.UpdateProgress(context.Background(), id, token, page, progress, completed)
	}()
}

func (a *App) readerPage(c *ui.Context, id int64, token string) {
	a.ensureReader(id, token)
	r := &a.reader
	t := c.Theme()

	if r.loaded {
		if c.Shortcut(0, ui.KeyRight) || c.Shortcut(0, ui.KeyDown) || c.Shortcut(0, ui.KeySpace) {
			a.readerNext(r)
		}
		if c.Shortcut(0, ui.KeyLeft) || c.Shortcut(0, ui.KeyUp) {
			a.readerPrev(r)
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
			if ui.Button(c, "← 返回").Clicked() {
				a.router.Push(fmt.Sprintf("/gallery/%d/%s", r.id, r.token))
			}
			ui.Column(c).Gap(1).Grow(1).Children(func() {
				ui.Text(c, title).Bold().FontSize(14).SingleLine()
				if r.loaded {
					ui.Textf(c, "%d / %d 页 · %d%%", r.page+1, len(r.pages), int(r.zoom*100)).
						FontSize(11).TextColor(t.TextMuted)
				} else {
					ui.Text(c, "正在加载…").FontSize(11).TextColor(t.TextMuted)
				}
			})
			if ui.Button(c, "−").Width(36).Label("缩小").Disabled(!r.loaded).Clicked() {
				r.zoomBy(-0.2)
			}
			if ui.Button(c, "适应").Label("重置缩放").Disabled(!r.loaded).Clicked() {
				r.resetView()
			}
			if ui.Button(c, "+").Width(36).Label("放大").Disabled(!r.loaded).Clicked() {
				r.zoomBy(0.2)
			}
			if ui.Button(c, "上一页").Disabled(!r.loaded || r.page == 0).Clicked() {
				a.readerPrev(r)
			}
			if ui.PrimaryButton(c, "下一页").Disabled(!r.loaded || r.page+1 >= len(r.pages)).Clicked() {
				a.readerNext(r)
			}
			fullscreen := "全屏"
			if a.isFullscreen() {
				fullscreen = "退出全屏"
			}
			if ui.Button(c, fullscreen).Label("切换全屏").Clicked() {
				a.toggleFullscreen()
			}
		})

		switch {
		case r.loading && !r.loaded:
			ui.Column(c).Fill().Center().Gap(10).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在获取页面列表…").FontSize(12).TextColor(t.TextMuted)
			})
		case r.err != "" && !r.loaded:
			a.errorBlock(c, r.err, func() { a.retryReader(r) })
		case !r.loaded || len(r.pages) == 0:
			a.emptyPage(c, "没有页面", "这个画廊没有可读取的页面", "")
		default:
			// Start the neighbouring pages downloading before the frame is
			// painted, so the visible page and its successors arrive together.
			a.preloadPages(r)

			area := ui.Box(c).Fill().Focusable().Clip()
			area.HandleInput(func(ev ui.InputEvent) bool {
				switch ev.Kind {
				case ui.InputScroll:
					if ev.Mods&ui.Ctrl != 0 || ev.Mods&ui.Cmd != 0 {
						if ev.DY > 0 {
							r.zoomBy(-0.1)
						} else {
							r.zoomBy(0.1)
						}
					} else {
						r.panX -= ev.DX
						r.panY -= ev.DY
					}
					return true
				case ui.InputPointerDown:
					r.drag = true
					r.lastX, r.lastY = ev.X, ev.Y
					return true
				case ui.InputPointerMove:
					if r.drag {
						r.panX += ev.X - r.lastX
						r.panY += ev.Y - r.lastY
						r.lastX, r.lastY = ev.X, ev.Y
					}
					return true
				case ui.InputPointerUp:
					r.drag = false
					return true
				}
				return false
			})
			area.Children(func() {
				src := r.pages[r.page]
				bitmap := a.pageImages.Get(a.client.PageImageURL(src))
				if bitmap == nil {
					bitmap = a.pageCache.Get(int(r.id), r.page)
				}
				ui.Image(c, bitmap).Fill().Fit(ui.Contain).
					TransformOrigin(0.5, 0.5).
					Translate(r.panX, r.panY).
					Scale(r.zoom, r.zoom)
			})

			ui.Progress(c, float64(r.page+1)/float64(len(r.pages))).FillWidth().Height(3)
			ui.Row(c).Padding(6, 16).Children(func() {
				ui.Text(c, "← → 翻页 · Ctrl + 滚轮缩放 · 拖拽平移 · 0 重置 · F 全屏").
					FontSize(11).TextColor(t.TextMuted)
			})
		}
	})
}

// readerPreloadPages is how many pages past the visible one we warm up, so
// turning the page shows a decoded bitmap instead of a blank frame. One page
// behind is warmed too, making a step back instant.
const readerPreloadPages = 2

// preloadPages asks the page-image store for the pages around r.page. Get is
// cheap once a page is queued or loaded; for a page that is not, it starts the
// download in the background. The visible page is asked for first so it is not
// queued behind its neighbours.
func (a *App) preloadPages(r *Reader) {
	warm := func(i int) {
		if i < 0 || i >= len(r.pages) {
			return
		}
		a.pageImages.Get(a.client.PageImageURL(r.pages[i]))
	}
	warm(r.page)
	for i := 1; i <= readerPreloadPages; i++ {
		warm(r.page + i)
	}
	warm(r.page - 1)
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
