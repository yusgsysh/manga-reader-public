//go:build android

package main

import "github.com/wailsapp/wails/v3/pkg/application"

// On Android the Go code is compiled to a C shared library (libwails.so). The
// Java host (com.wails.app.WailsBridge) calls nativeInit, which runs the
// registered main function on a goroutine. We reuse the same run() as the
// desktop build: it boots the in-process Gin API, serves the embedded frontend
// through Wails' asset server (which injects window.__MANGA_READER_CONFIG__ with
// the loopback API URL) and opens a fullscreen window.
func init() {
	application.RegisterAndroidMain(main)
}
