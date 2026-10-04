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
	"golang.org/x/sync/singleflight"

	"manga-reader/internal/exhentai"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/imageproc"
	"manga-reader/internal/model"
	"manga-reader/internal/storage"
)

const (
	spriteCachePrefix = "sprite/"
	// maxPageThumbDimension bounds a single crop request so a malicious caller
	// cannot ask the server to allocate an enormous image.
	maxPageThumbDimension = 4096
)

func spriteCacheKey(spriteURL string) string {
	sum := sha256.Sum256([]byte(spriteURL))
	return spriteCachePrefix + hex.EncodeToString(sum[:])
}

// cropSingleflightKey identifies a (sprite URL, rect) crop for in-flight
// coalescing. Crops themselves are not persisted: every request re-reads the
// cached sprite and crops it fresh, so the key carries no cache prefix.
func cropSingleflightKey(spriteURL string, rect image.Rectangle) string {
	raw := fmt.Sprintf("%s|%d|%d|%d|%d", spriteURL, rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy())
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
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

// pageThumbGroup coalesces the entire resolve + crop + expiry-recovery flow for
// one gallery page, so a burst of identical index requests shares a single
// sprite fetch and, when the cached sprite has expired, a single gallery
// refresh.
var pageThumbGroup singleflight.Group

// pageThumbFlowTimeout bounds one detached index-thumbnail flow. Each inner
// operation is bounded on its own (sprite fetch 60s, resolve 15s, recovery
// refresh spriteRecoveryTimeout); this is the outer backstop.
const pageThumbFlowTimeout = 2 * time.Minute

// spriteRecoveryTimeout bounds the request-path gallery refresh attempted when
// a cached sprite URL has expired; the direct single-page scrape that follows
// is bounded by its own upstream document timeout.
const spriteRecoveryTimeout = 30 * time.Second

// errPageThumbNotFound signals that the requested page index has no thumbnail
// geometry, so the handler can answer 404 rather than treating it as an
// upstream failure.
var errPageThumbNotFound = errors.New("page thumbnail not found")

// isExpiredSprite reports whether err is a permanent 404 for a sprite URL,
// which means the cached sprite reference has expired and the gallery must be
// re-scraped for a fresh URL.
func isExpiredSprite(err error) bool {
	return exhentai.HTTPStatusCode(err) == http.StatusNotFound
}

// handleCachedPageThumbnail is the MinIO-backed page-thumbnail endpoint. It
// reads the sprite through the cache and crops it on every request, and
// resolves gallery-index requests from gallery_cache first, scraping upstream
// on a miss.
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
	var data []byte
	if req.byIndex {
		data, err = s.loadCachedIndexThumbnail(ctx, req)
	} else {
		data, err = s.loadOrCropThumbnail(ctx, req.spriteURL, req.rect)
	}
	if err != nil {
		s.writeCachedThumbError(c, req, err)
		return
	}

	c.Header("Cache-Control", cacheControlHeader)
	c.Data(http.StatusOK, "image/webp", data)
}

// writeCachedThumbError maps a page-thumbnail failure to its HTTP response.
func (s *Server) writeCachedThumbError(c *gin.Context, req pageThumbRequest, err error) {
	switch {
	case errors.Is(err, errPageThumbNotFound):
		c.JSON(http.StatusNotFound, gin.H{"error": "page thumbnail not found"})
	case errors.Is(err, imageproc.ErrOutOfBounds):
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		slog.Error("cached page-thumbnail failed", "id", req.galleryID, "index", req.index, "error", err)
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("crop page thumbnail failed: %v", err)})
	}
}

