package app

import (
	"context"
	"fmt"
	"time"

	"github.com/egoist/mygo/ui"
)

// Downloads is the app's view of the backend's prefill jobs: offline copies of
// a gallery. The backend does the fetching one page at a time; the UI starts
// jobs, shows their progress and polls the ones still running.
//
// All fields are guarded by App.mu, like the rest of the app state.
type Downloads struct {
	items   []PrefillJob
	loaded  bool
	loading bool
	err     string
	loadID  int

	// polling is set while poll requests are in flight, keeping one frame
	// from starting a second round. polled is when the last round started.
	polling bool
	polled  time.Time

	// dropped holds the IDs deleted from the list. A reload that raced the
	// delete may still carry them, so they are filtered out until it is done.
	dropped map[string]bool
}

// downloadPollInterval is how often a running job is polled. The backend
// updates progress per page, and a page takes far longer than this.
const downloadPollInterval = 1500 * time.Millisecond

// ensureDownloads loads the job list once. The sidebar badge and the download
// page share it.
func (a *App) ensureDownloads() {
	d := &a.downloads
	if d.loading || d.loaded {
		return
	}
	a.loadDownloads()
}

func (a *App) loadDownloads() {
	d := &a.downloads
	if d.loading {
		return
	}
	d.loading = true
	d.err = ""
	d.loadID++
	lid := d.loadID
	go func() {
		jobs, err := a.client.PrefillJobs(context.Background())
		a.update(func() {
			d.loading = false
			if err != nil {
				d.err = err.Error()
				return
			}
			// A reload overlaps a delete that already removed the row from
			// the list; take its answer unless a newer reload superseded it.
			// The backend list may still hold a job deleted meanwhile, so
			// keep the rows the UI already dropped.
			if d.loadID != lid {
				return
			}
			if d.dropped != nil {
				kept := jobs[:0]
				for _, j := range jobs {
					if !d.dropped[j.ID] {
						kept = append(kept, j)
					}
				}
				jobs = kept
			}
			d.items = jobs
			d.loaded = true
		})
	}()
}

// pollDownloads refreshes running jobs. It is a no-op when nothing runs, so
// the download page can call it every frame.
//
// A round runs when one is not already in flight and either none ran yet or
// downloadPollInterval has passed. The timer between rounds is the page's
// c.After; this function is the only one that decides, so a late frame does
// not stack rounds.
func (a *App) pollDownloads(now time.Time) {
	d := &a.downloads
	if !d.loaded || d.polling {
		return
	}
	if !d.polled.IsZero() && now.Sub(d.polled) < downloadPollInterval {
		return
	}
	var ids []string
	for _, j := range d.items {
		if j.Status == "queued" || j.Status == "running" {
			ids = append(ids, j.ID)
		}
	}
	if len(ids) == 0 {
		return
	}
	d.polling = true
	d.polled = now
	go func() {
		fresh := make([]PrefillJob, 0, len(ids))
		for _, id := range ids {
			if j, err := a.client.PrefillJob(context.Background(), id); err == nil {
				fresh = append(fresh, j)
			}
		}
		a.update(func() {
			d.polling = false
			for _, j := range fresh {
				for i := range d.items {
					if d.items[i].ID == j.ID {
						d.items[i] = j
					}
				}
			}
		})
	}()
}

// startDownload queues an offline copy of the gallery. The page list is
// fetched again here: the prefill API wants the page URLs, and the reader may
// not have visited this gallery this session.
func (a *App) startDownload(c *ui.Context, s *detailState) {
	if s.downloadBusy {
		return
	}
	s.downloadBusy = true
	s.downloadErr = ""
	id, token, title := s.id, s.token, s.g.Title

	go func() {
		ctx := context.Background()
		_, urls, err := a.client.Pages(ctx, id, token)
		if err != nil {
			_, urls, err = a.client.CachedPages(ctx, id, token)
		}
		if err == nil && len(urls) == 0 {
			err = fmt.Errorf("这个画廊没有可下载的页面")
		}
		var job PrefillJob
		if err == nil {
			job, err = a.client.StartPrefill(ctx, id, token, title, urls)
		}
		a.update(func() {
			s.downloadBusy = false
			if err != nil {
				s.downloadErr = err.Error()
				return
			}
			s.downloadJob = job.ID
			a.downloads.loaded = false
			a.ensureDownloads()
			a.toastLocked(ui.Toast{
				ID: "download-" + job.ID, Type: "success",
				Title: "已加入下载队列",
				Description: fmt.Sprintf("%d 页 · %s", len(urls),
					downloadStatusLabel(job.Status)),
			})
		})
	}()
}

func (a *App) cancelDownload(id string) {
	go func() {
		_, err := a.client.CancelPrefill(context.Background(), id)
		a.update(func() {
			if err == nil {
				a.loadDownloads()
			}
		})
	}()
}

func (a *App) deleteDownload(id string) {
	go func() {
		err := a.client.DeletePrefill(context.Background(), id)
		if err != nil {
			return
		}
		// Drop the row at once instead of waiting for the reload, so the
		// list responds on the same frame; remember it so a reload that
		// started before the delete landed does not bring it back.
		a.update(func() {
			if a.downloads.dropped == nil {
				a.downloads.dropped = make(map[string]bool)
			}
			a.downloads.dropped[id] = true
			for i := range a.downloads.items {
				if a.downloads.items[i].ID == id {
					a.downloads.items = append(a.downloads.items[:i], a.downloads.items[i+1:]...)
					break
				}
			}
		})
		a.loadDownloads()
	}()
}

