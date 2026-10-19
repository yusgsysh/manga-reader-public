// Command desktop-native is the MyGo native-UI desktop app: one Go binary,
// no webview and no frontend build. It is the replacement for the React
// frontend and the Wails shell.
//
// It runs the Manga Reader API in-process on a random loopback port, using a
// per-user SQLite database and a local filesystem image cache — no external
// server, no MinIO. On first run it asks for ExHentai cookies and stores them
// in ~/.config/manga-reader/config.json.
//
// Set MANGA_READER_API to point the UI at an already-running backend instead
// (useful for development):
//
//	MANGA_READER_API=http://127.0.0.1:8080 go run ./cmd/desktop-native
package main

import (
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

	var a *app.App
	var embedded *embeddedBackend

	if baseURL != "" {
		// External backend mode: the client is configured up front and the
		// app never shows onboarding.
		client := app.NewClient(baseURL)
		client.UseImageCache(imageCacheFromEnv(false))
		a = app.New(client)
	} else {
		settings, err := desktop.LoadSettings()
		if err != nil {
			log.Fatalf("加载设置失败: %v", err)
		}
		embedded = &embeddedBackend{settings: settings}
		if settings.CookieConfig().IsValid() {
			if _, err := embedded.Start(settings.Cookie); err != nil {
				log.Printf("启动内嵌后端失败: %v", err)
			}
		}
		client := app.NewClient(embedded.BaseURL())
		client.UseImageCache(true)
		a = app.New(client)
		a.SetBackend(embedded)
	}

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

// imageCacheFromEnv reads MANGA_READER_IMAGE_CACHE, falling back to def.
func imageCacheFromEnv(def bool) bool {
	v := strings.TrimSpace(os.Getenv("MANGA_READER_IMAGE_CACHE"))
	if v == "" {
		return def
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}
