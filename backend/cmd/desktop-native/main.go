// Command desktop-native is the MyGo native-UI desktop app: one Go binary,
// no webview and no frontend build. It is the replacement for the React
// frontend and the Wails shell.
//
// By default it runs the Manga Reader API in-process on a random loopback
// port, using a per-user SQLite database and a local filesystem image cache —
// no external server, no MinIO. Cookies come from
// ~/.config/manga-reader/config.json (or the EHENTAI_COOKIE* environment
// variables).
//
// Set MANGA_READER_API to point the UI at an already-running backend instead
// (useful for development):
//
//	MANGA_READER_API=http://127.0.0.1:8080 go run ./cmd/desktop-native
package main

import (
	"context"
	"log"
	"os"
	"strings"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app"
	"manga-reader/cmd/desktop-native/internal/desktop"
)

func main() {
	baseURL := strings.TrimSpace(os.Getenv("MANGA_READER_API"))

	var embedded *desktop.Server
	if baseURL == "" {
		srv, err := startEmbedded()
		if err != nil {
			log.Fatalf("启动失败: %v", err)
		}
		embedded = srv
		baseURL = srv.BaseURL()
	}

	client := app.NewClient(baseURL)
	// The embedded server always has the filesystem image cache; enable the
	// read-through proxy for it. MANGA_READER_IMAGE_CACHE overrides, which is
	// useful when pointing at an external backend that does have a cache.
	client.UseImageCache(embedded != nil)
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_IMAGE_CACHE")); v != "" {
		client.UseImageCache(v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes"))
	}

	a := app.New(client)
	mygo.App.WhenReady(func() {
		win := mygo.NewWindow(mygo.WindowOptions{
			Title:     "Manga Reader",
			Width:     1280,
			Height:    860,
			MinWidth:  960,
			MinHeight: 640,
			StateKey:  "main",
			Content:   ui.View(a.View),
		})
		a.Attach(win)
	})

	runErr := mygo.App.Run()

	if embedded != nil {
		if err := embedded.Close(); err != nil {
			log.Printf("关闭内嵌后端: %v", err)
		}
	}
	if runErr != nil {
		log.Fatal(runErr)
	}
}

// startEmbedded loads the desktop settings and boots the in-process API.
func startEmbedded() (*desktop.Server, error) {
	settings, err := desktop.LoadSettings()
	if err != nil {
		return nil, err
	}

	cookie := settings.CookieConfig()
	if !cookie.IsValid() {
		if path := desktop.SettingsPath(); path != "" {
			return nil, &missingCookieError{path: path}
		}
		return nil, desktop.ErrMissingCookie
	}

	return desktop.Start(context.Background(), desktop.Options{
		Cookie:    cookie,
		DBPath:    settings.DatabasePathOrDefault(),
		CacheDir:  settings.CacheDirOrDefault(),
		SyncToken: settings.SyncToken,
		DevTools:  settings.DevTools,
	})
}

type missingCookieError struct{ path string }

func (e *missingCookieError) Error() string {
	return "未配置 ExHentai Cookies。请在 " + e.path +
		` 中填写 "cookie"，或设置环境变量 EHENTAI_COOKIE（例如 ipb_member_id=...; ipb_pass_hash=...）`
}
