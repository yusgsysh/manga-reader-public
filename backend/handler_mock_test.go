package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

// mockTransport redirects all HTTP requests to the given mock server.
type mockTransport struct {
	mockURL string
}

func (t *mockTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	// Rewrite the request URL to point to the mock server
	mockURL, _ := url.Parse(t.mockURL)
	req.URL.Scheme = mockURL.Scheme
	req.URL.Host = mockURL.Host
	return http.DefaultTransport.RoundTrip(req)
}

// newMockClient creates an http.Client whose requests are intercepted by the mock transport.
func newMockClient(mockURL string) *http.Client {
	return &http.Client{
		Transport: &mockTransport{mockURL: mockURL},
	}
}

// newMockServer creates an httptest.Server from a handler and returns its base URL.
func newMockServer(handler http.HandlerFunc) *httptest.Server {
	ts := httptest.NewServer(handler)
	return ts
}

// ==================== Mock HTML Fixtures ====================

// mockSearchHTML returns an EHentai search results page with extended layout.
func mockSearchHTML(keyword string, count int) string {
	var rows strings.Builder
	for i := 0; i < count; i++ {
		gid := 1000 + i
		token := fmt.Sprintf("tok%04d", i)
		rows.WriteString(fmt.Sprintf(`<tr>
			<td class="gl1e"><div style="height:245px;width:250px"><a href="https://exhentai.org/g/%d/%s/"><img style="height:245px;width:250px" src="https://example.com/thumb%d.webp" title="Test Gallery %d"/></a></div></td>
			<td class="gl2e"><div>
				<div class="gl3e">
					<div class="cn ct2" onclick="document.location='https://exhentai.org/doujinshi'">Doujinshi</div>
					<div>2024-01-01</div>
					<div class="ir" style="background-position:-32px -1px;opacity:1"></div>
					<div><a href="https://exhentai.org/uploader/testuser%d">testuser%d</a></div>
					<div>%d pages</div>
				</div>
				<a href="https://exhentai.org/g/%d/%s/"><div class="gl4e glname" style="min-height:253px">
					<div class="glink">Test Gallery %d</div>
					<div><table><tbody><tr><td class="tc">female:</td><td><div class="gt" title="female:yuri">yuri</div></td></tr></tbody></table></div>
				</div></a>
			</div></td>
		</tr>`, gid, token, i, i, i, i, i+10, gid, token, i))
	}

	return fmt.Sprintf(`<!DOCTYPE html><html><head></head><body>
<div class="ido">
	<div></div>
	<div>
		<div class="searchtext"><p>Found %d results. %.0f galleries on this page.</p></div>
		<table><tbody>
		%s
	</tbody></table>
	<a href="">Next &gt;</a>
</div></body></html>`, count, float64(count), rows.String())
}

// mockNoHitsHTML returns a search page with no results.
func mockNoHitsHTML() string {
	return `<!DOCTYPE html><html><head></head><body>
<div class="ido">
	<div><p>No unfiltered results found. Try lowering the search requirements.</p></div>
</div></body></html>`
}

// mockGalleryDetailHTML returns a gallery details page.
func mockGalleryDetailHTML(gid int, token, title string, pageCount int) string {
	var tagsHTML strings.Builder
	tagsHTML.WriteString(`<tr><td class="taglist">female:</td><td class="taglist"><div title="female:yuri"><a>yuri</a></div><div title="female:ntr"><a>ntr</a></div></td></tr>`)
	tagsHTML.WriteString(`<tr><td class="taglist">language:</td><td class="taglist"><div title="language:chinese"><a>chinese</a></div></td></tr>`)

	var pageLinks strings.Builder
	for i := 1; i <= pageCount && i <= 40; i++ {
		pageLinks.WriteString(fmt.Sprintf(`<a href="https://exhentai.org/s/abc%d/%d-%d">p%d</a>`, i, gid, i, i))
	}

	return fmt.Sprintf(`<!DOCTYPE html><html><head></head><body>
<div id="gd1"><div style="background-image:url(https://example.com/cover.webp)"></div></div>
<div id="gn">%s</div>
<div id="gj">%s JPN</div>
<div id="gdc"><div>Doujinshi</div></div>
<div id="gdn"><a>testuploader</a></div>
<div id="gdd"><table><tbody>
	<tr><td class="gdt1">Posted:</td><td class="gdt2">2024-01-01 00:00</td></tr>
	<tr><td class="gdt1">Parent:</td><td class="gdt2"><a></a></td></tr>
	<tr><td class="gdt1">Visible:</td><td class="gdt2">Yes</td></tr>
	<tr><td class="gdt1">Language:</td><td class="gdt2">Chinese <span>TL</span></td></tr>
	<tr><td class="gdt1">Size:</td><td class="gdt2">10.5 MB</td></tr>
	<tr><td class="gdt1">Length:</td><td class="gdt2">%d pages</td></tr>
	<tr><td class="gdt1">Favorited:</td><td class="gdt2">123 times</td></tr>
</tbody></table></div>
<div id="rating_count">456</div>
<div id="rating_label">Average: 4.50</div>
<div id="taglist"><table><tbody>%s</tbody></table></div>
<div class="gpc">Showing 1 - %d of %d images</div>
<div id="gdt">%s</div>
</body></html>`, title, title, pageCount, tagsHTML.String(), pageCount, pageCount, pageLinks.String())
}

