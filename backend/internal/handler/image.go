package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"

	"manga-reader/internal/cache"
	"manga-reader/internal/exhentai"
)

const (
	thumbnailCachePrefix  = "thumbnail/"
	maxThumbnailRetries   = 2
	thumbnailCacheControl = "public, max-age=3600"
	cacheControlHeader    = "public, max-age=31536000, immutable"
)

type imageResult struct {
	data        []byte
	contentType string
	cacheHit    bool
	stored      bool
}

var cachedImageGroup singleflight.Group

// streamCached writes a cache hit straight to the client without buffering the
// whole object in memory. It returns false when the cache backend does not
// support streaming, the object is missing, or opening failed, in which case
// the caller falls back to the buffered load-or-fetch path.
func (s *Server) streamCached(c *gin.Context, key string) bool {
	sc, ok := s.Cache.(streamCache)
	if !ok {
		return false
	}
	rc, info, err := sc.Open(c.Request.Context(), key)
	if err != nil {
		return false
	}
	defer rc.Close()

	c.Header("Cache-Control", cacheControlHeader)
	c.DataFromReader(http.StatusOK, info.Size, info.ContentType, rc, nil)
	return true
}

func ThumbnailCacheKey(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return thumbnailCachePrefix + hex.EncodeToString(sum[:])
}

func isNotFound(err error) bool {
	return cache.IsNotFound(err)
}

func fetchThumbnail(ctx context.Context, client *http.Client, thumbnailURL string) (data []byte, contentType string, err error) {
	data, contentType, err = exhentai.ProxyImage(ctx, client, thumbnailURL)
	if err == nil {
		return data, contentType, nil
	}
	for range maxThumbnailRetries {
		select {
		case <-ctx.Done():
			return nil, "", ctx.Err()
		case <-time.After(pageFetchRetryDelay):
		}
		data, contentType, err = exhentai.ProxyImage(ctx, client, thumbnailURL)
		if err == nil {
			return data, contentType, nil
		}
		if exhentai.IsPermanentUpstreamError(err) {
			return nil, "", err
		}
	}
	return nil, "", err
}

func (s *Server) handleThumbnail(c *gin.Context) {
	rawURL := c.Query("url")
	if rawURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing url parameter"})
		return
	}

	if err := exhentai.ValidateThumbnailURL(rawURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	data, contentType, err := fetchThumbnail(ctx, s.Client, rawURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download thumbnail failed: %v", err)})
		return
	}

	c.Header("Cache-Control", thumbnailCacheControl)
	c.Data(http.StatusOK, contentType, data)
}

func (s *Server) handleCachedThumbnail(c *gin.Context) {
	rawURL := c.Query("url")
	if rawURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing url parameter"})
		return
	}

	if err := exhentai.ValidateThumbnailURL(rawURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	key := ThumbnailCacheKey(rawURL)

	if s.Cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cache not configured"})
		return
	}

	if s.streamCached(c, key) {
		return
	}

	result, err := s.loadOrFetchThumbnail(ctx, key, rawURL)
	if err != nil {
		if cache.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "thumbnail not found"})
			return
		}
		slog.Error("cached-thumbnail error", "key", key[:16], "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download thumbnail failed: %v", err)})
		return
	}

	c.Header("Cache-Control", cacheControlHeader)
	c.Data(http.StatusOK, result.contentType, result.data)
}

func (s *Server) handleCachedImage(c *gin.Context) {
	rawURL := c.Query("url")
	if rawURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing url parameter"})
		return
	}

	if err := exhentai.ValidatePageURL(rawURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	key := cache.CacheKey(rawURL)

	if s.Cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cache not configured"})
		return
	}

	if s.streamCached(c, key) {
		return
	}

	result, err := s.loadOrFetchImage(ctx, key, rawURL)
	if err != nil {
		if cache.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "image not found"})
			return
		}
		slog.Error("cached-image error", "key", key[:16], "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download image failed: %v", err)})
		return
	}

	c.Header("Cache-Control", cacheControlHeader)
	c.Data(http.StatusOK, result.contentType, result.data)
}

