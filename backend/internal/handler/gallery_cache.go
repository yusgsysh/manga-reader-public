package handler

import (
	"context"
	"fmt"
	"log/slog"
	"manga-reader/internal/ent"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/httpx"
	"manga-reader/internal/ident"
	"manga-reader/internal/metrics"
	"manga-reader/internal/model"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// cachedPageCountProbeTTL throttles the stale-while-revalidate page-count
// probe: without it every cached page view hit ExHentai once.
const cachedPageCountProbeTTL = time.Hour

// cacheDB returns the ent client when a database is configured. Some tests
// construct a Server without a DB; callers must tolerate a nil result.
func (s *Server) cacheDB() *ent.Client {
	if s.DB == nil {
		return nil
	}
	return s.DB.Client
}

func cacheMetaFromGallery(g *model.Gallery) model.GalleryCacheSnapshot {
	return model.GalleryCacheSnapshot{
		Title:       g.Title,
		TitleJPN:    g.TitleJPN,
		Category:    string(g.Category),
		Thumbnail:   g.Thumbnail,
		PageCount:   g.PageCount,
		Rating:      g.Rating,
		RatingCount: g.RatingCount,
		Uploader:    g.Uploader,
		PostedAt:    g.PostedAt,
		FileSize:    g.FileSize,
		Expunged:    g.Expunged,
		Tags:        g.Tags,
	}
}

func cacheMetaFromDetails(d model.GalleryDetail) model.GalleryCacheSnapshot {
	tags := make([]model.Tag, len(d.Tags))
	for i, t := range d.Tags {
		tags[i] = model.Tag(t)
	}
	return model.GalleryCacheSnapshot{
		Title:       d.Title,
		TitleJPN:    d.TitleJpn,
		Category:    string(model.MapCategory(d.Cat)),
		Thumbnail:   d.Cover,
		PageCount:   d.Length,
		Rating:      d.Rating,
		RatingCount: d.RatingCount,
		Uploader:    d.Uploader,
		Posted:      d.Posted,
		Language:    d.Language,
		Translated:  d.Translated == "TR",
		FileSize:    d.FileSize,
		Favorited:   d.Favorited,
		Tags:        tags,
	}
}

func galleryFromCache(row *ent.GalleryCache) *model.Gallery {
	return &model.Gallery{
		ID:          row.GalleryID,
		Token:       row.Token,
		Title:       row.Title,
		TitleJPN:    row.TitleJpn,
		Category:    model.GalleryCategory(row.Category),
		Thumbnail:   row.Thumbnail,
		PageCount:   row.PageCount,
		Rating:      row.Rating,
		RatingCount: row.RatingCount,
		Uploader:    row.Uploader,
		PostedAt:    row.PostedAt,
		Tags:        row.Tags,
		FileSize:    row.FileSize,
		Expunged:    row.Expunged,
	}
}

func galleryDetailsFromCache(row *ent.GalleryCache) gin.H {
	return gin.H{
		"id":           row.GalleryID,
		"token":        row.Token,
		"domain":       "",
		"title":        row.Title,
		"title_jpn":    row.TitleJpn,
		"cover":        row.Thumbnail,
		"category":     model.GalleryCategory(row.Category),
		"uploader":     row.Uploader,
		"posted":       row.Posted,
		"parent":       0,
		"visible":      "",
		"language":     row.Language,
		"translated":   row.Translated,
		"file_size":    row.FileSize,
		"page_count":   row.PageCount,
		"favorited":    row.Favorited,
		"rating_count": row.RatingCount,
		"rating":       row.Rating,
		"tags":         row.Tags,
	}
}

func (s *Server) handleCachedGallery(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok || s.cacheUnavailable(c) {
		return
	}
	ctx := c.Request.Context()

	if row, found, err := gallerycache.Get(ctx, s.cacheDB(), id, token); err != nil {
		slog.Warn("gallery cache read failed", "id", id, "error", err)
	} else if found && row.Title != "" {
		metrics.ObserveGalleryCache("gallery", "hit")
		c.Header("Cache-Control", "no-store")
		httpx.JSON(c, http.StatusOK, galleryFromCache(row))
		return
	} else {
		metrics.ObserveGalleryCache("gallery", "miss")
	}

	client, _, ok := s.upstreamClientForRequest(c)
	if !ok {
		return
	}
	meta, err := exhentai.PostGalleryMetadata(ctx, client, id, token)
	if err != nil {
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("exhentai api failed: %v", err)})
		return
	}
	gallery := model.ConvertMetadataToGallery(meta)
	if db := s.cacheDB(); db != nil {
		if cacheErr := gallerycache.UpsertMeta(ctx, db, id, token, cacheMetaFromGallery(gallery)); cacheErr != nil {
			slog.Warn("gallery cache meta upsert failed", "id", id, "error", cacheErr)
		} else {
			s.recordGalleryCacheChange(ctx, id, token)
		}
	}
	c.Header("Cache-Control", "no-store")
	httpx.JSON(c, http.StatusOK, gallery)
}

