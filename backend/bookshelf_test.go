package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

// ==================== Test Helpers ====================

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if err := runMigrations(conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func newTestBookshelf() *Bookshelf {
	return &Bookshelf{
		GalleryID: 123456,
		Token:     "abcdef1234",
		Title:     "Test Gallery",
		TitleJPN:  "テストギャラリー",
		Category:  CategoryDoujinshi,
		Thumbnail: "https://example.com/thumb.webp",
		PageCount: 24,
	}
}

func newTestBookshelf2() *Bookshelf {
	return &Bookshelf{
		GalleryID: 789012,
		Token:     "xyz78901234",
		Title:     "Test Gallery 2",
		TitleJPN:  "テストギャラリー2",
		Category:  CategoryManga,
		Thumbnail: "https://example.com/thumb2.webp",
		PageCount: 30,
	}
}

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.RedirectTrailingSlash = false
	return r
}

// ==================== Repository Tests ====================

func TestBookshelfRepository_Add(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)
	b := newTestBookshelf()

	if err := repo.Add(t.Context(), b); err != nil {
		t.Fatalf("Add: %v", err)
	}

	exists, addedAt, err := repo.Exists(t.Context(), b.GalleryID, b.Token)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if !exists {
		t.Fatal("expected bookshelf to exist after Add")
	}
	if addedAt == nil {
		t.Fatal("expected added_at to be set")
	}
}

func TestBookshelfRepository_Add_Idempotent(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)
	b := newTestBookshelf()

	if err := repo.Add(t.Context(), b); err != nil {
		t.Fatalf("first Add: %v", err)
	}
	if err := repo.Add(t.Context(), b); err != nil {
		t.Fatalf("second Add (idempotent): %v", err)
	}

	count, err := repo.Count(t.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 bookshelf item, got %d", count)
	}
}

func TestBookshelfRepository_Remove(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)
	b := newTestBookshelf()

	if err := repo.Add(t.Context(), b); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if err := repo.Remove(t.Context(), b.GalleryID, b.Token); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	exists, _, err := repo.Exists(t.Context(), b.GalleryID, b.Token)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("expected bookshelf to not exist after Remove")
	}
}

func TestBookshelfRepository_Remove_NotExists(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)

	// Removing non-existent should not error
	if err := repo.Remove(t.Context(), 999999, "nonexistent"); err != nil {
		t.Fatalf("Remove non-existent: %v", err)
	}
}

func TestBookshelfRepository_Exists(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)
	b := newTestBookshelf()

	exists, _, err := repo.Exists(t.Context(), b.GalleryID, b.Token)
	if err != nil {
		t.Fatalf("Exists: %v", err)
	}
	if exists {
		t.Fatal("expected not exists before Add")
	}

	if err := repo.Add(t.Context(), b); err != nil {
		t.Fatalf("Add: %v", err)
	}

	exists, addedAt, err := repo.Exists(t.Context(), b.GalleryID, b.Token)
	if err != nil {
		t.Fatalf("Exists after Add: %v", err)
	}
	if !exists {
		t.Fatal("expected exists after Add")
	}
	if addedAt == nil {
		t.Fatal("expected added_at to be non-nil")
	}
}

func TestBookshelfRepository_Get(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)
	b := newTestBookshelf()

	if err := repo.Add(t.Context(), b); err != nil {
		t.Fatalf("Add: %v", err)
	}

	got, err := repo.Get(t.Context(), b.GalleryID, b.Token)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got == nil {
		t.Fatal("expected non-nil result")
	}
	if got.GalleryID != b.GalleryID {
		t.Errorf("GalleryID = %d, want %d", got.GalleryID, b.GalleryID)
	}
	if got.Token != b.Token {
		t.Errorf("Token = %q, want %q", got.Token, b.Token)
	}
	if got.Title != b.Title {
		t.Errorf("Title = %q, want %q", got.Title, b.Title)
	}
	if got.Category != b.Category {
		t.Errorf("Category = %q, want %q", got.Category, b.Category)
	}
	if got.PageCount != b.PageCount {
		t.Errorf("PageCount = %d, want %d", got.PageCount, b.PageCount)
	}
}

func TestBookshelfRepository_Get_NotExists(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)

	got, err := repo.Get(t.Context(), 999999, "nonexistent")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if got != nil {
		t.Fatal("expected nil for non-existent")
	}
}

