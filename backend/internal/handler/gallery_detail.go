package handler

import (
	"fmt"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/httpx"
	"manga-reader/internal/model"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"
)

// galleryDetailsJSON shapes a scraped gallery detail for the JSON response. It
// is shared by the online details endpoint and the cache read-through path.
func galleryDetailsJSON(details model.GalleryDetail) gin.H {
	tags := make([]model.Tag, len(details.Tags))
	for i, t := range details.Tags {
		tags[i] = model.Tag(t)
	}
	return gin.H{
		"id":            details.GalleryID,
		"token":         details.Token,
		"domain":        details.Domain,
		"title":         details.Title,
		"title_jpn":     details.TitleJpn,
		"cover":         details.Cover,
		"category":      model.MapCategory(details.Cat),
		"uploader":      details.Uploader,
		"posted":        details.Posted,
		"parent":        details.Parent,
		"visible":       details.Visible,
		"language":      details.Language,
		"translated":    details.Translated == "TR",
		"file_size":     details.FileSize,
		"page_count":    details.Length,
		"favorited":     details.Favorited,
		"rating_count":  details.RatingCount,
		"rating":        details.Rating,
		"torrent_count": details.TorrentCount,
		"tags":          tags,
	}
}

// parseGalleryIDToken reads and validates the :id/:token path params.
func parseGalleryIDToken(c *gin.Context) (int64, string, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid gallery id"})
		return 0, "", false
	}
	token := c.Param("token")
	if token == "" {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid gallery token"})
		return 0, "", false
	}
	return id, token, true
}

// cacheUnavailable reports whether the offline cache backend is configured.
func (s *Server) cacheUnavailable(c *gin.Context) bool {
	if s.cacheDB() == nil {
		httpx.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "gallery cache not configured"})
		return true
	}
	return false
}

// handleGetGallery is the online metadata endpoint. It always fetches upstream
// and never reads or writes the gallery cache; the cache endpoint is the
// read-through path and the frontend uses this one to stay fresh.
func (s *Server) handleGetGallery(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}

	client, _, ok := s.upstreamClientForRequest(c)
	if !ok {
		return
	}
	meta, err := exhentai.PostGalleryMetadata(c.Request.Context(), client, id, token)
	if err != nil {
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("exhentai api failed: %v", err)})
		return
	}
	httpx.JSON(c, http.StatusOK, model.ConvertMetadataToGallery(meta))
}

// handleGalleryDetails is the online details endpoint. Like handleGetGallery it
// always scrapes upstream and never touches the cache.
func (s *Server) handleGalleryDetails(c *gin.Context) {
	_, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()
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

	httpx.JSON(c, http.StatusOK, galleryDetailsJSON(details))
}
