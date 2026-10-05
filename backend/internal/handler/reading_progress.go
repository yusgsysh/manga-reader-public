package handler

import (
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/readingprogress"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/model"
	synclib "manga-reader/internal/sync"
)

func (s *Server) handleGetProgress(c *gin.Context) {
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

	p, err := s.DB.Client.ReadingProgress.Query().
		Where(
			readingprogress.GalleryID(id),
			readingprogress.Token(token),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			c.JSON(http.StatusOK, &model.ReadingProgress{
				GalleryID: id,
				Token:     token,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("get progress failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, &model.ReadingProgress{
		GalleryID:   p.GalleryID,
		Token:       p.Token,
		CurrentPage: p.CurrentPage,
		Progress:    p.Progress,
		Completed:   p.Completed,
		CreatedAt:   &p.CreatedAt,
		UpdatedAt:   &p.UpdatedAt,
	})
}

func (s *Server) handleUpdateProgress(c *gin.Context) {
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

	var req model.UpdateReadingProgressRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}

	if req.CurrentPage < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "current_page must be >= 0"})
		return
	}

	if req.Progress < 0 || req.Progress > 1 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "progress must be between 0 and 1"})
		return
	}

	if req.Completed {
		req.Progress = 1
	}

	ctx := c.Request.Context()
	now := time.Now().UTC()

	existing, err := s.DB.Client.ReadingProgress.Query().
		Where(
			readingprogress.GalleryID(id),
			readingprogress.Token(token),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("check reading progress: %v", err)})
		return
	}

	if existing == nil {
		_, err = s.DB.Client.ReadingProgress.Create().
			SetGalleryID(id).
			SetToken(token).
			SetCurrentPage(req.CurrentPage).
			SetProgress(req.Progress).
			SetCompleted(req.Completed).
			SetCreatedAt(now).
			SetUpdatedAt(now).
			Save(ctx)
	} else {
		update := s.DB.Client.ReadingProgress.Update().
			Where(
				readingprogress.GalleryID(id),
				readingprogress.Token(token),
			).
			SetCurrentPage(req.CurrentPage).
			SetProgress(req.Progress).
			SetCompleted(req.Completed).
			SetUpdatedAt(now)

		err = update.Exec(ctx)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("update progress failed: %v", err)})
		return
	}

	if s.syncSvc != nil {
		synclib.LogRecordError(
			s.syncSvc.RecordUpsert(ctx, synclib.EntityReadingProgress, id, token),
			synclib.EntityReadingProgress, id, token)
	}

	// Reading activity refreshes the bookshelf entry's updated_at so the
	// shelf can be ordered by recent activity. Best-effort: items not in the
	// bookshelf affect zero rows.
	if n, bErr := s.DB.Client.Bookshelf.Update().
		Where(
			bookshelf.GalleryID(id),
			bookshelf.Token(token),
		).
		SetUpdatedAt(now).
		Save(ctx); bErr != nil {
		slog.Warn("bump bookshelf updated_at failed", "error", bErr)
	} else if n > 0 && s.syncSvc != nil {
		synclib.LogRecordError(
			s.syncSvc.RecordUpsert(ctx, synclib.EntityBookshelf, id, token),
			synclib.EntityBookshelf, id, token)
	}

	p, err := s.DB.Client.ReadingProgress.Query().
		Where(
			readingprogress.GalleryID(id),
			readingprogress.Token(token),
		).
		Only(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("read progress failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, &model.ReadingProgress{
		GalleryID:   p.GalleryID,
		Token:       p.Token,
		CurrentPage: p.CurrentPage,
		Progress:    p.Progress,
		Completed:   p.Completed,
		CreatedAt:   &p.CreatedAt,
		UpdatedAt:   &p.UpdatedAt,
	})
}

func (s *Server) handleReadingProgressCleanup(c *gin.Context) {
	daysStr := c.DefaultQuery("days", "30")

	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "days must be a non-negative integer"})
		return
	}

	ctx := c.Request.Context()

	// Collect the keys first so the purge can be replicated as tombstones;
	// history cleanup is global by design (both ends keep the same retention).
	var purgeKeys []synclib.Key
	if s.syncSvc != nil {
		pred := readingprogress.UpdatedAtLT(time.Now().UTC().AddDate(0, 0, -days))
		if days == 0 {
			pred = nil
		}
		q := s.DB.Client.ReadingProgress.Query()
		if pred != nil {
			q = q.Where(pred)
		}
		rows, qErr := q.All(ctx)
		if qErr != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cleanup reading progress"})
			return
		}
		purgeKeys = make([]synclib.Key, 0, len(rows))
		for _, row := range rows {
			purgeKeys = append(purgeKeys, synclib.Key{GalleryID: row.GalleryID, Token: row.Token})
		}
	}

	var deleted int
	if days == 0 {
		deleted, err = s.DB.Client.ReadingProgress.Delete().Exec(ctx)
	} else {
		cutoff := time.Now().UTC().AddDate(0, 0, -days)
		deleted, err = s.DB.Client.ReadingProgress.Delete().
			Where(readingprogress.UpdatedAtLT(cutoff)).
			Exec(ctx)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "failed to cleanup reading progress"})
		return
	}

	if s.syncSvc != nil && len(purgeKeys) > 0 {
		if recErr := s.syncSvc.RecordDeletes(ctx, synclib.EntityReadingProgress, purgeKeys); recErr != nil {
			slog.Warn("record sync purge failed", "error", recErr)
		}
	}

	if s.DB != nil {
		if n, cleanErr := s.DB.CleanupGalleryCache(ctx); cleanErr != nil {
			slog.Warn("gallery cache cleanup failed", "error", cleanErr)
		} else if n > 0 {
			slog.Debug("gallery cache cleanup", "deleted", n)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"days":    days,
		"deleted": deleted,
	})
}
