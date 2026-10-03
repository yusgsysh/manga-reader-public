package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	json "encoding/json/v2"

	"manga-reader/internal/database"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/model"
)

func TestHandleBookshelfAdd_InvalidID(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.POST("/api/bookshelf/:id/:token", server.handleBookshelfAdd)

	req := httptest.NewRequest("POST", "/api/bookshelf/abc/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleBookshelfAdd_EmptyToken(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.POST("/api/bookshelf/:id/:token", server.handleBookshelfAdd)

	req := httptest.NewRequest("POST", "/api/bookshelf/123456/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("expected non-OK status for missing token")
	}
}

func TestHandleBookshelfAdd_Success(t *testing.T) {
	client := newTestDB(t)

	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{
			"gmetadata": []map[string]any{
				{
					"gid":       123456,
					"token":     "abcdef1234",
					"title":     "Test Gallery",
					"title_jpn": "テスト",
					"category":  "Doujinshi",
					"thumb":     "https://example.com/thumb.webp",
					"filecount": "24",
					"rating":    "4.50",
					"posted":    "1700000000",
					"tags":      []string{"female:yuri"},
				},
			},
		})
	})
	defer mockServer.Close()

	httpClient := newMockClient(mockServer.URL)
	r := setupTestRouter()
	server := &Server{Client: httpClient, DB: &database.DB{Client: client}}

	r.POST("/api/bookshelf/:id/:token", server.handleBookshelfAdd)

	req := httptest.NewRequest("POST", "/api/bookshelf/123456/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.BookshelfMutationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Success {
		t.Error("Success should be true")
	}
	if !resp.InBookshelf {
		t.Error("InBookshelf should be true")
	}
}

func TestHandleBookshelfAdd_Idempotent(t *testing.T) {
	client := newTestDB(t)

	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.MarshalWrite(w, map[string]any{
			"gmetadata": []map[string]any{
				{
					"gid":       123456,
					"token":     "abcdef1234",
					"title":     "Test Gallery",
					"title_jpn": "テスト",
					"category":  "Doujinshi",
					"thumb":     "https://example.com/thumb.webp",
					"filecount": "24",
					"rating":    "4.50",
					"posted":    "1700000000",
					"tags":      []string{},
				},
			},
		})
	})
	defer mockServer.Close()

	httpClient := newMockClient(mockServer.URL)
	r := setupTestRouter()
	server := &Server{Client: httpClient, DB: &database.DB{Client: client}}

	r.POST("/api/bookshelf/:id/:token", server.handleBookshelfAdd)

	req := httptest.NewRequest("POST", "/api/bookshelf/123456/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first add: status = %d", w.Code)
	}

	req = httptest.NewRequest("POST", "/api/bookshelf/123456/abcdef1234", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("second add: status = %d, want %d", w.Code, http.StatusOK)
	}

	count, _ := client.Bookshelf.Query().Count(t.Context())
	if count != 1 {
		t.Errorf("Count = %d, want 1 (no duplicate)", count)
	}
}

func TestHandleBookshelfAdd_OfflineUsesCachedMetadata(t *testing.T) {
	client := newTestDB(t)
	seedGalleryCache(t, client, 123456, "abcdef1234")

	r := setupTestRouter()
	server := &Server{Client: errorClient(), DB: &database.DB{Client: client}}
	r.POST("/api/bookshelf/:id/:token", server.handleBookshelfAdd)

	req := httptest.NewRequest("POST", "/api/bookshelf/123456/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp model.BookshelfMutationResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !resp.Success || !resp.InBookshelf {
		t.Errorf("success=%v in_bookshelf=%v, want both true", resp.Success, resp.InBookshelf)
	}
	if !resp.Offline {
		t.Error("offline flag should be true when falling back to cache")
	}

	b, err := client.Bookshelf.Query().Only(t.Context())
	if err != nil {
		t.Fatalf("bookshelf query: %v", err)
	}
	if b.GalleryID != 123456 || b.Token != "abcdef1234" {
		t.Errorf("bookshelf ref = %d/%q, want 123456/abcdef1234", b.GalleryID, b.Token)
	}

	// Metadata is not stored on the bookshelf row; it is served from
	// gallery_cache (seeded above).
	row, found, err := gallerycache.Get(t.Context(), client, 123456, "abcdef1234")
	if err != nil || !found {
		t.Fatalf("gallery cache lookup: found=%v err=%v", found, err)
	}
	if row.Title != "Cached Gallery" || row.PageCount != 2 {
		t.Errorf("cached meta = %q/%d, want Cached Gallery/2", row.Title, row.PageCount)
	}
}

func TestHandleBookshelfAdd_OfflineNoCacheFails(t *testing.T) {
	client := newTestDB(t)

	r := setupTestRouter()
	server := &Server{Client: errorClient(), DB: &database.DB{Client: client}}
	r.POST("/api/bookshelf/:id/:token", server.handleBookshelfAdd)

	req := httptest.NewRequest("POST", "/api/bookshelf/123456/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadGateway)
	}

	count, _ := client.Bookshelf.Query().Count(t.Context())
	if count != 0 {
		t.Errorf("bookshelf count = %d, want 0", count)
	}
}