func (s *Server) handleCachedGalleryDetails(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok || s.cacheUnavailable(c) {
		return
	}
	ctx := c.Request.Context()

	// A details hit requires that details were actually scraped at least once
	// (DetailsFetchedAt is a completeness flag here, not a TTL); a metadata-only
	// row falls through and reads through upstream.
	if row, found, err := gallerycache.Get(ctx, s.cacheDB(), id, token); err != nil {
		slog.Warn("gallery cache read failed", "id", id, "error", err)
	} else if found && row.Title != "" && row.DetailsFetchedAt != nil {
		metrics.ObserveGalleryCache("details", "hit")
		c.Header("Cache-Control", "no-store")
		httpx.JSON(c, http.StatusOK, galleryDetailsFromCache(row))
		return
	} else {
		metrics.ObserveGalleryCache("details", "miss")
	}

	client, uid, ok := s.upstreamClientForRequest(c)
	if !ok {
		return
	}
	u := exhentai.GalleryURLAt(s.upstreamBase(ctx, uid), c.Param("id"), token)
	details, err := exhentai.ScrapeGalleryDetails(ctx, client, u)
	if err != nil {
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery details failed: %v", err)})
		return
	}
	if db := s.cacheDB(); db != nil {
		if cacheErr := gallerycache.UpsertDetails(ctx, db, id, token, cacheMetaFromDetails(details)); cacheErr != nil {
			slog.Warn("gallery cache details upsert failed", "id", id, "error", cacheErr)
		} else {
			s.recordGalleryCacheChange(ctx, id, token)
		}
	}
	c.Header("Cache-Control", "no-store")
	httpx.JSON(c, http.StatusOK, galleryDetailsJSON(details))
}

func (s *Server) handleCachedGalleryPages(c *gin.Context) {
	// NDJSON stream (plus a background revalidation); skip latency recording.
	metrics.MarkLongRunning(c)
	id, token, ok := parseGalleryIDToken(c)
	if !ok || s.cacheUnavailable(c) {
		return
	}
	ctx := c.Request.Context()
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}

	row, found, err := gallerycache.Get(ctx, s.cacheDB(), id, token)
	if err != nil {
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("read gallery cache failed: %v", err)})
		return
	}
	if found && len(row.Pages) > 0 {
		metrics.ObserveGalleryCache("pages", "hit")
		if replayErr := replayGalleryPages(c, c.Param("id"), token, len(row.Pages), row.Pages, row.Thumbnails); replayErr != nil {
			slog.Warn("cached gallery pages replay failed", "id", id, "error", replayErr)
		}
		// Serve immediately, then verify the upstream page count in the
		// background (stale-while-revalidate): a gallery that gained or lost
		// pages is replaced without blocking the hit. Probe at most once per
		// TTL so browsing does not translate into an upstream request per view.
		if row.PagesFetchedAt == nil || time.Since(*row.PagesFetchedAt) > cachedPageCountProbeTTL {
			go s.verifyCachedPageCount(uid, id, token, len(row.Pages))
		}
		return
	}

	// Miss: stream upstream and backfill on a complete walk.
	metrics.ObserveGalleryCache("pages", "miss")
	streamGalleryPagesNDJSON(c, c.Param("id"), token, func(ctx context.Context, emit func(int, []string, []model.GalleryPageThumb) error) (int, error) {
		return s.scrapeAndCache(ctx, uid, id, token, emit)
	})
}

// verifyCachedPageCount is the background stale-while-revalidate probe: it
// fetches only the gallery's ".gpc" total and, when it no longer matches the
// cached page count, runs a full cache-fill walk. Concurrent probes for the
// same gallery collapse via galleryPagesFillGroup.
func (s *Server) verifyCachedPageCount(userID ident.UUID, galleryID int64, token string, cached int) {
	if s.cacheDB() == nil {
		return
	}
	client, err := s.upstreamClient(context.Background(), userID)
	if err != nil {
		return
	}
	key := fmt.Sprintf("%s/%d:%s", userID, galleryID, token)
	galleryPagesFillGroup.Do(key, func() (any, error) {
		// The probe and the refresh walk get separate budgets: the walk can
		// take up to galleryPagesHardCap and must not consume the probe's
		// 30s window, or the detected page-count change would never persist.
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		u := exhentai.GalleryURLAt(s.upstreamBase(probeCtx, userID), strconv.FormatInt(galleryID, 10), token)
		total, err := exhentai.ScrapeGalleryTotal(probeCtx, client, u)
		probeCancel()
		if err != nil {
			slog.Debug("cached page-count check failed", "id", galleryID, "error", err)
			return nil, nil
		}
		if total == cached {
			return nil, nil
		}
		slog.Info("cached page count changed; refreshing", "id", galleryID, "cached", cached, "upstream", total)
		walkCtx, walkCancel := context.WithTimeout(context.Background(), galleryPagesHardCap)
		defer walkCancel()
		if _, err := s.scrapeAndCache(walkCtx, userID, galleryID, token, nil); err != nil {
			slog.Warn("cached page-count refresh failed", "id", galleryID, "error", err)
		}
		return nil, nil
	})
}
