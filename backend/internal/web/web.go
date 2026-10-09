// Package web embeds the built frontend (Vite dist) into the backend binary
// and serves it from Gin, so a deployment ships exactly one image with no
// externally mounted static files.
//
// The image build copies frontend/dist into internal/web/dist before
// `go build`. A source checkout without that step only carries dist/.gitkeep,
// and the binary runs API-only (FS returns nil).
package web

import (
	"embed"
	"io/fs"
)

//go:embed all:dist
var embedded embed.FS

// FS returns the embedded frontend rooted at dist, or nil when the binary was
// built without frontend assets.
func FS() fs.FS {
	sub, err := fs.Sub(embedded, "dist")
	if err != nil {
		return nil
	}
	if _, err := fs.Stat(sub, "index.html"); err != nil {
		return nil
	}
	return sub
}
