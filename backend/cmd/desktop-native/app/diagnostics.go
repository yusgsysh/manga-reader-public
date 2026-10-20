package app

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// Diagnostics is the text the settings page can export: what the app is, where
// it keeps its data, and how big those files are. It never includes the
// cookie, the sync token or image bytes, so it can be attached to a report.
func (a *App) Diagnostics() string {
	var b strings.Builder
	a.mu.Lock()
	defer a.mu.Unlock()

	fmt.Fprintf(&b, "%s\n", i18n.T("Manga Reader diagnostics"))
	fmt.Fprintf(&b, "%s: %s\n", i18n.T("Generated"), time.Now().Format(time.RFC3339))
	fmt.Fprintf(&b, "%s: %s\n", i18n.T("Version"), ourEmpty(a.build))
	fmt.Fprintf(&b, "%s: %s/%s, Go %s\n", i18n.T("Platform"), runtime.GOOS, runtime.GOARCH, runtime.Version())
	fmt.Fprintf(&b, "%s: %s\n", i18n.T("Backend address"), ourEmpty(a.client.BaseURL()))
	fmt.Fprintf(&b, "%s: %s\n", i18n.T("Backend mode"), backendModeLabel(a.backend == nil))
	fmt.Fprintf(&b, "%s: %s\n", i18n.T("Config file"), ourEmpty(a.storage.ConfigPath))
	fmt.Fprintf(&b, "%s: %s\n", i18n.T("Local database"), ourEmpty(a.storage.DBPath))
	fmt.Fprintf(&b, "%s: %s\n", i18n.T("Image cache directory"), ourEmpty(a.storage.CacheDir))
	fmt.Fprintf(&b, "%s: %s / %s\n", i18n.T("Cache size"), humanBytes(a.cacheBytes), cacheLimitText(a.cacheMax))
	fmt.Fprintf(&b, "%s: %s\n", i18n.T("Image cache limit"), cacheLimitText(a.cacheMax))

	// File sizes, not contents: a database path with a surprising size is
	// often the clue in a report.
	for _, p := range []string{a.storage.DBPath, a.storage.ConfigPath} {
		if p == "" {
			continue
		}
		if info, err := os.Stat(p); err == nil {
			fmt.Fprintf(&b, "%s: %s %s\n", filepath.Base(p), i18n.T("Size"), humanBytes(info.Size()))
		}
	}
	fmt.Fprintf(&b, "%s: %s\n", i18n.T("Translation library"), a.translations.Status())
	fmt.Fprintf(&b, "%s: %d\n", i18n.T("Tag rules"), len(a.tags.rules))
	fmt.Fprintf(&b, "%s: %d\n", i18n.T("Download jobs"), len(a.downloads.items))
	fmt.Fprintf(&b, "%s: %s\n", i18n.T("Current page"), a.router.Path())
	return b.String()
}

func ourEmpty(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

// exportDiagnostics asks for a path and writes Diagnostics() there.
func (a *App) exportDiagnostics() {
	if a.diagBusy {
		return
	}
	a.diagBusy = true
	a.diagMsg = ""

	a.diagSeq++
	seq := a.diagSeq
	go func() {
		dir := ""
		if a.storage.CacheDir != "" {
			dir = filepath.Dir(a.storage.CacheDir)
		}
		name := fmt.Sprintf("manga-reader-diagnostics-%s.txt", time.Now().Format("20060102-150405"))
		path, err := pickSavePath(a.win, dir, name)
		if err != nil {
			a.update(func() {
				if a.diagSeq != seq {
					return
				}
				a.diagBusy = false
				a.diagMsg = err.Error()
			})
			return
		}
		if path == "" {
			a.update(func() {
				if a.diagSeq == seq {
					a.diagBusy = false
				}
			})
			return
		}
		text := a.Diagnostics()
		err = writeFileAtomic(path, []byte(text))
		a.update(func() {
			if a.diagSeq != seq {
				return
			}
			a.diagBusy = false
			if err != nil {
				a.diagMsg = i18n.T("Cannot write diagnostics") + ": " + err.Error()
				return
			}
			a.diagMsg = i18n.Tf("Diagnostics saved to %s", filepath.Base(path))
		})
	}()
}

// backendModeLabel names how the API runs for the diagnostics file: embedded
// in this process, or an external server the user pointed at.
func backendModeLabel(external bool) string {
	if external {
		return i18n.T("external (MANGA_READER_API)")
	}
	return i18n.T("embedded")
}
