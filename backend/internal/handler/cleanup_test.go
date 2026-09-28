package handler

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/readingprogress"
	"manga-reader/internal/model"

	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.RedirectTrailingSlash = false
	return r
}

func newTestDB(t *testing.T) *ent.Client {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	// A plain :memory: database is private to its connection; if the pool
	// opened a second connection it would see an empty database. One
	// connection keeps every query (including the ones made by background
	// goroutines) on the same database.
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := conn.Exec("PRAGMA busy_timeout=5000"); err != nil {
		t.Fatalf("set busy timeout: %v", err)
	}
	drv := entsql.OpenDB("sqlite3", conn)
	client := ent.NewClient(ent.Driver(drv))
	if err := client.Schema.Create(t.Context()); err != nil {
		client.Close()
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
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

// insertProgressWithTimestamp inserts a reading progress record with a custom updated_at timestamp for cleanup boundary testing.
func insertProgressWithTimestamp(t *testing.T, client *ent.Client, galleryID int64, token string, updatedAt time.Time) {
	t.Helper()
	_, err := client.ReadingProgress.Create().
		SetGalleryID(galleryID).
		SetToken(token).
		SetCurrentPage(1).
		SetProgress(0.1).
		SetCompleted(false).
		SetStartedAt(updatedAt).
		SetUpdatedAt(updatedAt).
		Save(t.Context())
	if err != nil {
		t.Fatalf("insert progress with timestamp: %v", err)
	}
}

func countReadingProgress(t *testing.T, client *ent.Client) int {
	t.Helper()
	n, err := client.ReadingProgress.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count reading progress: %v", err)
	}
	return n
}

type cleanupResponse struct {
	Days    int   `json:"days"`
	Deleted int64 `json:"deleted"`
}

func performCleanup(t *testing.T, client *ent.Client, query string) (int, cleanupResponse) {
	t.Helper()
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	server.RegisterRoutes(r)

	req := httptest.NewRequest("POST", "/api/reading-progress/cleanup"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp cleanupResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal cleanup response: %v", err)
		}
	}
	return w.Code, resp
}

// ==================== days parameter tests ====================

func TestHandleReadingProgressCleanup_DefaultDays(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "old", time.Now().UTC().AddDate(0, 0, -31))

	code, resp := performCleanup(t, client, "")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 30 {
		t.Errorf("days = %d, want 30 (default)", resp.Days)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", resp.Deleted)
	}
	if n := countReadingProgress(t, client); n != 0 {
		t.Errorf("remaining records = %d, want 0", n)
	}
}

func TestHandleReadingProgressCleanup_Days7(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "six", time.Now().UTC().AddDate(0, 0, -6))
	insertProgressWithTimestamp(t, client, 222222, "eight", time.Now().UTC().AddDate(0, 0, -8))

	code, resp := performCleanup(t, client, "?days=7")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 7 {
		t.Errorf("days = %d, want 7", resp.Days)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (only the 8-day-old record)", resp.Deleted)
	}
	if n := countReadingProgress(t, client); n != 1 {
		t.Errorf("remaining records = %d, want 1", n)
	}
}

func TestHandleReadingProgressCleanup_Days30(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "twenty-nine", time.Now().UTC().AddDate(0, 0, -29))
	insertProgressWithTimestamp(t, client, 222222, "thirty-one", time.Now().UTC().AddDate(0, 0, -31))

	code, resp := performCleanup(t, client, "?days=30")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 30 {
		t.Errorf("days = %d, want 30", resp.Days)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (only the 31-day-old record)", resp.Deleted)
	}
	if n := countReadingProgress(t, client); n != 1 {
		t.Errorf("remaining records = %d, want 1 (29-day-old kept)", n)
	}
}

func TestHandleReadingProgressCleanup_Days0(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "fresh", time.Now().UTC())
	insertProgressWithTimestamp(t, client, 222222, "old", time.Now().UTC().AddDate(0, 0, -60))

	code, resp := performCleanup(t, client, "?days=0")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 0 {
		t.Errorf("days = %d, want 0", resp.Days)
	}
	if resp.Deleted != 2 {
		t.Errorf("deleted = %d, want 2 (all records)", resp.Deleted)
	}
	if n := countReadingProgress(t, client); n != 0 {
		t.Errorf("remaining records = %d, want 0", n)
	}
}

func TestHandleReadingProgressCleanup_NoRecords(t *testing.T) {
	client := newTestDB(t)

	code, resp := performCleanup(t, client, "")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Deleted != 0 {
		t.Errorf("deleted = %d, want 0", resp.Deleted)
	}
}

// ==================== invalid parameter tests ====================

func TestHandleReadingProgressCleanup_NegativeDays(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "keep", time.Now().UTC().AddDate(0, 0, -31))

	code, _ := performCleanup(t, client, "?days=-1")

	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", code, http.StatusBadRequest)
	}
	if n := countReadingProgress(t, client); n != 1 {
		t.Errorf("records after failed cleanup = %d, want 1 (nothing deleted)", n)
	}
}

func TestHandleReadingProgressCleanup_NonNumericDays(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "keep", time.Now().UTC().AddDate(0, 0, -31))

	code, _ := performCleanup(t, client, "?days=abc")

	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", code, http.StatusBadRequest)
	}
	if n := countReadingProgress(t, client); n != 1 {
		t.Errorf("records after failed cleanup = %d, want 1 (nothing deleted)", n)
	}
}

// ==================== regression tests ====================

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

func TestHandleReadingProgressCleanup_PreservesBookshelf(t *testing.T) {
	client := newTestDB(t)
	b := newTestBookshelf()
	_, err := client.Bookshelf.Create().
		SetGalleryID(b.GalleryID).
		SetToken(b.Token).
		SetTitle(b.Title).
		SetTitleJpn(string(b.TitleJPN)).
		SetCategory(string(b.Category)).
		SetThumbnail(b.Thumbnail).
		SetPageCount(b.PageCount).
		Save(t.Context())
	if err != nil {
		t.Fatalf("add bookshelf: %v", err)
	}
	insertProgressWithTimestamp(t, client, b.GalleryID, b.Token, time.Now().UTC().AddDate(0, 0, -31))

	code, resp := performCleanup(t, client, "?days=30")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", resp.Deleted)
	}

	// Reading progress should be deleted
	exists, err := client.ReadingProgress.Query().
		Where(
			readingprogress.GalleryID(b.GalleryID),
			readingprogress.Token(b.Token),
		).
		Exist(t.Context())
	if err != nil {
		t.Fatalf("check progress: %v", err)
	}
	if exists {
		t.Error("reading progress should be deleted after cleanup")
	}

	// Bookshelf must be preserved
	exists, err = client.Bookshelf.Query().
		Where(
			bookshelf.GalleryID(b.GalleryID),
			bookshelf.Token(b.Token),
		).
		Exist(t.Context())
	if err != nil {
		t.Fatalf("check bookshelf: %v", err)
	}
	if !exists {
		t.Error("bookshelf should be preserved after cleanup")
	}
}
