package main

import (
	"fmt"
	"log"
	"net"
	"net/http"
	"net/url"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

var cachedImageGroup singleflight.Group

// isBlockedInternalHost reports whether a hostname resolves to a blocked
// internal/loopback/link-local address, guarding against SSRF.
func isBlockedInternalHost(host string) bool {
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
	}
	return false
}

type CachedImageApp struct {
	Client *http.Client
	Cache  *MinIOCache
}

const cacheControlHeader = "public, max-age=31536000, immutable"

func validatePageURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("missing url parameter")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url")
	}

	if u.Scheme != "https" && u.Scheme != "http" {
		return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("invalid url: missing host")
	}

	if isBlockedInternalHost(host) {
		return fmt.Errorf("unsupported URL: internal address not allowed")
	}

	allowed := false
	if host == "exhentai.org" || host == "e-hentai.org" {
		allowed = true
	}

	if !allowed {
		return fmt.Errorf("unsupported URL: domain not allowed")
	}

	return nil
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

	if err := validatePageURL(decodedURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	key := CacheKey(decodedURL)

	if a.Cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cache not configured"})
		return
	}

	// Try cache hit
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

	// Cache miss — use singleflight to coalesce concurrent requests
	log.Printf("cached-image miss key=%s", key[:16])

	v, sfErr, _ := cachedImageGroup.Do(key, func() (interface{}, error) {
		// Double-check cache after acquiring singleflight lock
		if _, headErr := a.Cache.Head(ctx, key); headErr == nil {
			data, contentType, getErr := a.Cache.Get(ctx, key)
			if getErr != nil {
				return nil, getErr
			}
			return &imageResult{data: data, contentType: contentType}, nil
		}

		data, contentType, fetchErr := fetchPageImage(ctx, a.Client, decodedURL)
		if fetchErr != nil {
			return nil, fetchErr
		}

		if putErr := a.Cache.Put(ctx, key, data, contentType); putErr != nil {
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

type imageResult struct {
	data        []byte
	contentType string
}
