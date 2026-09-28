package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"net/url"
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
}

var cachedImageGroup singleflight.Group

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
		data, contentType, err = exhentai.ProxyImage(ctx, client, thumbnailURL)
		if err == nil {
			return data, contentType, nil
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

	if _, err := s.Cache.Head(ctx, key); err == nil {
		data, contentType, err := s.Cache.Get(ctx, key)
		if err != nil {
			log.Printf("cached-thumbnail error key=%s err=%v", key[:16], err)
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("cache read failed: %v", err)})
			return
		}
		log.Printf("cached-thumbnail hit key=%s", key[:16])
		c.Header("Cache-Control", cacheControlHeader)
		c.Data(http.StatusOK, contentType, data)
		return
	} else if !isNotFound(err) {
		log.Printf("cached-thumbnail error key=%s err=%v", key[:16], err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("cache check failed: %v", err)})
		return
	}

	log.Printf("cached-thumbnail miss key=%s", key[:16])

	v, sfErr, _ := cachedImageGroup.Do(key, func() (interface{}, error) {
		if _, headErr := s.Cache.Head(ctx, key); headErr == nil {
			data, contentType, getErr := s.Cache.Get(ctx, key)
			if getErr != nil {
				return nil, getErr
			}
			return &imageResult{data: data, contentType: contentType}, nil
		}

		data, contentType, fetchErr := fetchThumbnail(ctx, s.Client, rawURL)
		if fetchErr != nil {
			return nil, fetchErr
		}

		if putErr := s.Cache.PutWithMeta(ctx, key, data, contentType, map[string]string{
			"source-url": rawURL,
		}, cacheControlHeader); putErr != nil {
			log.Printf("cached-thumbnail store failed key=%s err=%v", key[:16], putErr)
		} else {
			log.Printf("cached-thumbnail stored key=%s", key[:16])
		}

		return &imageResult{data: data, contentType: contentType}, nil
	})

	if sfErr != nil {
		log.Printf("cached-thumbnail error key=%s err=%v", key[:16], sfErr)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download thumbnail failed: %v", sfErr)})
		return
	}

	result := v.(*imageResult)
	c.Header("Cache-Control", cacheControlHeader)
	c.Data(http.StatusOK, result.contentType, result.data)
}

func (s *Server) handleCachedImage(c *gin.Context) {
	rawURL := c.Query("url")
	if rawURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing url parameter"})
		return
	}

	decodedURL, err := url.QueryUnescape(rawURL)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid url"})
		return
	}

	if err := exhentai.ValidatePageURL(decodedURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	key := cache.CacheKey(decodedURL)

	if s.Cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cache not configured"})
		return
	}

	if _, err := s.Cache.Head(ctx, key); err == nil {
		data, contentType, err := s.Cache.Get(ctx, key)
		if err != nil {
			log.Printf("cached-image error key=%s err=%v", key[:16], err)
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("cache read failed: %v", err)})
			return
		}
		log.Printf("cached-image hit key=%s", key[:16])
		c.Header("Cache-Control", cacheControlHeader)
		c.Data(http.StatusOK, contentType, data)
		return
	} else if !isNotFound(err) {
		log.Printf("cached-image error key=%s err=%v", key[:16], err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("cache check failed: %v", err)})
		return
	}

	log.Printf("cached-image miss key=%s", key[:16])

	result, err := s.loadOrFetchImage(ctx, key, decodedURL)
	if err != nil {
		log.Printf("cached-image error key=%s err=%v", key[:16], err)
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
func (s *Server) loadOrFetchImage(ctx context.Context, key string, decodedURL string) (*imageResult, error) {
	v, sfErr, _ := cachedImageGroup.Do(key, func() (interface{}, error) {
		if _, headErr := s.Cache.Head(ctx, key); headErr == nil {
			data, contentType, getErr := s.Cache.Get(ctx, key)
			if getErr != nil {
				return nil, getErr
			}
			return &imageResult{data: data, contentType: contentType}, nil
		}

		data, contentType, fetchErr := exhentai.FetchPageImage(ctx, s.Client, decodedURL)
		if fetchErr != nil {
			return nil, fetchErr
		}

		if putErr := s.Cache.Put(ctx, key, data, contentType, cacheControlHeader); putErr != nil {
			log.Printf("cached-image store failed key=%s err=%v", key[:16], putErr)
		} else {
			log.Printf("cached-image stored key=%s", key[:16])
		}

		return &imageResult{data: data, contentType: contentType}, nil
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

// fetchWithRetry loads an image through loadOrFetchImage, retrying transient
// failures up to pageFetchMaxAttempts times. It respects ctx cancellation and
// never retries after the context is done.
func (s *Server) fetchWithRetry(ctx context.Context, key string, decodedURL string) (*imageResult, error) {
	var lastErr error
	for attempt := 0; attempt < pageFetchMaxAttempts; attempt++ {
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
	}
	return nil, lastErr
}
