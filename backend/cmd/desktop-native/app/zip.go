package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// saveZip asks for a path with the native save dialog and streams the job's
// ZIP there. The dialog blocks, so it runs on its own goroutine; the button
// shows the job as busy meanwhile.
func (a *App) saveZip(j PrefillJob) {
	if a.zipBusy[j.ID] {
		return
	}
	if a.zipBusy == nil {
		a.zipBusy = make(map[string]bool)
	}
	a.zipBusy[j.ID] = true
	a.zipMsg = ""

	go func() {
		dir := ""
		if a.storage.CacheDir != "" {
			dir = filepath.Dir(a.storage.CacheDir)
		}
		path, err := pickSavePath(a.win, dir, zipFileName(j))
		if err != nil {
			a.update(func() {
				a.zipBusy[j.ID] = false
				a.zipMsg = err.Error()
			})
			return
		}
		if path == "" {
			// The user cancelled the dialog.
			a.update(func() { a.zipBusy[j.ID] = false })
			return
		}

		n, err := a.downloadZip(j.ID, path)
		a.update(func() {
			a.zipBusy[j.ID] = false
			if err != nil {
				a.zipMsg = err.Error()
				return
			}
			a.zipMsg = i18n.Tf("Saved %s (%s)", filepath.Base(path), humanBytes(n))
		})
	}()
}

// downloadZip streams GET /api/prefill/:id/zip into path, returning the
// number of bytes written.
func (a *App) downloadZip(jobID, path string) (int64, error) {
	return a.client.downloadTo(path, "/api/prefill/"+jobID+"/zip", 30*time.Minute)
}

// downloadTo streams an API path into path. The file is removed when the copy
// fails, so a cancelled download leaves no half-written file behind. It lives
// on the Client so ZIPs and torrents share it.
func (c *Client) downloadTo(path, apiPath string, timeout time.Duration) (int64, error) {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.BaseURL()+apiPath, nil)
	if err != nil {
		return 0, err
	}
	if token := c.authTokenValue(); token != "" {
		req.Header.Set("X-Manga-Reader-Token", token)
	}
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return 0, fmt.Errorf("%s: %w", i18n.T("Download failed"), err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return 0, fmt.Errorf(i18n.T("Backend returned %d: %s"), resp.StatusCode, apiError(msg))
	}

	f, err := os.Create(path)
	if err != nil {
		return 0, err
	}
	n, copyErr := io.Copy(f, resp.Body)
	closeErr := f.Close()
	if copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Remove(path)
		return 0, fmt.Errorf("%s: %w", i18n.T("Failed to write the file"), copyErr)
	}
	return n, nil
}

// zipFileName is the default name of a job's archive: the gallery title, cut
// to a length file systems accept, without separators.
func zipFileName(j PrefillJob) string {
	title := strings.TrimSpace(j.Title)
	if title == "" {
		title = fmt.Sprintf("gallery-%d", j.GalleryID)
	}
	var b strings.Builder
	for _, r := range title {
		switch {
		case unicode.IsLetter(r), unicode.IsDigit(r),
			r == '-', r == '_', r == '[', r == ']', r == '(', r == ')':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteRune(' ')
		}
		if b.Len() >= 80 {
			break
		}
	}
	name := strings.TrimSpace(b.String())
	if name == "" {
		name = fmt.Sprintf("gallery-%d", j.GalleryID)
	}
	return name + ".zip"
}

// pickSavePath is the native save dialog, a variable so tests can answer it.
var pickSavePath = func(parent *mygo.Window, dir, name string) (string, error) {
	opts := mygo.SaveDialogOptions{
		Parent:      parent,
		Title:       i18n.T("Save ZIP"),
		DefaultPath: filepath.Join(dir, name),
		ButtonLabel: i18n.T("Save"),
		Filters:     []mygo.FileFilter{{Name: "ZIP 压缩包", Extensions: []string{"zip"}}},
	}
	return mygo.Dialog.Save(opts)
}

// detailDownloadsBlock is the detail page's download section: the offline copy
// button plus a ZIP export once the job has completed.
func (a *App) detailDownloadsBlock(c *ui.Context, s *detailState) {
	t := c.Theme()
	status := ""
	for _, j := range a.downloads.items {
		if j.ID == s.downloadJob {
			status = j.Status
		}
	}

	ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
		label := i18n.T("Download")
		if s.downloadBusy {
			label = i18n.T("Creating the job…")
		}
		if s.downloadJob != "" {
			label = i18n.T("View download")
		}
		if ui.Button(c, label).Disabled(s.downloadBusy).Clicked() {
			if s.downloadJob != "" {
				a.router.Push("/downloads")
			} else {
				a.startDownload(c, s)
			}
		}
		if s.downloadJob != "" && status == "completed" {
			if ui.Button(c, i18n.T("Export ZIP")).Disabled(a.zipBusy[s.downloadJob]).Clicked() {
				a.saveZip(PrefillJob{ID: s.downloadJob, GalleryID: s.id, Title: s.g.Title})
			}
		}
	})
	if s.downloadErr != "" {
		ui.Text(c, s.downloadErr).FontSize(11).TextColor(t.Danger)
	}
}
