package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
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

	// lease is the shared membudget reservation for data. It is acquired once
	// by the singleflight leader and released when the last caller sharing the
	// payload is done, so N concurrent requests for one key reserve the bytes
	// once instead of N times. Every caller's release() drops one reference.
	lease *membudget.Lease
}

// release returns this caller's reference to the shared budget reservation.
// Idempotent, nil-safe; always defer it right after a successful load.
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
	lease, leaseErr := membudget.Acquire(ctx, membudget.ThumbnailWeight)
	if leaseErr != nil {
		// A client that went away is not a budget timeout; don't report it as
		// one (it would pollute the 5xx/budget signals).
		if errors.Is(leaseErr, context.Canceled) {
			c.AbortWithStatus(http.StatusServiceUnavailable)
			return
		}
		c.JSON(http.StatusGatewayTimeout, gin.H{"error": fmt.Sprintf("memory budget wait canceled: %v", leaseErr)})
		return
	}
	defer lease.Release()

	data, contentType, err := fetchThumbnail(ctx, s.Client, rawURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download thumbnail failed: %v", err)})
		return
	}
	if rerr := lease.Resize(ctx, int64(len(data))); rerr != nil {
		slog.Warn("thumbnail budget resize failed", "url", rawURL, "error", rerr)
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
// and whether it was successfully stored (stored). The caller owns one
// reference to the result's shared membudget reservation and must release() it
// once done with the bytes.
//
// The reservation is taken once, inside the flight, by the leader and shared by
// every waiter (via Lease.Retain): a burst of concurrent requests for one image
// is charged for the buffer once, not once per request. Only the leader proves
// its own flag true because singleflight runs the closure on the leader's
// goroutine; each caller has a private copy of the variable.
func (s *Server) loadOrFetchImage(ctx context.Context, key string, decodedURL string) (*imageResult, error) {
	var leader bool
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		leader = true
		// Use a context that won't be cancelled by the leader's cancellation
		// (e.g., client disconnect) so that the fetch and its reservation are
		// not torn down under the waiters. The timeout still bounds the flight.
		flightCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()

		lease, leaseErr := membudget.Acquire(flightCtx, membudget.DefaultWeight)
		if leaseErr != nil {
			return nil, leaseErr
		}

		data, contentType, getErr := s.Cache.Get(flightCtx, key)
		if getErr == nil {
			metrics.ObserveImageCache("page_image", "hit")
			if rerr := lease.Resize(flightCtx, int64(len(data))); rerr != nil {
				slog.Warn("image budget resize failed", "key", key[:16], "error", rerr)
			}
			return &imageResult{data: data, contentType: contentType, cacheHit: true, stored: true, lease: lease}, nil
		}
		if !cache.IsNotFound(getErr) {
			lease.Release()
			return nil, getErr
		}
		metrics.ObserveImageCache("page_image", "miss")

		data, contentType, fetchErr := exhentai.FetchPageImage(flightCtx, s.Client, decodedURL)
		if fetchErr != nil {
			lease.Release()
			s.triggerPageRefresh(flightCtx, decodedURL, fetchErr)
			return nil, fetchErr
		}
		if rerr := lease.Resize(flightCtx, int64(len(data))); rerr != nil {
			slog.Warn("image budget resize failed", "key", key[:16], "error", rerr)
		}

		var stored bool
		if putErr := s.Cache.Put(flightCtx, key, data, contentType, cacheControlHeader); putErr != nil {
			slog.Error("cached-image store failed", "key", key[:16], "error", putErr)
			stored = false
		} else {
			slog.Debug("cached-image stored", "key", key[:16])
			stored = true
		}

		return &imageResult{data: data, contentType: contentType, cacheHit: false, stored: stored, lease: lease}, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	// The singleflight result is shared across callers; give this caller its
	// own header so its reservation is never written by another goroutine. The
	// leader already owns the flight's reservation; a waiter takes a reference.
	shared := v.(*imageResult)
	res := *shared
	if !leader {
		res.lease.Retain()
	}
	return &res, nil
}

const pageFetchRetryDelay = 500 * time.Millisecond

// loadOrFetchThumbnail returns the cached thumbnail for key, fetching it from
// the upstream on a cache miss. Concurrent calls for the same key are coalesced.
// The reservation is taken once by the leader and shared by waiters, exactly as
// in loadOrFetchImage; the caller owns one reference and must release() it.
func (s *Server) loadOrFetchThumbnail(ctx context.Context, key string, rawURL string) (*imageResult, error) {
	var leader bool
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		leader = true
		flightCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 60*time.Second)
		defer cancel()

		lease, leaseErr := membudget.Acquire(flightCtx, membudget.ThumbnailWeight)
		if leaseErr != nil {
			return nil, leaseErr
		}

		data, contentType, getErr := s.Cache.Get(flightCtx, key)
		if getErr == nil {
			metrics.ObserveImageCache("thumbnail", "hit")
			if rerr := lease.Resize(flightCtx, int64(len(data))); rerr != nil {
				slog.Warn("thumbnail budget resize failed", "key", key[:16], "error", rerr)
			}
			return &imageResult{data: data, contentType: contentType, cacheHit: true, stored: true, lease: lease}, nil
		}
		if !cache.IsNotFound(getErr) {
			lease.Release()
			return nil, getErr
		}
		metrics.ObserveImageCache("thumbnail", "miss")

		data, contentType, fetchErr := fetchThumbnail(flightCtx, s.Client, rawURL)
		if fetchErr != nil {
			lease.Release()
			return nil, fetchErr
		}
		if rerr := lease.Resize(flightCtx, int64(len(data))); rerr != nil {
			slog.Warn("thumbnail budget resize failed", "key", key[:16], "error", rerr)
		}

		var stored bool
		if putErr := s.Cache.PutWithMeta(flightCtx, key, data, contentType, map[string]string{
			"source-url": rawURL,
		}, cacheControlHeader); putErr != nil {
			slog.Error("cached-thumbnail store failed", "key", key[:16], "error", putErr)
			stored = false
		} else {
			slog.Debug("cached-thumbnail stored", "key", key[:16])
			stored = true
		}

		return &imageResult{data: data, contentType: contentType, cacheHit: false, stored: stored, lease: lease}, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	shared := v.(*imageResult)
	res := *shared
	if !leader {
		res.lease.Retain()
	}
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
