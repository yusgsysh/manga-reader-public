package handler

import (
	"context"
	"io"
	"net/http"
	"sync"
	"sync/atomic"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"

	"manga-reader/internal/database"
)

type ImageCache interface {
	Head(ctx context.Context, key string) (minio.ObjectInfo, error)
	Get(ctx context.Context, key string) (data []byte, contentType string, err error)
	Put(ctx context.Context, key string, data []byte, contentType string, cacheControl string) error
	PutWithMeta(ctx context.Context, key string, data []byte, contentType string, meta map[string]string, cacheControl string) error
}

// streamCache is implemented by caches that can serve objects without buffering
// them fully in memory. Handlers type-assert the configured cache against it.
type streamCache interface {
	Open(ctx context.Context, key string) (io.ReadCloser, minio.ObjectInfo, error)
}

type Server struct {
	Client *http.Client
	DB     *database.DB
	Cache  ImageCache

	// devTools enables the /api/dev/* debug endpoints; simulateUpstreamDown is
	// a runtime switch they toggle to make upstream-backed endpoints fail.
	devTools             bool
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
}

func New(cfg Config) *Server {
	return &Server{
		Client:   cfg.Client,
		DB:       cfg.DB,
		Cache:    cfg.Cache,
		devTools: cfg.DevTools,
	}
}

func (s *Server) RegisterRoutes(r *gin.Engine) {
	// Simulated upstream outage must be checked before any upstream handler.
	r.Use(s.upstreamSimulationMiddleware())

	if s.devTools {
		r.GET("/api/dev/upstream-down", s.handleDevUpstreamDownGet)
		r.PUT("/api/dev/upstream-down", s.handleDevUpstreamDownPut)
	}

	r.GET("/api/gallery/:id/:token", s.handleGetGallery)
	r.GET("/api/gallery/:id/:token/details", s.handleGalleryDetails)
	r.GET("/api/gallery/:id/:token/pages", s.handleGalleryPages)

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

	// Image proxy caches (MinIO read-through).
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