func TestBookshelfRepository_List(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)

	// Empty list
	resp, err := repo.List(t.Context(), 0, 25)
	if err != nil {
		t.Fatalf("List: %v", err)
	}
	if resp.Total != 0 {
		t.Errorf("Total = %d, want 0", resp.Total)
	}
	if len(resp.Results) != 0 {
		t.Errorf("Results length = %d, want 0", len(resp.Results))
	}

	// Add items
	if err := repo.Add(t.Context(), newTestBookshelf()); err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	time.Sleep(10 * time.Millisecond) // ensure different timestamps
	if err := repo.Add(t.Context(), newTestBookshelf2()); err != nil {
		t.Fatalf("Add 2: %v", err)
	}

	resp, err = repo.List(t.Context(), 0, 25)
	if err != nil {
		t.Fatalf("List after add: %v", err)
	}
	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
	if resp.TotalPages != 1 {
		t.Errorf("TotalPages = %d, want 1", resp.TotalPages)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("Results length = %d, want 2", len(resp.Results))
	}

	// Verify ordering: added_at DESC (second added should be first)
	if resp.Results[0].ID != 789012 {
		t.Errorf("first result ID = %d, want 789012 (most recent)", resp.Results[0].ID)
	}
	if resp.Results[1].ID != 123456 {
		t.Errorf("second result ID = %d, want 123456 (oldest)", resp.Results[1].ID)
	}
}

func TestBookshelfRepository_List_Pagination(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)

	for i := range 5 {
		b := &Bookshelf{
			GalleryID: int64(1000 + i),
			Token:     fmt.Sprintf("tok%d", i),
			Title:     fmt.Sprintf("Gallery %d", i),
			Category:  CategoryDoujinshi,
			PageCount: 10 + i,
		}
		if err := repo.Add(t.Context(), b); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}

	// Page 0, size 2
	resp, err := repo.List(t.Context(), 0, 2)
	if err != nil {
		t.Fatalf("List page 0: %v", err)
	}
	if resp.Page != 0 {
		t.Errorf("Page = %d, want 0", resp.Page)
	}
	if resp.PageSize != 2 {
		t.Errorf("PageSize = %d, want 2", resp.PageSize)
	}
	if resp.Total != 5 {
		t.Errorf("Total = %d, want 5", resp.Total)
	}
	if resp.TotalPages != 3 {
		t.Errorf("TotalPages = %d, want 3", resp.TotalPages)
	}

	// Page 1, size 2
	resp, err = repo.List(t.Context(), 1, 2)
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if resp.PageSize != 2 {
		t.Errorf("PageSize = %d, want 2", resp.PageSize)
	}

	// Page 2, size 2 (only 1 remaining)
	resp, err = repo.List(t.Context(), 2, 2)
	if err != nil {
		t.Fatalf("List page 2: %v", err)
	}
	if resp.PageSize != 1 {
		t.Errorf("PageSize = %d, want 1", resp.PageSize)
	}
}

func TestBookshelfRepository_Count(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)

	count, err := repo.Count(t.Context())
	if err != nil {
		t.Fatalf("Count: %v", err)
	}
	if count != 0 {
		t.Errorf("Count = %d, want 0", count)
	}

	if err := repo.Add(t.Context(), newTestBookshelf()); err != nil {
		t.Fatalf("Add: %v", err)
	}

	count, err = repo.Count(t.Context())
	if err != nil {
		t.Fatalf("Count after add: %v", err)
	}
	if count != 1 {
		t.Errorf("Count = %d, want 1", count)
	}
}

// ==================== Reading Progress Repository Tests ====================

func TestReadingProgressRepository_Get_NotExists(t *testing.T) {
	conn := newTestDB(t)
	repo := NewReadingProgressRepository(conn)

	p, err := repo.Get(t.Context(), 123456, "abcdef")
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	if p == nil {
		t.Fatal("expected non-nil result (zero value)")
	}
	if p.GalleryID != 123456 {
		t.Errorf("GalleryID = %d, want 123456", p.GalleryID)
	}
	if p.CurrentPage != 0 {
		t.Errorf("CurrentPage = %d, want 0", p.CurrentPage)
	}
	if p.Progress != 0 {
		t.Errorf("Progress = %f, want 0", p.Progress)
	}
	if p.Completed {
		t.Error("Completed should be false")
	}
	if p.StartedAt != nil {
		t.Error("StartedAt should be nil")
	}
	if p.UpdatedAt != nil {
		t.Error("UpdatedAt should be nil")
	}
}

