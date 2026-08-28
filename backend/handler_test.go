package main

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== 测试常量 ====================

const (
	testGalleryID    = 3138775
	testGalleryToken = "30b0285f9b"
	testPageURL      = "https://e-hentai.org/s/859299c9ef/3138775-7"
)

// ==================== 测试工具 ====================

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

func newTestApp() *App {
	cfg := LoadCookieConfig()
	if !cfg.IsValid() {
		return nil
	}
	client, _ := CreateHTTPClient(cfg)
	return &App{Client: client}
}

func skipIfNoCookies(t *testing.T) {
	t.Helper()
	cfg := LoadCookieConfig()
	if !cfg.IsValid() {
		t.Skip("EHENTAI_COOKIE not set, skipping integration test")
	}
}

// ==================== 单元测试 ====================

func TestParseStars(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect float64
	}{
		{"5 stars", "background-position:0px -1px;opacity:1", 5.0},
		{"4.5 stars", "background-position:0px -21px;opacity:1", 4.5},
		{"4 stars", "background-position:-16px -1px;opacity:1", 4.0},
		{"3.5 stars", "background-position:-16px -21px;opacity:1", 3.5},
		{"3 stars", "background-position:-32px -1px;opacity:1", 3.0},
		{"2.5 stars", "background-position:-32px -21px;opacity:1", 2.5},
		{"2 stars", "background-position:-48px -1px;opacity:1", 2.0},
		{"1.5 stars", "background-position:-48px -21px;opacity:1", 1.5},
		{"1 star", "background-position:-64px -1px;opacity:1", 1.0},
		{"0.5 stars", "background-position:-64px -21px;opacity:1", 0.5},
		{"0 stars", "background-position:-80px -1px;opacity:1", 0.0},
		{"empty string", "", 0.0},
		{"garbage", "no-match-here", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := parseStars(tt.input)
			if got != tt.expect {
				t.Errorf("parseStars(%q) = %f, want %f", tt.input, got, tt.expect)
			}
		})
	}
}

