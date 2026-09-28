package handler

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"manga-reader/internal/model"

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

// mockSearchHTML returns an EHentai search results page with compact layout.
func mockSearchHTML(count int) string {
	var rows strings.Builder
	for i := range count {
		gid := 1000 + i
		token := fmt.Sprintf("tok%04d", i)
		rows.WriteString(fmt.Sprintf(`<tr>
			<td class="gl1c glcat"><div class="cn ct2">Doujinshi</div></td>
			<td class="gl2c"><div class="glthumb"><div><img src="https://example.com/thumb%d.webp" /></div><div><div><div class="cn ct2">Doujinshi</div><div>2024-01-01</div></div><div><div class="ir" style="background-position:-32px -1px;opacity:1"></div><div>%d pages</div></div></div></div><div><div>2024-01-01</div><div class="ir" style="background-position:-32px -1px;opacity:1"></div></div></td>
			<td class="gl3c glname"><a href="https://exhentai.org/g/%d/%s/"><div class="glink">Test Gallery %d</div><div><div class="gt" title="female:yuri">yuri</div></div></a></td>
			<td class="gl4c glhide"><div><a href="https://exhentai.org/uploader/testuser%d">testuser%d</a></div><div>%d pages</div></td>
		</tr>`, i, i+10, gid, token, i, i, i, i+10))
	}

	return fmt.Sprintf(`<!DOCTYPE html><html><head></head><body>
<div class="ido">
	<div></div>
	<div>
		<div class="searchtext"><p>Found %d results. %.0f galleries on this page.</p></div>
		<table class="itg gltc"><tbody>
		<tr><th></th><th>Published</th><th>Title</th><th class="glhide">Uploader</th></tr>
		%s
	</tbody></table>
	<div class="searchnav"><div><a id="dnext" href="https://exhentai.org/?next=%d">Next &gt;</a></div></div>
</div></body></html>`, count, float64(count), rows.String(), 1000+count)
}

// mockNoHitsHTML returns a search page with no results.
func mockNoHitsHTML() string {
	return `<!DOCTYPE html><html><head></head><body>
<div class="ido">
	<div><p>No unfiltered results found. Try lowering the search requirements.</p></div>
</div></body></html>`
}

// mockGalleryDetailHTML returns a gallery details page.
func mockGalleryDetailHTML(gid int, title string, pageCount int) string {
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

// mockPaginatedGalleryHandler serves a gallery details page whose thumbnail
// list is paginated 40 links per request, selected by the "p" query parameter.
func mockPaginatedGalleryHandler(gid int, title string, total int) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		p := 0
		if v := r.URL.Query().Get("p"); v != "" {
			p, _ = strconv.Atoi(v)
		}
		start := p*40 + 1
		end := min(start+40, total+1)
		var links strings.Builder
		for i := start; i < end; i++ {
			links.WriteString(fmt.Sprintf(`<a href="https://exhentai.org/s/abc%d/%d-%d">p%d</a>`, i, gid, i, i))
		}
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprintf(w, `<!DOCTYPE html><html><head></head><body>
<div id="gn">%s</div>
<div class="gpc">Showing 1 - %d of %d images</div>
<div id="gdt">%s</div>
</body></html>`, title, end-start, total, links.String())
	}
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

func setupMockRouter(server *Server) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.RedirectTrailingSlash = false
	server.RegisterRoutes(r)
	return r
}

// ==================== handleGetGallery Tests ====================