func TestReadingProgressRepository_Upsert_Insert(t *testing.T) {
	conn := newTestDB(t)
	repo := NewReadingProgressRepository(conn)

	req := &UpdateReadingProgressRequest{
		CurrentPage: 5,
		Progress:    0.208,
		Completed:   false,
	}

	p, err := repo.Upsert(t.Context(), 123456, "abcdef", req)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if p.CurrentPage != 5 {
		t.Errorf("CurrentPage = %d, want 5", p.CurrentPage)
	}
	if p.Progress != 0.208 {
		t.Errorf("Progress = %f, want 0.208", p.Progress)
	}
	if p.Completed {
		t.Error("Completed should be false")
	}
	if p.StartedAt == nil {
		t.Error("StartedAt should be set on first insert")
	}
	if p.UpdatedAt == nil {
		t.Error("UpdatedAt should be set")
	}
}

func TestReadingProgressRepository_Upsert_Update(t *testing.T) {
	conn := newTestDB(t)
	repo := NewReadingProgressRepository(conn)

	// First insert
	req1 := &UpdateReadingProgressRequest{
		CurrentPage: 5,
		Progress:    0.208,
		Completed:   false,
	}
	p1, err := repo.Upsert(t.Context(), 123456, "abcdef", req1)
	if err != nil {
		t.Fatalf("first Upsert: %v", err)
	}
	startedAt := p1.StartedAt

	time.Sleep(10 * time.Millisecond)

	// Update
	req2 := &UpdateReadingProgressRequest{
		CurrentPage: 10,
		Progress:    0.416,
		Completed:   false,
	}
	p2, err := repo.Upsert(t.Context(), 123456, "abcdef", req2)
	if err != nil {
		t.Fatalf("second Upsert: %v", err)
	}
	if p2.CurrentPage != 10 {
		t.Errorf("CurrentPage = %d, want 10", p2.CurrentPage)
	}
	if p2.Progress != 0.416 {
		t.Errorf("Progress = %f, want 0.416", p2.Progress)
	}
	// started_at should NOT change
	if startedAt != nil && p2.StartedAt != nil && !startedAt.Equal(*p2.StartedAt) {
		t.Error("started_at should not change on update")
	}
	// updated_at should change
	if p2.UpdatedAt == nil {
		t.Error("UpdatedAt should be set")
	}
}

func TestReadingProgressRepository_Upsert_Completed(t *testing.T) {
	conn := newTestDB(t)
	repo := NewReadingProgressRepository(conn)

	req := &UpdateReadingProgressRequest{
		CurrentPage: 24,
		Progress:    0.5,
		Completed:   true,
	}

	p, err := repo.Upsert(t.Context(), 123456, "abcdef", req)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}
	if !p.Completed {
		t.Error("Completed should be true")
	}
	if p.Progress != 1 {
		t.Errorf("Progress = %f, want 1 (auto-set when completed=true)", p.Progress)
	}
}

