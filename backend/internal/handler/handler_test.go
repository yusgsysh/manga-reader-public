package handler

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"

	"manga-reader/internal/model"

	"github.com/gin-gonic/gin"
)

// ==================== 测试工具 ====================

func setupRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	return gin.New()
}

// pagesStreamResult is the decoded form of an NDJSON /pages response.
type pagesStreamResult struct {
	Meta     galleryPagesLine
	Pages    []model.CachedPage
	Done     bool
	Error    string
	Terminal int
}

// parsePagesStream decodes the NDJSON body of GET /api/gallery/:id/:token/pages
// and enforces the protocol: meta first, then pages, then exactly one terminal
// done/error line.
func parsePagesStream(t *testing.T, body []byte) pagesStreamResult {
	t.Helper()

	text := strings.TrimRight(string(body), "\n")
	if text == "" {
		t.Fatal("empty stream body")
	}

	var res pagesStreamResult
	sawMeta := false
	for i, line := range strings.Split(text, "\n") {
		var l galleryPagesLine
		if err := json.Unmarshal([]byte(line), &l); err != nil {
			t.Fatalf("line %d: unmarshal %q: %v", i, line, err)
		}
		switch l.Type {
		case "meta":
			if sawMeta {
				t.Fatalf("duplicate meta line at %d", i)
			}
			if l.Total == nil {
				t.Fatalf("meta line %d is missing total", i)
			}
			sawMeta = true
			res.Meta = l
		case "page":
			if !sawMeta {
				t.Fatalf("page line before meta at %d", i)
			}
			if l.Index == nil {
				t.Fatalf("page line %d is missing index", i)
			}
			res.Pages = append(res.Pages, model.CachedPage{
				PageURL:   l.PageURL,
				Index:     *l.Index,
				Thumbnail: l.Thumbnail,
			})
		case "done":
			if l.Total == nil {
				t.Fatalf("done line %d is missing total", i)
			}
			res.Done = true
			res.Terminal++
		case "error":
			if l.Error == "" {
				t.Fatalf("error line %d is missing error", i)
			}
			res.Error = l.Error
			res.Terminal++
		default:
			t.Fatalf("unknown line type %q at %d: %s", l.Type, i, line)
		}
	}
	if !sawMeta {
		t.Fatal("missing meta line")
	}
	if res.Terminal != 1 {
		t.Fatalf("terminal lines = %d, want exactly 1", res.Terminal)
	}
	if res.Done && *res.Meta.Total > 0 && len(res.Pages) < *res.Meta.Total {
		t.Fatalf("pages = %d, want >= meta total %d", len(res.Pages), *res.Meta.Total)
	}
	return res
}

// ==================== 参数校验测试 ====================

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

func TestAPI_PageImage_MissingURL(t *testing.T) {
	r := setupRouter()
	app := &Server{Client: &http.Client{}}
	r.GET("/api/image/page", app.handlePageImage)

	req := httptest.NewRequest("GET", "/api/image/page", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
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
