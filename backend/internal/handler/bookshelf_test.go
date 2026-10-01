package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"

	json "encoding/json/v2"

	"manga-reader/internal/database"
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
	if b.Title != "Cached Gallery" {
		t.Errorf("title = %q, want %q", b.Title, "Cached Gallery")
	}
	if b.PageCount != 2 {
		t.Errorf("page_count = %d, want 2", b.PageCount)
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
		SetTitle(b.Title).
		SetTitleJpn(b.TitleJPN).
		SetCategory(string(b.Category)).
		SetThumbnail(b.Thumbnail).
		SetPageCount(b.PageCount).
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
		SetTitle(b.Title).
		SetTitleJpn(b.TitleJPN).
		SetCategory(string(b.Category)).
		SetThumbnail(b.Thumbnail).
		SetPageCount(b.PageCount).
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
	if resp.AddedAt == nil {
		t.Error("AddedAt should be set")
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
	if resp.AddedAt != nil {
		t.Error("AddedAt should be nil")
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

func TestHandleBookshelfList_WithItems(t *testing.T) {
	client := newTestDB(t)
	b1 := newTestBookshelf()
	client.Bookshelf.Create().
		SetGalleryID(b1.GalleryID).
		SetToken(b1.Token).
		SetTitle(b1.Title).
		SetTitleJpn(b1.TitleJPN).
		SetCategory(string(b1.Category)).
		SetThumbnail(b1.Thumbnail).
		SetPageCount(b1.PageCount).
		Save(t.Context())
	b2 := newTestBookshelf2()
	client.Bookshelf.Create().
		SetGalleryID(b2.GalleryID).
		SetToken(b2.Token).
		SetTitle(b2.Title).
		SetTitleJpn(b2.TitleJPN).
		SetCategory(string(b2.Category)).
		SetThumbnail(b2.Thumbnail).
		SetPageCount(b2.PageCount).
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