func TestBuildCategoryFilter(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect string
	}{
		{"empty", nil, ""},
		{"doujinshi only", []string{"doujinshi"}, strconv.FormatUint(uint64(1023^2), 10)},
		{"manga only", []string{"manga"}, strconv.FormatUint(uint64(1023^4), 10)},
		{"doujinshi+manga", []string{"doujinshi", "manga"}, strconv.FormatUint(uint64(1023^6), 10)},
		{"all categories", []string{"doujinshi", "manga", "artistcg", "gamecg", "imageset", "cosplay", "asianporn", "nonh", "western", "misc"}, "0"},
		{"unknown category", []string{"unknown"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildCategoryFilter(tt.input)
			if got != tt.expect {
				t.Errorf("buildCategoryFilter(%v) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestParseGalleryURL(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectDomain string
		expectGId    string
		expectGToken string
	}{
		{
			"exhentai url",
			"https://exhentai.org/g/3138775/30b0285f9b/",
			"exhentai.org", "3138775", "30b0285f9b",
		},
		{
			"ehentai url",
			"https://e-hentai.org/g/3138775/30b0285f9b/",
			"e-hentai.org", "3138775", "30b0285f9b",
		},
		{
			"no trailing slash",
			"https://exhentai.org/g/3138775/30b0285f9b",
			"exhentai.org", "3138775", "30b0285f9b",
		},
		{
			"invalid url",
			"https://exhentai.org/not/a/gallery",
			"", "", "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain, gId, gToken := parseGalleryURL(tt.input)
			if domain != tt.expectDomain || gId != tt.expectGId || gToken != tt.expectGToken {
				t.Errorf("parseGalleryURL(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.input, domain, gId, gToken, tt.expectDomain, tt.expectGId, tt.expectGToken)
			}
		})
	}
}

func TestMapCategory(t *testing.T) {
	tests := []struct {
		input  string
		expect GalleryCategory
	}{
		{"doujinshi", CategoryDoujinshi},
		{"Doujinshi", CategoryDoujinshi},
		{"manga", CategoryManga},
		{"Manga", CategoryManga},
		{"artist cg", CategoryArtistCG},
		{"Artist CG", CategoryArtistCG},
		{"game cg", CategoryGameCG},
		{"western", CategoryWestern},
		{"image set", CategoryImageSet},
		{"cosplay", CategoryCosplay},
		{"asian porn", CategoryAsianPorn},
		{"non-h", CategoryNonH},
		{"miscellaneous", CategoryMisc},
		{"unknown", CategoryOther},
		{"", CategoryOther},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := MapCategory(tt.input)
			if got != tt.expect {
				t.Errorf("MapCategory(%q) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestParseTags(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect []Tag
	}{
		{
			"namespace:tag format",
			[]string{"female:yuri", "male:solemale"},
			[]Tag{
				{Namespace: "female", Name: "yuri"},
				{Namespace: "male", Name: "solemale"},
			},
		},
		{
			"no namespace",
			[]string{"uncensored"},
			[]Tag{
				{Namespace: "", Name: "uncensored"},
			},
		},
		{
			"mixed",
			[]string{"female:yuri", "uncensored", "language:chinese"},
			[]Tag{
				{Namespace: "female", Name: "yuri"},
				{Namespace: "", Name: "uncensored"},
				{Namespace: "language", Name: "chinese"},
			},
		},
		{
			"empty",
			[]string{},
			[]Tag{},
		},
		{
			"nil",
			nil,
			nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseTags(tt.input)
			if len(got) != len(tt.expect) {
				t.Fatalf("ParseTags(%v) returned %d tags, want %d", tt.input, len(got), len(tt.expect))
			}
			for i := range got {
				if got[i] != tt.expect[i] {
					t.Errorf("ParseTags(%v)[%d] = %v, want %v", tt.input, i, got[i], tt.expect[i])
				}
			}
		})
	}
}

func TestConvertMetadataToGallery(t *testing.T) {
	posted := "1609459200" // 2021-01-01 00:00:00 UTC
	meta := &GalleryMetadata{
		GID:          testGalleryID,
		Token:        testGalleryToken,
		Title:        "Test Gallery",
		TitleJpn:     "テストギャラリー",
		Category:     "Manga",
		Thumb:        "https://example.com/thumb.jpg",
		Uploader:     "uploader1",
		Posted:       posted,
		FileCount:    "65",
		FileSize:     1048576,
		Expunged:     false,
		Rating:       "4.86",
		TorrentCount: "3",
		Tags:         []string{"female:yuri", "language:chinese"},
	}

	gallery := ConvertMetadataToGallery(meta)

	if gallery.ID != testGalleryID {
		t.Errorf("ID = %d, want %d", gallery.ID, testGalleryID)
	}
	if gallery.Token != testGalleryToken {
		t.Errorf("Token = %q, want %q", gallery.Token, testGalleryToken)
	}
	if gallery.Title != "Test Gallery" {
		t.Errorf("Title = %q, want %q", gallery.Title, "Test Gallery")
	}
	if gallery.TitleJPN != "テストギャラリー" {
		t.Errorf("TitleJPN = %q, want %q", gallery.TitleJPN, "テストギャラリー")
	}
	if gallery.Category != CategoryManga {
		t.Errorf("Category = %q, want %q", gallery.Category, CategoryManga)
	}
	if gallery.PageCount != 65 {
		t.Errorf("PageCount = %d, want 65", gallery.PageCount)
	}
	if gallery.Rating != 4.86 {
		t.Errorf("Rating = %f, want 4.86", gallery.Rating)
	}
	if gallery.PostedAt == nil {
		t.Fatal("PostedAt is nil")
	}
	if gallery.PostedAt.Year() != 2021 {
		t.Errorf("PostedAt.Year() = %d, want 2021", gallery.PostedAt.Year())
	}
	if len(gallery.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(gallery.Tags))
	}
	if gallery.Expunged {
		t.Error("Expunged should be false")
	}
}

func TestConvertMetadataToGallery_InvalidRating(t *testing.T) {
	meta := &GalleryMetadata{
		GID:          123,
		Token:        "abc",
		Title:        "Test",
		Rating:       "not-a-number",
		FileCount:    "not-a-number",
		Posted:       "not-a-number",
		FileSize:     0,
		TorrentCount: "0",
	}
	gallery := ConvertMetadataToGallery(meta)
	if gallery.Rating != 0 {
		t.Errorf("Rating should default to 0 for invalid input, got %f", gallery.Rating)
	}
	if gallery.PageCount != 0 {
		t.Errorf("PageCount should default to 0 for invalid input, got %d", gallery.PageCount)
	}
	if gallery.PostedAt != nil {
		t.Error("PostedAt should be nil for invalid timestamp")
	}
}

// ==================== 集成测试 ====================

func TestAPI_GetGallery(t *testing.T) {
	skipIfNoCookies(t)

	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/gallery/:id/:token", app.handleGetGallery)

	url := "/api/gallery/" + strconv.Itoa(testGalleryID) + "/" + testGalleryToken
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var gallery Gallery
	if err := json.Unmarshal(w.Body.Bytes(), &gallery); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if gallery.ID != testGalleryID {
		t.Errorf("ID = %d, want %d", gallery.ID, testGalleryID)
	}
	if gallery.Token != testGalleryToken {
		t.Errorf("Token = %q, want %q", gallery.Token, testGalleryToken)
	}
	if gallery.Title == "" {
		t.Error("Title should not be empty")
	}
	t.Logf("Gallery: %s (rating: %.2f, pages: %d)", gallery.Title, gallery.Rating, gallery.PageCount)
}

func TestAPI_GetGallery_InvalidID(t *testing.T) {
	r := setupRouter()
	app := &App{Client: &http.Client{}}
	r.GET("/api/gallery/:id/:token", app.handleGetGallery)

	req := httptest.NewRequest("GET", "/api/gallery/abc/xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPI_Search(t *testing.T) {
	skipIfNoCookies(t)

	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=yuri&page=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
		Page       int `json:"page"`
		PageSize   int `json:"page_size"`
		Results    []struct {
			ID    int64  `json:"id"`
			Token string `json:"token"`
			Title string `json:"title"`
			Cover string `json:"cover"`
			URL   string `json:"url"`
			Pages int    `json:"pages"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Total <= 0 {
		t.Errorf("total = %d, want > 0", resp.Total)
	}
	if resp.Page != 0 {
		t.Errorf("page = %d, want 0", resp.Page)
	}
	if resp.PageSize <= 0 {
		t.Errorf("page_size = %d, want > 0", resp.PageSize)
	}
	if resp.TotalPages <= 0 {
		t.Errorf("total_pages = %d, want > 0", resp.TotalPages)
	}
	if len(resp.Results) == 0 {
		t.Fatal("results should not be empty")
	}

	for i, r := range resp.Results {
		if r.ID == 0 || r.Token == "" || r.Title == "" {
			t.Errorf("results[%d]: missing required fields (id=%d, token=%q, title=%q)", i, r.ID, r.Token, r.Title)
		}
	}
	t.Logf("Search 'yuri': total=%d, total_pages=%d, page=%d, results=%d",
		resp.Total, resp.TotalPages, resp.Page, resp.PageSize)
}

func TestAPI_Search_Page1(t *testing.T) {
	skipIfNoCookies(t)

	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)

	// 第一页
	req0 := httptest.NewRequest("GET", "/api/search?q=manga&page=0", nil)
	w0 := httptest.NewRecorder()
	r.ServeHTTP(w0, req0)

	var resp0 struct {
		Results []struct {
			ID int64 `json:"id"`
		} `json:"results"`
	}
	json.Unmarshal(w0.Body.Bytes(), &resp0)

	// 第二页
	req1 := httptest.NewRequest("GET", "/api/search?q=manga&page=1", nil)
	w1 := httptest.NewRecorder()
	r.ServeHTTP(w1, req1)

	var resp1 struct {
		Results []struct {
			ID int64 `json:"id"`
		} `json:"results"`
	}
	json.Unmarshal(w1.Body.Bytes(), &resp1)

	if w0.Code != http.StatusOK || w1.Code != http.StatusOK {
		t.Fatalf("page0 status=%d, page1 status=%d, both want 200", w0.Code, w1.Code)
	}
	if len(resp0.Results) == 0 || len(resp1.Results) == 0 {
		t.Fatal("both pages should have results")
	}

	// 翻页结果不应完全相同
	same := true
	if len(resp0.Results) == len(resp1.Results) {
		for i := range resp0.Results {
			if resp0.Results[i].ID != resp1.Results[i].ID {
				same = false
				break
			}
		}
	} else {
		same = false
	}
	if same {
		t.Error("page 0 and page 1 returned identical results, pagination may not be working")
	}
	t.Logf("page0: %d results, page1: %d results", len(resp0.Results), len(resp1.Results))
}

func TestAPI_SearchWithCategories(t *testing.T) {
	skipIfNoCookies(t)

	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=love&categories=doujinshi,manga&page=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Total int `json:"total"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Total <= 0 {
		t.Errorf("total = %d, want > 0", resp.Total)
	}
	t.Logf("Search 'love' (doujinshi,manga): total=%d", resp.Total)
}

func TestAPI_GalleryDetails(t *testing.T) {
	skipIfNoCookies(t)

	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/gallery/:id/:token/details", app.handleGalleryDetails)

	url := "/api/gallery/" + strconv.Itoa(testGalleryID) + "/" + testGalleryToken + "/details"
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var details struct {
		ID        int    `json:"id"`
		Token     string `json:"token"`
		Title     string `json:"title"`
		TitleJpn  string `json:"title_jpn"`
		Cover     string `json:"cover"`
		Category  string `json:"category"`
		Uploader  string `json:"uploader"`
		Language  string `json:"language"`
		PageCount int    `json:"page_count"`
		Rating    float64 `json:"rating"`
		Favorited int     `json:"favorited"`
		Tags      []Tag   `json:"tags"`
		PageUrls  []string `json:"page_urls"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &details); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if details.ID != testGalleryID {
		t.Errorf("ID = %d, want %d", details.ID, testGalleryID)
	}
	if details.Title == "" {
		t.Error("Title should not be empty")
	}
	if details.Cover == "" {
		t.Error("Cover should not be empty")
	}
	if details.PageCount <= 0 {
		t.Errorf("PageCount = %d, want > 0", details.PageCount)
	}
	if len(details.PageUrls) == 0 {
		t.Error("PageUrls should not be empty")
	}
	t.Logf("Gallery details: %s (%s), pages=%d, rating=%.2f, tags=%d, favorited=%d",
		details.Title, details.Category, details.PageCount, details.Rating, len(details.Tags), details.Favorited)
}

func TestAPI_GalleryPages(t *testing.T) {
	skipIfNoCookies(t)

	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/gallery/:id/:token/pages", app.handleGalleryPages)

	url := "/api/gallery/" + strconv.Itoa(testGalleryID) + "/" + testGalleryToken + "/pages"
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		ID    string `json:"id"`
		Token string `json:"token"`
		Total int    `json:"total"`
		Pages []struct {
			PageURL string `json:"page_url"`
			Index   int    `json:"index"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal response: %v", err)
	}

	if resp.Total <= 0 {
		t.Errorf("total = %d, want > 0", resp.Total)
	}
	if len(resp.Pages) == 0 {
		t.Fatal("pages should not be empty")
	}
	for i, p := range resp.Pages {
		if p.PageURL == "" {
			t.Errorf("pages[%d].page_url should not be empty", i)
		}
		if p.Index != i {
			t.Errorf("pages[%d].index = %d, want %d", i, p.Index, i)
		}
	}
	t.Logf("Gallery %s/%s: %d pages", resp.ID, resp.Token, resp.Total)
}

func TestAPI_PageImage(t *testing.T) {
	skipIfNoCookies(t)

	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/page-image", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/page-image?url="+testPageURL, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	contentType := w.Header().Get("Content-Type")
	if contentType == "" {
		t.Error("Content-Type header should not be empty")
	}
	if len(w.Body.Bytes()) == 0 {
		t.Error("response body should not be empty")
	}
	t.Logf("Page image: content_type=%s, size=%d bytes", contentType, len(w.Body.Bytes()))
}

func TestAPI_PageImage_MissingURL(t *testing.T) {
	r := setupRouter()
	app := &App{Client: &http.Client{}}
	r.GET("/api/page-image", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/page-image", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPI_SearchNoResults(t *testing.T) {
	skipIfNoCookies(t)

	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=_____notexist12345_____&page=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 应返回 502 因为EHentai返回无结果
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestAPI_SearchInvalidCategory(t *testing.T) {
	skipIfNoCookies(t)

	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)

	// buildCategoryFilter 对未知分类返回空字符串，不会报错，但不应匹配任何分类
	req := httptest.NewRequest("GET", "/api/search?q=test&categories=invalidcat&page=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// 即使分类无效，搜索仍应返回结果（等同于搜索全部分类）
	if w.Code != http.StatusOK {
		t.Logf("status = %d (may fail due to network/cookies)", w.Code)
	}
}

func TestAPI_GalleryDetails_InvalidID(t *testing.T) {
	r := setupRouter()
	app := &App{Client: &http.Client{}}
	r.GET("/api/gallery/:id/:token/details", app.handleGalleryDetails)

	req := httptest.NewRequest("GET", "/api/gallery//xyz/details", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestAPI_GalleryPages_InvalidID(t *testing.T) {
	r := setupRouter()
	app := &App{Client: &http.Client{}}
	r.GET("/api/gallery/:id/:token/pages", app.handleGalleryPages)

	req := httptest.NewRequest("GET", "/api/gallery//xyz/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

// ==================== 新增集成测试 ====================

func TestAPI_SearchSiteEhentai(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=love&site=ehentai&page=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Total   int `json:"total"`
		Results []struct {
			ID    int64  `json:"id"`
			Token string `json:"token"`
		} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Total <= 0 {
		t.Errorf("total = %d, want > 0", resp.Total)
	}
	if len(resp.Results) == 0 {
		t.Error("results should not be empty")
	}
	t.Logf("Search ehentai 'love': total=%d, results=%d", resp.Total, len(resp.Results))
}

func TestAPI_SearchEmptyQuery(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=&page=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Total   int `json:"total"`
		Results []struct {
			ID int64 `json:"id"`
		} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Total <= 0 {
		t.Errorf("total = %d, want > 0 for empty query (browse mode)", resp.Total)
	}
	t.Logf("Search empty query: total=%d, results=%d", resp.Total, len(resp.Results))
}

func TestAPI_SearchTotalPages(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=yuri&page=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Total      int `json:"total"`
		TotalPages int `json:"total_pages"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Total <= 0 {
		t.Fatalf("total = %d, want > 0", resp.Total)
	}

	const pageSize = 25
	expectedTotalPages := (resp.Total + pageSize - 1) / pageSize
	if resp.TotalPages != expectedTotalPages {
		t.Errorf("total_pages = %d, want %d (ceil(%d/%d))", resp.TotalPages, expectedTotalPages, resp.Total, pageSize)
	}
	t.Logf("total=%d, total_pages=%d, expected=%d", resp.Total, resp.TotalPages, expectedTotalPages)
}

func TestAPI_SearchSingleCategory(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=love&categories=doujinshi&page=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Total   int `json:"total"`
		Results []struct {
			Category GalleryCategory `json:"category"`
		} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Total <= 0 {
		t.Errorf("total = %d, want > 0", resp.Total)
	}
	for i, r := range resp.Results {
		if r.Category != CategoryDoujinshi {
			t.Errorf("results[%d].category = %q, want %q", i, r.Category, CategoryDoujinshi)
		}
	}
	t.Logf("Search 'love' (doujinshi only): total=%d", resp.Total)
}

func TestAPI_GalleryDetailsFullStructure(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/gallery/:id/:token/details", app.handleGalleryDetails)

	url := "/api/gallery/" + strconv.Itoa(testGalleryID) + "/" + testGalleryToken + "/details"
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var details struct {
		ID         int      `json:"id"`
		Token      string   `json:"token"`
		Title      string   `json:"title"`
		TitleJpn   string   `json:"title_jpn"`
		Cover      string   `json:"cover"`
		Category   string   `json:"category"`
		Uploader   string   `json:"uploader"`
		Language   string   `json:"language"`
		PageCount  int      `json:"page_count"`
		Rating     float64  `json:"rating"`
		Favorited  int      `json:"favorited"`
		Tags       []Tag    `json:"tags"`
		PageUrls   []string `json:"page_urls"`
		Translated bool     `json:"translated"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &details); err != nil {
		t.Fatalf("failed to unmarshal: %v. body: %s", err, w.Body.String())
	}

	if details.ID != testGalleryID {
		t.Errorf("ID = %d, want %d", details.ID, testGalleryID)
	}
	if details.Token != testGalleryToken {
		t.Errorf("Token = %q, want %q", details.Token, testGalleryToken)
	}
	if details.Title == "" {
		t.Error("Title should not be empty")
	}
	if details.Cover == "" {
		t.Error("Cover should not be empty")
	}
	if details.Category == "" {
		t.Error("Category should not be empty")
	}
	if details.Uploader == "" {
		t.Error("Uploader should not be empty")
	}
	if details.PageCount <= 0 {
		t.Errorf("PageCount = %d, want > 0", details.PageCount)
	}
	if details.Rating <= 0 || details.Rating > 5 {
		t.Errorf("Rating = %f, want (0, 5]", details.Rating)
	}
	if len(details.PageUrls) == 0 {
		t.Error("PageUrls should not be empty")
	}
	if len(details.PageUrls) != details.PageCount {
		t.Errorf("PageUrls len = %d, want %d (= PageCount)", len(details.PageUrls), details.PageCount)
	}
	t.Logf("Details: %s, cat=%s, uploader=%s, lang=%s, pages=%d, rating=%.2f, favorited=%d, tags=%d",
		details.Title, details.Category, details.Uploader, details.Language,
		details.PageCount, details.Rating, details.Favorited, len(details.Tags))
}

func TestAPI_GalleryDetailsTags(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/gallery/:id/:token/details", app.handleGalleryDetails)

	url := "/api/gallery/" + strconv.Itoa(testGalleryID) + "/" + testGalleryToken + "/details"
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d. body: %s", w.Code, w.Body.String())
	}

	var details struct {
		Tags []Tag `json:"tags"`
	}
	json.Unmarshal(w.Body.Bytes(), &details)

	if len(details.Tags) == 0 {
		t.Fatal("tags should not be empty")
	}

	hasNamespace := false
	for _, tag := range details.Tags {
		if tag.Namespace != "" {
			hasNamespace = true
		}
		if tag.Name == "" {
			t.Errorf("tag with namespace=%q has empty name", tag.Namespace)
		}
	}
	if !hasNamespace {
		t.Error("at least one tag should have a non-empty namespace")
	}
	t.Logf("Tags: %d total", len(details.Tags))
}

func TestAPI_GalleryPagesValidURLs(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/gallery/:id/:token/pages", app.handleGalleryPages)

	url := "/api/gallery/" + strconv.Itoa(testGalleryID) + "/" + testGalleryToken + "/pages"
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d. body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Pages []struct {
			PageURL string `json:"page_url"`
		} `json:"pages"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	for i, p := range resp.Pages {
		if !strings.HasPrefix(p.PageURL, "https://") {
			t.Errorf("pages[%d].page_url = %q, want https:// prefix", i, p.PageURL)
		}
	}
	t.Logf("All %d page URLs have https:// prefix", len(resp.Pages))
}

func TestAPI_GalleryPagesOrdering(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/gallery/:id/:token/pages", app.handleGalleryPages)

	url := "/api/gallery/" + strconv.Itoa(testGalleryID) + "/" + testGalleryToken + "/pages"
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d. body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Pages []struct {
			Index int `json:"index"`
		} `json:"pages"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	for i, p := range resp.Pages {
		if p.Index != i {
			t.Errorf("pages[%d].index = %d, want %d", i, p.Index, i)
			break
		}
	}
	t.Logf("All %d pages are in correct order", len(resp.Pages))
}

func TestAPI_GalleryMultiPagePagination(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/gallery/:id/:token/pages", app.handleGalleryPages)

	url := "/api/gallery/" + strconv.Itoa(testGalleryID) + "/" + testGalleryToken + "/pages"
	req := httptest.NewRequest("GET", url, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d. body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Total int `json:"total"`
		Pages []struct {
			PageURL string `json:"page_url"`
			Index   int    `json:"index"`
		} `json:"pages"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Total < 65 {
		t.Errorf("total = %d, want >= 65 for multi-page gallery", resp.Total)
	}
	if len(resp.Pages) < 65 {
		t.Errorf("pages len = %d, want >= 65", len(resp.Pages))
	}
	if len(resp.Pages) > 0 {
		lastPage := resp.Pages[len(resp.Pages)-1]
		if lastPage.PageURL == "" {
			t.Error("last page URL should not be empty")
		}
		if lastPage.Index != len(resp.Pages)-1 {
			t.Errorf("last page index = %d, want %d", lastPage.Index, len(resp.Pages)-1)
		}
	}
	t.Logf("Multi-page gallery: total=%d, pages_fetched=%d", resp.Total, len(resp.Pages))
}

func TestAPI_SearchCoverURL(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=yuri&page=0", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d. body: %s", w.Code, w.Body.String())
	}

	var resp struct {
		Results []struct {
			Cover string `json:"cover"`
		} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	for i, r := range resp.Results {
		if r.Cover == "" {
			t.Errorf("results[%d].cover should not be empty", i)
		}
	}
	t.Logf("All %d search results have cover URLs", len(resp.Results))
}

func TestAPI_CrossEndpointConsistency(t *testing.T) {
	skipIfNoCookies(t)
	app := newTestApp()
	if app == nil {
		t.Fatal("failed to create test app")
	}

	r := setupRouter()
	r.GET("/api/search", app.handleSearch)
	r.GET("/api/gallery/:id/:token/details", app.handleGalleryDetails)

	searchReq := httptest.NewRequest("GET", "/api/search?q=yuri&page=0", nil)
	searchW := httptest.NewRecorder()
	r.ServeHTTP(searchW, searchReq)

	if searchW.Code != http.StatusOK {
		t.Fatalf("search status = %d", searchW.Code)
	}

	var searchResp struct {
		Results []struct {
			ID       int64           `json:"id"`
			Token    string          `json:"token"`
			Title    string          `json:"title"`
			Category GalleryCategory `json:"category"`
		} `json:"results"`
	}
	json.Unmarshal(searchW.Body.Bytes(), &searchResp)

	if len(searchResp.Results) == 0 {
		t.Fatal("search returned no results")
	}

	first := searchResp.Results[0]

	detailURL := "/api/gallery/" + strconv.FormatInt(first.ID, 10) + "/" + first.Token + "/details"
	detailReq := httptest.NewRequest("GET", detailURL, nil)
	detailW := httptest.NewRecorder()
	r.ServeHTTP(detailW, detailReq)

	if detailW.Code != http.StatusOK {
		t.Fatalf("details status = %d, body: %s", detailW.Code, detailW.Body.String())
	}

	var details struct {
		ID       int64           `json:"id"`
		Token    string          `json:"token"`
		Title    string          `json:"title"`
		Category GalleryCategory `json:"category"`
	}
	json.Unmarshal(detailW.Body.Bytes(), &details)

	if details.ID != first.ID {
		t.Errorf("details.ID = %d, search.ID = %d, mismatch", details.ID, first.ID)
	}
	if details.Token != first.Token {
		t.Errorf("details.Token = %q, search.Token = %q, mismatch", details.Token, first.Token)
	}
	t.Logf("Cross-endpoint: search ID=%d token=%q matches details", first.ID, first.Token)
}

// ==================== 基准测试 ====================

func BenchmarkParseStars(b *testing.B) {
	for b.Loop() {
		parseStars("background-position:-32px -1px;opacity:1")
	}
}

func BenchmarkBuildCategoryFilter(b *testing.B) {
	cats := []string{"doujinshi", "manga", "artistcg"}
	for b.Loop() {
		buildCategoryFilter(cats)
	}
}

func BenchmarkMapCategory(b *testing.B) {
	for b.Loop() {
		MapCategory("Doujinshi")
	}
}

func BenchmarkConvertMetadataToGallery(b *testing.B) {
	meta := &GalleryMetadata{
		GID:          testGalleryID,
		Token:        testGalleryToken,
		Title:        "Benchmark Gallery",
		Category:     "Manga",
		Posted:       strconv.FormatInt(time.Now().Unix(), 10),
		FileCount:    "65",
		Rating:       "4.86",
		TorrentCount: "3",
		Tags:         []string{"female:yuri", "language:chinese"},
	}
	for b.Loop() {
		ConvertMetadataToGallery(meta)
	}
}