// loadCachedIndexThumbnail resolves and crops a gallery-index thumbnail,
// coalescing the whole flow per page. Coalescing the resolution too (not just
// the sprite fetch) is what keeps a burst of expired-sprite requests down to a
// single gallery refresh.
func (s *Server) loadCachedIndexThumbnail(ctx context.Context, req pageThumbRequest) ([]byte, error) {
	key := fmt.Sprintf("%d:%s:%d", req.galleryID, req.token, req.index)
	v, err, _ := pageThumbGroup.Do(key, func() (any, error) {
		flowCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pageThumbFlowTimeout)
		defer cancel()
		return s.loadCachedIndexThumbnailOnce(flowCtx, req)
	})
	if err != nil {
		return nil, err
	}
	return v.([]byte), nil
}

// loadCachedIndexThumbnailOnce resolves the page geometry and crops the sprite,
// self-healing an expired sprite URL: it refreshes the gallery cache, and when
// the shared document cache still serves the expired reference, forces a fresh
// single-page scrape that bypasses that cache before retrying once.
func (s *Server) loadCachedIndexThumbnailOnce(ctx context.Context, req pageThumbRequest) ([]byte, error) {
	thumb, found, err := s.resolveGalleryPageThumb(ctx, req.galleryID, req.token, req.index)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errPageThumbNotFound
	}

	expiredURL := thumb.SpriteURL
	data, err := s.loadOrCropThumbnail(ctx, expiredURL, thumbRect(thumb))
	if err == nil {
		return data, nil
	}
	if !isExpiredSprite(err) {
		return nil, err
	}

	slog.Info("cached sprite expired; refreshing gallery cache",
		"id", req.galleryID, "token", req.token, "index", req.index)

	refreshCtx, cancel := context.WithTimeout(ctx, spriteRecoveryTimeout)
	defer cancel()
	if rerr := s.refreshPagesSync(refreshCtx, req.galleryID, req.token); rerr != nil {
		slog.Warn("sprite expiry refresh failed", "id", req.galleryID, "error", rerr)
	}

	if refreshed, found, rerr := s.resolveGalleryPageThumb(ctx, req.galleryID, req.token, req.index); rerr == nil && found {
		thumb = refreshed
	}
	if thumb.SpriteURL == expiredURL {
		// The gallery document is still inside its short-lived dedup window, so
		// the refresh saw the expired reference. Force a fresh single-page
		// scrape, which bypasses that window.
		fresh, ok, ferr := s.scrapeFreshPageThumb(ctx, req.galleryID, req.token, req.index)
		if ferr != nil {
			slog.Warn("sprite expiry fresh scrape failed", "id", req.galleryID, "error", ferr)
		} else if ok {
			thumb = fresh
			s.backfillThumbnail(ctx, req.galleryID, req.token, req.index, fresh)
		}
	}
	if thumb.SpriteURL == expiredURL {
		return nil, err
	}
	return s.loadOrCropThumbnail(ctx, thumb.SpriteURL, thumbRect(thumb))
}

// scrapeFreshPageThumb fetches the geometry for one page index directly,
// bypassing the shared document dedup window so an expired sprite reference is
// never re-read.
func (s *Server) scrapeFreshPageThumb(ctx context.Context, galleryID int64, token string, index int) (model.GalleryPageThumb, bool, error) {
	u := exhentai.GalleryURL(strconv.FormatInt(galleryID, 10), token)
	return exhentai.ScrapeGalleryPageThumb(ctx, s.Client, u, index)
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
			if index < len(row.Thumbnails) {
				if thumb := row.Thumbnails[index]; thumb.SpriteURL != "" {
					return thumb, true, nil
				}
				// Present but without geometry: fall through to a fresh walk,
				// which upgrades the row through UpsertThumbnails.
				behindStoredList = true
			}
			// Only verified, complete walks are ever written, and they emit
			// pages in ascending order — so an index past the stored end does
			// not exist upstream either. Without this, probing an invalid
			// index on a warm gallery would start a full walk every time.
			if !behindStoredList && len(row.Thumbnails) > 0 && index >= len(row.Thumbnails) {
				return model.GalleryPageThumb{}, false, nil
			}
		}
	}

	// Cache miss or missing geometry: fill the cache in the background from the
	// same shared walk this resolve subscribes to, so the verified list is
	// persisted even though the resolve itself only waits for one page.
	go s.refreshPages(galleryID, token)

	resolveCtx, cancel := context.WithTimeout(ctx, pageThumbResolveTimeout)
	defer cancel()

	thumb, found, err := s.resolveGalleryPageThumbFromStream(resolveCtx, galleryID, token, index)
	if errors.Is(err, context.DeadlineExceeded) {
		u := exhentai.GalleryURL(strconv.FormatInt(galleryID, 10), token)
		scraped, scrapedFound, scrapedErr := exhentai.ScrapeGalleryPageThumb(ctx, s.Client, u, index)
		if scrapedErr == nil && scrapedFound {
			s.backfillThumbnail(ctx, galleryID, token, index, scraped)
		}
		return scraped, scrapedFound, scrapedErr
	}
	if err != nil {
		return model.GalleryPageThumb{}, false, err
	}
	return thumb, found, nil
}

