package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/database"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/model"
)

func (s *Server) handleBookshelfList(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "0")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		page = 0
	}

	repo := database.NewBookshelfRepository(s.DB.Conn)
	resp, err := repo.List(c.Request.Context(), page, 25)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list bookshelf failed: %v", err)})
		return
	}

	progressRepo := database.NewReadingProgressRepository(s.DB.Conn)
	for i := range resp.Results {
		progress, err := progressRepo.Get(c.Request.Context(), resp.Results[i].ID, resp.Results[i].Token)
		if err == nil && progress != nil && progress.UpdatedAt != nil {
			resp.Results[i].Reading = progress
		}
	}

	c.JSON(http.StatusOK, resp)
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

	bookshelfRepo := database.NewBookshelfRepository(s.DB.Conn)
	exists, _, err := bookshelfRepo.Exists(ctx, id, token)
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
	bookshelf := model.GalleryToBookshelf(gallery)
	if bookshelf == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "convert gallery to bookshelf failed"})
		return
	}

	if err := bookshelfRepo.Add(ctx, bookshelf); err != nil {
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

	repo := database.NewBookshelfRepository(s.DB.Conn)
	if err := repo.Remove(c.Request.Context(), id, token); err != nil {
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

	repo := database.NewBookshelfRepository(s.DB.Conn)
	exists, addedAt, err := repo.Exists(c.Request.Context(), id, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("check bookshelf failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, model.BookshelfStatus{
		InBookshelf: exists,
		AddedAt:     addedAt,
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

	repo := database.NewReadingProgressRepository(s.DB.Conn)
	progress, err := repo.Get(c.Request.Context(), id, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("get progress failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, progress)
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

	repo := database.NewReadingProgressRepository(s.DB.Conn)
	progress, err := repo.Upsert(c.Request.Context(), id, token, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("update progress failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, progress)
}

func (s *Server) handleRecentlyRead(c *gin.Context) {
	repo := database.NewReadingProgressRepository(s.DB.Conn)
	items, err := repo.ListRecentlyRead(c.Request.Context(), 25)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list recently read failed: %v", err)})
		return
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
	repo := database.NewReadingProgressRepository(s.DB.Conn)

	var deleted int64
	if days == 0 {
		deleted, err = repo.DeleteAll(ctx)
	} else {
		cutoff := time.Now().UTC().AddDate(0, 0, -days)
		deleted, err = repo.DeleteBefore(ctx, cutoff)
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
