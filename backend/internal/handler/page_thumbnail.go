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

// pageThumbRequest addresses a page thumbnail either directly (sprite URL +
// crop rectangle) or by gallery page index.
type pageThumbRequest struct {
	spriteURL string
	rect      image.Rectangle

	galleryID int64
	token     string
	index     int

	byIndex bool
}

func parsePageThumbRequest(c *gin.Context) (pageThumbRequest, error) {
	if rawURL := c.Query("url"); rawURL != "" {
		if err := exhentai.ValidatePageThumbnailURL(rawURL); err != nil {
			return pageThumbRequest{}, err
		}
		rect, err := parseCropRect(c)
		if err != nil {
			return pageThumbRequest{}, err
		}
		return pageThumbRequest{spriteURL: rawURL, rect: rect}, nil
	}

	idRaw := c.Query("id")
	token := c.Query("token")
	if idRaw == "" || token == "" {
		return pageThumbRequest{}, fmt.Errorf("provide url+x+y+w+h or id+token+index")
	}
	galleryID, err := strconv.ParseInt(idRaw, 10, 64)
	if err != nil {
		return pageThumbRequest{}, fmt.Errorf("invalid id parameter")
	}
	indexRaw := c.Query("index")
	if indexRaw == "" {
		return pageThumbRequest{}, fmt.Errorf("missing index parameter")
	}
	index, err := strconv.Atoi(indexRaw)
	if err != nil || index < 0 {
		return pageThumbRequest{}, fmt.Errorf("invalid index parameter")
	}
	return pageThumbRequest{galleryID: galleryID, token: token, index: index, byIndex: true}, nil
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

// resolveLiveRect resolves a request to a sprite + rectangle, scraping upstream
// for the gallery-index form. Live resolution never reads local caches.
func (s *Server) resolveLiveRect(ctx context.Context, req pageThumbRequest) (string, image.Rectangle, bool, error) {
	if !req.byIndex {
		return req.spriteURL, req.rect, true, nil
	}
	u := exhentai.GalleryURL(strconv.FormatInt(req.galleryID, 10), req.token)
	thumb, found, err := exhentai.ScrapeGalleryPageThumb(ctx, s.Client, u, req.index)
	if err != nil || !found {
		return "", image.Rectangle{}, false, err
	}
	return thumb.SpriteURL, thumbRect(thumb), true, nil
}

func thumbRect(thumb model.GalleryPageThumb) image.Rectangle {
	return image.Rect(thumb.X, thumb.Y, thumb.X+thumb.Width, thumb.Y+thumb.Height)
}

// handlePageThumbnail is the live page-thumbnail endpoint: it always fetches
// the sprite from upstream and crops it, without touching any cache.
func (s *Server) handlePageThumbnail(c *gin.Context) {
	req, err := parsePageThumbRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	spriteURL, rect, found, err := s.resolveLiveRect(ctx, req)
	if err != nil {
		slog.Error("page-thumbnail resolve failed", "index", req.byIndex, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("resolve page thumbnail failed: %v", err)})
		return
	}
	if !found {
		c.JSON(http.StatusNotFound, gin.H{"error": "page thumbnail not found"})
		return
	}

	data, err := s.cropLive(ctx, spriteURL, rect)
	if err != nil {
		if errors.Is(err, imageproc.ErrOutOfBounds) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		slog.Error("page-thumbnail crop failed", "url", spriteURL, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("crop page thumbnail failed: %v", err)})
		return
	}

	c.Header("Cache-Control", thumbnailCacheControl)
	c.Data(http.StatusOK, "image/webp", data)
}

// handleCachedPageThumbnail is the MinIO-backed page-thumbnail endpoint. It
// reads through the cache (sprite + crop) and resolves gallery-index requests
// from gallery_cache first, scraping upstream on a miss.
func (s *Server) handleCachedPageThumbnail(c *gin.Context) {
	if s.Cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cache not configured"})
		return
	}

	req, err := parsePageThumbRequest(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	spriteURL, rect := req.spriteURL, req.rect
	if req.byIndex {
		thumb, found, err := s.resolveGalleryPageThumb(ctx, req.galleryID, req.token, req.index)
		if err != nil {
			slog.Error("cached page-thumbnail resolve failed", "id", req.galleryID, "index", req.index, "error", err)
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("resolve page thumbnail failed: %v", err)})
			return
		}
		if !found {
			c.JSON(http.StatusNotFound, gin.H{"error": "page thumbnail not found"})
			return
		}
		spriteURL, rect = thumb.SpriteURL, thumbRect(thumb)
	}

	data, err := s.loadOrCropThumbnail(ctx, spriteURL, rect)
	if err != nil {
		if errors.Is(err, imageproc.ErrOutOfBounds) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		slog.Error("cached page-thumbnail crop failed", "url", spriteURL, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("crop page thumbnail failed: %v", err)})
		return
	}

	c.Header("Cache-Control", cacheControlHeader)
	c.Data(http.StatusOK, "image/webp", data)
}

// pageThumbResolveTimeout bounds how long an index resolve waits for the
// shared /pages walk to deliver the requested page before falling back to a
// direct scrape. It is a var so tests can shorten it.
var pageThumbResolveTimeout = 15 * time.Second

// errThumbResolved stops the pages-stream drain once the requested index has
// been seen (or proven absent from the walk). emit's error is returned as-is
// by drain, so the sentinel is how the subscriber reports "stop, we are done".
var errThumbResolved = errors.New("page thumbnail resolved from pages stream")

