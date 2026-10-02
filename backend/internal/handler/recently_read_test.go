package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	json "encoding/json/v2"

	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/model"
)

func seedGalleryMeta(t *testing.T, client *ent.Client, g *model.Gallery) {
	t.Helper()
	err := gallerycache.UpsertMeta(t.Context(), client, g.ID, g.Token, model.GalleryCacheSnapshot{
		Title:     g.Title,
		TitleJPN:  g.TitleJPN,
		Category:  string(g.Category),
		Thumbnail: g.Thumbnail,
		PageCount: g.PageCount,
	})
	if err != nil {
		t.Fatalf("seed gallery meta: %v", err)
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
	if resp.Page != 0 {
		t.Errorf("Page = %d, want 0", resp.Page)
	}
	if resp.PageSize != 25 {
		t.Errorf("PageSize = %d, want 25", resp.PageSize)
	}
	if resp.Total != 0 {
		t.Errorf("Total = %d, want 0", resp.Total)
	}
	if resp.TotalPages != 0 {
		t.Errorf("TotalPages = %d, want 0", resp.TotalPages)
	}
}

func TestHandleRecentlyRead_WithRecords(t *testing.T) {
	client := newTestDB(t)

	meta := newTestGallery()
	seedGalleryMeta(t, client, meta)
	client.ReadingProgress.Create().
		SetGalleryID(meta.ID).
		SetToken(meta.Token).
		SetCurrentPage(5).
		SetProgress(0.2).
		SetCompleted(false).
		SetCreatedAt(time.Now().UTC()).
		SetUpdatedAt(time.Now().UTC()).
		Save(t.Context())

	time.Sleep(10 * time.Millisecond)

	client.ReadingProgress.Create().
		SetGalleryID(222222).
		SetToken("bbb").
		SetCurrentPage(10).
		SetProgress(0.4).
		SetCompleted(false).
		SetCreatedAt(time.Now().UTC()).
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
	if resp.Results[1].Title != meta.Title {
		t.Errorf("second result Title = %q, want %q", resp.Results[1].Title, meta.Title)
	}
	if resp.Results[1].Thumbnail != meta.Thumbnail {
		t.Errorf("second result Thumbnail = %q, want %q", resp.Results[1].Thumbnail, meta.Thumbnail)
	}
	if resp.Results[1].Pages != meta.PageCount {
		t.Errorf("second result Pages = %d, want %d", resp.Results[1].Pages, meta.PageCount)
	}
}

func TestHandleRecentlyRead_MetadataSurvivesBookshelfRemove(t *testing.T) {
	client := newTestDB(t)

	meta := newTestGallery()
	seedGalleryMeta(t, client, meta)
	client.Bookshelf.Create().
		SetGalleryID(meta.ID).
		SetToken(meta.Token).
		Save(t.Context())

	client.ReadingProgress.Create().
		SetGalleryID(meta.ID).
		SetToken(meta.Token).
		SetCurrentPage(5).
		SetProgress(0.2).
		SetCompleted(false).
		SetCreatedAt(time.Now().UTC()).
		SetUpdatedAt(time.Now().UTC()).
		Save(t.Context())

	client.Bookshelf.Delete().
		Where(
			bookshelf.GalleryID(meta.ID),
			bookshelf.Token(meta.Token),
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
	if resp.Results[0].Title != meta.Title {
		t.Errorf("Title should persist after bookshelf removal, got %q, want %q", resp.Results[0].Title, meta.Title)
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

func TestHandleRecentlyRead_Pagination(t *testing.T) {
	client := newTestDB(t)

	const records = 30
	base := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	for i := range records {
		insertProgressWithTimestamp(t, client, int64(1000+i), fmt.Sprintf("tok-%d", i), base.Add(time.Duration(i)*time.Minute))
	}

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	r.GET("/api/recently-read", server.handleRecentlyRead)

	fetch := func(page string) model.RecentlyReadResponse {
		t.Helper()
		req := httptest.NewRequest("GET", "/api/recently-read?page="+page, nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("page=%s status = %d, want %d", page, w.Code, http.StatusOK)
		}
		var resp model.RecentlyReadResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("page=%s unmarshal: %v", page, err)
		}
		return resp
	}

	first := fetch("0")
	if first.Page != 0 || first.PageSize != 25 || first.Total != 30 || first.TotalPages != 2 {
		t.Errorf("page 0 meta = {page:%d page_size:%d total:%d total_pages:%d}, want {0 25 30 2}",
			first.Page, first.PageSize, first.Total, first.TotalPages)
	}
	if len(first.Results) != 25 {
		t.Fatalf("page 0 results = %d, want 25", len(first.Results))
	}
	if first.Results[0].ID != 1029 {
		t.Errorf("page 0 first ID = %d, want 1029 (newest)", first.Results[0].ID)
	}
	if first.Results[24].ID != 1005 {
		t.Errorf("page 0 last ID = %d, want 1005", first.Results[24].ID)
	}

	second := fetch("1")
	if second.Page != 1 || second.Total != 30 || second.TotalPages != 2 {
		t.Errorf("page 1 meta = {page:%d total:%d total_pages:%d}, want {1 30 2}",
			second.Page, second.Total, second.TotalPages)
	}
	if len(second.Results) != 5 {
		t.Fatalf("page 1 results = %d, want 5", len(second.Results))
	}
	if second.Results[0].ID != 1004 {
		t.Errorf("page 1 first ID = %d, want 1004", second.Results[0].ID)
	}
	if second.Results[4].ID != 1000 {
		t.Errorf("page 1 last ID = %d, want 1000 (oldest)", second.Results[4].ID)
	}

	if n := countReadingProgress(t, client); n != records {
		t.Errorf("records after paginated reads = %d, want %d (no auto-cleanup)", n, records)
	}
}

func TestHandleRecentlyRead_InvalidPage(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "old", time.Now().UTC())

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	server.RegisterRoutes(r)

	req := httptest.NewRequest("GET", "/api/recently-read?page=-1", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}

	var body map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("unmarshal error body: %v", err)
	}
	if body["error"] != "invalid page" {
		t.Errorf("error = %q, want %q", body["error"], "invalid page")
	}
}
