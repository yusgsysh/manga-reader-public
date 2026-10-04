package handler

import (
	"database/sql"
	"testing"
	"time"

	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	"manga-reader/internal/model"

	"github.com/gin-gonic/gin"

	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"
)

func setupTestRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.RedirectTrailingSlash = false
	return r
}

func newTestDBConn(t *testing.T) (*ent.Client, *sql.DB) {
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
	return client, conn
}

func newTestDB(t *testing.T) *ent.Client {
	client, _ := newTestDBConn(t)
	return client
}

// newTestDatabase returns a database.DB with both the ent client and the raw
// connection, so methods that use raw SQL (e.g. CleanupGalleryCache) can run.
func newTestDatabase(t *testing.T) *database.DB {
	client, conn := newTestDBConn(t)
	return &database.DB{Client: client, Conn: conn}
}

func newTestGallery() *model.Gallery {
	return &model.Gallery{
		ID:        123456,
		Token:     "abcdef1234",
		Title:     "Test Gallery",
		TitleJPN:  "テストギャラリー",
		Category:  model.CategoryDoujinshi,
		Thumbnail: "https://example.com/thumb.webp",
		PageCount: 24,
	}
}

func newTestBookshelf() *model.Bookshelf {
	return &model.Bookshelf{
		GalleryID: 123456,
		Token:     "abcdef1234",
	}
}

func newTestBookshelf2() *model.Bookshelf {
	return &model.Bookshelf{
		GalleryID: 789012,
		Token:     "xyz78901234",
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
		SetCreatedAt(updatedAt).
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
