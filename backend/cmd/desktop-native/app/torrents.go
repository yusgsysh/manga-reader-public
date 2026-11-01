package app

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// Torrent is one torrent the gallery page lists; the backend scrapes it live.
type Torrent struct {
	GTID      string
	Name      string
	Size      string
	Posted    string
	Seeds     int
	Peers     int
	Downloads int
	Uploader  string
}

// torrentsState is one gallery's torrent list and the Information view of the
// rows the user expanded, loaded live each time the dialog opens.
type torrentsState struct {
	id      int64
	token   string
	items   []Torrent
	loaded  bool
	loading bool
	err     string
	loadID  int

	// busy is the gtid whose .torrent file is being saved.
	busy string
	msg  string

	infos       map[string]TorrentInfo
	infoLoading map[string]bool
	infoErr     map[string]string
	infoOpen    map[string]bool
}

// openTorrents shows the torrents dialog, fetching the list again as main
// does: torrents are live-only, so each open asks the source.
func (a *App) openTorrents(s *detailState) {
	s.torrentsOpen = true
	a.loadTorrents(s)
}

// loadTorrents fetches a gallery's torrents for the dialog.
func (a *App) loadTorrents(s *detailState) {
	t := &s.torrents
	if t.loading {
		return
	}
	t.id, t.token = s.id, s.token
	t.loading = true
	t.loaded = false
	t.err = ""
	t.items = nil
	t.msg = ""
	t.infoOpen = make(map[string]bool)
	t.infoLoading = make(map[string]bool)
	t.infoErr = make(map[string]string)
	t.infos = make(map[string]TorrentInfo)
	t.loadID++
	lid := t.loadID
	go func() {
		items, err := a.client.Torrents(context.Background(), t.id, t.token)
		a.update(func() {
			if t.loadID != lid {
				return
			}
			t.loading = false
			if err != nil {
				t.err = err.Error()
				return
			}
			t.items = items
			t.loaded = true
		})
	}()
}

// toggleTorrentInfo opens or closes one torrent's Information view, loading
// it the first time it opens.
func (a *App) toggleTorrentInfo(s *detailState, tr Torrent) {
	t := &s.torrents
	if t.infoOpen == nil {
		t.infoOpen = make(map[string]bool)
	}
	open := !t.infoOpen[tr.GTID]
	t.infoOpen[tr.GTID] = open
	if !open || t.infoLoading[tr.GTID] {
		return
	}
	if _, ok := t.infos[tr.GTID]; ok {
		return
	}
	if t.infoLoading == nil {
		t.infoLoading = make(map[string]bool)
	}
	if t.infoErr == nil {
		t.infoErr = make(map[string]string)
	}
	if t.infos == nil {
		t.infos = make(map[string]TorrentInfo)
	}
	t.infoLoading[tr.GTID] = true
	t.infoErr[tr.GTID] = ""
	id, token, gtid := t.id, t.token, tr.GTID
	go func() {
		info, err := a.client.TorrentInfo(context.Background(), id, token, gtid)
		a.update(func() {
			t.infoLoading[gtid] = false
			if err != nil {
				t.infoErr[gtid] = err.Error()
				return
			}
			t.infos[gtid] = info
		})
	}()
}

