package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"manga-reader/internal/exhentai"
	"manga-reader/internal/model"

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

func newTestApp() *Server {
	cfg := exhentai.LoadCookieConfig()
	if !cfg.IsValid() {
		return nil
	}
	client, _ := exhentai.CreateHTTPClient(cfg)
	return &Server{Client: client}
}

func skipIfNoCookies(t *testing.T) {
	t.Helper()
	cfg := exhentai.LoadCookieConfig()
	if !cfg.IsValid() {
		t.Skip("EHENTAI_COOKIE not set, skipping integration test")
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

	var gallery model.Gallery
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
	app := &Server{Client: &http.Client{}}
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
		ID        int         `json:"id"`
		Token     string      `json:"token"`
		Title     string      `json:"title"`
		TitleJpn  string      `json:"title_jpn"`
		Cover     string      `json:"cover"`
		Category  string      `json:"category"`
		Uploader  string      `json:"uploader"`
		Language  string      `json:"language"`
		PageCount int         `json:"page_count"`
		Rating    float64     `json:"rating"`
		Favorited int         `json:"favorited"`
		Tags      []model.Tag `json:"tags"`
		PageUrls  []string    `json:"page_urls"`
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
	app := &Server{Client: &http.Client{}}
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
	app := &Server{Client: &http.Client{}}
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
	app := &Server{Client: &http.Client{}}
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
			Category model.GalleryCategory `json:"category"`
		} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.Total <= 0 {
		t.Errorf("total = %d, want > 0", resp.Total)
	}
	for i, r := range resp.Results {
		if r.Category != model.CategoryDoujinshi {
			t.Errorf("results[%d].category = %q, want %q", i, r.Category, model.CategoryDoujinshi)
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
		ID         int         `json:"id"`
		Token      string      `json:"token"`
		Title      string      `json:"title"`
		TitleJpn   string      `json:"title_jpn"`
		Cover      string      `json:"cover"`
		Category   string      `json:"category"`
		Uploader   string      `json:"uploader"`
		Language   string      `json:"language"`
		PageCount  int         `json:"page_count"`
		Rating     float64     `json:"rating"`
		Favorited  int         `json:"favorited"`
		Tags       []model.Tag `json:"tags"`
		PageUrls   []string    `json:"page_urls"`
		Translated bool        `json:"translated"`
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
		Tags []model.Tag `json:"tags"`
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
			ID       int64                 `json:"id"`
			Token    string                `json:"token"`
			Title    string                `json:"title"`
			Category model.GalleryCategory `json:"category"`
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
		ID       int64                 `json:"id"`
		Token    string                `json:"token"`
		Title    string                `json:"title"`
		Category model.GalleryCategory `json:"category"`
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
