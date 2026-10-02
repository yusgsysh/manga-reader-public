package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/readingprogress"

	"manga-reader/internal/exhentai"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/model"
)

// galleryPrefetchGroup coalesces concurrent background prefetches for the same
// gallery (e.g. repeated add-to-bookshelf requests).
var galleryPrefetchGroup singleflight.Group

const galleryPrefetchTimeout = 60 * time.Second

// prefetchGallery fills the gallery cache (metadata + pages) in the background
// so the item stays usable offline. Failures are logged and otherwise ignored.
func (s *Server) prefetchGallery(galleryID int64, token string, seed *model.Gallery) {
	if s.Client == nil {
		return
	}

	key := fmt.Sprintf("%d:%s", galleryID, token)
	galleryPrefetchGroup.Do(key, func() (any, error) {
		ctx, cancel := context.WithTimeout(context.Background(), galleryPrefetchTimeout)
		defer cancel()

		db := s.cacheDB()
		if db != nil && seed != nil {
			if err := gallerycache.UpsertMeta(ctx, db, galleryID, token, cacheMetaFromGallery(seed)); err != nil {
				slog.Warn("gallery prefetch meta failed", "id", galleryID, "error", err)
			}
		}

		u := exhentai.GalleryURL(strconv.FormatInt(galleryID, 10), token)

		// Shares the in-flight scrape with the streaming /pages handler so a
		// gallery is never fetched from upstream twice at the same time.
		if _, scrapeErr := s.scrapeGalleryPages(ctx, galleryID, token, nil); scrapeErr != nil {
			slog.Debug("gallery prefetch pages failed", "id", galleryID, "error", scrapeErr)
		}

		if details, err := exhentai.ScrapeGalleryDetails(ctx, s.Client, u); err != nil {
			slog.Debug("gallery prefetch details failed", "id", galleryID, "error", err)
		} else if db != nil {
			if err := gallerycache.UpsertDetails(ctx, db, galleryID, token, cacheMetaFromDetails(details)); err != nil {
				slog.Warn("gallery prefetch details upsert failed", "id", galleryID, "error", err)
			}
		}
		return nil, nil
	})
}

// galleryRef identifies a gallery by its id/token pair.
type galleryRef struct {
	id    int64
	token string
}

func (s *Server) handleBookshelfList(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "0")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		page = 0
	}

	ctx := c.Request.Context()
	pageSize := 25
	offset := page * pageSize

	total, err := s.DB.Client.Bookshelf.Query().Count(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("count bookshelf failed: %v", err)})
		return
	}

	totalPages := (total + pageSize - 1) / pageSize

	items, err := s.DB.Client.Bookshelf.Query().
		Order(ent.Desc(bookshelf.FieldAddedAt)).
		Offset(offset).
		Limit(pageSize).
		All(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list bookshelf failed: %v", err)})
		return
	}

	results := make([]model.BookshelfItem, 0, len(items))

	ids := make([]int64, 0, len(items))
	for _, b := range items {
		ids = append(ids, b.GalleryID)
	}

	progressByKey := make(map[galleryRef]*ent.ReadingProgress, len(ids))
	if len(ids) > 0 {
		progressRows, err := s.DB.Client.ReadingProgress.Query().
			Where(readingprogress.GalleryIDIn(ids...)).
			All(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list reading progress failed: %v", err)})
			return
		}
		for _, p := range progressRows {
			progressByKey[galleryRef{id: p.GalleryID, token: p.Token}] = p
		}
	}

	// Metadata lives in gallery_cache (the bookshelf table only stores the
	// reference), so a bookshelf entry always reflects the latest known
	// upstream data regardless of when or how it was added.
	refs := make([]gallerycache.Ref, 0, len(items))
	for _, b := range items {
		refs = append(refs, gallerycache.Ref{GalleryID: b.GalleryID, Token: b.Token})
	}
	metaByRef, err := gallerycache.GetMany(ctx, s.DB.Client, refs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("load gallery cache failed: %v", err)})
		return
	}

	for _, b := range items {
		progress := progressByKey[galleryRef{id: b.GalleryID, token: b.Token}]

		item := model.BookshelfItem{
			ID:        b.GalleryID,
			Token:     b.Token,
			AddedAt:   b.AddedAt,
			UpdatedAt: b.UpdatedAt,
		}

		if meta := metaByRef[gallerycache.Ref{GalleryID: b.GalleryID, Token: b.Token}]; meta != nil {
			item.Title = meta.Title
			item.TitleJPN = meta.TitleJpn
			item.Category = model.GalleryCategory(meta.Category)
			item.Thumbnail = meta.Thumbnail
			item.Pages = meta.PageCount
		}

		if progress != nil && progress.UpdatedAt != nil {
			item.Reading = &model.ReadingProgress{
				GalleryID:   progress.GalleryID,
				Token:       progress.Token,
				CurrentPage: progress.CurrentPage,
				Progress:    progress.Progress,
				Completed:   progress.Completed,
				StartedAt:   progress.StartedAt,
				UpdatedAt:   progress.UpdatedAt,
			}
		}

		results = append(results, item)
	}

	c.JSON(http.StatusOK, model.BookshelfListResponse{
		Page:       page,
		PageSize:   len(results),
		Total:      total,
		TotalPages: totalPages,
		Results:    results,
	})
}