// torrentsDialog is main's GalleryTorrentsDialog: a 40rem panel over the page,
// with one bordered card per torrent.
func (a *App) torrentsDialog(c *ui.Context, s *detailState) {
	if !s.torrentsOpen {
		return
	}
	t := c.Theme()
	ts := &s.torrents

	w, _ := c.Size()
	width := w * 0.94
	if width > 640 {
		width = 640
	}
	if width < 280 {
		width = 280
	}

	// main's dialog sits under the header (top-16), not in the middle of the
	// window: the page behind it stays visible from the top down.
	ui.DialogBase(c, &s.torrentsOpen, func(backdrop, panel ui.Element) {
		backdrop.Background(ui.RGBA(0, 0, 0, 0.4)).Justify(ui.Start).
			AlignItems(ui.Center).Padding(64, 0, 0, 0)
		panel.Width(width).Padding(20).Radius(16).Background(t.Background).
			Border(1, t.Border).Shadow(0, 10, 30, 0, ui.RGBA(0, 0, 0, 0.3))
		panel.Children(func() {
			ui.Column(c).FillWidth().Gap(16).Children(func() {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Icon(c, detailIconSVG[iconMagnet]).Size(16, 16)
					ui.Text(c, i18n.T("Torrent downloads")).FontSize(16).FontWeight(600).SingleLine()
					if ts.loaded && len(ts.items) > 0 {
						ui.Textf(c, "(%d)", len(ts.items)).FontSize(14).TextColor(t.TextMuted)
					}
				})

				switch {
				case ts.loading && !ts.loaded:
					ui.Row(c).FillWidth().Center().Padding(32, 0).Children(func() {
						ui.Spinner(c).Size(24, 24)
					})
				case ts.err != "" && !ts.loaded:
					ui.Text(c, ts.err).FontSize(13).TextColor(t.Danger)
				case len(ts.items) == 0:
					ui.Text(c, i18n.T("No torrents yet")).FontSize(13).
						TextColor(t.TextMuted).Padding(24, 0)
				default:
					ui.Scroll(c).FillWidth().MaxHeight(480).Children(func() {
						ui.Column(c).Gap(12).Children(func() {
							for _, tr := range ts.items {
								a.torrentCard(c, s, tr)
							}
						})
					})
				}

				if ts.msg != "" {
					ui.Text(c, ts.msg).FontSize(12).TextColor(t.TextMuted)
				}

				ui.Row(c).Justify(ui.End).Children(func() {
					if ui.Button(c, i18n.T("Close")).Clicked() {
						s.torrentsOpen = false
					}
				})
			})
		})
	})
}

// torrentCard is one row of the torrents dialog: the name, its size and
// tracker numbers, the download link and the Information toggle.
func (a *App) torrentCard(c *ui.Context, s *detailState, tr Torrent) {
	t := c.Theme()
	ts := &s.torrents

	ui.Column(c).Gap(10).Padding(12).Radius(12).Border(1, t.Border).Children(func() {
		ui.Row(c).Gap(12).AlignItems(ui.Start).Children(func() {
			ui.Column(c).Gap(4).Grow(1).Children(func() {
				ui.Text(c, tr.Name).FontSize(13).FontWeight(500).FixedLineHeight(20).SingleLine()
				meta := strings.TrimSpace(strings.Join(nonEmpty(tr.Size, tr.Posted, tr.Uploader), " · "))
				ui.Text(c, meta).FontSize(12).TextColor(t.TextMuted).SingleLine()
				ui.Textf(c, i18n.T("%d seeds · %d leechers · %d completed"),
					tr.Seeds, tr.Peers, tr.Downloads).FontSize(12).TextColor(t.TextMuted).SingleLine()
			})

			ui.Column(c).Gap(8).AlignItems(ui.End).Shrink(0).Children(func() {
				a.torrentDownloadButton(c, ts, tr)
				label := i18n.T("Information")
				if ts.infoOpen[tr.GTID] {
					label = i18n.T("Hide information")
				}
				toggle := ui.Button(c, "").Height(22).Padding(0, 4).Radius(6).
					Background(ui.Transparent).BorderWidth(0)
				toggle.Children(func() {
					ui.Text(c, label).FontSize(12).TextColor(t.TextMuted).SingleLine()
				})
				if toggle.Clicked() {
					a.toggleTorrentInfo(s, tr)
				}
			})
		})

		if !ts.infoOpen[tr.GTID] {
			return
		}
		ui.Divider(c)
		switch {
		case ts.infoLoading[tr.GTID]:
			ui.Row(c).FillWidth().Center().Padding(12, 0).Children(func() {
				ui.Spinner(c).Size(18, 18)
			})
		case ts.infoErr[tr.GTID] != "":
			ui.Text(c, ts.infoErr[tr.GTID]).FontSize(12).TextColor(t.Danger)
		default:
			info, ok := ts.infos[tr.GTID]
			if !ok {
				return
			}
			ui.Column(c).Gap(8).Children(func() {
				ui.Row(c).Gap(16).Wrap().Children(func() {
					for _, line := range []string{
						i18n.Tf("Posted: %s", info.Posted),
						i18n.Tf("Size: %s", info.Size),
						i18n.Tf("Uploader: %s", info.Uploader),
						i18n.Tf("Seeds: %d", info.Seeds),
						i18n.Tf("Leechers: %d", info.Dlers),
						i18n.Tf("Completed: %d", info.Completes),
					} {
						ui.Text(c, line).FontSize(12).TextColor(t.TextMuted)
					}
				})
				if info.Comments != "" {
					ui.Text(c, info.Comments).FontSize(12).TextColor(t.Text)
				}
				if info.Personalized {
					ui.Row(c).FillWidth().Center().Children(func() {
						a.torrentDownloadButtonLabel(c, ts, tr, i18n.T("Personalized torrent"))
					})
				}
			})
		}
	})
}

