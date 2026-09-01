package handler

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/exhentai"
	"manga-reader/internal/model"
)

func (s *Server) handleGetGallery(c *gin.Context) {
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

	meta, err := exhentai.PostGalleryMetadata(ctx, s.Client, id, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("exhentai api failed: %v", err)})
		return
	}

	gallery := model.ConvertMetadataToGallery(meta)
	c.JSON(http.StatusOK, gallery)
}

func (s *Server) handleGalleryDetails(c *gin.Context) {
	id := c.Param("id")
	token := c.Param("token")
	if id == "" || token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery id or token"})
		return
	}

	u := exhentai.GalleryURL(id, token)
	ctx := c.Request.Context()

	details, err := exhentai.ScrapeGalleryDetails(ctx, s.Client, u)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery details failed: %v", err)})
		return
	}

	tags := make([]model.Tag, len(details.Tags))
	for i, t := range details.Tags {
		tags[i] = model.Tag{Namespace: t.Namespace, Name: t.Name}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           details.GalleryID,
		"token":        details.Token,
		"domain":       details.Domain,
		"title":        details.Title,
		"title_jpn":    details.TitleJpn,
		"cover":        details.Cover,
		"category":     model.MapCategory(details.Cat),
		"uploader":     details.Uploader,
		"posted":       details.Posted,
		"parent":       details.Parent,
		"visible":      details.Visible,
		"language":     details.Language,
		"translated":   details.Translated == "TR",
		"file_size":    details.FileSize,
		"page_count":   details.Length,
		"favorited":    details.Favorited,
		"rating_count": details.RatingCount,
		"rating":       details.Rating,
		"tags":         tags,
		"page_urls":    details.PageUrls,
	})
}

func (s *Server) handleGalleryPages(c *gin.Context) {
	id := c.Param("id")
	token := c.Param("token")
	if id == "" || token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery id or token"})
		return
	}

	u := exhentai.GalleryURL(id, token)
	ctx := c.Request.Context()

	details, err := exhentai.ScrapeGalleryDetails(ctx, s.Client, u)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery details failed: %v", err)})
		return
	}

	type pageInfo struct {
		PageURL string `json:"page_url"`
		Index   int    `json:"index"`
	}

	pages := make([]pageInfo, len(details.PageUrls))
	for i, p := range details.PageUrls {
		pages[i] = pageInfo{
			PageURL: p,
			Index:   i,
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":    id,
		"token": token,
		"total": len(pages),
		"pages": pages,
	})
}

func (s *Server) handleSearch(c *gin.Context) {
	keyword := c.Query("q")
	categoryStr := c.Query("categories")
	site := c.DefaultQuery("site", "exhentai")
	pageStr := c.DefaultQuery("page", "0")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		page = 0
	}

	minPages, err := parseOptionalInt(c, "min_pages")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid min_pages"})
		return
	}
	maxPages, err := parseOptionalInt(c, "max_pages")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid max_pages"})
		return
	}
	if minPages != nil && *minPages < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "min_pages must be >= 0"})
		return
	}
	if maxPages != nil && *maxPages < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "max_pages must be >= 0"})
		return
	}
	if minPages != nil && maxPages != nil && *minPages > *maxPages {
		c.JSON(http.StatusBadRequest, gin.H{"error": "min_pages must be <= max_pages"})
		return
	}

	minRating, err := parseOptionalInt(c, "min_rating")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid min_rating"})
		return
	}
	if minRating != nil {
		switch *minRating {
		case 2, 3, 4, 5:
		default:
			c.JSON(http.StatusBadRequest, gin.H{"error": "min_rating must be 2, 3, 4, or 5"})
			return
		}
	}

	hasTorrent, err := parseOptionalBool(c, "has_torrent")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid has_torrent"})
		return
	}
	includeExpunged, err := parseOptionalBool(c, "include_expunged")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid include_expunged"})
		return
	}
	searchName, err := parseOptionalBool(c, "search_name")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid search_name"})
		return
	}
	searchTags, err := parseOptionalBool(c, "search_tags")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid search_tags"})
		return
	}
	searchDescription, err := parseOptionalBool(c, "search_description")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid search_description"})
		return
	}
	includeLowPowerTags, err := parseOptionalBool(c, "include_low_power_tags")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid include_low_power_tags"})
		return
	}
	includeDownvotedTags, err := parseOptionalBool(c, "include_downvoted_tags")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid include_downvoted_tags"})
		return
	}
	disableLanguageFilter, err := parseOptionalBool(c, "disable_language_filter")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid disable_language_filter"})
		return
	}
	disableUploaderFilter, err := parseOptionalBool(c, "disable_uploader_filter")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid disable_uploader_filter"})
		return
	}
	disableTagFilter, err := parseOptionalBool(c, "disable_tag_filter")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid disable_tag_filter"})
		return
	}

	ctx := c.Request.Context()

	var siteURL string
	if site == "ehentai" {
		siteURL = exhentai.EhentaiURL
	} else {
		siteURL = exhentai.ExhentaiURL
	}

	var categories []string
	if categoryStr != "" {
		categories = strings.Split(categoryStr, ",")
	}

	opts := &exhentai.SearchOptions{
		MinPages:             minPages,
		MaxPages:             maxPages,
		MinRating:            minRating,
		HasTorrent:           hasTorrent,
		IncludeExpunged:      includeExpunged,
		SearchName:           searchName,
		SearchTags:           searchTags,
		SearchDescription:    searchDescription,
		IncludeLowPowerTags:  includeLowPowerTags,
		IncludeDownvotedTags: includeDownvotedTags,
		DisableLanguageFilter: disableLanguageFilter,
		DisableUploaderFilter: disableUploaderFilter,
		DisableTagFilter:      disableTagFilter,
	}

	total, results, err := exhentai.ScrapeSearch(ctx, s.Client, siteURL, keyword, categories, page, opts)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("search failed: %v", err)})
		return
	}

	type searchResult struct {
		ID       int64                 `json:"id"`
		Token    string                `json:"token"`
		Title    string                `json:"title"`
		Category model.GalleryCategory `json:"category"`
		Cover    string                `json:"cover"`
		Posted   string                `json:"posted"`
		Rating   float64               `json:"rating"`
		URL      string                `json:"url"`
		Tags     []string              `json:"tags"`
		Uploader string                `json:"uploader"`
		Pages    int                   `json:"pages"`
		Domain   string                `json:"domain"`
	}

	items := make([]searchResult, len(results))
	for i, r := range results {
		items[i] = searchResult{
			ID:       int64(r.GalleryID),
			Token:    r.Token,
			Title:    r.Title,
			Category: model.MapCategory(r.Cat),
			Cover:    r.Cover,
			Posted:   r.Posted,
			Rating:   r.Rating,
			URL:      r.URL,
			Tags:     r.Tags,
			Uploader: r.Uploader,
			Pages:    r.Pages,
			Domain:   r.Domain,
		}
	}

	const pageSize = 25
	totalPages := (total + pageSize - 1) / pageSize

	c.JSON(http.StatusOK, gin.H{
		"total":       total,
		"total_pages": totalPages,
		"page":        page,
		"page_size":   len(items),
		"results":     items,
	})
}

