package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/readingprogress"

	"manga-reader/internal/exhentai"
	"manga-reader/internal/model"
)

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
	for _, b := range items {
		progress, _ := s.DB.Client.ReadingProgress.Query().
			Where(
				readingprogress.GalleryID(b.GalleryID),
				readingprogress.Token(b.Token),
			).
			Only(ctx)

		item := model.BookshelfItem{
			ID:        b.GalleryID,
			Token:     b.Token,
			Title:     b.Title,
			TitleJPN:  b.TitleJpn,
			Category:  model.GalleryCategory(b.Category),
			Thumbnail: b.Thumbnail,
			Pages:     b.PageCount,
			AddedAt:   b.AddedAt,
			UpdatedAt: b.UpdatedAt,
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

	meta, err := exhentai.PostGalleryMetadata(ctx, s.Client, id, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("exhentai api failed: %v", err)})
		return
	}

	gallery := model.ConvertMetadataToGallery(meta)
	bookshelfModel := model.GalleryToBookshelf(gallery)
	if bookshelfModel == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "convert gallery to bookshelf failed"})
		return
	}

	_, err = s.DB.Client.Bookshelf.Create().
		SetGalleryID(bookshelfModel.GalleryID).
		SetToken(bookshelfModel.Token).
		SetTitle(bookshelfModel.Title).
		SetTitleJpn(bookshelfModel.TitleJPN).
		SetCategory(string(bookshelfModel.Category)).
		SetThumbnail(bookshelfModel.Thumbnail).
		SetPageCount(bookshelfModel.PageCount).
		Save(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("add bookshelf failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, model.BookshelfMutationResponse{Success: true, InBookshelf: true})
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

	_, err = s.DB.Client.Bookshelf.Delete().
		Where(
			bookshelf.GalleryID(id),
			bookshelf.Token(token),
		).
		Exec(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("remove bookshelf failed: %v", err)})
		return
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

	exists, err := s.DB.Client.ReadingProgress.Query().
		Where(
			readingprogress.GalleryID(id),
			readingprogress.Token(token),
		).
		Exist(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("check reading progress: %v", err)})
		return
	}

	if exists {
		err = s.DB.Client.ReadingProgress.Update().
			Where(
				readingprogress.GalleryID(id),
				readingprogress.Token(token),
			).
			SetCurrentPage(req.CurrentPage).
			SetProgress(req.Progress).
			SetCompleted(req.Completed).
			SetUpdatedAt(now).
			Exec(ctx)
	} else {
		_, err = s.DB.Client.ReadingProgress.Create().
			SetGalleryID(id).
			SetToken(token).
			SetCurrentPage(req.CurrentPage).
			SetProgress(req.Progress).
			SetCompleted(req.Completed).
			SetStartedAt(now).
			SetUpdatedAt(now).
			Save(ctx)
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

func (s *Server) handleRecentlyRead(c *gin.Context) {
	ctx := c.Request.Context()

	entities, err := s.DB.Client.ReadingProgress.Query().
		Order(ent.Desc(readingprogress.FieldUpdatedAt)).
		Limit(25).
		All(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list recently read failed: %v", err)})
		return
	}

	items := make([]model.RecentlyReadItem, 0, len(entities))
	for _, rp := range entities {
		item := model.RecentlyReadItem{
			ID:   rp.GalleryID,
			Token: rp.Token,
			Reading: model.ReadingProgress{
				GalleryID:   rp.GalleryID,
				Token:       rp.Token,
				CurrentPage: rp.CurrentPage,
				Progress:    rp.Progress,
				Completed:   rp.Completed,
				StartedAt:   rp.StartedAt,
				UpdatedAt:   rp.UpdatedAt,
			},
		}

		b, err := s.DB.Client.Bookshelf.Query().
			Where(
				bookshelf.GalleryID(rp.GalleryID),
				bookshelf.Token(rp.Token),
			).
			Only(ctx)
		if err == nil {
			item.Title = b.Title
			item.TitleJPN = b.TitleJpn
			item.Category = model.GalleryCategory(b.Category)
			item.Thumbnail = b.Thumbnail
			item.Pages = b.PageCount
		}

		items = append(items, item)
	}

	c.JSON(http.StatusOK, model.RecentlyReadResponse{Results: items})
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
