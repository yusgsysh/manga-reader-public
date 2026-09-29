package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/readingprogress"

	"manga-reader/internal/model"
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
		StartedAt:   p.StartedAt,
		UpdatedAt:   p.UpdatedAt,
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
			SetTitle(req.Title).
			SetTitleJpn(req.TitleJPN).
			SetCategory(req.Category).
			SetThumbnail(req.Thumbnail).
			SetPageCount(req.PageCount).
			SetStartedAt(now).
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

		// Snapshot gallery metadata on the first opportunity only; never
		// overwrite already stored values or clear them with empty ones.
		if existing.Title == "" && req.Title != "" {
			update.SetTitle(req.Title)
		}
		if existing.TitleJpn == "" && req.TitleJPN != "" {
			update.SetTitleJpn(req.TitleJPN)
		}
		if existing.Category == "" && req.Category != "" {
			update.SetCategory(req.Category)
		}
		if existing.Thumbnail == "" && req.Thumbnail != "" {
			update.SetThumbnail(req.Thumbnail)
		}
		if existing.PageCount == 0 && req.PageCount > 0 {
			update.SetPageCount(req.PageCount)
		}

		err = update.Exec(ctx)
	}
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("update progress failed: %v", err)})
		return
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
		StartedAt:   p.StartedAt,
		UpdatedAt:   p.UpdatedAt,
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

	c.JSON(http.StatusOK, gin.H{
		"days":    days,
		"deleted": deleted,
	})
}
