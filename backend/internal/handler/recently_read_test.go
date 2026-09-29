package handler

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	json "encoding/json/v2"

	"manga-reader/internal/database"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/model"
)

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

func TestHandleRecentlyRead_NoAutoCleanup(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "old", time.Now().UTC().AddDate(0, 0, -31))

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	server.RegisterRoutes(r)

	req := httptest.NewRequest("GET", "/api/recently-read", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.RecentlyReadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal recently-read: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("Results length = %d, want 1 (recently-read must not auto-cleanup)", len(resp.Results))
	}
	if resp.Results[0].ID != 111111 {
		t.Errorf("result ID = %d, want 111111", resp.Results[0].ID)
	}
	if n := countReadingProgress(t, client); n != 1 {
		t.Errorf("records after recently-read = %d, want 1 (no auto-cleanup)", n)
	}
}
