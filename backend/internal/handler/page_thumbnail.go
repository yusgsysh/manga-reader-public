package handler

import (
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/httpx"
	"manga-reader/internal/ident"
	"manga-reader/internal/imageproc"
	"manga-reader/internal/usercookie"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
)

const (
	spriteCachePrefix = "sprite/"
	cropCachePrefix   = "crop/"
	// maxPageThumbDimension bounds a single crop request so a malicious caller
	// cannot ask the server to allocate an enormous image.
	maxPageThumbDimension = 4096
)

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

// handlePageThumbnail is the live page-thumbnail endpoint: it always fetches
// the sprite from upstream and crops it, without touching any cache.
func (s *Server) handlePageThumbnail(c *gin.Context) {
	req, err := parsePageThumbRequest(c)
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	client, uid, ok := s.upstreamClientForRequest(c)
	if !ok {
		return
	}
	spriteURL, rect, found, err := s.resolveLiveRect(ctx, client, uid, req)
	if err != nil {
		slog.Error("page-thumbnail resolve failed", "index", req.byIndex, "error", err)
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("resolve page thumbnail failed: %v", err)})
		return
	}
	if !found {
		httpx.JSON(c, http.StatusNotFound, gin.H{"error": "page thumbnail not found"})
		return
	}

	data, err := s.cropLive(ctx, uid, client, spriteURL, rect)
	if err != nil {
		if errors.Is(err, imageproc.ErrOutOfBounds) {
			httpx.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		slog.Error("page-thumbnail crop failed", "url", spriteURL, "error", err)
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("crop page thumbnail failed: %v", err)})
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

// handleCachedPageThumbnail is the MinIO-backed page-thumbnail endpoint. It
// reads the sprite through the cache and crops it on every request, and
// resolves gallery-index requests from gallery_cache first, scraping upstream
// on a miss.
func (s *Server) handleCachedPageThumbnail(c *gin.Context) {
	if s.Cache == nil {
		// MinIO is optional: fall back to the live crop instead of failing.
		s.handlePageThumbnail(c)
		return
	}

	req, err := parsePageThumbRequest(c)
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	client, err := s.upstreamClient(ctx, uid)
	if err != nil {
		if errors.Is(err, usercookie.ErrNotConfigured) {
			httpx.JSON(c, http.StatusPreconditionFailed, gin.H{"error": "exhentai cookie is not configured"})
			return
		}
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": "exhentai session unavailable"})
		return
	}
	var data []byte
	if req.byIndex {
		data, err = s.loadCachedIndexThumbnail(ctx, uid, client, req)
	} else {
		data, err = s.loadOrCropThumbnail(ctx, uid, client, req.spriteURL, req.rect)
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
		httpx.JSON(c, http.StatusNotFound, gin.H{"error": "page thumbnail not found"})
	case errors.Is(err, imageproc.ErrOutOfBounds):
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
	default:
		slog.Error("cached page-thumbnail failed", "id", req.galleryID, "index", req.index, "error", err)
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("crop page thumbnail failed: %v", err)})
	}
}

// loadCachedIndexThumbnail resolves and crops a gallery-index thumbnail,
// coalescing the whole flow per page. Coalescing the resolution too (not just
// the sprite fetch) is what keeps a burst of expired-sprite requests down to a
// single gallery refresh.
func (s *Server) loadCachedIndexThumbnail(ctx context.Context, userID ident.UUID, client *http.Client, req pageThumbRequest) ([]byte, error) {
	key := fmt.Sprintf("%s/%d:%s:%d", userID, req.galleryID, req.token, req.index)
	v, err, _ := pageThumbGroup.Do(key, func() (any, error) {
		flowCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), pageThumbFlowTimeout)
		defer cancel()
		return s.loadCachedIndexThumbnailOnce(flowCtx, userID, client, req)
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
func (s *Server) loadCachedIndexThumbnailOnce(ctx context.Context, userID ident.UUID, client *http.Client, req pageThumbRequest) ([]byte, error) {
	thumb, found, err := s.resolveGalleryPageThumb(ctx, userID, client, req.galleryID, req.token, req.index)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, errPageThumbNotFound
	}

	expiredURL := thumb.SpriteURL
	data, err := s.loadOrCropThumbnail(ctx, userID, client, expiredURL, thumbRect(thumb))
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
	if rerr := s.refreshPagesSync(refreshCtx, userID, req.galleryID, req.token); rerr != nil {
		slog.Warn("sprite expiry refresh failed", "id", req.galleryID, "error", rerr)
	}

	if refreshed, found, rerr := s.resolveGalleryPageThumb(ctx, userID, client, req.galleryID, req.token, req.index); rerr == nil && found {
		thumb = refreshed
	}
	if thumb.SpriteURL == expiredURL {
		// The gallery document is still inside its short-lived dedup window, so
		// the refresh saw the expired reference. Force a fresh single-page
		// scrape, which bypasses that window.
		fresh, ok, ferr := s.scrapeFreshPageThumb(ctx, client, userID, req.galleryID, req.token, req.index)
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
	return s.loadOrCropThumbnail(ctx, userID, client, thumb.SpriteURL, thumbRect(thumb))
}
