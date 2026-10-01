package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/cache"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/imageproc"
	"manga-reader/internal/model"
)

const (
	pageSpriteCachePrefix = "page-sprite/"
	pageThumbCachePrefix  = "page-thumb/"
	// maxPageThumbDimension bounds a single crop request so a malicious caller
	// cannot ask the server to allocate an enormous image.
	maxPageThumbDimension = 4096
)

func pageSpriteCacheKey(spriteURL string) string {
	sum := sha256.Sum256([]byte(spriteURL))
	return pageSpriteCachePrefix + hex.EncodeToString(sum[:])
}

func pageThumbCacheKey(spriteURL string, rect image.Rectangle) string {
	raw := fmt.Sprintf("%s|%d|%d|%d|%d", spriteURL, rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy())
	sum := sha256.Sum256([]byte(raw))
	return pageThumbCachePrefix + hex.EncodeToString(sum[:])
}

// parseCropRect reads x/y/w/h query parameters and returns the crop rectangle.
func parseCropRect(c *gin.Context) (image.Rectangle, error) {
	x, err := queryCoord(c, "x")
	if err != nil {
		return image.Rectangle{}, err
	}
	y, err := queryCoord(c, "y")
	if err != nil {
		return image.Rectangle{}, err
	}
	w, err := queryDimension(c, "w")
	if err != nil {
		return image.Rectangle{}, err
	}
	h, err := queryDimension(c, "h")
	if err != nil {
		return image.Rectangle{}, err
	}
	return image.Rect(x, y, x+w, y+h), nil
}

func queryCoord(c *gin.Context, key string) (int, error) {
	raw := c.Query(key)
	if raw == "" {
		return 0, fmt.Errorf("missing %s parameter", key)
	}
	v, err := strconv.Atoi(raw)
	if err != nil || v < 0 {
		return 0, fmt.Errorf("invalid %s parameter", key)
	}
	return v, nil
}

func queryDimension(c *gin.Context, key string) (int, error) {
	v, err := queryCoord(c, key)
	if err != nil {
		return 0, err
	}
	if v == 0 || v > maxPageThumbDimension {
		return 0, fmt.Errorf("invalid %s parameter", key)
	}
	return v, nil
}

// handlePageThumbnail crops a single page thumbnail out of an ExHentai sprite
// image. The sprite URL and the cell rectangle come from the gallery pages
// response (see GalleryPageThumb).
func (s *Server) handlePageThumbnail(c *gin.Context) {
	rawURL := c.Query("url")
	if err := exhentai.ValidatePageThumbnailURL(rawURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	rect, err := parseCropRect(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	data, err := s.loadOrCropThumbnail(c.Request.Context(), rawURL, rect)
	if err != nil {
		if errors.Is(err, imageproc.ErrOutOfBounds) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		slog.Error("page-thumbnail error", "url", rawURL, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("crop page thumbnail failed: %v", err)})
		return
	}

	c.Header("Cache-Control", cacheControlHeader)
	c.Data(http.StatusOK, "image/webp", data)
}

// handleGalleryPageThumbnail resolves the sprite geometry for a single page
// index of a gallery and returns the cropped WebP. It lets clients request a
// thumbnail without first calling /pages.
func (s *Server) handleGalleryPageThumbnail(c *gin.Context) {
	galleryID, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}

	indexRaw := c.Query("index")
	if indexRaw == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing index parameter"})
		return
	}
	index, err := strconv.Atoi(indexRaw)
	if err != nil || index < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid index parameter"})
		return
	}

	ctx := c.Request.Context()
	thumb, found, err := s.resolveGalleryPageThumb(ctx, c.Param("id"), token, galleryID, index)
	if err != nil {
		slog.Error("gallery page-thumbnail resolve failed", "id", galleryID, "index", index, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("resolve page thumbnail failed: %v", err)})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "page thumbnail not found"})
		return
	}

	rect := image.Rect(thumb.X, thumb.Y, thumb.X+thumb.Width, thumb.Y+thumb.Height)
	data, err := s.loadOrCropThumbnail(ctx, thumb.SpriteURL, rect)
	if err != nil {
		slog.Error("gallery page-thumbnail crop failed", "id", galleryID, "index", index, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("crop page thumbnail failed: %v", err)})
		return
	}

	c.Header("Cache-Control", cacheControlHeader)
	c.Data(http.StatusOK, "image/webp", data)
}

// resolveGalleryPageThumb returns the sprite geometry for a page index, using
// the gallery cache when it already holds the thumbnail metadata and scraping
// upstream otherwise.
func (s *Server) resolveGalleryPageThumb(ctx context.Context, idParam, token string, galleryID int64, index int) (model.GalleryPageThumb, bool, error) {
	if db := s.cacheDB(); db != nil {
		row, found, err := gallerycache.Get(ctx, db, galleryID, token)
		if err != nil {
			slog.Warn("gallery page-thumbnail cache read failed", "id", galleryID, "error", err)
		} else if found {
			for _, p := range row.Pages {
				if p.Index == index && p.Thumbnail != nil {
					return *p.Thumbnail, true, nil
				}
			}
		}
	}

	u := exhentai.GalleryURL(idParam, token)
	return exhentai.ScrapeGalleryPageThumb(ctx, s.Client, u, index)
}

// loadOrCropThumbnail returns the cropped thumbnail, fetching the sprite and
// cropping on a cache miss. Concurrent calls for the same crop are coalesced.
func (s *Server) loadOrCropThumbnail(ctx context.Context, spriteURL string, rect image.Rectangle) ([]byte, error) {
	key := pageThumbCacheKey(spriteURL, rect)
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		fetchCtx := context.WithoutCancel(ctx)
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
		defer cancel()

		if s.Cache != nil {
			if data, _, getErr := s.Cache.Get(fetchCtx, key); getErr == nil {
				return data, nil
			} else if !cache.IsNotFound(getErr) {
				return nil, getErr
			}
		}

		sprite, err := s.loadOrFetchSprite(fetchCtx, spriteURL)
		if err != nil {
			return nil, err
		}

		cropped, err := imageproc.CropWEBP(sprite, rect)
		if err != nil {
			return nil, err
		}

		if s.Cache != nil {
			meta := map[string]string{
				"source-url": spriteURL,
				"crop":       fmt.Sprintf("%d,%d,%d,%d", rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy()),
			}
			if putErr := s.Cache.PutWithMeta(fetchCtx, key, cropped, "image/webp", meta, cacheControlHeader); putErr != nil {
				slog.Error("page-thumbnail store failed", "key", key[:16], "error", putErr)
			}
		}
		return cropped, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	return v.([]byte), nil
}

// loadOrFetchSprite returns the sprite bytes, reading through the cache when
// configured. The original sprite is cached separately from the crops so a
// single download can serve every page in the sprite.
func (s *Server) loadOrFetchSprite(ctx context.Context, spriteURL string) ([]byte, error) {
	key := pageSpriteCacheKey(spriteURL)

	if s.Cache != nil {
		if data, _, getErr := s.Cache.Get(ctx, key); getErr == nil {
			return data, nil
		} else if !cache.IsNotFound(getErr) {
			return nil, getErr
		}
	}

	data, contentType, err := fetchThumbnail(ctx, s.Client, spriteURL)
	if err != nil {
		return nil, err
	}

	if s.Cache != nil {
		if putErr := s.Cache.PutWithMeta(ctx, key, data, contentType, map[string]string{
			"source-url": spriteURL,
		}, cacheControlHeader); putErr != nil {
			slog.Error("page-sprite store failed", "key", key[:16], "error", putErr)
		}
	}
	return data, nil
}
