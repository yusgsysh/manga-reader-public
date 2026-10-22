package app

import (
	"context"
	"fmt"
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

// torrentsState is one gallery's torrent list, loaded on demand.
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
}

// ensureTorrents loads a gallery's torrents once, when the user asks for them.
func (a *App) loadTorrents(s *detailState) {
	if s.showTorrents {
		return
	}
	s.showTorrents = true
	t := &s.torrents
	if t.id == s.id && (t.loaded || t.loading) {
		return
	}
	t.id, t.token = s.id, s.token
	t.loading = true
	t.err = ""
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

// torrentsBlock is the gallery page's torrent list.
func (a *App) torrentsBlock(c *ui.Context, s *detailState) {
	t := c.Theme()
	ts := &s.torrents

	ui.Row(c).AlignItems(ui.Center).Gap(10).Children(func() {
		ui.Text(c, i18n.T("Torrents")).FontSize(16).Bold().Grow(1)
		if !s.showTorrents {
			// Torrents load once, on request; there is no refresh button, so
			// a second click is not needed.
			if ui.Button(c, i18n.T("Show torrents")).Clicked() {
				a.loadTorrents(s)
			}
			return
		}
	})
	if !s.showTorrents {
		return
	}

	switch {
	case ts.loading && !ts.loaded:
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			inlineLoading(c, i18n.T("Loading torrents…"))
			ui.Text(c, i18n.T("Loading torrents…")).FontSize(12).TextColor(t.TextMuted)
		})
	case ts.err != "" && !ts.loaded:
		a.errorBlock(c, ts.err, func() {
			s.showTorrents = false
			a.loadTorrents(s)
		})
	case len(ts.items) == 0:
		ui.Text(c, i18n.T("This gallery has no torrents.")).FontSize(12).TextColor(t.TextMuted)
	default:
		ui.Column(c).Gap(6).Children(func() {
			for _, tr := range ts.items {
				a.torrentRow(c, ts, tr)
			}
		})
	}
	if ts.msg != "" {
		ui.Text(c, ts.msg).FontSize(11).TextColor(t.TextMuted)
	}
}

func (a *App) torrentRow(c *ui.Context, ts *torrentsState, tr Torrent) {
	t := c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(8).Radius(8).
		Border(1, t.Border).Children(func() {
		ui.Column(c).Gap(2).Grow(1).Children(func() {
			ui.Text(c, tr.Name).FontSize(13).SingleLine()
			meta := fmt.Sprintf("%s · %d 做种 · %d 下载", tr.Size, tr.Seeds, tr.Downloads)
			if tr.Uploader != "" {
				meta += " · " + tr.Uploader
			}
			ui.Text(c, meta).FontSize(11).TextColor(t.TextMuted).SingleLine()
		})
		label := i18n.T("Download torrent")
		if ts.busy == tr.GTID {
			label = i18n.T("Saving…")
		}
		if ui.Button(c, label).Disabled(ts.busy != "").Clicked() {
			a.saveTorrent(ts, tr)
		}
	})
}
