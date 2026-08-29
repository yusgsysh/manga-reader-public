package main

import (
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

type App struct {
	Client *http.Client
	DB     *DB
}

func galleryURL(id, token string) string {
	return exhentaiURL + "/g/" + id + "/" + token + "/"
}

func (a *App) handleGetGallery(c *gin.Context) {
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

	meta, err := PostGalleryMetadata(ctx, a.Client, id, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("exhentai api failed: %v", err)})
		return
	}

	gallery := ConvertMetadataToGallery(meta)
	c.JSON(http.StatusOK, gallery)
}

func (a *App) handleGalleryDetails(c *gin.Context) {
	id := c.Param("id")
	token := c.Param("token")
	if id == "" || token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery id or token"})
		return
	}

	u := galleryURL(id, token)
	ctx := c.Request.Context()

	details, err := scrapeGalleryDetails(ctx, a.Client, u)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery details failed: %v", err)})
		return
	}

	tags := make([]Tag, len(details.Tags))
	for i, t := range details.Tags {
		tags[i] = Tag{Namespace: t.Namespace, Name: t.Name}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           details.GalleryID,
		"token":        details.Token,
		"domain":       details.Domain,
		"title":        details.Title,
		"title_jpn":    details.TitleJpn,
		"cover":        details.Cover,
		"category":     MapCategory(details.Cat),
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

func (a *App) handleGalleryPages(c *gin.Context) {
	id := c.Param("id")
	token := c.Param("token")
	if id == "" || token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery id or token"})
		return
	}

	u := galleryURL(id, token)
	ctx := c.Request.Context()

	details, err := scrapeGalleryDetails(ctx, a.Client, u)
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

func (a *App) handleSearch(c *gin.Context) {
	keyword := c.Query("q")
	categoryStr := c.Query("categories")
	site := c.DefaultQuery("site", "exhentai")
	pageStr := c.DefaultQuery("page", "0")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		page = 0
	}

	ctx := c.Request.Context()

	var siteURL string
	if site == "ehentai" {
		siteURL = ehentaiURL
	} else {
		siteURL = exhentaiURL
	}

	var categories []string
	if categoryStr != "" {
		categories = strings.Split(categoryStr, ",")
	}

	total, results, err := scrapeSearch(ctx, a.Client, siteURL, keyword, categories, page)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("search failed: %v", err)})
		return
	}

	type searchResult struct {
		ID       int64           `json:"id"`
		Token    string          `json:"token"`
		Title    string          `json:"title"`
		Category GalleryCategory `json:"category"`
		Cover    string          `json:"cover"`
		Posted   string          `json:"posted"`
		Rating   float64         `json:"rating"`
		URL      string          `json:"url"`
		Tags     []string        `json:"tags"`
		Uploader string          `json:"uploader"`
		Pages    int             `json:"pages"`
		Domain   string          `json:"domain"`
	}

	items := make([]searchResult, len(results))
	for i, r := range results {
		items[i] = searchResult{
			ID:       int64(r.GalleryID),
			Token:    r.Token,
			Title:    r.Title,
			Category: MapCategory(r.Cat),
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

const maxNlRetries = 2

func (a *App) handlePageImage(c *gin.Context) {
	pageURL := c.Query("url")
	if pageURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing url parameter"})
		return
	}

	ctx := c.Request.Context()

	data, contentType, err := fetchPageImage(ctx, a.Client, pageURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download image failed: %v", err)})
		return
	}

	c.Data(http.StatusOK, contentType, data)
}

func (a *App) handleGalleryList(listURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		pageStr := c.DefaultQuery("page", "0")
		page, _ := strconv.Atoi(pageStr)
		if page < 0 {
			page = 0
		}

		ctx := c.Request.Context()

		results, err := scrapeGalleryList(ctx, a.Client, listURL, page)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery list failed: %v", err)})
			return
		}

		type galleryResult struct {
			ID       int64           `json:"id"`
			Token    string          `json:"token"`
			Title    string          `json:"title"`
			Category GalleryCategory `json:"category"`
			Cover    string          `json:"cover"`
			Posted   string          `json:"posted"`
			Rating   float64         `json:"rating"`
			URL      string          `json:"url"`
			Tags     []string        `json:"tags"`
			Uploader string          `json:"uploader"`
			Pages    int             `json:"pages"`
			Domain   string          `json:"domain"`
		}

		items := make([]galleryResult, len(results))
		for i, r := range results {
			items[i] = galleryResult{
				ID:       int64(r.GalleryID),
				Token:    r.Token,
				Title:    r.Title,
				Category: MapCategory(r.Cat),
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

func (a *App) handleGallerys(c *gin.Context) {
	a.handleGalleryList(exhentaiURL + "/")(c)
}

func (a *App) handleWatched(c *gin.Context) {
	a.handleGalleryList(exhentaiURL + "/watched")(c)
}

func (a *App) handlePopular(c *gin.Context) {
	a.handleGalleryList(exhentaiURL + "/popular")(c)
}