// loadOrFetchImage returns the cached image for key, fetching it from the
// upstream page URL on a cache miss. Concurrent calls for the same key are
// coalesced through the shared singleflight group, so the prefill worker, the
// streaming ZIP endpoint and handleCachedImage never fetch the same URL twice.
// The returned imageResult indicates whether the data came from cache (cacheHit)
// and whether it was successfully stored (stored).
func (s *Server) loadOrFetchImage(ctx context.Context, key string, decodedURL string) (*imageResult, error) {
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		// Use a context that won't be cancelled by the leader's cancellation
		// (e.g., client disconnect) so that waiters aren't affected.
		// The outer context still bounds the operation.
		fetchCtx := context.WithoutCancel(ctx)
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
		defer cancel()

		data, contentType, getErr := s.Cache.Get(fetchCtx, key)
		if getErr == nil {
			return &imageResult{data: data, contentType: contentType, cacheHit: true, stored: true}, nil
		}
		if !cache.IsNotFound(getErr) {
			return nil, getErr
		}

		data, contentType, fetchErr := exhentai.FetchPageImage(fetchCtx, s.Client, decodedURL)
		if fetchErr != nil {
			return nil, fetchErr
		}

		var stored bool
		if putErr := s.Cache.Put(fetchCtx, key, data, contentType, cacheControlHeader); putErr != nil {
			slog.Error("cached-image store failed", "key", key[:16], "error", putErr)
			stored = false
		} else {
			slog.Debug("cached-image stored", "key", key[:16])
			stored = true
		}

		return &imageResult{data: data, contentType: contentType, cacheHit: false, stored: stored}, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	return v.(*imageResult), nil
}

const (
	pageFetchMaxAttempts = 3
	pageFetchRetryDelay  = 500 * time.Millisecond
)

// loadOrFetchThumbnail returns the cached thumbnail for key, fetching it from
// the upstream on a cache miss. Concurrent calls for the same key are coalesced.
func (s *Server) loadOrFetchThumbnail(ctx context.Context, key string, rawURL string) (*imageResult, error) {
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		fetchCtx := context.WithoutCancel(ctx)
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
		defer cancel()

		data, contentType, getErr := s.Cache.Get(fetchCtx, key)
		if getErr == nil {
			return &imageResult{data: data, contentType: contentType, cacheHit: true, stored: true}, nil
		}
		if !cache.IsNotFound(getErr) {
			return nil, getErr
		}

		data, contentType, fetchErr := fetchThumbnail(fetchCtx, s.Client, rawURL)
		if fetchErr != nil {
			return nil, fetchErr
		}

		var stored bool
		if putErr := s.Cache.PutWithMeta(fetchCtx, key, data, contentType, map[string]string{
			"source-url": rawURL,
		}, cacheControlHeader); putErr != nil {
			slog.Error("cached-thumbnail store failed", "key", key[:16], "error", putErr)
			stored = false
		} else {
			slog.Debug("cached-thumbnail stored", "key", key[:16])
			stored = true
		}

		return &imageResult{data: data, contentType: contentType, cacheHit: false, stored: stored}, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	return v.(*imageResult), nil
}

// fetchWithRetry loads an image through loadOrFetchImage, retrying transient
// failures up to pageFetchMaxAttempts times. It respects ctx cancellation and
// never retries after the context is done. Permanent upstream errors (IP banned,
// sad panda, 4xx errors) are not retried.
func (s *Server) fetchWithRetry(ctx context.Context, key string, decodedURL string) (*imageResult, error) {
	var lastErr error
	for attempt := range pageFetchMaxAttempts {
		if attempt > 0 {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(pageFetchRetryDelay):
			}
		}
		result, err := s.loadOrFetchImage(ctx, key, decodedURL)
		if err == nil {
			return result, nil
		}
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		lastErr = err
		if exhentai.IsPermanentUpstreamError(err) {
			return nil, err
		}
	}
	return nil, lastErr
}
