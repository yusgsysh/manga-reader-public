package handler

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/ident"
	"manga-reader/internal/imagecache"
	"manga-reader/internal/imageproc"
	"manga-reader/internal/membudget"
	"manga-reader/internal/metrics"
	"manga-reader/internal/model"
	"net/http"
	"strconv"
	"time"

	"golang.org/x/sync/singleflight"
)

func spriteCacheKey(spriteURL string) string {
	sum := sha256.Sum256([]byte(spriteURL))
	return spriteCachePrefix + hex.EncodeToString(sum[:])
}

// cropSingleflightKey identifies a (sprite URL, rect) crop for in-flight
// coalescing. The same digest under cropCachePrefix is the persisted crop's
// cache key.
func cropSingleflightKey(spriteURL string, rect image.Rectangle) string {
	raw := fmt.Sprintf("%s|%d|%d|%d|%d", spriteURL, rect.Min.X, rect.Min.Y, rect.Dx(), rect.Dy())
	sum := sha256.Sum256([]byte(raw))
	return hex.EncodeToString(sum[:])
}

// cropCacheKey addresses the persisted crop of one sprite cell. The sprite
// URL is inside the digest, so an expired sprite URL (fresh gallery refresh)
// misses naturally and re-crops; stale crops age out with the bucket
// lifecycle policy, same as the sprites themselves.
func cropCacheKey(spriteURL string, rect image.Rectangle) string {
	return cropCachePrefix + cropSingleflightKey(spriteURL, rect)
}

// resolveLiveRect resolves a request to a sprite + rectangle, scraping upstream
// for the gallery-index form. Live resolution never reads local caches.
func (s *Server) resolveLiveRect(ctx context.Context, client *http.Client, userID ident.UUID, req pageThumbRequest) (string, image.Rectangle, bool, error) {
	if !req.byIndex {
		return req.spriteURL, req.rect, true, nil
	}
	u := exhentai.GalleryURLAt(s.upstreamBase(ctx, userID), strconv.FormatInt(req.galleryID, 10), req.token)
	thumb, found, err := exhentai.ScrapeGalleryPageThumb(ctx, client, u, req.index)
	if err != nil || !found {
		return "", image.Rectangle{}, false, err
	}
	return thumb.SpriteURL, thumbRect(thumb), true, nil
}

func thumbRect(thumb model.GalleryPageThumb) image.Rectangle {
	return image.Rect(thumb.X, thumb.Y, thumb.X+thumb.Width, thumb.Y+thumb.Height)
}

// isExpiredSprite reports whether err is a permanent 404 for a sprite URL,
// which means the cached sprite reference has expired and the gallery must be
// re-scraped for a fresh URL.
func isExpiredSprite(err error) bool {
	return exhentai.HTTPStatusCode(err) == http.StatusNotFound
}

