package main

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

func (a *App) handleBookshelfList(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "0")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		page = 0
	}

	repo := NewBookshelfRepository(a.DB.conn)
	resp, err := repo.List(c.Request.Context(), page, 25)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list bookshelf failed: %v", err)})
		return
	}

	// 填充阅读进度
	progressRepo := NewReadingProgressRepository(a.DB.conn)
	for i := range resp.Results {
		progress, err := progressRepo.Get(c.Request.Context(), resp.Results[i].ID, resp.Results[i].Token)
		if err == nil && progress != nil && progress.UpdatedAt != nil {
			resp.Results[i].Reading = progress
		}
	}

	c.JSON(http.StatusOK, resp)
}

func (a *App) handleBookshelfAdd(c *gin.Context) {
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

	// 检查是否已在书架
	bookshelfRepo := NewBookshelfRepository(a.DB.conn)
	exists, _, err := bookshelfRepo.Exists(ctx, id, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("check bookshelf failed: %v", err)})
		return
	}
	if exists {
		c.JSON(http.StatusOK, BookshelfMutationResponse{Success: true, InBookshelf: true})
		return
	}

	// 从 ExHentai 获取 Gallery 信息
	meta, err := PostGalleryMetadata(ctx, a.Client, id, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("exhentai api failed: %v", err)})
		return
	}

	gallery := ConvertMetadataToGallery(meta)
	bookshelf := GalleryToBookshelf(gallery)
	if bookshelf == nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "convert gallery to bookshelf failed"})
		return
	}

	if err := bookshelfRepo.Add(ctx, bookshelf); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("add bookshelf failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, BookshelfMutationResponse{Success: true, InBookshelf: true})
}

func (a *App) handleBookshelfRemove(c *gin.Context) {
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

	repo := NewBookshelfRepository(a.DB.conn)
	if err := repo.Remove(c.Request.Context(), id, token); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("remove bookshelf failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, BookshelfMutationResponse{Success: true, InBookshelf: false})
}

func (a *App) handleBookshelfStatus(c *gin.Context) {
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

	repo := NewBookshelfRepository(a.DB.conn)
	exists, addedAt, err := repo.Exists(c.Request.Context(), id, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("check bookshelf failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, BookshelfStatus{
		InBookshelf: exists,
		AddedAt:     addedAt,
	})
}

func (a *App) handleGetProgress(c *gin.Context) {
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

	repo := NewReadingProgressRepository(a.DB.conn)
	progress, err := repo.Get(c.Request.Context(), id, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("get progress failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, progress)
}

func (a *App) handleUpdateProgress(c *gin.Context) {
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

	var req UpdateReadingProgressRequest
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

	repo := NewReadingProgressRepository(a.DB.conn)
	progress, err := repo.Upsert(c.Request.Context(), id, token, &req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("update progress failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, progress)
}

func (a *App) handleRecentlyRead(c *gin.Context) {
	repo := NewReadingProgressRepository(a.DB.conn)
	items, err := repo.ListRecentlyRead(c.Request.Context(), 25)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list recently read failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, RecentlyReadResponse{Results: items})
}
