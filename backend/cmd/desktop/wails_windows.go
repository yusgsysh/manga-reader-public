//go:build windows

package main

import (
	"context"
	"errors"
	"runtime"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"

	"golang.org/x/sys/windows"
)

const (
	// RPC_E_CHANGED_MODE (0x80010106) indicates the thread is already
	// initialized with a different apartment model.
	RPC_E_CHANGED_MODE = windows.Errno(0x80010106)
)

// runWails runs the Wails application on Windows with proper COM initialization.
// It locks the current OS thread, initializes COM for the WebView2 WebView,
// runs Wails, and uninitializes COM when Wails returns.
func runWails(
	ctx context.Context,
	title string,
	width, height, minWidth, minHeight int,
	assets *assetserver.Options,
	onShutdown func(context.Context),
) error {
	// Lock the OS thread so COM initialization stays on this thread.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	// Initialize COM for this thread. WebView2 requires an STA apartment.
	// COINIT_APARTMENTTHREADED = 0x2
	err := windows.CoInitializeEx(0, windows.COINIT_APARTMENTTHREADED)
	var errno windows.Errno
	comInitialized := false
	if err == nil {
		comInitialized = true
	} else if errors.As(err, &errno) && errno == RPC_E_CHANGED_MODE {
		// Thread already initialized with a different apartment model.
		// We proceed but cannot call CoUninitialize.
	} else {
		return err
	}

	// Run Wails on this locked thread.
	err = wails.Run(&options.App{
		Title:     title,
		Width:     width,
		Height:    height,
		MinWidth:  minWidth,
		MinHeight: minHeight,
		AssetServer: &assetserver.Options{
			Assets:     assets.Assets,
			Middleware: assets.Middleware,
		},
		OnShutdown: onShutdown,
	})

	// Uninitialize COM if we initialized it.
	if comInitialized {
		windows.CoUninitialize()
	}

	return err
}