// scrapeFreshPageThumb fetches the geometry for one page index directly,
// bypassing the shared document dedup window so an expired sprite reference is
// never re-read.
func (s *Server) scrapeFreshPageThumb(ctx context.Context, client *http.Client, userID ident.UUID, galleryID int64, token string, index int) (model.GalleryPageThumb, bool, error) {
	u := exhentai.GalleryURLAt(s.upstreamBase(ctx, userID), strconv.FormatInt(galleryID, 10), token)
	return exhentai.ScrapeGalleryPageThumb(ctx, client, u, index)
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
func (s *Server) resolveGalleryPageThumb(ctx context.Context, userID ident.UUID, client *http.Client, galleryID int64, token string, index int) (model.GalleryPageThumb, bool, error) {
	if db := s.cacheDB(); db != nil {
		row, found, err := gallerycache.Get(ctx, db, galleryID, token)
		if err != nil {
			slog.Warn("gallery page-thumbnail cache read failed", "id", galleryID, "error", err)
		} else if found {
			behindStoredList := false
			if index < len(row.Thumbnails) {
				if thumb := row.Thumbnails[index]; thumb.SpriteURL != "" {
					metrics.ObserveGalleryCache("page_thumb", "hit")
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
				metrics.ObserveGalleryCache("page_thumb", "hit")
				return model.GalleryPageThumb{}, false, nil
			}
			metrics.ObserveGalleryCache("page_thumb", "miss")
		} else {
			metrics.ObserveGalleryCache("page_thumb", "miss")
		}
	}

	// Cache miss or missing geometry: fill the cache in the background from the
	// same shared walk this resolve subscribes to, so the verified list is
	// persisted even though the resolve itself only waits for one page.
	go s.refreshPages(ctx, userID, galleryID, token)

	resolveCtx, cancel := context.WithTimeout(ctx, pageThumbResolveTimeout)
	defer cancel()

	thumb, found, err := s.resolveGalleryPageThumbFromStream(resolveCtx, userID, galleryID, token, index)
	if errors.Is(err, context.DeadlineExceeded) {
		u := exhentai.GalleryURLAt(s.upstreamBase(ctx, userID), strconv.FormatInt(galleryID, 10), token)
		scraped, scrapedFound, scrapedErr := exhentai.ScrapeGalleryPageThumb(ctx, client, u, index)
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
	} else {
		s.recordGalleryCacheChange(ctx, galleryID, token)
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
	userID ident.UUID, galleryID int64,
	token string,
	index int,
) (model.GalleryPageThumb, bool, error) {
	var thumb model.GalleryPageThumb
	found := false
	// scrapeGalleryPages drains the shared walk in batches: each emit call gets
	// only the pages appended since the previous call (drain passes
	// s.thumbnails[cursor:]), not the full prefix. Track the running offset so
	// the absolute `index` maps to the right element of the current batch.
	offset := 0

	_, err := s.scrapeGalleryPages(ctx, userID, galleryID, token, func(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error {
		if total > 0 && index >= total {
			// The walk already knows the gallery size: no need to wait for the
			// remaining pages to conclude the index does not exist.
			return errThumbResolved
		}
		local := index - offset
		if local >= 0 && local < len(thumbnails) {
			thumb, found = thumbnails[local], thumbnails[local].SpriteURL != ""
			return errThumbResolved
		}
		offset += len(thumbnails)
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

// decodedSprite is a decoded sprite shared across the crop flights of the
// different cells of one sprite sheet. Its lease covers the sprite bytes plus
// the decoded bitmap and is reference-counted, so a cold grid load charges the
// one bitmap (and the one sheet) once no matter how many cells are cropped at
// once.
type decodedSprite struct {
	img   image.Image
	lease *membudget.Lease
}

// spriteDecodeGroup shares one decode of a sprite across the concurrent crop
// flights of *different* cells of that sprite: a cold grid load would otherwise
// decode the same sheet once per cell, and a decoded sheet costs width*height*4
// bytes. The value escapes the flight, so every crop flight keeps the shared
// reservation alive until it finishes cropping.
var spriteDecodeGroup singleflight.Group

// cropSprite crops one cell of sprite. It reserves budget for the sprite bytes
// plus the decoded bitmap for the duration of the crop, and shares the decode —
// and its reservation — with concurrent crops of other cells of the same sprite.
func (s *Server) cropSprite(ctx context.Context, spriteURL string, sprite []byte, rect image.Rectangle) ([]byte, error) {
	var leader bool
	v, sfErr, _ := spriteDecodeGroup.Do("decode:"+spriteURL, func() (any, error) {
		leader = true
		cfg, err := imageproc.ProbeSprite(sprite)
		if err != nil {
			return nil, err
		}
		weight := int64(len(sprite)) + int64(cfg.Width)*int64(cfg.Height)*4
		lease, err := membudget.Acquire(ctx, weight)
		if err != nil {
			return nil, err
		}
		img, err := imageproc.DecodeSprite(sprite)
		if err != nil {
			lease.Release()
			return nil, err
		}
		return &decodedSprite{img: img, lease: lease}, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	shared := v.(*decodedSprite)
	if !leader {
		shared.lease.Retain()
	}
	defer shared.lease.Release()
	return imageproc.CropDecoded(shared.img, rect)
}

// cropLive fetches the sprite from upstream and crops it, without touching
// MinIO. Concurrent identical requests are coalesced.
func (s *Server) cropLive(ctx context.Context, userID ident.UUID, client *http.Client, spriteURL string, rect image.Rectangle) ([]byte, error) {
	key := fmt.Sprintf("%s|live:", userID) + cropSingleflightKey(spriteURL, rect)
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		fetchCtx := context.WithoutCancel(ctx)
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
		defer cancel()

		sprite, _, err := fetchThumbnail(fetchCtx, client, spriteURL)
		if err != nil {
			return nil, err
		}
		return s.cropSprite(fetchCtx, spriteURL, sprite, rect)
	})
	if sfErr != nil {
		return nil, sfErr
	}
	return v.([]byte), nil
}

// loadOrCropThumbnail returns the crop of the cached sprite. A cell that has
// been cropped before is served from the persisted crop without touching the
// sprite; otherwise the sprite is read through the MinIO cache and cropped,
// and the result is persisted. Concurrent calls for the same crop are
// coalesced.
func (s *Server) loadOrCropThumbnail(ctx context.Context, userID ident.UUID, client *http.Client, spriteURL string, rect image.Rectangle) ([]byte, error) {
	key := fmt.Sprintf("%s|", userID) + cropSingleflightKey(spriteURL, rect)
	v, sfErr, _ := cachedImageGroup.Do(key, func() (any, error) {
		fetchCtx := context.WithoutCancel(ctx)
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
		defer cancel()

		cropKey := cropCacheKey(spriteURL, rect)
		if s.Cache != nil {
			if data, _, getErr := s.Cache.Get(fetchCtx, cropKey); getErr == nil {
				metrics.ObserveImageCache("page_crop", "hit")
				return data, nil
			} else if !imagecache.IsNotFound(getErr) {
				// Unreadable crop: recompute rather than fail the request;
				// the store below overwrites whatever is there.
				slog.Warn("crop cache read failed; recomputing", "key", cropKey[:16], "error", getErr)
			} else {
				metrics.ObserveImageCache("page_crop", "miss")
			}
		}

		sprite, err := s.loadOrFetchSprite(fetchCtx, userID, client, spriteURL)
		if err != nil {
			return nil, err
		}

		crop, err := s.cropSprite(fetchCtx, spriteURL, sprite, rect)
		if err != nil {
			return nil, err
		}

		if s.Cache != nil {
			if putErr := s.Cache.PutWithMeta(fetchCtx, cropKey, crop, "image/webp", map[string]string{
				"source-url": spriteURL,
			}, cacheControlHeader); putErr != nil {
				slog.Error("crop store failed", "key", cropKey[:16], "error", putErr)
			}
		}
		return crop, nil
	})
	if sfErr != nil {
		return nil, sfErr
	}
	return v.([]byte), nil
}

// loadOrFetchSprite returns the sprite bytes, reading through the cache when
// configured. The original sprite is cached separately from the crops so a
// single download can serve every page in the sprite. All reads — including
// cache hits — run inside the singleflight, so concurrent crops of different
// cells of one sprite share a single buffered copy instead of each holding
// its own.
func (s *Server) loadOrFetchSprite(ctx context.Context, userID ident.UUID, client *http.Client, spriteURL string) ([]byte, error) {
	key := spriteCacheKey(spriteURL)

	v, sfErr, _ := cachedImageGroup.Do(fmt.Sprintf("%s|sprite:", userID)+key, func() (any, error) {
		fetchCtx := context.WithoutCancel(ctx)
		fetchCtx, cancel := context.WithTimeout(fetchCtx, 60*time.Second)
		defer cancel()

		if s.Cache != nil {
			if data, _, getErr := s.Cache.Get(fetchCtx, key); getErr == nil {
				metrics.ObserveImageCache("sprite", "hit")
				return data, nil
			} else if !imagecache.IsNotFound(getErr) {
				return nil, getErr
			} else {
				metrics.ObserveImageCache("sprite", "miss")
			}
		}

		data, contentType, err := fetchThumbnail(fetchCtx, client, spriteURL)
		if err != nil {
			return nil, err
		}

		if s.Cache != nil {
			if putErr := s.Cache.PutWithMeta(fetchCtx, key, data, contentType, map[string]string{
				"source-url": spriteURL,
			}, cacheControlHeader); putErr != nil {
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