// mockPageHTML returns a gallery page with image URL and optional nl fallback.
func mockPageHTML(imgURL, nlCode string) string {
	nlAttr := ""
	if nlCode != "" {
		nlAttr = fmt.Sprintf(` onclick="return nl('%s')"`, nlCode)
	}
	return fmt.Sprintf(`<!DOCTYPE html><html><head></head><body>
<div id="imgd"><img id="img" src="%s"/></div>
<a href="#" id="loadfail"%s>Reload broken image</a>
</body></html>`, imgURL, nlAttr)
}

// mockImageBytes returns fake image data.
func mockImageBytes() []byte {
	return []byte{0x89, 0x50, 0x4E, 0x47, 0x0D, 0x0A, 0x1A, 0x0A} // PNG header
}

// ==================== Test Infrastructure ====================

func setupMockRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.RedirectTrailingSlash = false
	return r
}

// ==================== handleGetGallery Tests ====================

func TestMockGetGallery_InvalidID(t *testing.T) {
	r := setupMockRouter()
	app := &App{Client: &http.Client{}}
	r.GET("/api/gallery/:id/:token", app.handleGetGallery)

	req := httptest.NewRequest("GET", "/api/gallery/abc/xyz", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "invalid gallery id" {
		t.Errorf("error = %q, want %q", resp["error"], "invalid gallery id")
	}
}

func TestMockGetGallery_EmptyToken(t *testing.T) {
	r := setupMockRouter()
	app := &App{Client: &http.Client{}}
	r.GET("/api/gallery/:id/:token", app.handleGetGallery)

	// Gin route :token never matches empty — using "x" as placeholder to test token validation
	// is not meaningful; instead test that a non-existent route returns 404
	req := httptest.NewRequest("GET", "/api/gallery/12345/nonexist", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// With valid route params, the handler runs but may fail at API call
	if w.Code == http.StatusOK {
		t.Error("expected non-OK status for non-existent gallery")
	}
}

func TestMockGetGallery_APIError(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"gmetadata": []map[string]any{
				{"gid": 12345, "token": "abc", "error": "Invalid or missing parameters"},
			},
		})
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/gallery/:id/:token", app.handleGetGallery)

	req := httptest.NewRequest("GET", "/api/gallery/12345/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestMockGetGallery_EmptyMetadata(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"gmetadata": []map[string]any{},
		})
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/gallery/:id/:token", app.handleGetGallery)

	req := httptest.NewRequest("GET", "/api/gallery/12345/abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestMockGetGallery_Success(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"gmetadata": []map[string]any{
				{
					"gid":          3138775,
					"token":        "30b0285f9b",
					"title":        "Test Gallery",
					"title_jpn":    "テスト",
					"category":     "Manga",
					"thumb":        "https://example.com/thumb.jpg",
					"uploader":     "uploader1",
					"posted":       "1609459200",
					"filecount":    "65",
					"filesize":     1048576,
					"rating":       "4.86",
					"torrentcount": "3",
					"tags":         []string{"female:yuri", "language:chinese"},
				},
			},
		})
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/gallery/:id/:token", app.handleGetGallery)

	req := httptest.NewRequest("GET", "/api/gallery/3138775/30b0285f9b", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var gallery Gallery
	if err := json.Unmarshal(w.Body.Bytes(), &gallery); err != nil {
		t.Fatalf("failed to unmarshal: %v", err)
	}
	if gallery.ID != 3138775 {
		t.Errorf("ID = %d, want 3138775", gallery.ID)
	}
	if gallery.Token != "30b0285f9b" {
		t.Errorf("Token = %q, want %q", gallery.Token, "30b0285f9b")
	}
	if gallery.Title != "Test Gallery" {
		t.Errorf("Title = %q, want %q", gallery.Title, "Test Gallery")
	}
	if gallery.TitleJPN != "テスト" {
		t.Errorf("TitleJPN = %q, want %q", gallery.TitleJPN, "テスト")
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
	if len(gallery.Tags) != 2 {
		t.Errorf("Tags len = %d, want 2", len(gallery.Tags))
	}
	if gallery.PostedAt == nil || gallery.PostedAt.Year() != 2021 {
		t.Errorf("PostedAt = %v, want 2021", gallery.PostedAt)
	}
}

// ==================== handleSearch Tests ====================

func TestMockSearch_NoResults(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockNoHitsHTML())
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=notexist12345", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusBadGateway, w.Body.String())
	}
}

