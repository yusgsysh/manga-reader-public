package main

import (
	"fmt"
	"log"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/cache"
	"manga-reader/internal/exhentai"
)

type CachedImageApp struct {
	Client *http.Client
	Cache  *cache.MinIOCache
}

const cacheControlHeader = "public, max-age=31536000, immutable"

type imageResult struct {
	data        []byte
	contentType string
}

func (a *CachedImageApp) handleCachedImage(c *gin.Context) {
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

	if a.Cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cache not configured"})
		return
	}

	if _, err := a.Cache.Head(ctx, key); err == nil {
		data, contentType, err := a.Cache.Get(ctx, key)
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

	v, sfErr, _ := cachedImageGroup.Do(key, func() (interface{}, error) {
		if _, headErr := a.Cache.Head(ctx, key); headErr == nil {
			data, contentType, getErr := a.Cache.Get(ctx, key)
			if getErr != nil {
				return nil, getErr
			}
			return &imageResult{data: data, contentType: contentType}, nil
		}

		data, contentType, fetchErr := exhentai.FetchPageImage(ctx, a.Client, decodedURL)
		if fetchErr != nil {
			return nil, fetchErr
		}

		if putErr := a.Cache.Put(ctx, key, data, contentType, cacheControlHeader); putErr != nil {
			log.Printf("cached-image store failed key=%s err=%v", key[:16], putErr)
		} else {
			log.Printf("cached-image stored key=%s", key[:16])
		}

		return &imageResult{data: data, contentType: contentType}, nil
	})

	if sfErr != nil {
		log.Printf("cached-image error key=%s err=%v", key[:16], sfErr)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download image failed: %v", sfErr)})
		return
	}

	result := v.(*imageResult)
	c.Header("Cache-Control", cacheControlHeader)
	c.Data(http.StatusOK, result.contentType, result.data)
}
