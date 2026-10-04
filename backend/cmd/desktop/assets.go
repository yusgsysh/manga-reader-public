package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httputil"
	"net/url"
	"path"
	"strings"
)

// desktopAssets decides what the WebView gets for each request: the in-process
// API, a static file from the built frontend, or the SPA shell.
type desktopAssets struct {
	logger *slog.Logger
	// dist is the embedded frontend with the "dist" directory as its root.
	dist fs.FS
	// index is index.html with the API origin baked in, so the UI never has to
	// guess which port Gin picked.
	index []byte
	proxy http.Handler
}

// newDesktopAssets prepares the WebView's request router for apiBaseURL
// (an absolute http://127.0.0.1:<port> URL). dist is the frontend's dist
// directory as an fs.FS, rooted at index.html.
func newDesktopAssets(dist fs.FS, apiBaseURL string, logger *slog.Logger) (*desktopAssets, error) {
	index, err := fs.ReadFile(dist, "index.html")
	if err != nil {
		return nil, fmt.Errorf("desktop: read frontend index.html (did the frontend build run?): %w", err)
	}
	target, err := url.Parse(apiBaseURL)
	if err != nil || target.Scheme == "" || target.Host == "" {
		return nil, fmt.Errorf("desktop: api base url %q must be an absolute http(s) URL", apiBaseURL)
	}

	return &desktopAssets{
		logger: logger,
		dist:   dist,
		index:  injectRuntimeConfig(index, apiBaseURL),
		proxy:  newAPIProxy(target, logger),
	}, nil
}

// Middleware wraps Wails' static asset handler.
//
//	/api/*, /healthz  -> reverse proxy to the in-process Gin server
//	real static file  -> Wails serves it
//	anything else     -> index.html (SPA route), with the API origin injected
func (d *desktopAssets) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isAPIPath(r.URL.Path) {
			d.proxy.ServeHTTP(w, r)
			return
		}

		if r.Method == http.MethodGet || r.Method == http.MethodHead {
			if name, ok := staticAssetName(r.URL.Path); ok {
				if info, err := fs.Stat(d.dist, name); err == nil && !info.IsDir() {
					next.ServeHTTP(w, r)
					return
				}
			}
		}

		d.serveIndex(w, r)
	})
}

func (d *desktopAssets) serveIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.WriteHeader(http.StatusOK)
	if r.Method == http.MethodHead {
		return
	}
	if _, err := w.Write(d.index); err != nil && d.logger != nil {
		d.logger.Debug("write index.html", "error", err)
	}
}

func isAPIPath(p string) bool {
	return p == "/healthz" || p == "/api" || strings.HasPrefix(p, "/api/")
}

// staticAssetName maps a URL path to an embedded file name, rejecting anything
// that is not a plain relative file path. "/" and "/index.html" are handled by
// the SPA fallback instead.
func staticAssetName(raw string) (string, bool) {
	name := strings.TrimPrefix(path.Clean("/"+raw), "/")
	if name == "" || name == "." || name == "index.html" {
		return "", false
	}
	return name, true
}

// newAPIProxy forwards API traffic to the loopback Gin server. FlushInterval -1
// flushes after every write so ZIP and image streaming stay live.
func newAPIProxy(target *url.URL, logger *slog.Logger) http.Handler {
	proxy := httputil.NewSingleHostReverseProxy(target)
	proxy.FlushInterval = -1
	proxy.ErrorLog = slog.NewLogLogger(logger.Handler(), slog.LevelWarn)
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		logger.Error("desktop api proxy failed", "method", r.Method, "path", r.URL.Path, "error", err)
		http.Error(w, "api unavailable", http.StatusBadGateway)
	}
	director := proxy.Director
	proxy.Director = func(req *http.Request) {
		director(req)
		// The proxy is single-host; announce the loopback target instead of
		// the wails:// origin the WebView sent.
		req.Host = target.Host
	}
	return proxy
}

// injectRuntimeConfig prepends a script that publishes the API origin to the
// frontend. It runs before Wails injects its own runtime.js, so both survive.
func injectRuntimeConfig(html []byte, apiBaseURL string) []byte {
	payload, err := json.Marshal(runtimeConfig{APIBaseURL: apiBaseURL})
	if err != nil {
		payload = []byte(`{"apiBaseUrl":""}`)
	}
	snippet := append([]byte(`<script>window.__MANGA_READER_CONFIG__=`), payload...)
	snippet = append(snippet, []byte(`;</script>`)...)

	if i := indexBeforeHeadClose(html); i >= 0 {
		out := make([]byte, 0, len(html)+len(snippet))
		out = append(out, html[:i]...)
		out = append(out, snippet...)
		out = append(out, html[i:]...)
		return out
	}
	return append(snippet, html...)
}

var headCloseTag = []byte("</head>")

// indexBeforeHeadClose finds "</head>" in html without case-sensitivity and
// without mutating the original bytes.
func indexBeforeHeadClose(html []byte) int {
	for i := 0; i+len(headCloseTag) <= len(html); i++ {
		if bytes.EqualFold(html[i:i+len(headCloseTag)], headCloseTag) {
			return i
		}
	}
	return -1
}

// ServeHTTP implements http.Handler for Wails v3 AssetOptions.Handler
func (d *desktopAssets) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	// Create a file server for the embedded dist
	fileServer := http.FileServer(http.FS(d.dist))
	d.Middleware(fileServer).ServeHTTP(w, r)
}
