package database

import (
	"database/sql"
	"fmt"
	"testing"
	"time"

	"manga-reader/internal/model"

	_ "modernc.org/sqlite"
)

func newTestDB(t *testing.T) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if err := RunMigrations(conn); err != nil {
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func newTestBookshelf() *model.Bookshelf {
	return &model.Bookshelf{
		GalleryID: 123456,
		Token:     "abcdef1234",
		Title:     "Test Gallery",
		TitleJPN:  "テストギャラリー",
		Category:  model.CategoryDoujinshi,
		Thumbnail: "https://example.com/thumb.webp",
		PageCount: 24,
	}
}

func newTestBookshelf2() *model.Bookshelf {
	return &model.Bookshelf{
		GalleryID: 789012,
		Token:     "xyz78901234",
		Title:     "Test Gallery 2",
		TitleJPN:  "テストギャラリー2",
		Category:  model.CategoryManga,
		Thumbnail: "https://example.com/thumb2.webp",
		PageCount: 30,
	}
}

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

	if err := repo.Add(t.Context(), newTestBookshelf()); err != nil {
		t.Fatalf("Add 1: %v", err)
	}
	time.Sleep(10 * time.Millisecond)
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
		b := &model.Bookshelf{
			GalleryID: int64(1000 + i),
			Token:     fmt.Sprintf("tok%d", i),
			Title:     fmt.Sprintf("Gallery %d", i),
			Category:  model.CategoryDoujinshi,
			PageCount: 10 + i,
		}
		if err := repo.Add(t.Context(), b); err != nil {
			t.Fatalf("Add %d: %v", i, err)
		}
	}

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

	resp, err = repo.List(t.Context(), 1, 2)
	if err != nil {
		t.Fatalf("List page 1: %v", err)
	}
	if resp.PageSize != 2 {
		t.Errorf("PageSize = %d, want 2", resp.PageSize)
	}

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

	req := &model.UpdateReadingProgressRequest{
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

	req1 := &model.UpdateReadingProgressRequest{
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

	req2 := &model.UpdateReadingProgressRequest{
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
	if startedAt != nil && p2.StartedAt != nil && !startedAt.Equal(*p2.StartedAt) {
		t.Error("started_at should not change on update")
	}
	if p2.UpdatedAt == nil {
		t.Error("UpdatedAt should be set")
	}
}

func TestReadingProgressRepository_Upsert_Completed(t *testing.T) {
	conn := newTestDB(t)
	repo := NewReadingProgressRepository(conn)

	req := &model.UpdateReadingProgressRequest{
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

	items, err := repo.ListRecentlyRead(t.Context(), 25)
	if err != nil {
		t.Fatalf("ListRecentlyRead: %v", err)
	}
	if len(items) != 0 {
		t.Errorf("expected 0 items, got %d", len(items))
	}

	req1 := &model.UpdateReadingProgressRequest{CurrentPage: 5, Progress: 0.2}
	_, err = repo.Upsert(t.Context(), 111111, "aaa", req1)
	if err != nil {
		t.Fatalf("Upsert 1: %v", err)
	}

	time.Sleep(10 * time.Millisecond)

	req2 := &model.UpdateReadingProgressRequest{CurrentPage: 10, Progress: 0.4}
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

	b := newTestBookshelf()
	if err := bookshelfRepo.Add(t.Context(), b); err != nil {
		t.Fatalf("Add bookshelf: %v", err)
	}

	req := &model.UpdateReadingProgressRequest{CurrentPage: 10, Progress: 0.416}
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