// backfillThumbnail persists a single scraped page geometry, index-aligned with
// the cached page list. It is a best-effort cache write: failures are logged and
// never fail the request.
func (s *Server) backfillThumbnail(ctx context.Context, galleryID int64, token string, index int, thumb model.GalleryPageThumb) {
	db := s.cacheDB()
	if db == nil {
		return
	}
	sparse := make([]model.GalleryPageThumb, index+1)
	sparse[index] = thumb
	if err := gallerycache.UpsertThumbnails(ctx, db, galleryID, token, sparse); err != nil {
		slog.Warn("page-thumbnail backfill failed", "id", galleryID, "index", index, "error", err)
	}
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

	_, err := s.scrapeGalleryPages(ctx, galleryID, token, func(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error {
		if total > 0 && index >= total {
			// The walk already knows the gallery size: no need to wait for the
			// remaining pages to conclude the index does not exist.
			return errThumbResolved
		}
		if index < len(thumbnails) {
			thumb, found = thumbnails[index], thumbnails[index].SpriteURL != ""
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
	key := "live:" + cropSingleflightKey(spriteURL, rect)
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

// loadOrCropThumbnail returns the crop of the cached sprite. The sprite is
// read through the MinIO cache and cropped on every request; the crop itself
// is never persisted. Concurrent calls for the same crop are coalesced.
func (s *Server) loadOrCropThumbnail(ctx context.Context, spriteURL string, rect image.Rectangle) ([]byte, error) {
	key := cropSingleflightKey(spriteURL, rect)
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		fetchCtx := context.WithoutCancel(ctx)
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
		defer cancel()

		sprite, err := s.loadOrFetchSprite(fetchCtx, spriteURL)
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

// loadOrFetchSprite returns the sprite bytes, reading through the cache when
// configured. The original sprite is cached separately from the crops so a
// single download can serve every page in the sprite. Concurrent misses for
// the same sprite are coalesced on the sprite key, so a cold cache downloads
// each sheet once even when many crop (or index) requests arrive together.
func (s *Server) loadOrFetchSprite(ctx context.Context, spriteURL string) ([]byte, error) {
	key := spriteCacheKey(spriteURL)

	if s.Cache != nil {
		if data, _, getErr := s.Cache.Get(ctx, key); getErr == nil {
			return data, nil
		} else if !storage.IsNotFound(getErr) {
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
			} else if !storage.IsNotFound(getErr) {
				return nil, getErr
			}
		}

		data, contentType, err := fetchThumbnail(fetchCtx, s.Client, spriteURL)
		if err != nil {
			return nil, err
		}

		if s.Cache != nil {
			putOpts := storage.PutOptions{
				CacheControl: cacheControlHeader,
				Meta:         map[string]string{"source-url": spriteURL},
			}
			if putErr := s.Cache.Put(fetchCtx, key, data, contentType, putOpts); putErr != nil {
				slog.Error("sprite store failed", "key", key[:16], "error", putErr)
			}
		}
		return data, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	return v.([]byte), nil
}
