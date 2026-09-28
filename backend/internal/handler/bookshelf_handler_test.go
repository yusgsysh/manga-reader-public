package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	json "encoding/json/v2"

	"manga-reader/internal/database"
	"manga-reader/internal/ent/bookshelf"
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

func TestHandleGetProgress_NotExists(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/progress/:id/:token", server.handleGetProgress)

	req := httptest.NewRequest("GET", "/api/progress/123456/abcdef", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.GalleryID != 123456 {
		t.Errorf("GalleryID = %d, want 123456", resp.GalleryID)
	}
	if resp.CurrentPage != 0 {
		t.Errorf("CurrentPage = %d, want 0", resp.CurrentPage)
	}
}

func TestHandleGetProgress_Exists(t *testing.T) {
	client := newTestDB(t)
	client.ReadingProgress.Create().
		SetGalleryID(123456).
		SetToken("abcdef").
		SetCurrentPage(10).
		SetProgress(0.416).
		SetCompleted(false).
		SetStartedAt(time.Now().UTC()).
		SetUpdatedAt(time.Now().UTC()).
		Save(t.Context())

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/progress/:id/:token", server.handleGetProgress)

	httpReq := httptest.NewRequest("GET", "/api/progress/123456/abcdef", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.CurrentPage != 10 {
		t.Errorf("CurrentPage = %d, want 10", resp.CurrentPage)
	}
	if resp.Progress != 0.416 {
		t.Errorf("Progress = %f, want 0.416", resp.Progress)
	}
}

func TestHandleUpdateProgress_First(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: 5,
		Progress:    0.208,
		Completed:   false,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.CurrentPage != 5 {
		t.Errorf("CurrentPage = %d, want 5", resp.CurrentPage)
	}
	if resp.StartedAt == nil {
		t.Error("StartedAt should be set on first insert")
	}
}

func TestHandleUpdateProgress_Update(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{CurrentPage: 5, Progress: 0.2})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var first model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &first)

	time.Sleep(10 * time.Millisecond)

	body, _ = json.Marshal(model.UpdateReadingProgressRequest{CurrentPage: 10, Progress: 0.4})
	req = httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var second model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &second)

	if second.CurrentPage != 10 {
		t.Errorf("CurrentPage = %d, want 10", second.CurrentPage)
	}
	if first.StartedAt != nil && second.StartedAt != nil && !first.StartedAt.Equal(*second.StartedAt) {
		t.Error("started_at should not change")
	}
}

func TestHandleUpdateProgress_Completed(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: 24,
		Progress:    0.5,
		Completed:   true,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Completed {
		t.Error("Completed should be true")
	}
	if resp.Progress != 1 {
		t.Errorf("Progress = %f, want 1 (auto-set when completed=true)", resp.Progress)
	}
}

func TestHandleUpdateProgress_InvalidProgress(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: 5,
		Progress:    1.5,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateProgress_NegativeProgress(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: 5,
		Progress:    -0.1,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateProgress_NegativePage(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: -1,
		Progress:    0.5,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleRecentlyRead_Empty(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/recently-read", server.handleRecentlyRead)

	req := httptest.NewRequest("GET", "/api/recently-read", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.RecentlyReadResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Results) != 0 {
		t.Errorf("Results length = %d, want 0", len(resp.Results))
	}
}

func TestHandleRecentlyRead_WithRecords(t *testing.T) {
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

	client.ReadingProgress.Create().
		SetGalleryID(b1.GalleryID).
		SetToken(b1.Token).
		SetCurrentPage(5).
		SetProgress(0.2).
		SetCompleted(false).
		SetStartedAt(time.Now().UTC()).
		SetUpdatedAt(time.Now().UTC()).
		Save(t.Context())

	time.Sleep(10 * time.Millisecond)

	client.ReadingProgress.Create().
		SetGalleryID(222222).
		SetToken("bbb").
		SetCurrentPage(10).
		SetProgress(0.4).
		SetCompleted(false).
		SetStartedAt(time.Now().UTC()).
		SetUpdatedAt(time.Now().UTC()).
		Save(t.Context())

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/recently-read", server.handleRecentlyRead)

	req := httptest.NewRequest("GET", "/api/recently-read", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.RecentlyReadResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Results) != 2 {
		t.Fatalf("Results length = %d, want 2", len(resp.Results))
	}
	if resp.Results[0].ID != 222222 {
		t.Errorf("first result ID = %d, want 222222", resp.Results[0].ID)
	}
	if resp.Results[1].Title != b1.Title {
		t.Errorf("second result Title = %q, want %q", resp.Results[1].Title, b1.Title)
	}
}

func TestHandleRecentlyRead_AfterBookshelfRemove(t *testing.T) {
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

	client.ReadingProgress.Create().
		SetGalleryID(b.GalleryID).
		SetToken(b.Token).
		SetCurrentPage(5).
		SetProgress(0.2).
		SetCompleted(false).
		SetStartedAt(time.Now().UTC()).
		SetUpdatedAt(time.Now().UTC()).
		Save(t.Context())

	client.Bookshelf.Delete().
		Where(
			bookshelf.GalleryID(b.GalleryID),
			bookshelf.Token(b.Token),
		).
		Exec(t.Context())

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/recently-read", server.handleRecentlyRead)

	req := httptest.NewRequest("GET", "/api/recently-read", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.RecentlyReadResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Results) != 1 {
		t.Fatalf("Results length = %d, want 1 (reading persists after bookshelf removal)", len(resp.Results))
	}
	if resp.Results[0].Title != "" {
		t.Errorf("Title should be empty after bookshelf removal, got %q", resp.Results[0].Title)
	}
	if resp.Results[0].Reading.CurrentPage != 5 {
		t.Errorf("Reading.CurrentPage = %d, want 5", resp.Results[0].Reading.CurrentPage)
	}
}
