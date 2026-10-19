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
	"crypto/rand"
	"encoding/hex"
	"log"
	"os"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app"
	"manga-reader/cmd/desktop-native/internal/desktop"
	"manga-reader/cmd/desktop-native/internal/singleinstance"
)

// mainWindow is the window a second launch asks to focus.
var mainWindow atomic.Pointer[mygo.Window]

func main() {
	baseURL := strings.TrimSpace(os.Getenv("MANGA_READER_API"))

	// Single instance is a shipped-app concern: only one process should own
	// the backend, database and cache. Developers pointing at an external
	// backend may run several.
	var instance *singleinstance.Instance
	if baseURL == "" {
		dir, err := desktop.ConfigDir()
		switch {
		case err != nil:
			log.Printf("无法解析配置目录，跳过单实例检查: %v", err)
		default:
			inst, primary, err := singleinstance.Acquire(dir, focusMainWindow)
			switch {
			case err != nil:
				log.Printf("单实例检查失败，继续启动: %v", err)
			case !primary:
				log.Print("Manga Reader 已在运行，已请求它聚焦窗口")
				return
			default:
				instance = inst
			}
		}
	}

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
		token, err := newAuthToken()
		if err != nil {
			log.Fatalf("生成访问令牌失败: %v", err)
		}
		embedded = &embeddedBackend{settings: settings, token: token}
		// Apply the saved appearance before the first window is drawn.
		switch settings.AppearanceSource() {
		case "dark":
			mygo.Theme.SetSource(mygo.ThemeDark)
		case "light":
			mygo.Theme.SetSource(mygo.ThemeLight)
		}
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
		mainWindow.Store(win)
	})

	runErr := mygo.App.Run()

	if instance != nil {
		if err := instance.Close(); err != nil {
			log.Printf("关闭单实例监听: %v", err)
		}
	}
	if embedded != nil {
		if err := embedded.Close(); err != nil {
			log.Printf("关闭内嵌后端: %v", err)
		}
	}
	if runErr != nil {
		log.Fatal(runErr)
	}
}

// focusMainWindow brings the running window to the front, showing it again if
// it was hidden. It is called from the single-instance listener goroutine;
// the mygo Window methods marshal to the UI thread themselves.
func focusMainWindow() {
	if w := mainWindow.Load(); w != nil {
		w.Show()
		w.Focus()
	}
}

// newAuthToken returns a random per-run token for the loopback API.
func newAuthToken() (string, error) {
	var raw [32]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(raw[:]), nil
}

// imageCacheFromEnv reads MANGA_READER_IMAGE_CACHE, falling back to def.
func imageCacheFromEnv(def bool) bool {
	v := strings.TrimSpace(os.Getenv("MANGA_READER_IMAGE_CACHE"))
	if v == "" {
		return def
	}
	return v == "1" || strings.EqualFold(v, "true") || strings.EqualFold(v, "yes")
}