func TestHandleBookshelfRemove_Success(t *testing.T) {
	client := newTestDB(t)
	b := newTestBookshelf()
	client.Bookshelf.Create().
		SetGalleryID(b.GalleryID).
		SetToken(b.Token).
		Save(t.Context())

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.DELETE("/api/bookshelf/:id/:token", server.handleBookshelfRemove)

	req := httptest.NewRequest("DELETE", "/api/bookshelf/123456/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.BookshelfMutationResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Success {
		t.Error("Success should be true")
	}
	if resp.InBookshelf {
		t.Error("InBookshelf should be false after remove")
	}
}

func TestHandleBookshelfRemove_InvalidID(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.DELETE("/api/bookshelf/:id/:token", server.handleBookshelfRemove)

	req := httptest.NewRequest("DELETE", "/api/bookshelf/abc/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleBookshelfStatus_InBookshelf(t *testing.T) {
	client := newTestDB(t)
	b := newTestBookshelf()
	client.Bookshelf.Create().
		SetGalleryID(b.GalleryID).
		SetToken(b.Token).
		Save(t.Context())

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/bookshelf/:id/:token/status", server.handleBookshelfStatus)

	req := httptest.NewRequest("GET", "/api/bookshelf/123456/abcdef1234/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.BookshelfStatus
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.InBookshelf {
		t.Error("InBookshelf should be true")
	}
	if resp.CreatedAt == nil {
		t.Error("CreatedAt should be set")
	}
}

func TestHandleBookshelfStatus_NotInBookshelf(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/bookshelf/:id/:token/status", server.handleBookshelfStatus)

	req := httptest.NewRequest("GET", "/api/bookshelf/999999/nonexist/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.BookshelfStatus
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.InBookshelf {
		t.Error("InBookshelf should be false")
	}
	if resp.CreatedAt != nil {
		t.Error("CreatedAt should be nil")
	}
}

func TestHandleBookshelfList_Empty(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/bookshelf", server.handleBookshelfList)

	req := httptest.NewRequest("GET", "/api/bookshelf", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.BookshelfListResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 0 {
		t.Errorf("Total = %d, want 0", resp.Total)
	}
	if len(resp.Results) != 0 {
		t.Errorf("Results length = %d, want 0", len(resp.Results))
	}
}

func TestHandleBookshelfList_PrefersGalleryCache(t *testing.T) {
	client := newTestDB(t)
	b := newTestBookshelf()

	// The bookshelf row stores only the reference.
	client.Bookshelf.Create().
		SetGalleryID(b.GalleryID).
		SetToken(b.Token).
		Save(t.Context())

	// gallery_cache holds the metadata served for it.
	seedGalleryCache(t, client, b.GalleryID, b.Token)

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	r.GET("/api/bookshelf", server.handleBookshelfList)

	req := httptest.NewRequest("GET", "/api/bookshelf", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	var resp model.BookshelfListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(resp.Results))
	}
	item := resp.Results[0]
	if item.Title != "Cached Gallery" {
		t.Errorf("title = %q, want the cached title", item.Title)
	}
	if item.Thumbnail != "https://example.com/thumb.webp" {
		t.Errorf("thumbnail = %q, want the cached thumbnail", item.Thumbnail)
	}
	if item.Pages != 2 {
		t.Errorf("pages = %d, want the cached 2", item.Pages)
	}
}

func TestHandleBookshelfList_WithItems(t *testing.T) {
	client := newTestDB(t)
	b1 := newTestBookshelf()
	client.Bookshelf.Create().
		SetGalleryID(b1.GalleryID).
		SetToken(b1.Token).
		Save(t.Context())
	b2 := newTestBookshelf2()
	client.Bookshelf.Create().
		SetGalleryID(b2.GalleryID).
		SetToken(b2.Token).
		Save(t.Context())

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/bookshelf", server.handleBookshelfList)

	req := httptest.NewRequest("GET", "/api/bookshelf", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.BookshelfListResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
	if len(resp.Results) != 2 {
		t.Errorf("Results length = %d, want 2", len(resp.Results))
	}
}

func TestHandleBookshelfList_OrdersByUpdatedAt(t *testing.T) {
	client := newTestDB(t)
	ctx := t.Context()

	older := time.Now().UTC().Add(-48 * time.Hour)
	newer := time.Now().UTC().Add(-1 * time.Hour)

	b1 := newTestBookshelf()
	client.Bookshelf.Create().
		SetGalleryID(b1.GalleryID).
		SetToken(b1.Token).
		SetCreatedAt(older).
		SetUpdatedAt(older).
		Save(ctx)

	// Added more recently but read less recently than b2.
	b2 := newTestBookshelf2()
	client.Bookshelf.Create().
		SetGalleryID(b2.GalleryID).
		SetToken(b2.Token).
		SetCreatedAt(newer).
		SetUpdatedAt(newer).
		Save(ctx)

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	r.GET("/api/bookshelf", server.handleBookshelfList)

	req := httptest.NewRequest("GET", "/api/bookshelf", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d. body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp model.BookshelfListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("Results length = %d, want 2", len(resp.Results))
	}
	if resp.Results[0].ID != b2.GalleryID {
		t.Errorf("first item = %d, want %d (most recently updated first)", resp.Results[0].ID, b2.GalleryID)
	}
	if resp.Results[1].ID != b1.GalleryID {
		t.Errorf("second item = %d, want %d", resp.Results[1].ID, b1.GalleryID)
	}
}