func (s *Server) handleBookshelfAdd(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery id"})
		return
	}

	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery token"})
		return
	}

	ctx := c.Request.Context()

	exists, err := s.DB.Client.Bookshelf.Query().
		Where(
			bookshelf.GalleryID(id),
			bookshelf.Token(token),
		).
		Exist(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("check bookshelf failed: %v", err)})
		return
	}
	if exists {
		c.JSON(http.StatusOK, model.BookshelfMutationResponse{Success: true, InBookshelf: true})
		return
	}

	// Resolve metadata once: it reports the offline flag and seeds the
	// gallery_cache row the list endpoint reads from. Only the reference is
	// stored in the bookshelf table itself.
	gallery, offline, err := s.galleryForBookshelf(ctx, id, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("exhentai api failed: %v", err)})
		return
	}

	_, err = s.DB.Client.Bookshelf.Create().
		SetGalleryID(id).
		SetToken(token).
		Save(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("add bookshelf failed: %v", err)})
		return
	}

	// Warm the offline cache in the background (metadata + pages).
	go s.prefetchGallery(id, token, gallery)

	c.JSON(http.StatusOK, model.BookshelfMutationResponse{
		Success:     true,
		InBookshelf: true,
		Offline:     offline,
	})
}

// galleryForBookshelf resolves gallery metadata for a bookshelf snapshot. When
// the upstream ExHentai API is unreachable it falls back to the cached snapshot
// so collecting a gallery keeps working offline. The returned bool reports
// whether the cache fallback was used.
func (s *Server) galleryForBookshelf(ctx context.Context, id int64, token string) (*model.Gallery, bool, error) {
	meta, err := exhentai.PostGalleryMetadata(ctx, s.Client, id, token)
	if err == nil {
		return model.ConvertMetadataToGallery(meta), false, nil
	}

	db := s.cacheDB()
	if db == nil {
		return nil, false, err
	}

	row, found, cacheErr := gallerycache.Get(ctx, db, id, token)
	if cacheErr != nil {
		return nil, false, fmt.Errorf("%w (cache read failed: %v)", err, cacheErr)
	}
	if !found || row.Title == "" {
		return nil, false, err
	}
	return galleryFromCache(row), true, nil
}

func (s *Server) handleBookshelfRemove(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery id"})
		return
	}

	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery token"})
		return
	}

	ctx := c.Request.Context()
	_, err = s.DB.Client.Bookshelf.Delete().
		Where(
			bookshelf.GalleryID(id),
			bookshelf.Token(token),
		).
		Exec(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("remove bookshelf failed: %v", err)})
		return
	}

	if s.DB != nil {
		if n, cleanErr := s.DB.CleanupGalleryCache(ctx); cleanErr != nil {
			slog.Warn("gallery cache cleanup failed", "error", cleanErr)
		} else if n > 0 {
			slog.Debug("gallery cache cleanup", "deleted", n)
		}
	}

	c.JSON(http.StatusOK, model.BookshelfMutationResponse{Success: true, InBookshelf: false})
}

func (s *Server) handleBookshelfStatus(c *gin.Context) {
	idStr := c.Param("id")
	id, err := strconv.ParseInt(idStr, 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery id"})
		return
	}

	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery token"})
		return
	}

	ctx := c.Request.Context()

	b, err := s.DB.Client.Bookshelf.Query().
		Where(
			bookshelf.GalleryID(id),
			bookshelf.Token(token),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			c.JSON(http.StatusOK, model.BookshelfStatus{InBookshelf: false})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("check bookshelf failed: %v", err)})
		return
	}

	addedAt := b.AddedAt
	c.JSON(http.StatusOK, model.BookshelfStatus{
		InBookshelf: true,
		AddedAt:     &addedAt,
	})
}