func TestReadingProgressRepository_ListRecentlyRead(t *testing.T) {
	conn := newTestDB(t)
	repo := NewReadingProgressRepository(conn)

	// Empty
	items, err := repo.ListRecentlyRead(t.Context(), 25)
	if err != nil {
		t.Fatalf("ListRecentlyRead: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}

	// Insert progress for two galleries
	req1 := &UpdateReadingProgressRequest{CurrentPage: 5, Progress: 0.2}
	_, err = repo.Upsert(t.Context(), 111111, "aaa", req1)
	if err != nil {
		t.Fatalf("Upsert 1: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	req2 := &UpdateReadingProgressRequest{CurrentPage: 10, Progress: 0.4}
	_, err = repo.Upsert(t.Context(), 222222, "bbb", req2)
	if err != nil {
		t.Fatalf("Upsert 2: %v", err)
	}

	items, err = repo.ListRecentlyRead(t.Context(), 25)
	if err != nil {
		t.Fatalf("ListRecentlyRead: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	// Most recent first
	if items[0].ID != 222222 {
		t.Errorf("first item ID = %d, want 222222 (most recent)", items[0].ID)
	}
	if items[1].ID != 111111 {
		t.Errorf("second item ID = %d, want 111111 (oldest)", items[1].ID)
	}
}

func TestReadingProgressRepository_ListRecentlyRead_WithBookshelf(t *testing.T) {
	conn := newTestDB(t)
	progressRepo := NewReadingProgressRepository(conn)
	bookshelfRepo := NewBookshelfRepository(conn)

	// Add to bookshelf
	b := newTestBookshelf()
	if err := bookshelfRepo.Add(t.Context(), b); err != nil {
		t.Fatalf("Add bookshelf: %v", err)
	}

	// Add reading progress
	req := &UpdateReadingProgressRequest{CurrentPage: 10, Progress: 0.416}
	_, err := progressRepo.Upsert(t.Context(), b.GalleryID, b.Token, req)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	items, err := progressRepo.ListRecentlyRead(t.Context(), 25)
	if err != nil {
		t.Fatalf("ListRecentlyRead: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item, got %d", len(items))
	}
	if items[0].Title != b.Title {
		t.Errorf("Title = %q, want %q (should come from bookshelf)", items[0].Title, b.Title)
	}
	if items[0].Pages != b.PageCount {
		t.Errorf("Pages = %d, want %d (should come from bookshelf)", items[0].Pages, b.PageCount)
	}
}

func TestReadingProgress_PersistsAfterBookshelfRemove(t *testing.T) {
	conn := newTestDB(t)
	bookshelfRepo := NewBookshelfRepository(conn)
	progressRepo := NewReadingProgressRepository(conn)

	// Add to bookshelf
	b := newTestBookshelf()
	if err := bookshelfRepo.Add(t.Context(), b); err != nil {
		t.Fatalf("Add: %v", err)
	}

	// Add reading progress
	req := &UpdateReadingProgressRequest{CurrentPage: 10, Progress: 0.416}
	_, err := progressRepo.Upsert(t.Context(), b.GalleryID, b.Token, req)
	if err != nil {
		t.Fatalf("Upsert: %v", err)
	}

	// Remove from bookshelf
	if err := bookshelfRepo.Remove(t.Context(), b.GalleryID, b.Token); err != nil {
		t.Fatalf("Remove: %v", err)
	}

	// Reading progress should still exist
	p, err := progressRepo.Get(t.Context(), b.GalleryID, b.Token)
	if err != nil {
		t.Fatalf("Get after remove: %v", err)
	}
	if p == nil {
		t.Fatal("reading progress should persist after bookshelf removal")
	}
	if p.CurrentPage != 10 {
		t.Errorf("CurrentPage = %d, want 10", p.CurrentPage)
	}

	// Recently read should still include it
	items, err := progressRepo.ListRecentlyRead(t.Context(), 25)
	if err != nil {
		t.Fatalf("ListRecentlyRead: %v", err)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 item in recently read, got %d", len(items))
	}
	// Title should be empty since bookshelf was deleted
	if items[0].Title != "" {
		t.Errorf("Title should be empty after bookshelf removal, got %q", items[0].Title)
	}
}

// ==================== Converter Tests ====================

func TestGalleryToBookshelf(t *testing.T) {
	g := &Gallery{
		ID:        123,
		Token:     "tok123",
		Title:     "My Gallery",
		TitleJPN:  "私のギャラリー",
		Category:  CategoryManga,
		Thumbnail: "https://example.com/thumb.webp",
		PageCount: 42,
		Rating:    4.5,
		Tags:      []Tag{{Namespace: "female", Name: "yuri"}},
	}

	b := GalleryToBookshelf(g)
	if b == nil {
		t.Fatal("expected non-nil")
	}
	if b.GalleryID != 123 {
		t.Errorf("GalleryID = %d, want 123", b.GalleryID)
	}
	if b.Token != "tok123" {
		t.Errorf("Token = %q, want tok123", b.Token)
	}
	if b.Title != "My Gallery" {
		t.Errorf("Title = %q, want My Gallery", b.Title)
	}
	if b.PageCount != 42 {
		t.Errorf("PageCount = %d, want 42", b.PageCount)
	}

	// Should NOT contain tags, rating, etc.
	// (Bookshelf doesn't have those fields)

	// nil gallery
	if GalleryToBookshelf(nil) != nil {
		t.Error("expected nil for nil gallery")
	}
}

func TestBookshelfToItem(t *testing.T) {
	b := newTestBookshelf()
	b.AddedAt = time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)
	b.UpdatedAt = time.Date(2026, 8, 29, 10, 0, 0, 0, time.UTC)

	item := BookshelfToItem(b, nil)
	if item.ID != b.GalleryID {
		t.Errorf("ID = %d, want %d", item.ID, b.GalleryID)
	}
	if item.Pages != b.PageCount {
		t.Errorf("Pages = %d, want %d", item.Pages, b.PageCount)
	}
	if item.Reading != nil {
		t.Error("Reading should be nil when no progress")
	}

	// With progress
	p := &ReadingProgress{CurrentPage: 5, Progress: 0.2}
	item = BookshelfToItem(b, p)
	if item.Reading == nil {
		t.Fatal("Reading should be non-nil when progress provided")
	}
	if item.Reading.CurrentPage != 5 {
		t.Errorf("Reading.CurrentPage = %d, want 5", item.Reading.CurrentPage)
	}
}

// ==================== Handler Tests ====================

func TestHandleBookshelfAdd_InvalidID(t *testing.T) {
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.POST("/api/bookshelf/:id/:token", app.handleBookshelfAdd)

	req := httptest.NewRequest("POST", "/api/bookshelf/abc/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleBookshelfAdd_EmptyToken(t *testing.T) {
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.POST("/api/bookshelf/:id/:token", app.handleBookshelfAdd)

	// Request with no token segment — Gin returns 404 (route doesn't match)
	req := httptest.NewRequest("POST", "/api/bookshelf/123456/", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code == http.StatusOK {
		t.Error("expected non-OK status for missing token")
	}
}

func TestHandleBookshelfAdd_Success(t *testing.T) {
	conn := newTestDB(t)

	// Mock ExHentai API
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"gmetadata": []map[string]any{
				{
					"gid":     123456,
					"token":   "abcdef1234",
					"title":   "Test Gallery",
					"title_jpn": "テスト",
					"category": "Doujinshi",
					"thumb":   "https://example.com/thumb.webp",
					"filecount": "24",
					"rating":  "4.50",
					"posted":  "1700000000",
					"tags":    []string{"female:yuri"},
				},
			},
		})
	})
	defer mockServer.Close()

	client := newMockClient(mockServer.URL)
	r := setupTestRouter()
	app := &App{Client: client, DB: &DB{conn: conn}}

	r.POST("/api/bookshelf/:id/:token", app.handleBookshelfAdd)

	req := httptest.NewRequest("POST", "/api/bookshelf/123456/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp BookshelfMutationResponse
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
	conn := newTestDB(t)

	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"gmetadata": []map[string]any{
				{
					"gid":     123456,
					"token":   "abcdef1234",
					"title":   "Test Gallery",
					"title_jpn": "テスト",
					"category": "Doujinshi",
					"thumb":   "https://example.com/thumb.webp",
					"filecount": "24",
					"rating":  "4.50",
					"posted":  "1700000000",
					"tags":    []string{},
				},
			},
		})
	})
	defer mockServer.Close()

	client := newMockClient(mockServer.URL)
	r := setupTestRouter()
	app := &App{Client: client, DB: &DB{conn: conn}}

	r.POST("/api/bookshelf/:id/:token", app.handleBookshelfAdd)

	// First add
	req := httptest.NewRequest("POST", "/api/bookshelf/123456/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("first add: status = %d", w.Code)
	}

	// Second add (idempotent)
	req = httptest.NewRequest("POST", "/api/bookshelf/123456/abcdef1234", nil)
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Errorf("second add: status = %d, want %d", w.Code, http.StatusOK)
	}

	repo := NewBookshelfRepository(conn)
	count, _ := repo.Count(t.Context())
	if count != 1 {
		t.Errorf("Count = %d, want 1 (no duplicate)", count)
	}
}

