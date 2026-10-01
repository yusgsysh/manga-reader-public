package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/readingprogress"
	"manga-reader/internal/gallerycache"

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

	// Metadata now lives in gallery_cache; the reading record only carries
	// progress. Batched lookup keeps this at two queries per page.
	refs := make([]gallerycache.Ref, 0, len(entities))
	for _, rp := range entities {
		refs = append(refs, gallerycache.Ref{GalleryID: rp.GalleryID, Token: rp.Token})
	}
	metaByRef, err := gallerycache.GetMany(ctx, s.DB.Client, refs)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("load gallery cache failed: %v", err)})
		return
	}

	items := make([]model.RecentlyReadItem, 0, len(entities))

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

		if meta := metaByRef[gallerycache.Ref{GalleryID: rp.GalleryID, Token: rp.Token}]; meta != nil {
			item.Title = meta.Title
			item.TitleJPN = meta.TitleJpn
			item.Category = model.GalleryCategory(meta.Category)
			item.Thumbnail = meta.Thumbnail
			item.Pages = meta.PageCount
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
