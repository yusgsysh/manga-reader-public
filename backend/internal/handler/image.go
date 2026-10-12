package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"

	"manga-reader/internal/cache"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/membudget"
	"manga-reader/internal/metrics"
	"manga-reader/internal/ttl"
)

const (
	thumbnailCachePrefix = "thumbnail/"
	maxThumbnailRetries  = 2
)

var (
	thumbnailCacheControl = ttl.PublicMaxAge(ttl.ImageThumbnailHTTP)
	cacheControlHeader    = ttl.PublicImmutableMaxAge(ttl.ImageImmutableHTTP)
)

type imageResult struct {
	data        []byte
	contentType string
	cacheHit    bool
	stored      bool

	// lease is this caller's membudget reservation for data. The payload
	// itself is shared with every waiter of the same singleflight, but each
	// caller holds its own reservation until it stops using the bytes.
	lease *membudget.Lease
}

// release returns the caller's budget reservation. Idempotent, nil-safe;
// always defer it right after a successful load.
func (r *imageResult) release() {
	if r != nil {
		r.lease.Release()
	}
}

var cachedImageGroup singleflight.Group

func ThumbnailCacheKey(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return thumbnailCachePrefix + hex.EncodeToString(sum[:])
}

func fetchThumbnail(ctx context.Context, client *http.Client, thumbnailURL string) (data []byte, contentType string, err error) {
	data, contentType, err = exhentai.ProxyImage(ctx, client, thumbnailURL)
	if err == nil {
		return data, contentType, nil
	}
	if exhentai.IsPermanentUpstreamError(err) {
		return nil, "", err
	}
	for range maxThumbnailRetries {
		metrics.ObserveUpstreamRetry(metrics.Endpoint(thumbnailURL), "thumbnail")
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

// writeCachedStream serves a cache hit by copying the object straight from
// the store to the client, so warm reads never allocate an in-memory copy of
// the image — this is what keeps image-heavy browsing off the heap. It
// reports whether the response was written; a miss (or any stream error)
// reports false so the caller falls back to the buffered fetch path, which
// owns error classification and the miss metric.
func (s *Server) writeCachedStream(c *gin.Context, key string, metricKind string) bool {
	rc, contentType, size, err := s.Cache.GetStream(c.Request.Context(), key)
	if err != nil {
		return false
	}
	defer rc.Close()

	metrics.ObserveImageCache(metricKind, "hit")
	c.Header("Cache-Control", cacheControlHeader)
	c.Header("Content-Type", contentType)
	c.Header("Content-Length", strconv.FormatInt(size, 10))
	c.Status(http.StatusOK)
	if _, err := io.Copy(c.Writer, io.LimitReader(rc, size)); err != nil {
		// Status and headers are already on the wire: log and cut the
		// response short rather than attempting an error body.
		slog.Error("cached stream copy failed", "key", key[:16], "error", err)
	}
	return true
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

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

	// Thumbnails are small, but the reservation keeps the concurrent sum of
	// live thumbnail buffers inside the global budget.
	lease, leaseErr := membudget.Acquire(ctx, membudget.DefaultWeight)
	if leaseErr != nil {
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": fmt.Sprintf("memory budget wait canceled: %v", leaseErr)})
		return
	}
	defer lease.Release()

	data, contentType, err := fetchThumbnail(ctx, s.Client, rawURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download thumbnail failed: %v", err)})
		return
	}
	lease.Resize(int64(len(data)))

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

	if s.writeCachedStream(c, key, "thumbnail") {
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
	defer result.release()

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

	if s.writeCachedStream(c, key, "page_image") {
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
	defer result.release()

	c.Header("Cache-Control", cacheControlHeader)
	c.Data(http.StatusOK, result.contentType, result.data)
}

// loadOrFetchImage returns the cached image for key, fetching it from the
// upstream page URL on a cache miss. Concurrent calls for the same key are
// coalesced through the shared singleflight group, so the prefill worker, the
// streaming ZIP endpoint and handleCachedImage never fetch the same URL twice.
// The returned imageResult indicates whether the data came from cache (cacheHit)
// and whether it was successfully stored (stored). The caller owns the result's
// membudget reservation and must release() it once done with the bytes.
func (s *Server) loadOrFetchImage(ctx context.Context, key string, decodedURL string) (*imageResult, error) {
	lease, leaseErr := membudget.Acquire(ctx, membudget.DefaultWeight)
	if leaseErr != nil {
		return nil, leaseErr
	}
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		// Use a context that won't be cancelled by the leader's cancellation
		// (e.g., client disconnect) so that waiters aren't affected.
		// The outer context still bounds the operation.
		fetchCtx := context.WithoutCancel(ctx)
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
		defer cancel()

		data, contentType, getErr := s.Cache.Get(fetchCtx, key)
		if getErr == nil {
			metrics.ObserveImageCache("page_image", "hit")
			return &imageResult{data: data, contentType: contentType, cacheHit: true, stored: true}, nil
		}
		if !cache.IsNotFound(getErr) {
			return nil, getErr
		}
		metrics.ObserveImageCache("page_image", "miss")

		data, contentType, fetchErr := exhentai.FetchPageImage(fetchCtx, s.Client, decodedURL)
		if fetchErr != nil {
			s.triggerPageRefresh(fetchCtx, decodedURL, fetchErr)
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
		lease.Release()
		return nil, sfErr
	}
	// The singleflight result is shared across waiters; give this caller its
	// own view so its reservation is never written by another goroutine.
	shared := v.(*imageResult)
	res := *shared
	res.lease = lease
	res.lease.Resize(int64(len(res.data)))
	return &res, nil
}

const pageFetchRetryDelay = 500 * time.Millisecond

// loadOrFetchThumbnail returns the cached thumbnail for key, fetching it from
// the upstream on a cache miss. Concurrent calls for the same key are coalesced.
// The caller owns the result's membudget reservation and must release() it.
func (s *Server) loadOrFetchThumbnail(ctx context.Context, key string, rawURL string) (*imageResult, error) {
	lease, leaseErr := membudget.Acquire(ctx, membudget.DefaultWeight)
	if leaseErr != nil {
		return nil, leaseErr
	}
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		fetchCtx := context.WithoutCancel(ctx)
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
		defer cancel()

		data, contentType, getErr := s.Cache.Get(fetchCtx, key)
		if getErr == nil {
			metrics.ObserveImageCache("thumbnail", "hit")
			return &imageResult{data: data, contentType: contentType, cacheHit: true, stored: true}, nil
		}
		if !cache.IsNotFound(getErr) {
			return nil, getErr
		}
		metrics.ObserveImageCache("thumbnail", "miss")

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
		lease.Release()
		return nil, sfErr
	}
	shared := v.(*imageResult)
	res := *shared
	res.lease = lease
	res.lease.Resize(int64(len(res.data)))
	return &res, nil
}

// triggerPageRefresh maps a permanently failed page URL back to its gallery and
// starts a background cache refresh so a stale cached page list self-heals.
// Transient upstream errors are ignored. The refresh is best-effort: it never
// blocks or fails the image request, and concurrent failures for the same
// gallery collapse into one walk via the fill group.
func (s *Server) triggerPageRefresh(ctx context.Context, pageURL string, fetchErr error) {
	if !exhentai.IsPermanentUpstreamError(fetchErr) {
		return
	}
	if s.cacheDB() == nil {
		return
	}
	galleryID, ok := exhentai.PageURLGalleryID(pageURL)
	if !ok {
		return
	}
	row, found, err := gallerycache.GetByGalleryID(ctx, s.cacheDB(), galleryID)
	if err != nil || !found {
		return
	}
	slog.Info("cached page URL failed; refreshing gallery cache", "id", galleryID, "error", fetchErr)
	go s.refreshPages(ctx, galleryID, row.Token)
}