func TestHandleBookshelfRemove_Success(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)
	repo.Add(t.Context(), newTestBookshelf())

	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.DELETE("/api/bookshelf/:id/:token", app.handleBookshelfRemove)

	req := httptest.NewRequest("DELETE", "/api/bookshelf/123456/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp BookshelfMutationResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Success {
		t.Error("Success should be true")
	}
	if resp.InBookshelf {
		t.Error("InBookshelf should be false after remove")
	}
}

func TestHandleBookshelfRemove_InvalidID(t *testing.T) {
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.DELETE("/api/bookshelf/:id/:token", app.handleBookshelfRemove)

	req := httptest.NewRequest("DELETE", "/api/bookshelf/abc/abcdef1234", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleBookshelfStatus_InBookshelf(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)
	repo.Add(t.Context(), newTestBookshelf())

	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.GET("/api/bookshelf/:id/:token/status", app.handleBookshelfStatus)

	req := httptest.NewRequest("GET", "/api/bookshelf/123456/abcdef1234/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp BookshelfStatus
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.InBookshelf {
		t.Error("InBookshelf should be true")
	}
	if resp.AddedAt == nil {
		t.Error("AddedAt should be set")
	}
}

func TestHandleBookshelfStatus_NotInBookshelf(t *testing.T) {
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.GET("/api/bookshelf/:id/:token/status", app.handleBookshelfStatus)

	req := httptest.NewRequest("GET", "/api/bookshelf/999999/nonexist/status", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp BookshelfStatus
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.InBookshelf {
		t.Error("InBookshelf should be false")
	}
	if resp.AddedAt != nil {
		t.Error("AddedAt should be nil")
	}
}

func TestHandleBookshelfList_Empty(t *testing.T) {
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.GET("/api/bookshelf", app.handleBookshelfList)

	req := httptest.NewRequest("GET", "/api/bookshelf", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp BookshelfListResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 0 {
		t.Errorf("Total = %d, want 0", resp.Total)
	}
	if len(resp.Results) != 0 {
		t.Errorf("Results length = %d, want 0", len(resp.Results))
	}
}

func TestHandleBookshelfList_WithItems(t *testing.T) {
	conn := newTestDB(t)
	repo := NewBookshelfRepository(conn)
	repo.Add(t.Context(), newTestBookshelf())
	repo.Add(t.Context(), newTestBookshelf2())

	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.GET("/api/bookshelf", app.handleBookshelfList)

	req := httptest.NewRequest("GET", "/api/bookshelf", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp BookshelfListResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.Total != 2 {
		t.Errorf("Total = %d, want 2", resp.Total)
	}
	if len(resp.Results) != 2 {
		t.Errorf("Results length = %d, want 2", len(resp.Results))
	}
	// Verify ordering: most recent first
	if resp.Results[0].ID != 789012 {
		t.Errorf("first result ID = %d, want 789012", resp.Results[0].ID)
	}
}

func TestHandleGetProgress_NotExists(t *testing.T) {
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.GET("/api/progress/:id/:token", app.handleGetProgress)

	req := httptest.NewRequest("GET", "/api/progress/123456/abcdef", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.GalleryID != 123456 {
		t.Errorf("GalleryID = %d, want 123456", resp.GalleryID)
	}
	if resp.CurrentPage != 0 {
		t.Errorf("CurrentPage = %d, want 0", resp.CurrentPage)
	}
}

func TestHandleGetProgress_Exists(t *testing.T) {
	conn := newTestDB(t)
	progressRepo := NewReadingProgressRepository(conn)
	req := &UpdateReadingProgressRequest{CurrentPage: 10, Progress: 0.416}
	progressRepo.Upsert(t.Context(), 123456, "abcdef", req)

	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.GET("/api/progress/:id/:token", app.handleGetProgress)

	httpReq := httptest.NewRequest("GET", "/api/progress/123456/abcdef", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.CurrentPage != 10 {
		t.Errorf("CurrentPage = %d, want 10", resp.CurrentPage)
	}
	if resp.Progress != 0.416 {
		t.Errorf("Progress = %f, want 0.416", resp.Progress)
	}
}

func TestHandleUpdateProgress_First(t *testing.T) {
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.PUT("/api/progress/:id/:token", app.handleUpdateProgress)

	body, _ := json.Marshal(UpdateReadingProgressRequest{
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

	var resp ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.CurrentPage != 5 {
		t.Errorf("CurrentPage = %d, want 5", resp.CurrentPage)
	}
	if resp.StartedAt == nil {
		t.Error("StartedAt should be set on first insert")
	}
}

func TestHandleUpdateProgress_Update(t *testing.T) {
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.PUT("/api/progress/:id/:token", app.handleUpdateProgress)

	// First update
	body, _ := json.Marshal(UpdateReadingProgressRequest{CurrentPage: 5, Progress: 0.2})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var first ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &first)

	time.Sleep(10 * time.Millisecond)

	// Second update
	body, _ = json.Marshal(UpdateReadingProgressRequest{CurrentPage: 10, Progress: 0.4})
	req = httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var second ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &second)

	if second.CurrentPage != 10 {
		t.Errorf("CurrentPage = %d, want 10", second.CurrentPage)
	}
	if first.StartedAt != nil && second.StartedAt != nil && !first.StartedAt.Equal(*second.StartedAt) {
		t.Error("started_at should not change")
	}
}

func TestHandleUpdateProgress_Completed(t *testing.T) {
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.PUT("/api/progress/:id/:token", app.handleUpdateProgress)

	body, _ := json.Marshal(UpdateReadingProgressRequest{
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

	var resp ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Completed {
		t.Error("Completed should be true")
	}
	if resp.Progress != 1 {
		t.Errorf("Progress = %f, want 1 (auto-set when completed=true)", resp.Progress)
	}
}

func TestHandleUpdateProgress_InvalidProgress(t *testing.T) {
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.PUT("/api/progress/:id/:token", app.handleUpdateProgress)

	body, _ := json.Marshal(UpdateReadingProgressRequest{
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
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.PUT("/api/progress/:id/:token", app.handleUpdateProgress)

	body, _ := json.Marshal(UpdateReadingProgressRequest{
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
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.PUT("/api/progress/:id/:token", app.handleUpdateProgress)

	body, _ := json.Marshal(UpdateReadingProgressRequest{
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
	conn := newTestDB(t)
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.GET("/api/recently-read", app.handleRecentlyRead)

	req := httptest.NewRequest("GET", "/api/recently-read", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp RecentlyReadResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Results) != 0 {
		t.Errorf("Results length = %d, want 0", len(resp.Results))
	}
}

func TestHandleRecentlyRead_WithRecords(t *testing.T) {
	conn := newTestDB(t)
	progressRepo := NewReadingProgressRepository(conn)
	bookshelfRepo := NewBookshelfRepository(conn)

	// Add bookshelf + progress for gallery 1
	b1 := newTestBookshelf()
	bookshelfRepo.Add(t.Context(), b1)
	progressRepo.Upsert(t.Context(), b1.GalleryID, b1.Token, &UpdateReadingProgressRequest{CurrentPage: 5, Progress: 0.2})

	time.Sleep(10 * time.Millisecond)

	// Add progress only for gallery 2 (no bookshelf)
	progressRepo.Upsert(t.Context(), 222222, "bbb", &UpdateReadingProgressRequest{CurrentPage: 10, Progress: 0.4})

	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.GET("/api/recently-read", app.handleRecentlyRead)

	req := httptest.NewRequest("GET", "/api/recently-read", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp RecentlyReadResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	if len(resp.Results) != 2 {
		t.Fatalf("Results length = %d, want 2", len(resp.Results))
	}
	// Most recent first (gallery 2)
	if resp.Results[0].ID != 222222 {
		t.Errorf("first result ID = %d, want 222222", resp.Results[0].ID)
	}
	// Gallery 1 should have title from bookshelf
	if resp.Results[1].Title != b1.Title {
		t.Errorf("second result Title = %q, want %q", resp.Results[1].Title, b1.Title)
	}
}

func TestHandleRecentlyRead_AfterBookshelfRemove(t *testing.T) {
	conn := newTestDB(t)
	progressRepo := NewReadingProgressRepository(conn)
	bookshelfRepo := NewBookshelfRepository(conn)

	b := newTestBookshelf()
	bookshelfRepo.Add(t.Context(), b)
	progressRepo.Upsert(t.Context(), b.GalleryID, b.Token, &UpdateReadingProgressRequest{CurrentPage: 5, Progress: 0.2})

	// Remove from bookshelf
	bookshelfRepo.Remove(t.Context(), b.GalleryID, b.Token)

	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}

	r.GET("/api/recently-read", app.handleRecentlyRead)

	req := httptest.NewRequest("GET", "/api/recently-read", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp RecentlyReadResponse
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
