package handler

import (
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/readingprogress"

	"manga-reader/internal/model"
)

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