// torrentDownloadButton is the card's small download link, named for assistive
// technology so the dialog's 下载 is told apart from the page's.
func (a *App) torrentDownloadButton(c *ui.Context, ts *torrentsState, tr Torrent) {
	a.torrentDownloadButtonLabel(c, ts, tr, i18n.T("Download torrent"))
}

func (a *App) torrentDownloadButtonLabel(c *ui.Context, ts *torrentsState, tr Torrent, label string) {
	t := c.Theme()
	btn := ui.Button(c, "").Height(24).Padding(0, 10).Radius(8).Gap(4).
		Background(t.Background).Border(1, t.Border).Label(label).
		Disabled(ts.busy != "")
	if btn.Hovered() && ts.busy == "" {
		btn.Background(t.SurfaceHover)
	}
	btn.Children(func() {
		if ts.busy == tr.GTID {
			ui.Spinner(c).Size(14, 14)
		} else {
			ui.Icon(c, detailIconSVG[iconDownload]).Size(14, 14)
		}
		ui.Text(c, i18n.T("Download")).FontSize(12).FontWeight(500).FixedLineHeight(16).SingleLine()
	})
	if btn.Clicked() {
		a.saveTorrent(ts, tr)
	}
}

// nonEmpty keeps the parts that carry text, for joining as one meta line.
func nonEmpty(parts ...string) []string {
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		if strings.TrimSpace(part) != "" {
			out = append(out, part)
		}
	}
	return out
}

// saveTorrent asks where to keep a .torrent file and streams it there.
func (a *App) saveTorrent(t *torrentsState, tr Torrent) {
	if t.busy != "" {
		return
	}
	t.busy = tr.GTID
	t.msg = ""
	id, token := t.id, t.token
	go func() {
		dir := ""
		if a.storage.CacheDir != "" {
			dir = filepath.Dir(a.storage.CacheDir)
		}
		path, err := pickSavePath(a.win, dir, torrentFileName(tr))
		if err != nil {
			a.update(func() {
				t.busy = ""
				t.msg = err.Error()
			})
			return
		}
		if path == "" {
			a.update(func() { t.busy = "" })
			return
		}
		n, err := a.client.DownloadTorrent(context.Background(), id, token, tr.GTID, path)
		a.update(func() {
			t.busy = ""
			if err != nil {
				t.msg = err.Error()
				return
			}
			t.msg = i18n.Tf("Saved %s (%s)", filepath.Base(path), humanBytes(n))
		})
	}()
}

// torrentFileName is the default file name of a torrent, from its name.
func torrentFileName(tr Torrent) string {
	name := strings.TrimSpace(tr.Name)
	if name == "" {
		name = "torrent-" + tr.GTID
	}
	// Torrent names usually end in the archive's extension (.zip); strip it
	// and .torrent before adding our own.
	lower := strings.ToLower(name)
	for _, ext := range []string{".torrent", ".zip", ".rar", ".7z"} {
		if strings.HasSuffix(lower, ext) {
			name = name[:len(name)-len(ext)]
			break
		}
	}
	var b strings.Builder
	for _, r := range name {
		if r == '/' || r == '\\' || r == ':' || r == 0 {
			b.WriteRune('_')
			continue
		}
		b.WriteRune(r)
		if b.Len() >= 80 {
			break
		}
	}
	out := strings.TrimSpace(b.String())
	if out == "" {
		out = "torrent-" + tr.GTID
	}
	return out + ".torrent"
}
