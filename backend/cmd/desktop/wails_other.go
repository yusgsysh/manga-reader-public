//go:build !windows

package main

import (
	"context"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
)

// runWails runs the Wails application on non-Windows platforms.
// No COM initialization is needed.
func runWails(
	ctx context.Context,
	title string,
	width, height, minWidth, minHeight int,
	assets *assetserver.Options,
	onShutdown func(context.Context),
) error {
	return wails.Run(&options.App{
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
}