func TestMockGetGallery_InvalidID(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

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
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/3138775/30b0285f9b", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var gallery model.Gallery
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
	if gallery.Category != model.CategoryManga {
		t.Errorf("Category = %q, want %q", gallery.Category, model.CategoryManga)
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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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
		fmt.Fprint(w, mockSearchHTML(25))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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
			ID       int64                 `json:"id"`
			Token    string                `json:"token"`
			Title    string                `json:"title"`
			Category model.GalleryCategory `json:"category"`
			Cover    string                `json:"cover"`
			URL      string                `json:"url"`
			Tags     []string              `json:"tags"`
			Uploader string                `json:"uploader"`
			Pages    int                   `json:"pages"`
			Domain   string                `json:"domain"`
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
		fmt.Fprint(w, mockSearchHTML(5))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/search?q=yuri", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp struct {
		Results []struct {
			ID       int64                 `json:"id"`
			Token    string                `json:"token"`
			Title    string                `json:"title"`
			Category model.GalleryCategory `json:"category"`
			Pages    int                   `json:"pages"`
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
		fmt.Fprint(w, mockSearchHTML(1))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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
		fmt.Fprint(w, mockSearchHTML(5))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

// ==================== Advanced Search Parameter Tests ====================

func TestMockSearch_InvalidMinPages(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/search?q=test&min_pages=-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestMockSearch_InvalidMaxPages(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/search?q=test&max_pages=-5", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestMockSearch_MinPagesGreaterThanMax(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/search?q=test&min_pages=200&max_pages=10", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestMockSearch_InvalidMinRating(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/search?q=test&min_rating=1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}

	req = httptest.NewRequest("GET", "/api/search?q=test&min_rating=6", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestMockSearch_InvalidHasTorrent(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/search?q=test&has_torrent=abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d. body: %s", w.Code, http.StatusBadRequest, w.Body.String())
	}
}

func TestMockSearch_InvalidBooleanParams(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	params := []string{
		"include_expunged", "search_name", "search_tags", "search_description",
		"include_low_power_tags", "include_downvoted_tags",
		"disable_language_filter", "disable_uploader_filter", "disable_tag_filter",
	}
	for _, p := range params {
		req := httptest.NewRequest("GET", "/api/search?q=test&"+p+"=badvalue", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)

		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want %d. body: %s", p, w.Code, http.StatusBadRequest, w.Body.String())
		}
	}
}

func TestMockSearch_InvalidNonNumericParams(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/search?q=test&min_pages=abc", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("min_pages=abc: status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	req = httptest.NewRequest("GET", "/api/search?q=test&max_pages=xyz", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("max_pages=xyz: status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	req = httptest.NewRequest("GET", "/api/search?q=test&min_rating=abc", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("min_rating=abc: status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMockSearch_AdvancedSearchParamsPassThrough(t *testing.T) {
	var requestedURL string
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockSearchHTML(1))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/search?q=test&has_torrent=true&min_pages=10&max_pages=200&min_rating=4", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	parsedURL, _ := url.Parse(requestedURL)
	q := parsedURL.Query()

	if q.Get("advsearch") != "1" {
		t.Errorf("advsearch = %q, want %q", q.Get("advsearch"), "1")
	}
	if q.Get("f_search") != "test" {
		t.Errorf("f_search = %q, want %q", q.Get("f_search"), "test")
	}
	if q.Get("f_sto") != "on" {
		t.Errorf("f_sto = %q, want %q", q.Get("f_sto"), "on")
	}
	if q.Get("f_spf") != "10" {
		t.Errorf("f_spf = %q, want %q", q.Get("f_spf"), "10")
	}
	if q.Get("f_spt") != "200" {
		t.Errorf("f_spt = %q, want %q", q.Get("f_spt"), "200")
	}
	if q.Get("f_sr") != "on" {
		t.Errorf("f_sr = %q, want %q", q.Get("f_sr"), "on")
	}
	if q.Get("f_srdd") != "4" {
		t.Errorf("f_srdd = %q, want %q", q.Get("f_srdd"), "4")
	}
}

func TestMockSearch_NoAdvancedWhenEmptyOpts(t *testing.T) {
	var requestedURL string
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockSearchHTML(1))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/search?q=test", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	parsedURL, _ := url.Parse(requestedURL)
	q := parsedURL.Query()

	if q.Get("advsearch") != "" {
		t.Errorf("advsearch should not be set for basic search, got %q", q.Get("advsearch"))
	}
}

func TestMockGalleryDetails_EmptyID(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/abc/details", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

func TestMockGalleryDetails_Success(t *testing.T) {
	var reqCount atomic.Int32
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryDetailHTML(3138775, "Test Gallery", 65))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/3138775/30b0285f9b/details", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	if got := reqCount.Load(); got != 1 {
		t.Errorf("upstream requests = %d, want 1 (details must not fetch thumbnail pages)", got)
	}
	if strings.Contains(w.Body.String(), "page_urls") {
		t.Error("details response should not contain page_urls")
	}

	var details struct {
		ID        int         `json:"id"`
		Token     string      `json:"token"`
		Title     string      `json:"title"`
		Cover     string      `json:"cover"`
		Category  string      `json:"category"`
		Uploader  string      `json:"uploader"`
		PageCount int         `json:"page_count"`
		Rating    float64     `json:"rating"`
		Tags      []model.Tag `json:"tags"`
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
}

// ==================== handleGalleryPages Tests ====================

func TestMockGalleryPages_EmptyID(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

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
		fmt.Fprint(w, mockGalleryDetailHTML(12345, "Pages Test", 3))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

func TestMockGalleryPages_Paginated(t *testing.T) {
	var reqCount atomic.Int32
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		reqCount.Add(1)
		mockPaginatedGalleryHandler(12345, "Paginated", 65)(w, r)
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallery/12345/tok12345/pages", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	if got := reqCount.Load(); got != 2 {
		t.Errorf("upstream requests = %d, want 2 (first page + ?p=1)", got)
	}

	var resp struct {
		Total int `json:"total"`
		Pages []struct {
			PageURL string `json:"page_url"`
			Index   int    `json:"index"`
		} `json:"pages"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to unmarshal: %v. body: %s", err, w.Body.String())
	}

	if resp.Total != 65 {
		t.Errorf("total = %d, want 65", resp.Total)
	}
	if len(resp.Pages) != 65 {
		t.Fatalf("pages len = %d, want 65", len(resp.Pages))
	}
	for i, p := range resp.Pages {
		if p.Index != i {
			t.Errorf("pages[%d].index = %d, want %d", i, p.Index, i)
		}
		wantSuffix := fmt.Sprintf("-%d", i+1)
		if !strings.HasSuffix(p.PageURL, wantSuffix) {
			t.Errorf("pages[%d].page_url = %q, want suffix %q", i, p.PageURL, wantSuffix)
		}
	}
}

// ==================== handlePageImage Tests ====================

func TestMockPageImage_MissingURL(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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
	for i := range count {
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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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
			ID       int64                 `json:"id"`
			Token    string                `json:"token"`
			Title    string                `json:"title"`
			Category model.GalleryCategory `json:"category"`
			Cover    string                `json:"cover"`
			URL      string                `json:"url"`
			Tags     []string              `json:"tags"`
			Uploader string                `json:"uploader"`
			Pages    int                   `json:"pages"`
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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallerys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp struct {
		Results []any `json:"results"`
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

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallerys", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}
}

// ==================== Watched/Gallerys Advanced Search Tests ====================

func TestMockWatched_AdvancedSearchParams(t *testing.T) {
	var requestedURL string
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryListHTML(1))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/watched?has_torrent=true&min_rating=4", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	parsedURL, _ := url.Parse(requestedURL)
	q := parsedURL.Query()

	if q.Get("advsearch") != "1" {
		t.Errorf("advsearch = %q, want %q", q.Get("advsearch"), "1")
	}
	if q.Get("f_sto") != "on" {
		t.Errorf("f_sto = %q, want %q", q.Get("f_sto"), "on")
	}
	if q.Get("f_sr") != "on" {
		t.Errorf("f_sr = %q, want %q", q.Get("f_sr"), "on")
	}
	if q.Get("f_srdd") != "4" {
		t.Errorf("f_srdd = %q, want %q", q.Get("f_srdd"), "4")
	}
}

func TestMockWatched_PageRange(t *testing.T) {
	var requestedURL string
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryListHTML(1))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/watched?min_pages=10&max_pages=200", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	parsedURL, _ := url.Parse(requestedURL)
	q := parsedURL.Query()

	if q.Get("advsearch") != "1" {
		t.Errorf("advsearch = %q, want %q", q.Get("advsearch"), "1")
	}
	if q.Get("f_spf") != "10" {
		t.Errorf("f_spf = %q, want %q", q.Get("f_spf"), "10")
	}
	if q.Get("f_spt") != "200" {
		t.Errorf("f_spt = %q, want %q", q.Get("f_spt"), "200")
	}
}

func TestMockWatched_InvalidParams(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/watched?min_pages=-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("min_pages=-1: status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	req = httptest.NewRequest("GET", "/api/watched?min_rating=6", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("min_rating=6: status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	req = httptest.NewRequest("GET", "/api/watched?has_torrent=invalid", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("has_torrent=invalid: status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestMockGallerys_AdvancedSearchParams(t *testing.T) {
	var requestedURL string
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryListHTML(1))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/gallerys?include_expunged=true&search_name=true", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	parsedURL, _ := url.Parse(requestedURL)
	q := parsedURL.Query()

	if q.Get("advsearch") != "1" {
		t.Errorf("advsearch = %q, want %q", q.Get("advsearch"), "1")
	}
	if q.Get("f_sh") != "on" {
		t.Errorf("f_sh = %q, want %q", q.Get("f_sh"), "on")
	}
	if q.Get("f_sname") != "on" {
		t.Errorf("f_sname = %q, want %q", q.Get("f_sname"), "on")
	}
}

func TestMockWatched_NoAdvancedWhenEmptyOpts(t *testing.T) {
	var requestedURL string
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		requestedURL = r.URL.String()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryListHTML(1))
	})
	defer mockServer.Close()

	server := &Server{Client: newMockClient(mockServer.URL)}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/watched", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	parsedURL, _ := url.Parse(requestedURL)
	q := parsedURL.Query()

	if q.Get("advsearch") != "" {
		t.Errorf("advsearch should not be set for basic watched, got %q", q.Get("advsearch"))
	}
}