func (s *Server) handlePageImage(c *gin.Context) {
	pageURL := c.Query("url")
	if pageURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing url parameter"})
		return
	}

	ctx := c.Request.Context()

	data, contentType, err := exhentai.FetchPageImage(ctx, s.Client, pageURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download image failed: %v", err)})
		return
	}

	c.Data(http.StatusOK, contentType, data)
}

func (s *Server) handleGalleryList(listURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		pageStr := c.DefaultQuery("page", "0")
		page, _ := strconv.Atoi(pageStr)
		if page < 0 {
			page = 0
		}

		ctx := c.Request.Context()

		results, err := exhentai.ScrapeGalleryList(ctx, s.Client, listURL, page)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery list failed: %v", err)})
			return
		}

		type galleryResult struct {
			ID       int64                 `json:"id"`
			Token    string                `json:"token"`
			Title    string                `json:"title"`
			Category model.GalleryCategory `json:"category"`
			Cover    string                `json:"cover"`
			Posted   string                `json:"posted"`
			Rating   float64               `json:"rating"`
			URL      string                `json:"url"`
			Tags     []string              `json:"tags"`
			Uploader string                `json:"uploader"`
			Pages    int                   `json:"pages"`
			Domain   string                `json:"domain"`
		}

		items := make([]galleryResult, len(results))
		for i, r := range results {
			items[i] = galleryResult{
				ID:       int64(r.GalleryID),
				Token:    r.Token,
				Title:    r.Title,
				Category: model.MapCategory(r.Cat),
				Cover:    r.Cover,
				Posted:   r.Posted,
				Rating:   r.Rating,
				URL:      r.URL,
				Tags:     r.Tags,
				Uploader: r.Uploader,
				Pages:    r.Pages,
				Domain:   r.Domain,
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"page":      page,
			"page_size": len(items),
			"results":   items,
		})
	}
}

func (s *Server) handleGallerys(c *gin.Context) {
	s.handleGalleryList(exhentai.ExhentaiURL + "/")(c)
}

func (s *Server) handleWatched(c *gin.Context) {
	s.handleGalleryList(exhentai.ExhentaiURL + "/watched")(c)
}

func (s *Server) handlePopular(c *gin.Context) {
	s.handleGalleryList(exhentai.ExhentaiURL + "/popular")(c)
}

func parseOptionalInt(c *gin.Context, key string) (*int, error) {
	v := c.Query(key)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func parseOptionalBool(c *gin.Context, key string) (bool, error) {
	v := c.Query(key)
	if v == "" {
		return false, nil
	}
	switch strings.ToLower(v) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %s", v)
	}
}
