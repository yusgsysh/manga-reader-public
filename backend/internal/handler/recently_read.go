package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/readingprogress"

	"manga-reader/internal/model"
)

// recentlyReadPageSize is the fixed number of records per page.
const recentlyReadPageSize = 25

func (s *Server) handleRecentlyRead(c *gin.Context) {
	pageStr := c.DefaultQuery("page", "0")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid page"})
		return
	}

	ctx := c.Request.Context()
	offset := page * recentlyReadPageSize

	total, err := s.DB.Client.ReadingProgress.Query().Count(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("count recently read failed: %v", err)})
		return
	}
	totalPages := (total + recentlyReadPageSize - 1) / recentlyReadPageSize

	entities, err := s.DB.Client.ReadingProgress.Query().
		Order(ent.Desc(readingprogress.FieldUpdatedAt)).
		Offset(offset).
		Limit(recentlyReadPageSize).
		All(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list recently read failed: %v", err)})
		return
	}

	items := make([]model.RecentlyReadItem, 0, len(entities))

	ids := make([]int64, 0, len(entities))
	for _, rp := range entities {
		ids = append(ids, rp.GalleryID)
	}

	shelfByKey := make(map[galleryRef]*ent.Bookshelf, len(ids))
	if len(ids) > 0 {
		shelfRows, err := s.DB.Client.Bookshelf.Query().
			Where(bookshelf.GalleryIDIn(ids...)).
			All(ctx)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list bookshelf failed: %v", err)})
			return
		}
		for _, b := range shelfRows {
			shelfByKey[galleryRef{id: b.GalleryID, token: b.Token}] = b
		}
	}

	for _, rp := range entities {
		item := model.RecentlyReadItem{
			ID:    rp.GalleryID,
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

		if b := shelfByKey[galleryRef{id: rp.GalleryID, token: rp.Token}]; b != nil {
			item.Title = b.Title
			item.TitleJPN = b.TitleJpn
			item.Category = model.GalleryCategory(b.Category)
			item.Thumbnail = b.Thumbnail
			item.Pages = b.PageCount
		}

		items = append(items, item)
	}

	c.JSON(http.StatusOK, model.RecentlyReadResponse{
		Page:       page,
		PageSize:   recentlyReadPageSize,
		Total:      total,
		TotalPages: totalPages,
		Results:    items,
	})
}
