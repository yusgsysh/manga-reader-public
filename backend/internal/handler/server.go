package handler

import (
	"context"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/database"
	"manga-reader/internal/settings"
	"manga-reader/internal/storage"
	synclib "manga-reader/internal/sync"
)

// ImageCache is the slice of storage.Storage the handlers actually use. The
// concrete backend (local files or S3) is decided outside this package, so no
// handler ever names a vendor SDK.
type ImageCache interface {
	Get(ctx context.Context, key string) (data []byte, contentType string, err error)
	Put(ctx context.Context, key string, data []byte, contentType string, opts storage.PutOptions) error
	Exists(ctx context.Context, key string) (bool, error)
}

type Server struct {
	Client *http.Client
	DB     *database.DB
	Cache  ImageCache

	// settings is the live configuration service backing /api/settings; it is
	// nil in tests that do not care about it, which leaves the route unmounted.
	settings *settings.Service

	// syncSvc serves the sync protocol endpoints and records outbox entries
	// for local mutations of synced tables. Optional (nil disables sync).
	syncSvc *synclib.Service

	// devTools enables the /api/dev/* debug endpoints and simulateUpstreamDown
	// is the runtime switch they toggle to make upstream-backed endpoints fail.
	// Both are atomic because the settings page can change them while requests
	// are in flight.
	devTools             atomic.Bool
	simulateUpstreamDown atomic.Bool

	prefillOnce sync.Once
	prefillMu   sync.Mutex
	prefillMgr  *prefillManager
}

type Config struct {
	Client   *http.Client
	DB       *database.DB
	Cache    ImageCache
	DevTools bool
	// Settings backs /api/settings. When nil the route is not mounted.
	Settings *settings.Service
	// Sync serves the sync protocol endpoints. When nil they are not mounted.
	Sync *synclib.Service
}

func New(cfg Config) *Server {
	s := &Server{
		Client:   cfg.Client,
		DB:       cfg.DB,
		Cache:    cfg.Cache,
		settings: cfg.Settings,
		syncSvc:  cfg.Sync,
	}
	s.devTools.Store(cfg.DevTools)
	return s
}

// SetDevTools toggles the /api/dev/* endpoints. The routes are always mounted
// and gate on this flag so the settings page can turn them on without a
// restart; when disabled they answer 404, exactly as an unmounted route would.
func (s *Server) SetDevTools(enabled bool) { s.devTools.Store(enabled) }

// DevToolsEnabled reports whether the /api/dev/* endpoints are available.
func (s *Server) DevToolsEnabled() bool { return s.devTools.Load() }

func (s *Server) RegisterRoutes(r *gin.Engine) {
	// Simulated upstream outage must be checked before any upstream handler.
	r.Use(s.upstreamSimulationMiddleware())

	// Mounted unconditionally: the handlers gate on the live devTools flag so
	// the setting can be toggled at runtime.
	r.GET("/api/dev/upstream-down", s.handleDevUpstreamDownGet)
	r.PUT("/api/dev/upstream-down", s.handleDevUpstreamDownPut)

	if s.syncSvc != nil {
		s.syncSvc.RegisterRoutes(r)
	}

	if s.settings != nil {
		r.GET("/api/settings", s.handleSettingsGet)
		r.PUT("/api/settings", s.handleSettingsPut)
	}

	r.GET("/api/gallery/:id/:token", s.handleGetGallery)
	r.GET("/api/gallery/:id/:token/details", s.handleGalleryDetails)
	r.GET("/api/gallery/:id/:token/pages", s.handleGalleryPages)
	r.GET("/api/gallery/:id/:token/torrents", s.handleGalleryTorrents)
	r.GET("/api/gallery/:id/:token/torrents/:gtid/info", s.handleGalleryTorrentInfo)
	r.GET("/api/gallery/:id/:token/torrents/:gtid/download", s.handleGalleryTorrentDownload)

	// Offline cache (read-only fallback; the frontend decides when to use it).
	r.GET("/api/gallery-cache/:id/:token", s.handleCachedGallery)
	r.GET("/api/gallery-cache/:id/:token/details", s.handleCachedGalleryDetails)
	r.GET("/api/gallery-cache/:id/:token/pages", s.handleCachedGalleryPages)
	r.GET("/api/search", s.handleSearch)
	r.GET("/api/galleries", s.handleGalleries)
	r.GET("/api/watched", s.handleWatched)
	r.GET("/api/popular", s.handlePopular)

	r.GET("/api/bookshelf", s.handleBookshelfList)
	r.POST("/api/bookshelf/:id/:token", s.handleBookshelfAdd)
	r.DELETE("/api/bookshelf/:id/:token", s.handleBookshelfRemove)
	r.GET("/api/bookshelf/:id/:token/status", s.handleBookshelfStatus)
	r.GET("/api/progress/:id/:token", s.handleGetProgress)
	r.PUT("/api/progress/:id/:token", s.handleUpdateProgress)
	r.GET("/api/recently-read", s.handleRecentlyRead)
	r.POST("/api/reading-progress/cleanup", s.handleReadingProgressCleanup)

	// Image proxies (live): fetched straight from upstream, no caching.
	r.GET("/api/image/page", s.handlePageImage)
	r.GET("/api/image/thumbnail", s.handleThumbnail)
	r.GET("/api/image/page-thumbnail", s.handlePageThumbnail)

	// Image proxy caches (read-through object storage).
	r.GET("/api/image-cache/page", s.handleCachedImage)
	r.GET("/api/image-cache/thumbnail", s.handleCachedThumbnail)
	r.GET("/api/image-cache/page-thumbnail", s.handleCachedPageThumbnail)

	r.POST("/api/prefill", s.handlePrefillStart)
	r.GET("/api/prefill", s.handlePrefillList)
	r.POST("/api/prefill/cleanup", s.handlePrefillCleanup)
	r.GET("/api/prefill/:id", s.handlePrefillGet)
	r.POST("/api/prefill/:id/cancel", s.handlePrefillCancel)
	r.DELETE("/api/prefill/:id", s.handlePrefillDelete)
	r.GET("/api/prefill/:id/zip", s.handlePrefillZip)
	r.HEAD("/api/prefill/:id/zip", s.handlePrefillZipHead)
}
