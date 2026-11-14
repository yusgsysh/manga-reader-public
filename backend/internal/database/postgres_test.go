package database

import (
	"os"
	"strconv"
	"testing"
	"time"

	"manga-reader/internal/ident"

	"manga-reader/internal/ent/bookshelf"
)

// TestPostgresCompatibility is opt-in because a PostgreSQL server is not part
// of the default test environment. Point MANGA_READER_TEST_POSTGRES_DSN at a
// disposable database to verify the same schema/migration/query paths that
// SQLite covers by default.
func TestPostgresCompatibility(t *testing.T) {
	dsn := os.Getenv("MANGA_READER_TEST_POSTGRES_DSN")
	if dsn == "" {
		t.Skip("set MANGA_READER_TEST_POSTGRES_DSN to run PostgreSQL compatibility tests")
	}
	db, err := NewDB(Options{Driver: DriverPostgres, DSN: dsn})
	if err != nil {
		t.Fatalf("NewDB postgres: %v", err)
	}
	defer db.Close()
	ctx := t.Context()

	suffix := strconv.FormatInt(time.Now().UnixNano(), 10)
	userID := ident.New()
	if _, err := db.Client.Bookshelf.Create().
		SetUserID(userID).SetGalleryID(1).SetToken("pg-" + suffix).Save(ctx); err != nil {
		t.Fatalf("create bookshelf: %v", err)
	}
	rows, err := db.Client.Bookshelf.Query().
		Where(bookshelf.UserIDEQ(userID), bookshelf.GalleryID(1)).
		All(ctx)
	if err != nil || len(rows) != 1 {
		t.Fatalf("user-scoped query = %+v (err %v), want one row", rows, err)
	}

	// The multi-user migration is idempotent on the PostgreSQL backend too.
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	again, err := NewDB(Options{Driver: DriverPostgres, DSN: dsn})
	if err != nil {
		t.Fatalf("reopen postgres: %v", err)
	}
	defer again.Close()
	if _, err := again.Client.Bookshelf.Query().
		Where(bookshelf.UserIDEQ(userID), bookshelf.GalleryID(1)).All(ctx); err != nil {
		t.Fatalf("query bookshelf after reopen: %v", err)
	}
}
