package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/gallerycache"
	"manga-reader/internal/ent/prefilljob"
	"manga-reader/internal/ent/readingprogress"
)

func tableDDL(t *testing.T, conn *sql.DB, table string) string {
	t.Helper()
	var ddl string
	if err := conn.QueryRow(
		`SELECT sql FROM sqlite_master WHERE type='table' AND name=?`, table,
	).Scan(&ddl); err != nil {
		t.Fatalf("read schema for %s: %v", table, err)
	}
	return ddl
}

func hasColumn(t *testing.T, conn *sql.DB, table, column string) bool {
	t.Helper()
	var n int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info(?) WHERE name=?`, table, column,
	).Scan(&n); err != nil {
		t.Fatalf("read columns for %s: %v", table, err)
	}
	return n > 0
}

func hasIndex(t *testing.T, conn *sql.DB, table, index string) bool {
	t.Helper()
	var n int
	if err := conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_index_list(?) WHERE name=?`, table, index,
	).Scan(&n); err != nil {
		t.Fatalf("read indexes for %s: %v", table, err)
	}
	return n > 0
}

// A fresh database must create the composite primary keys directly, without
// an auto-increment "id" column and without a redundant unique index backing
// the key.
func TestCompositePK_FreshSchema(t *testing.T) {
	db, err := NewDB(Options{Driver: DriverSQLite, DSN: filepath.Join(t.TempDir(), "fresh.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	legacyUniqueIndex := map[string]string{
		"bookshelf":        "bookshelf_gallery_id_token",
		"gallery_cache":    "gallerycache_gallery_id_token",
		"reading_progress": "readingprogress_gallery_id_token",
	}
	for table, index := range legacyUniqueIndex {
		ddl := tableDDL(t, db.Conn, table)
		if !strings.Contains(ddl, "PRIMARY KEY (`gallery_id`, `token`)") {
			t.Errorf("%s DDL missing composite primary key:\n%s", table, ddl)
		}
		if hasColumn(t, db.Conn, table, "id") {
			t.Errorf("%s still has an auto-increment id column:\n%s", table, ddl)
		}
		if hasIndex(t, db.Conn, table, index) {
			t.Errorf("%s still has the unique index that the primary key replaces", table)
		}
	}

	ddl := tableDDL(t, db.Conn, "prefill_job")
	if !strings.Contains(ddl, "PRIMARY KEY (`job_id`)") {
		t.Errorf("prefill_job DDL missing job_id primary key:\n%s", ddl)
	}
	if hasColumn(t, db.Conn, "prefill_job", "id") {
		t.Errorf("prefill_job still has an auto-increment id column:\n%s", ddl)
	}
	if !strings.Contains(ddl, "job_id") || strings.Contains(ddl, "`id`") {
		t.Errorf("prefill_job should key on job_id, not id:\n%s", ddl)
	}
	if !hasIndex(t, db.Conn, "prefill_job", "prefilljob_gallery_id_token") {
		t.Error("prefill_job lost the (gallery_id, token) lookup index")
	}
	if !hasIndex(t, db.Conn, "prefill_job", "prefilljob_status_created_at") {
		t.Error("prefill_job lost the (status, created_at) index")
	}
}

// A database created before the primary keys changed must migrate in place:
// tables are rebuilt with the new keys, existing rows survive, and ent can
// read them back through the new composite predicates.
func TestCompositePK_LegacySchemaMigration(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "legacy.db")

	seed, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	legacyDDL := `
CREATE TABLE "bookshelf" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "gallery_id" bigint NOT NULL,
  "token" text NOT NULL,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "bookshelf_gallery_id_token" ON "bookshelf"("gallery_id", "token");
CREATE TABLE "gallery_cache" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "gallery_id" bigint NOT NULL,
  "token" text NOT NULL,
  "title" text NOT NULL DEFAULT '',
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "gallerycache_gallery_id_token" ON "gallery_cache"("gallery_id", "token");
CREATE TABLE "reading_progress" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "gallery_id" bigint NOT NULL,
  "token" text NOT NULL,
  "current_page" integer NOT NULL DEFAULT 0,
  "progress" real NOT NULL DEFAULT 0,
  "completed" bool NOT NULL DEFAULT false,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "readingprogress_gallery_id_token" ON "reading_progress"("gallery_id", "token");
INSERT INTO bookshelf (gallery_id, token, created_at, updated_at)
  VALUES (111, 'aaa', '2024-01-01 00:00:00', '2024-01-01 00:00:00');
INSERT INTO gallery_cache (gallery_id, token, title, created_at, updated_at)
  VALUES (111, 'aaa', 'Legacy Title', '2024-01-01 00:00:00', '2024-01-01 00:00:00');
INSERT INTO reading_progress (gallery_id, token, current_page, progress, completed, created_at, updated_at)
  VALUES (111, 'aaa', 3, 0.3, false, '2024-01-01 00:00:00', '2024-01-01 00:00:00');
`
	if _, err := seed.Exec(legacyDDL); err != nil {
		t.Fatalf("seed legacy schema: %v", err)
	}
	if err := seed.Close(); err != nil {
		t.Fatalf("close seed: %v", err)
	}

	db, err := NewDB(Options{Driver: DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("NewDB on legacy database: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	for table, index := range map[string]string{
		"bookshelf":        "bookshelf_gallery_id_token",
		"gallery_cache":    "gallerycache_gallery_id_token",
		"reading_progress": "readingprogress_gallery_id_token",
	} {
		ddl := tableDDL(t, db.Conn, table)
		if !strings.Contains(ddl, "PRIMARY KEY (`gallery_id`, `token`)") {
			t.Errorf("%s was not rebuilt with a composite primary key:\n%s", table, ddl)
		}
		if hasColumn(t, db.Conn, table, "id") {
			t.Errorf("%s kept the obsolete id column:\n%s", table, ddl)
		}
		if hasIndex(t, db.Conn, table, index) {
			t.Errorf("%s kept the unique index superseded by the primary key", table)
		}
	}

	shelf, err := db.Client.Bookshelf.Query().
		Where(bookshelf.GalleryID(111), bookshelf.Token("aaa")).
		Only(ctx)
	if err != nil {
		t.Fatalf("read bookshelf row: %v", err)
	}
	if shelf.GalleryID != 111 || shelf.Token != "aaa" {
		t.Errorf("bookshelf row = (%d, %q), want (111, aaa)", shelf.GalleryID, shelf.Token)
	}

	cached, err := db.Client.GalleryCache.Query().
		Where(gallerycache.GalleryID(111), gallerycache.Token("aaa")).
		Only(ctx)
	if err != nil {
		t.Fatalf("read gallery_cache row: %v", err)
	}
	if cached.Title != "Legacy Title" {
		t.Errorf("gallery_cache title = %q, want %q", cached.Title, "Legacy Title")
	}

	prog, err := db.Client.ReadingProgress.Query().
		Where(readingprogress.GalleryID(111), readingprogress.Token("aaa")).
		Only(ctx)
	if err != nil {
		t.Fatalf("read reading_progress row: %v", err)
	}
	if prog.CurrentPage != 3 || prog.Progress != 0.3 {
		t.Errorf("reading_progress = (%d, %v), want (3, 0.3)", prog.CurrentPage, prog.Progress)
	}

	// Rows written after the migration must collide with the legacy row.
	if _, err := db.Client.Bookshelf.Create().
		SetGalleryID(111).SetToken("aaa").
		Save(ctx); err == nil || !ent.IsConstraintError(err) {
		t.Errorf("duplicate bookshelf key: err = %v, want constraint error", err)
	}
}

// The composite key replaces the unique index as the thing that rejects
// duplicates, and prefill jobs deliberately keep a separate identity.
func TestCompositePK_Uniqueness(t *testing.T) {
	db, err := NewDB(Options{Driver: DriverSQLite, DSN: filepath.Join(t.TempDir(), "unique.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if _, err := db.Client.Bookshelf.Create().
		SetGalleryID(1).SetToken("tok").Save(ctx); err != nil {
		t.Fatalf("create bookshelf: %v", err)
	}
	if _, err := db.Client.Bookshelf.Create().
		SetGalleryID(1).SetToken("tok").Save(ctx); err == nil || !ent.IsConstraintError(err) {
		t.Errorf("duplicate (gallery_id, token): err = %v, want constraint error", err)
	}
	if _, err := db.Client.Bookshelf.Create().
		SetGalleryID(1).SetToken("tok2").Save(ctx); err != nil {
		t.Errorf("same gallery with another token must be allowed: %v", err)
	}
	if _, err := db.Client.Bookshelf.Create().
		SetGalleryID(2).SetToken("tok").Save(ctx); err != nil {
		t.Errorf("another gallery with the same token must be allowed: %v", err)
	}

	if _, err := db.Client.ReadingProgress.Create().
		SetGalleryID(7).SetToken("p").Save(ctx); err != nil {
		t.Fatalf("create reading progress: %v", err)
	}
	if _, err := db.Client.ReadingProgress.Create().
		SetGalleryID(7).SetToken("p").Save(ctx); err == nil || !ent.IsConstraintError(err) {
		t.Errorf("duplicate reading progress: err = %v, want constraint error", err)
	}

	// A gallery may hold any number of prefill jobs: (gallery_id, token) is
	// only a lookup key here, never the identity.
	first, err := db.Client.PrefillJob.Create().
		SetGalleryID(7).SetToken("p").SetUrls([]string{"u1"}).Save(ctx)
	if err != nil {
		t.Fatalf("create first prefill job: %v", err)
	}
	second, err := db.Client.PrefillJob.Create().
		SetGalleryID(7).SetToken("p").SetUrls([]string{"u2"}).Save(ctx)
	if err != nil {
		t.Fatalf("create second prefill job: %v", err)
	}
	if first.ID == second.ID {
		t.Errorf("both prefill jobs share id %s, want distinct job ids", first.ID)
	}
	n, err := db.Client.PrefillJob.Query().
		Where(prefilljob.GalleryID(7), prefilljob.Token("p")).
		Count(ctx)
	if err != nil {
		t.Fatalf("count prefill jobs: %v", err)
	}
	if n != 2 {
		t.Errorf("prefill jobs for (7, p) = %d, want 2", n)
	}
}
