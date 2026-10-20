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
	"fmt"
	"log"
	"os"
	"strings"
	"sync/atomic"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app"
	"manga-reader/cmd/desktop-native/app/i18n"
	"manga-reader/cmd/desktop-native/assets"
	"manga-reader/cmd/desktop-native/internal/desktop"
	"manga-reader/cmd/desktop-native/internal/singleinstance"
)

// mainWindow is the window a second launch asks to focus.
var mainWindow atomic.Pointer[mygo.Window]

func main() {
	// Crashes go to a file next to config.json: a GUI app has no visible
	// stderr, so a panic would otherwise leave nothing behind.
	if path := desktop.InstallCrashLog(); path != "" {
		log.Printf("崩溃日志: %s", path)
	}

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
		a.SetBuild(appVersion() + " · " + platformLabel())
	} else {
		// A config.json that cannot be parsed must not take the window with
		// it: the app opens and says what is wrong, and settings saved from
		// it replace the broken file.
		settings, settingsErr := desktop.LoadSettings()
		if settingsErr != nil {
			log.Printf("加载设置失败，将使用默认值: %v", settingsErr)
		}
		token, err := newAuthToken()
		if err != nil {
			log.Fatalf("生成访问令牌失败: %v", err)
		}
		embedded = &embeddedBackend{settings: settings, token: token}
		if settingsErr != nil {
			embedded.startErr = fmt.Errorf("读取配置文件失败：%v\n下面的设置页会把当前设置写回去，覆盖这个文件。", settingsErr)
		}
		// Apply the saved appearance before the first window is drawn.
		switch settings.AppearanceSource() {
		case "dark":
			mygo.Theme.SetSource(mygo.ThemeDark)
		case "light":
			mygo.Theme.SetSource(mygo.ThemeLight)
		}
		// The interface language: the saved one, or the system's when it
		// asks for Chinese. The localizer is read by every frame; mygo's
		// own texts follow once the app is ready.
		locale := i18n.Normalise(settings.LanguageOrDefault())
		i18n.SetLanguage(locale)
		mygo.App.SetLocale(locale.String())
		if settings.CookieConfig().IsValid() {
			if _, err := embedded.Start(settings.Cookie); err != nil {
				// A database or cache path that cannot be opened must not
				// take the window down: the setup/settings screen shows the
				// reason and a save from it retries. Start failures carry
				// their own advice (desktop.StartError.Explain).
				log.Printf("启动内嵌后端失败: %v", err)
				embedded.setStartError(err)
			}
		}
		client := app.NewClient(embedded.BaseURL())
		client.UseImageCache(true)
		a = app.New(client)
		a.SetBuild(appVersion() + " · " + platformLabel())
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
		// The taskbar/title-bar icon (Linux, Windows). A failure leaves the
		// platform default; a missing icon is not worth a dialog.
		if err := win.SetIcon(assets.PNG); err != nil {
			log.Printf("设置窗口图标失败: %v", err)
		}
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
