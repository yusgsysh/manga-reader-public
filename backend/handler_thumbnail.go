package main

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
	"github.com/minio/minio-go/v7"
	"golang.org/x/sync/singleflight"

	"manga-reader/internal/cache"
	"manga-reader/internal/exhentai"
)

const (
	thumbnailCachePrefix  = "thumbnail/"
	maxThumbnailRetries   = 2
	thumbnailCacheControl = "public, max-age=3600"
)

type ImageCache interface {
	Head(ctx context.Context, key string) (minio.ObjectInfo, error)
	Get(ctx context.Context, key string) (data []byte, contentType string, err error)
	PutWithMeta(ctx context.Context, key string, data []byte, contentType string, meta map[string]string, cacheControl string) error
}

type ThumbnailApp struct {
	Client *http.Client
	Cache  ImageCache
}

func ThumbnailCacheKey(rawURL string) string {
	sum := sha256.Sum256([]byte(rawURL))
	return thumbnailCachePrefix + hex.EncodeToString(sum[:])
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

func (a *ThumbnailApp) handleThumbnail(c *gin.Context) {
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
	data, contentType, err := fetchThumbnail(ctx, a.Client, rawURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download thumbnail failed: %v", err)})
		return
	}

	c.Header("Cache-Control", thumbnailCacheControl)
	c.Data(http.StatusOK, contentType, data)
}

func (a *ThumbnailApp) handleCachedThumbnail(c *gin.Context) {
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

	if a.Cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cache not configured"})
		return
	}

	if _, err := a.Cache.Head(ctx, key); err == nil {
		data, contentType, err := a.Cache.Get(ctx, key)
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
		if _, headErr := a.Cache.Head(ctx, key); headErr == nil {
			data, contentType, getErr := a.Cache.Get(ctx, key)
			if getErr != nil {
				return nil, getErr
			}
			return &imageResult{data: data, contentType: contentType}, nil
		}

		data, contentType, fetchErr := fetchThumbnail(ctx, a.Client, rawURL)
		if fetchErr != nil {
			return nil, fetchErr
		}

		if putErr := a.Cache.PutWithMeta(ctx, key, data, contentType, map[string]string{
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

var cachedImageGroup singleflight.Group

func isNotFound(err error) bool {
	return cache.IsNotFound(err)
}