// resolveGalleryPageThumb returns the sprite geometry for a page index. The
// gallery cache is the fast path; on a miss the resolve subscribes to the
// shared /pages walk instead of scraping on its own, so concurrent index
// requests (and the /pages stream itself) share a single upstream walk, which
// also backfills the cache once complete. Only when the walk does not reach
// the page within pageThumbResolveTimeout does it fall back to a direct
// one-page scrape, so a cold, far-away index cannot stall the request.
func (s *Server) resolveGalleryPageThumb(ctx context.Context, galleryID int64, token string, index int) (model.GalleryPageThumb, bool, error) {
	if db := s.cacheDB(); db != nil {
		row, found, err := gallerycache.Get(ctx, db, galleryID, token)
		if err != nil {
			slog.Warn("gallery page-thumbnail cache read failed", "id", galleryID, "error", err)
		} else if found {
			behindStoredList := false
			for _, p := range row.Pages {
				if p.Index != index {
					continue
				}
				if p.Thumbnail != nil {
					return *p.Thumbnail, true, nil
				}
				// Present but without geometry: fall through to a fresh walk,
				// which upgrades the row through UpsertPages once complete.
				behindStoredList = true
				break
			}
			// Only verified, complete walks are ever written, and they emit
			// pages in ascending order — so an index past the stored end does
			// not exist upstream either. Without this, probing an invalid
			// index on a warm gallery would start a full walk every time.
			if !behindStoredList && len(row.Pages) > 0 && index > row.Pages[len(row.Pages)-1].Index {
				return model.GalleryPageThumb{}, false, nil
			}
		}
	}

	resolveCtx, cancel := context.WithTimeout(ctx, pageThumbResolveTimeout)
	defer cancel()

	thumb, found, err := s.resolveGalleryPageThumbFromStream(resolveCtx, galleryID, token, index)
	if errors.Is(err, context.DeadlineExceeded) {
		u := exhentai.GalleryURL(strconv.FormatInt(galleryID, 10), token)
		return exhentai.ScrapeGalleryPageThumb(ctx, s.Client, u, index)
	}
	if err != nil {
		return model.GalleryPageThumb{}, false, err
	}
	return thumb, found, nil
}

// resolveGalleryPageThumbFromStream finds the sprite geometry for index in the
// shared pages walk. The walk delivers pages in order, so the requested index
// either arrives in the replayed prefix (the common case: the frontend only
// requests thumbnails for pages it already received from /pages) or once the
// walk reaches it. It returns found=false when the walk completes without the
// page or the page carries no thumbnail geometry.
func (s *Server) resolveGalleryPageThumbFromStream(
	ctx context.Context,
	galleryID int64,
	token string,
	index int,
) (model.GalleryPageThumb, bool, error) {
	var thumb model.GalleryPageThumb
	found := false

	_, err := s.scrapeGalleryPages(ctx, galleryID, token, func(total int, batch []model.CachedPage) error {
		if total > 0 && index >= total {
			// The walk already knows the gallery size: no need to wait for the
			// remaining pages to conclude the index does not exist.
			return errThumbResolved
		}
		for _, p := range batch {
			if p.Index != index {
				continue
			}
			if p.Thumbnail != nil {
				thumb, found = *p.Thumbnail, true
			}
			// Each index appears exactly once per walk, so this is final.
			return errThumbResolved
		}
		return nil
	})

	switch {
	case errors.Is(err, errThumbResolved):
		return thumb, found, nil
	case err != nil:
		return model.GalleryPageThumb{}, false, err
	default:
		// Walk finished: index out of range, or no thumbnail metadata.
		return model.GalleryPageThumb{}, false, nil
	}
}

// cropLive fetches the sprite from upstream and crops it, without touching
// MinIO. Concurrent identical requests are coalesced.
func (s *Server) cropLive(ctx context.Context, spriteURL string, rect image.Rectangle) ([]byte, error) {
	key := "live:" + pageThumbCacheKey(spriteURL, rect)
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		fetchCtx := context.WithoutCancel(ctx)
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
		defer cancel()

		sprite, _, err := fetchThumbnail(fetchCtx, s.Client, spriteURL)
		if err != nil {
			return nil, err
		}
		return imageproc.CropWEBP(sprite, rect)
	})
	if sfErr != nil {
		return nil, sfErr
	}
	return v.([]byte), nil
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
// single download can serve every page in the sprite. Concurrent misses for
// the same sprite are coalesced on the sprite key, so a cold cache downloads
// each sheet once even when many crop (or index) requests arrive together.
func (s *Server) loadOrFetchSprite(ctx context.Context, spriteURL string) ([]byte, error) {
	key := pageSpriteCacheKey(spriteURL)

	if s.Cache != nil {
		if data, _, getErr := s.Cache.Get(ctx, key); getErr == nil {
			return data, nil
		} else if !cache.IsNotFound(getErr) {
			return nil, getErr
		}
	}

	v, sfErr, _ := cachedImageGroup.Do("sprite:"+key, func() (any, error) {
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

		data, contentType, err := fetchThumbnail(fetchCtx, s.Client, spriteURL)
		if err != nil {
			return nil, err
		}

		if s.Cache != nil {
			if putErr := s.Cache.PutWithMeta(fetchCtx, key, data, contentType, map[string]string{
				"source-url": spriteURL,
			}, cacheControlHeader); putErr != nil {
				slog.Error("page-sprite store failed", "key", key[:16], "error", putErr)
			}
		}
		return data, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	return v.([]byte), nil
}
