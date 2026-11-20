package handler

import (
	"fmt"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/httpx"
	"manga-reader/internal/model"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
)

// maxListingPage bounds upstream listing pagination: a cold request for page N
// walks N sequential upstream fetches, so unbounded page numbers would let one
// request hammer the upstream for hours. 50 pages is already ~1250 results.
const maxListingPage = 50

// maxDBPage bounds DB-offset pagination so page*pageSize cannot overflow int.
const maxDBPage = 1_000_000

func (s *Server) handleSearch(c *gin.Context) {
	keyword := c.Query("q")
	categoryStr := c.Query("categories")
	pageStr := c.DefaultQuery("page", "0")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		page = 0
	}
	if page > maxListingPage {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": fmt.Sprintf("page too large (max %d)", maxListingPage)})
		return
	}

	opts, err := parseSearchOptions(c)
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	navOpts, err := parseListingNavOptions(c)
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()
	client, uid, ok := s.upstreamClientForRequest(c)
	if !ok {
		return
	}

	siteURL := s.upstreamBase(ctx, uid)

	var categories []string
	if categoryStr != "" {
		categories = strings.Split(categoryStr, ",")
	}

	total, results, nav, err := exhentai.ScrapeSearch(ctx, client, siteURL, keyword, categories, page, opts, navOpts)
	if err != nil {
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("search failed: %v", err)})
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

	httpx.JSON(c, http.StatusOK, gin.H{
		"total":       total,
		"total_pages": totalPages,
		"page":        page,
		"page_size":   len(items),
		"results":     items,
		"nav":         nav,
	})
}

func (s *Server) handleGalleryListAt(c *gin.Context, listPath string) {
	pageStr := c.DefaultQuery("page", "0")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		page = 0
	}
	if page > maxListingPage {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": fmt.Sprintf("page too large (max %d)", maxListingPage)})
		return
	}

	opts, err := parseSearchOptions(c)
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	navOpts, err := parseListingNavOptions(c)
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	var categories []string
	if categoryStr := c.Query("categories"); categoryStr != "" {
		categories = strings.Split(categoryStr, ",")
	}

	ctx := c.Request.Context()
	client, uid, ok := s.upstreamClientForRequest(c)
	if !ok {
		return
	}
	listURL := s.upstreamBase(ctx, uid) + listPath

	results, nav, err := exhentai.ScrapeGalleryList(ctx, client, listURL, page, categories, opts, navOpts)
	if err != nil {
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery list failed: %v", err)})
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

	httpx.JSON(c, http.StatusOK, gin.H{
		"page":      page,
		"page_size": len(items),
		"results":   items,
		"nav":       nav,
	})
}

func (s *Server) handleGalleries(c *gin.Context) {
	s.handleGalleryListAt(c, "/")
}

func (s *Server) handleWatched(c *gin.Context) {
	s.handleGalleryListAt(c, "/watched")
}

func (s *Server) handlePopular(c *gin.Context) {
	s.handleGalleryListAt(c, "/popular")
}

var (
	listingSeekPattern = regexp.MustCompile(`^\d{2,4}(-\d{1,2}(-\d{1,2})?)?$`)
	listingJumpPattern = regexp.MustCompile(`^\d+[dwmy-]?$`)
)

// parseListingNavOptions reads the optional Jump/Seek query parameters. It
// returns (nil, nil) when neither is supplied.
func parseListingNavOptions(c *gin.Context) (*exhentai.ListingNavOptions, error) {
	seek := strings.TrimSpace(c.Query("seek"))
	jump := strings.TrimSpace(c.Query("jump"))
	if seek == "" && jump == "" {
		return nil, nil
	}
	if seek != "" && !listingSeekPattern.MatchString(seek) {
		return nil, fmt.Errorf("invalid seek")
	}
	if jump != "" && !listingJumpPattern.MatchString(jump) {
		return nil, fmt.Errorf("invalid jump")
	}
	return &exhentai.ListingNavOptions{Seek: seek, Jump: jump}, nil
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

// parseTagsQuery splits a comma-separated tag list, trimming whitespace and
// dropping empty or duplicate entries while preserving order.
func parseTagsQuery(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	tags := make([]string, 0)
	for part := range strings.SplitSeq(raw, ",") {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

func parseSearchOptions(c *gin.Context) (*exhentai.SearchOptions, error) {
	tags := parseTagsQuery(c.Query("tags"))

	minPages, err := parseOptionalInt(c, "min_pages")
	if err != nil {
		return nil, fmt.Errorf("invalid min_pages")
	}
	maxPages, err := parseOptionalInt(c, "max_pages")
	if err != nil {
		return nil, fmt.Errorf("invalid max_pages")
	}
	if minPages != nil && *minPages < 0 {
		return nil, fmt.Errorf("min_pages must be >= 0")
	}
	if maxPages != nil && *maxPages < 0 {
		return nil, fmt.Errorf("max_pages must be >= 0")
	}
	if minPages != nil && maxPages != nil && *minPages > *maxPages {
		return nil, fmt.Errorf("min_pages must be <= max_pages")
	}

	minRating, err := parseOptionalInt(c, "min_rating")
	if err != nil {
		return nil, fmt.Errorf("invalid min_rating")
	}
	if minRating != nil {
		switch *minRating {
		case 2, 3, 4, 5:
		default:
			return nil, fmt.Errorf("min_rating must be 2, 3, 4, or 5")
		}
	}

	hasTorrent, err := parseOptionalBool(c, "has_torrent")
	if err != nil {
		return nil, fmt.Errorf("invalid has_torrent")
	}
	includeExpunged, err := parseOptionalBool(c, "include_expunged")
	if err != nil {
		return nil, fmt.Errorf("invalid include_expunged")
	}
	disableLanguageFilter, err := parseOptionalBool(c, "disable_language_filter")
	if err != nil {
		return nil, fmt.Errorf("invalid disable_language_filter")
	}
	disableUploaderFilter, err := parseOptionalBool(c, "disable_uploader_filter")
	if err != nil {
		return nil, fmt.Errorf("invalid disable_uploader_filter")
	}
	disableTagFilter, err := parseOptionalBool(c, "disable_tag_filter")
	if err != nil {
		return nil, fmt.Errorf("invalid disable_tag_filter")
	}

	return &exhentai.SearchOptions{
		Tags:                  tags,
		MinPages:              minPages,
		MaxPages:              maxPages,
		MinRating:             minRating,
		HasTorrent:            hasTorrent,
		IncludeExpunged:       includeExpunged,
		DisableLanguageFilter: disableLanguageFilter,
		DisableUploaderFilter: disableUploaderFilter,
		DisableTagFilter:      disableTagFilter,
	}, nil
}
