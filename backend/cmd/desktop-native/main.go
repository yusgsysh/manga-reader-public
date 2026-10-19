// Command desktop-native is the MyGo native-UI desktop app: one Go binary,
// no webview and no frontend build. It is the replacement for the React
// frontend and the Wails shell.
//
// It talks to the Manga Reader API at MANGA_READER_API, or
// http://127.0.0.1:8080 when the variable is unset:
//
//	MANGA_READER_API=http://127.0.0.1:8080 go run ./cmd/desktop-native
package main

import (
	"log"
	"os"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app"
)

func main() {
	baseURL := os.Getenv("MANGA_READER_API")
	if baseURL == "" {
		baseURL = "http://127.0.0.1:8080"
	}

	a := app.New(app.NewClient(baseURL))
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
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
