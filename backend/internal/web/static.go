package web

import (
	"io/fs"
	"net/http"
	"path"
	"strings"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/metrics"
)

const indexPath = "index.html"

// Register wires the embedded frontend into r as the SPA fallback:
//
//   - unmatched /api/* requests answer JSON 404 (never the HTML shell),
//   - every other GET/HEAD serves a file from fsys, falling back to
//     index.html so react-router deep links survive a reload.
//
// fsys nil → nothing is registered and the engine stays API-only.
func Register(r *gin.Engine, fsys fs.FS) {
	if fsys == nil {
		return
	}
	if _, err := fs.Stat(fsys, indexPath); err != nil {
		return
	}
	fileServer := http.FileServer(http.FS(fsys))

	r.NoRoute(func(c *gin.Context) {
		p := c.Request.URL.Path
		if isAPIPath(p) || (c.Request.Method != http.MethodGet && c.Request.Method != http.MethodHead) {
			c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
			return
		}

		name := cleanPath(p)
		if name != "/" && name != "/"+indexPath {
			if fi, err := fs.Stat(fsys, strings.TrimPrefix(name, "/")); err == nil && !fi.IsDir() {
				setCacheControl(c, name)
				// Static files and the SPA shell are served from NoRoute, so
				// FullPath() is empty for them; label them explicitly instead
				// of folding 200s into route="unmatched" with real 404s.
				metrics.SetRouteLabel(c, "/static")
				serveFile(c, fileServer, name)
				return
			}
		}
		// SPA fallback: the HTML shell always, so a deep link survives a
		// reload and a fresh deploy is picked up on the next navigation.
		metrics.SetRouteLabel(c, "/spa")
		serveShell(c, fsys)
	})
}

func isAPIPath(p string) bool {
	return p == "/api" || strings.HasPrefix(p, "/api/")
}

// cleanPath maps any request path onto a root-relative, traversal-free one
// ("../../etc/passwd" → "/etc/passwd"): what fs.Stat expects (no leading
// slash) and what the file server is handed.
func cleanPath(p string) string {
	if p == "" {
		return "/"
	}
	cleaned := path.Clean("/" + strings.TrimPrefix(p, "/"))
	if !strings.HasPrefix(cleaned, "/") {
		cleaned = "/" + cleaned
	}
	return cleaned
}

func setCacheControl(c *gin.Context, name string) {
	switch {
	case strings.HasPrefix(name, "/assets/"):
		// Vite fingerprints these; safe to cache forever.
		c.Header("Cache-Control", "public, max-age=31536000, immutable")
	case name == "/db.text.js":
		// EhTagTranslation dictionary, refreshed daily upstream.
		c.Header("Cache-Control", "public, max-age=86400")
	}
}

// serveShell writes index.html directly. Going through http.FileServer would
// make it issue a canonicalizing redirect (…/index.html → ./) instead of a
// body, and the shell must carry no-store regardless.
func serveShell(c *gin.Context, fsys fs.FS) {
	data, err := fs.ReadFile(fsys, indexPath)
	if err != nil {
		c.Status(http.StatusInternalServerError)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "text/html; charset=utf-8", data)
}

func serveFile(c *gin.Context, fileServer http.Handler, name string) {
	req := c.Request.Clone(c.Request.Context())
	req.URL.Path = name
	fileServer.ServeHTTP(c.Writer, req)
	c.Abort()
}