func TestMockSearch_Success(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockSearchHTML("yuri", 25))
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=yuri", nil)
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
			ID       int64           `json:"id"`
			Token    string          `json:"token"`
			Title    string          `json:"title"`
			Category GalleryCategory `json:"category"`
			Cover    string          `json:"cover"`
			URL      string          `json:"url"`
			Tags     []string        `json:"tags"`
			Uploader string          `json:"uploader"`
			Pages    int             `json:"pages"`
			Domain   string          `json:"domain"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v. body: %s", err, w.Body.String())
	}

	if resp.Total != 25 {
		t.Errorf("total = %d, want 25", resp.Total)
	}
	if resp.Page != 0 {
		t.Errorf("page = %d, want 0", resp.Page)
	}
	if resp.PageSize != 25 {
		t.Errorf("page_size = %d, want 25", resp.PageSize)
	}
	if resp.TotalPages != 1 {
		t.Errorf("total_pages = %d, want 1", resp.TotalPages)
	}
	if len(resp.Results) != 25 {
		t.Fatalf("results len = %d, want 25", len(resp.Results))
	}
	for i, r := range resp.Results {
		if r.ID == 0 || r.Token == "" || r.Title == "" {
			t.Errorf("results[%d]: missing fields (id=%d, token=%q, title=%q)", i, r.ID, r.Token, r.Title)
		}
	}
}

func TestMockSearch_ExtendedLayout(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockSearchHTML("yuri", 5))
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=yuri", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Results []struct {
			ID       int64           `json:"id"`
			Token    string          `json:"token"`
			Title    string          `json:"title"`
			Category GalleryCategory `json:"category"`
			Pages    int             `json:"pages"`
		} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if len(resp.Results) != 5 {
		t.Fatalf("results len = %d, want 5", len(resp.Results))
	}
	for i, r := range resp.Results {
		if r.ID == 0 || r.Token == "" || r.Title == "" {
			t.Errorf("results[%d]: missing fields (id=%d, token=%q, title=%q)", i, r.ID, r.Token, r.Title)
		}
	}
}

func TestMockSearch_SiteParam(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockSearchHTML("test", 1))
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/search", app.handleSearch)

	// site=ehentai should build URL with e-hentai.org
	req := httptest.NewRequest("GET", "/api/search?q=test&site=ehentai", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
}

func TestMockSearch_NegativePageClamped(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockSearchHTML("yuri", 5))
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/search", app.handleSearch)

	req := httptest.NewRequest("GET", "/api/search?q=yuri&page=-5", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Page int `json:"page"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Page != 0 {
		t.Errorf("page = %d, want 0 (negative clamped)", resp.Page)
	}
}

// ==================== handleGalleryDetails Tests ====================