// downloadStatusLabel is the Chinese label of a backend job status.
func downloadStatusLabel(status string) string {
	switch status {
	case "queued":
		return "排队中"
	case "running":
		return "下载中"
	case "completed":
		return "已完成"
	case "cancelled":
		return "已取消"
	case "failed":
		return "失败"
	}
	return status
}

// downloadProgress is the 0..1 fraction of a job's pages that are done, or -1
// while the backend has no progress counter.
func downloadProgress(j PrefillJob) float64 {
	if j.Total <= 0 {
		return -1
	}
	switch {
	case j.Status == "completed":
		return 1
	case j.Progress != nil:
		return float64(j.Progress.Done) / float64(j.Total)
	case j.Status == "queued":
		return 0
	}
	return -1
}

// downloadsPage lists prefill jobs, newest first, with live progress. A frame
// while jobs run is requested again after the poll interval, so the list keeps
// itself up to date without a timer per row.
func (a *App) downloadsPage(c *ui.Context) {
	t := c.Theme()
	a.ensureDownloads()
	now := c.Now()
	a.pollDownloads(now)
	if len(a.downloads.items) > 0 && a.hasRunningDownload() {
		// Ask for the frame that polls again when this round is due.
		c.After(a.nextPollWait(now))
	}

	ui.Column(c).Fill().Padding(20, 24).Gap(14).Children(func() {
		ui.Row(c).AlignItems(ui.End).Gap(12).Children(func() {
			ui.Column(c).Gap(2).Grow(1).Children(func() {
				ui.Text(c, "下载管理").FontSize(26).Bold()
				ui.Text(c, "离线下载的页面会存入本地图片缓存，断网也能阅读。").
					FontSize(12).TextColor(t.TextMuted)
			})
			if ui.Button(c, "刷新").Clicked() {
				a.loadDownloads()
			}
		})

		switch {
		case a.downloads.loading && len(a.downloads.items) == 0:
			ui.Column(c).Fill().Center().Gap(10).Children(func() {
				ui.Spinner(c)
				ui.Text(c, "正在加载任务…").FontSize(12).TextColor(t.TextMuted)
			})
		case a.downloads.err != "" && len(a.downloads.items) == 0:
			a.errorBlock(c, a.downloads.err, func() { a.loadDownloads() })
		case len(a.downloads.items) == 0:
			a.emptyPage(c, "还没有离线下载任务", "在画廊详情页点击「离线下载」", "")
		default:
			if a.zipMsg != "" {
				ui.Text(c, a.zipMsg).FontSize(11).TextColor(t.TextMuted)
			}
			ui.Scroll(c).Grow(1).Children(func() {
				ui.Column(c).Gap(10).Children(func() {
					for _, j := range a.downloads.items {
						a.downloadRow(c, j)
					}
				})
			})
		}
	})
}

func (a *App) downloadRow(c *ui.Context, j PrefillJob) {
	t := c.Theme()
	running := j.Status == "queued" || j.Status == "running"

	ui.Column(c).Padding(12).Gap(8).Radius(10).Border(1, t.Border).Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			title := j.Title
			if title == "" {
				title = fmt.Sprintf("画廊 %d", j.GalleryID)
			}
			open := ui.Box(c).Focusable().Label("打开 " + title).Grow(1)
			open.Children(func() {
				ui.Text(c, title).FontSize(14).Bold().SingleLine()
			})
			if open.Clicked() && j.GalleryID > 0 && j.Token != "" {
				a.router.Push(fmt.Sprintf("/gallery/%d/%s", j.GalleryID, j.Token))
			}
			ui.Text(c, downloadStatusLabel(j.Status)).FontSize(12).TextColor(t.TextMuted)
			if running {
				if ui.Button(c, "取消").Clicked() {
					a.cancelDownload(j.ID)
				}
			} else {
				if j.Status == "completed" {
					label := "导出 ZIP"
					if a.zipBusy[j.ID] {
						label = "正在导出…"
					}
					if ui.Button(c, label).Disabled(a.zipBusy[j.ID]).Clicked() {
						a.saveZip(j)
					}
				}
				if ui.Button(c, "删除").Clicked() {
					a.deleteDownload(j.ID)
				}
			}
		})

		if p := downloadProgress(j); p >= 0 {
			ui.Progress(c, p).FillWidth()
		}
		detail := fmt.Sprintf("%d / %d 页", donePages(j), j.Total)
		if j.FailedCount > 0 {
			detail += fmt.Sprintf(" · %d 页失败", j.FailedCount)
		}
		ui.Text(c, detail).FontSize(11).TextColor(t.TextMuted)
	})
}

// hasRunningDownload reports whether any job is queued or running.
func (a *App) hasRunningDownload() bool {
	for _, j := range a.downloads.items {
		if j.Status == "queued" || j.Status == "running" {
			return true
		}
	}
	return false
}

// nextPollWait is how long to wait before the next poll: the rest of the
// interval, or nothing when this round is already due (it just started).
func (a *App) nextPollWait(now time.Time) time.Duration {
	d := &a.downloads
	if d.polled.IsZero() {
		return downloadPollInterval
	}
	wait := downloadPollInterval - now.Sub(d.polled)
	if wait < 0 {
		wait = 0
	}
	return wait
}

func donePages(j PrefillJob) int {
	if j.Status == "completed" {
		return j.Total
	}
	if j.Progress != nil {
		return j.Progress.Done
	}
	return 0
}
