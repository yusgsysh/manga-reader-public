package handler

import (
	"context"
	"net/http"
	"sync"

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

type Server struct {
	Client *http.Client
	DB     *database.DB
	Cache  ImageCache

	prefillOnce sync.Once
	prefillMgr  *prefillManager
}

type Config struct {
	Client *http.Client
	DB     *database.DB
	Cache  ImageCache
}

func New(cfg Config) *Server {
	return &Server{
		Client: cfg.Client,
		DB:     cfg.DB,
		Cache:  cfg.Cache,
	}
}

func (s *Server) RegisterRoutes(r *gin.Engine) {
	r.GET("/api/gallery/:id/:token", s.handleGetGallery)
	r.GET("/api/gallery/:id/:token/details", s.handleGalleryDetails)
	r.GET("/api/gallery/:id/:token/pages", s.handleGalleryPages)
	r.GET("/api/search", s.handleSearch)
	r.GET("/api/page-image", s.handlePageImage)
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

	r.GET("/api/thumbnail", s.handleThumbnail)
	r.GET("/api/cached-thumbnail", s.handleCachedThumbnail)
	r.GET("/api/cached-image", s.handleCachedImage)

	r.POST("/api/prefill", s.handlePrefillStart)
	r.GET("/api/prefill", s.handlePrefillList)
	r.POST("/api/prefill/cleanup", s.handlePrefillCleanup)
	r.GET("/api/prefill/:id", s.handlePrefillGet)
	r.POST("/api/prefill/:id/cancel", s.handlePrefillCancel)
	r.DELETE("/api/prefill/:id", s.handlePrefillDelete)
	r.GET("/api/prefill/:id/zip", s.handlePrefillZip)
	r.HEAD("/api/prefill/:id/zip", s.handlePrefillZipHead)
}