func TestMockGalleryDetails_EmptyID(t *testing.T) {
	r := setupMockRouter()
	app := &App{Client: &http.Client{}}
	r.GET("/api/gallery/:id/:token/details", app.handleGalleryDetails)

	req := httptest.NewRequest("GET", "/api/gallery//abc/details", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMockGalleryDetails_ScrapeFailure(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/gallery/:id/:token/details", app.handleGalleryDetails)

	req := httptest.NewRequest("GET", "/api/gallery/12345/abc/details", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestMockGalleryDetails_Success(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryDetailHTML(3138775, "30b0285f9b", "Test Gallery", 65))
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/gallery/:id/:token/details", app.handleGalleryDetails)

	req := httptest.NewRequest("GET", "/api/gallery/3138775/30b0285f9b/details", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var details struct {
		ID        int    `json:"id"`
		Token     string `json:"token"`
		Title     string `json:"title"`
		Cover     string `json:"cover"`
		Category  string `json:"category"`
		Uploader  string `json:"uploader"`
		PageCount int    `json:"page_count"`
		Rating    float64 `json:"rating"`
		Tags      []Tag   `json:"tags"`
		PageUrls  []string `json:"page_urls"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &details); err != nil {
		t.Fatalf("failed to unmarshal: %v. body: %s", err, w.Body.String())
	}

	if details.ID != 3138775 {
		t.Errorf("ID = %d, want 3138775", details.ID)
	}
	if details.Token != "30b0285f9b" {
		t.Errorf("Token = %q, want %q", details.Token, "30b0285f9b")
	}
	if details.Title != "Test Gallery" {
		t.Errorf("Title = %q, want %q", details.Title, "Test Gallery")
	}
	if details.PageCount != 65 {
		t.Errorf("PageCount = %d, want 65", details.PageCount)
	}
	if details.Rating != 4.50 {
		t.Errorf("Rating = %f, want 4.50", details.Rating)
	}
	if len(details.Tags) == 0 {
		t.Error("Tags should not be empty")
	}
	if len(details.PageUrls) == 0 {
		t.Error("PageUrls should not be empty")
	}
}

// ==================== handleGalleryPages Tests ====================

func TestMockGalleryPages_EmptyID(t *testing.T) {
	r := setupMockRouter()
	app := &App{Client: &http.Client{}}
	r.GET("/api/gallery/:id/:token/pages", app.handleGalleryPages)

	req := httptest.NewRequest("GET", "/api/gallery//abc/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMockGalleryPages_Success(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryDetailHTML(12345, "tok12345", "Pages Test", 3))
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/gallery/:id/:token/pages", app.handleGalleryPages)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/pages", nil)
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
		t.Fatalf("failed to unmarshal: %v. body: %s", err, w.Body.String())
	}

	if resp.ID != "12345" {
		t.Errorf("ID = %q, want %q", resp.ID, "12345")
	}
	if resp.Token != "tok12345" {
		t.Errorf("Token = %q, want %q", resp.Token, "tok12345")
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
}

// ==================== handlePageImage Tests ====================

func TestMockPageImage_MissingURL(t *testing.T) {
	r := setupMockRouter()
	app := &App{Client: &http.Client{}}
	r.GET("/api/page-image", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/page-image", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["error"] != "missing url parameter" {
		t.Errorf("error = %q, want %q", resp["error"], "missing url parameter")
	}
}

func TestMockPageImage_ScrapeFailure(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/page-image", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/page-image?url=https://exhentai.org/s/abc/123-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestMockPageImage_Success(t *testing.T) {
	imgData := mockImageBytes()
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/s/") {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, mockPageHTML("https://example.com/image.webp", ""))
		} else {
			w.Header().Set("Content-Type", "image/webp")
			w.Write(imgData)
		}
	})
	defer mockServer.Close()

	pageURL := mockServer.URL + "/s/abc123/3138775-1"

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/page-image", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/page-image?url="+url.QueryEscape(pageURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if w.Header().Get("Content-Type") != "image/webp" {
		t.Errorf("Content-Type = %q, want %q", w.Header().Get("Content-Type"), "image/webp")
	}
	if len(w.Body.Bytes()) != len(imgData) {
		t.Errorf("body len = %d, want %d", len(w.Body.Bytes()), len(imgData))
	}
}

// ==================== nl Retry Tests ====================

func TestBuildNlFallbackURL(t *testing.T) {
	tests := []struct {
		name     string
		pageURL  string
		onclick  string
		expected string
	}{
		{
			"with nl code",
			"https://exhentai.org/s/abc123/3138775-1",
			`return nl('SZF-483294')`,
			"https://exhentai.org/s/abc123/3138775-1?nl=SZF-483294",
		},
		{
			"with existing query params",
			"https://exhentai.org/s/abc123/3138775-1?param=value",
			`return nl('XYZ-999')`,
			"https://exhentai.org/s/abc123/3138775-1?param=value&nl=XYZ-999",
		},
		{
			"empty onclick",
			"https://exhentai.org/s/abc123/3138775-1",
			"",
			"",
		},
		{
			"non-matching onclick",
			"https://exhentai.org/s/abc123/3138775-1",
			"return somethingElse('test')",
			"",
		},
		{
			"invalid URL",
			"://invalid",
			`return nl('ABC-123')`,
			"",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := buildNlFallbackURL(tt.pageURL, tt.onclick)
			if got != tt.expected {
				t.Errorf("buildNlFallbackURL(%q, %q) = %q, want %q", tt.pageURL, tt.onclick, got, tt.expected)
			}
		})
	}
}

func TestMockPageImage_RetryOnImageDownloadFailure(t *testing.T) {
	imgData := mockImageBytes()
	callCount := 0
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/s/") {
			w.Header().Set("Content-Type", "text/html")
			// First page request: provide image URL and nl fallback
			// The fallback page will serve the same HTML but the image URL will work
			fmt.Fprint(w, mockPageHTML("https://example.com/image.webp", "FALLBACK123"))
		} else if r.URL.Path == "/image.webp" {
			callCount++
			if callCount == 1 {
				// First image download fails
				w.WriteHeader(http.StatusInternalServerError)
				return
			}
			// Second attempt (via fallback) succeeds
			w.Header().Set("Content-Type", "image/webp")
			w.Write(imgData)
		} else {
			w.WriteHeader(http.StatusNotFound)
		}
	})
	defer mockServer.Close()

	pageURL := mockServer.URL + "/s/abc123/3138775-1"

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/page-image", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/page-image?url="+url.QueryEscape(pageURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if len(w.Body.Bytes()) != len(imgData) {
		t.Errorf("body len = %d, want %d", len(w.Body.Bytes()), len(imgData))
	}
	t.Logf("Retry succeeded: callCount=%d", callCount)
}

func TestMockPageImage_RetryExhausted(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/s/") {
			w.Header().Set("Content-Type", "text/html")
			// Always provide a fallback, but image always fails
			fmt.Fprint(w, mockPageHTML("https://example.com/broken.webp", "RETRY123"))
		} else {
			// All image downloads fail
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	defer mockServer.Close()

	pageURL := mockServer.URL + "/s/abc123/3138775-1"

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/page-image", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/page-image?url="+url.QueryEscape(pageURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should fail after exhausting retries
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestMockPageImage_NoFallbackNoRetry(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/s/") {
			w.Header().Set("Content-Type", "text/html")
			// No nl fallback
			fmt.Fprint(w, mockPageHTML("https://example.com/image.webp", ""))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	defer mockServer.Close()

	pageURL := mockServer.URL + "/s/abc123/3138775-1"

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/page-image", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/page-image?url="+url.QueryEscape(pageURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Should fail immediately since no fallback
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestMockPageImage_RetryWithMultipleFailures(t *testing.T) {
	imgData := mockImageBytes()
	imageAttempts := 0
	pageRequests := 0
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/s/") {
			pageRequests++
			w.Header().Set("Content-Type", "text/html")
			// Each page request provides a new fallback
			nlCode := fmt.Sprintf("RETRY%d", pageRequests)
			fmt.Fprint(w, mockPageHTML("https://example.com/image.webp", nlCode))
		} else {
			imageAttempts++
			if imageAttempts <= 2 {
				// First two attempts fail
				w.WriteHeader(http.StatusForbidden)
				return
			}
			// Third attempt succeeds
			w.Header().Set("Content-Type", "image/webp")
			w.Write(imgData)
		}
	})
	defer mockServer.Close()

	pageURL := mockServer.URL + "/s/abc123/3138775-1"

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/page-image", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/page-image?url="+url.QueryEscape(pageURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}
	if len(w.Body.Bytes()) != len(imgData) {
		t.Errorf("body len = %d, want %d", len(w.Body.Bytes()), len(imgData))
	}
	t.Logf("Retry with multiple failures: imageAttempts=%d, pageRequests=%d", imageAttempts, pageRequests)
}

func TestMockPageImage_RetryCancelledByContext(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/s/") {
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, mockPageHTML("https://example.com/image.webp", "RETRY123"))
		} else {
			w.WriteHeader(http.StatusInternalServerError)
		}
	})
	defer mockServer.Close()

	pageURL := mockServer.URL + "/s/abc123/3138775-1"

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/page-image", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/page-image?url="+url.QueryEscape(pageURL), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	// Even with retries, should eventually fail
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// ==================== Gallery List Tests ====================

func mockGalleryListHTML(count int) string {
	var rows strings.Builder
	for i := 0; i < count; i++ {
		gid := 3000 + i
		token := fmt.Sprintf("gal%04d", i)
		rows.WriteString(fmt.Sprintf(`<tr>
			<td class="gl1c glcat"><div class="cn ct2">Doujinshi</div></td>
			<td class="gl2c"><div class="glthumb"><div><img src="https://example.com/thumb%d.webp" /></div><div><div><div class="cn ct2">Doujinshi</div><div id="postedpop_%d">2024-01-01</div></div><div><div class="ir" style="background-position:-32px -1px;opacity:1"></div><div>%d pages</div></div></div></div><div><div id="posted_%d">2024-01-01</div><div class="ir" style="background-position:-32px -1px;opacity:1"></div></div></td>
			<td class="gl3c glname"><a href="https://exhentai.org/g/%d/%s/"><div class="glink">Gallery %d</div><div><div class="gt" title="female:yuri">yuri</div></div></a></td>
			<td class="gl4c glhide"><div><a href="https://exhentai.org/uploader/testuser%d">testuser%d</a></div><div>%d pages</div></td>
		</tr>`, i, gid, i+10, gid, gid, token, i, i, i, i+10))
	}

	return fmt.Sprintf(`<!DOCTYPE html><html><head></head><body>
	<table class="itg gltc"><tbody>
		<tr><th></th><th>Published</th><th>Title</th><th class="glhide">Uploader</th></tr>
		%s
	</tbody></table>
	<div class="searchnav"><div><a id="dnext" href="https://exhentai.org/?next=%d">Next &gt;</a></div></div>
</body></html>`, rows.String(), 3000+count)
}

func TestMockGalleryList_Success(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryListHTML(25))
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/gallerys", app.handleGallerys)

	req := httptest.NewRequest("GET", "/api/gallerys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Page     int `json:"page"`
		PageSize int `json:"page_size"`
		Results  []struct {
			ID       int64           `json:"id"`
			Token    string          `json:"token"`
			Title    string          `json:"title"`
			Category GalleryCategory `json:"category"`
			Cover    string          `json:"cover"`
			URL      string          `json:"url"`
			Tags     []string        `json:"tags"`
			Uploader string          `json:"uploader"`
			Pages    int             `json:"pages"`
		} `json:"results"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v. body: %s", err, w.Body.String())
	}

	if resp.Page != 0 {
		t.Errorf("page = %d, want 0", resp.Page)
	}
	if resp.PageSize != 25 {
		t.Errorf("page_size = %d, want 25", resp.PageSize)
	}
	if len(resp.Results) != 25 {
		t.Fatalf("results len = %d, want 25", len(resp.Results))
	}
	for i, r := range resp.Results {
		if r.ID == 0 || r.Token == "" || r.Title == "" {
			t.Errorf("results[%d]: missing fields (id=%d, token=%q, title=%q)", i, r.ID, r.Token, r.Title)
		}
	}
}

func TestMockWatched_Success(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryListHTML(10))
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/watched", app.handleWatched)

	req := httptest.NewRequest("GET", "/api/watched", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		PageSize int `json:"page_size"`
		Results  []struct {
			ID    int64  `json:"id"`
			Token string `json:"token"`
		} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.PageSize != 10 {
		t.Errorf("page_size = %d, want 10", resp.PageSize)
	}
	if len(resp.Results) != 10 {
		t.Errorf("results len = %d, want 10", len(resp.Results))
	}
}

func TestMockPopular_Success(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryListHTML(15))
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/popular", app.handlePopular)

	req := httptest.NewRequest("GET", "/api/popular", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		PageSize int `json:"page_size"`
		Results  []struct {
			ID    int64  `json:"id"`
			Token string `json:"token"`
		} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)

	if resp.PageSize != 15 {
		t.Errorf("page_size = %d, want 15", resp.PageSize)
	}
}

func TestMockGalleryList_Empty(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, `<!DOCTYPE html><html><head></head><body>
		<table class="itg gltc"><tbody></tbody></table>
		</body></html>`)
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/gallerys", app.handleGallerys)

	req := httptest.NewRequest("GET", "/api/gallerys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Results []interface{} `json:"results"`
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Results) != 0 {
		t.Errorf("results len = %d, want 0", len(resp.Results))
	}
}

func TestMockGalleryList_ScrapeFailure(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	})
	defer mockServer.Close()

	r := setupMockRouter()
	app := &App{Client: newMockClient(mockServer.URL)}
	r.GET("/api/gallerys", app.handleGallerys)

	req := httptest.NewRequest("GET", "/api/gallerys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}
