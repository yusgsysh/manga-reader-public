package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/prefilljob"
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

	// GalleryCache is global: its natural (gallery_id, token) key stays a
	// composite primary key shared by the whole instance.
	ddl := tableDDL(t, db.Conn, "gallery_cache")
	if !strings.Contains(ddl, "PRIMARY KEY (`gallery_id`, `token`)") {
		t.Errorf("gallery_cache DDL missing composite primary key:\n%s", ddl)
	}
	if hasIndex(t, db.Conn, "gallery_cache", "gallerycache_gallery_id_token") {
		t.Error("gallery_cache still has the unique index that the primary key replaces")
	}

	// Bookshelf / ReadingProgress are user-owned: they keep a UUID single-column
	// primary key (ent 0.14.6 cannot update a three-column composite key) and a
	// unique (user_id, gallery_id, token) index. The id is a UUID, not an
	// auto-increment counter, so it survives cross-instance migration.
	for table, index := range map[string]string{
		"bookshelf":        "bookshelf_user_id_gallery_id_token",
		"reading_progress": "readingprogress_user_id_gallery_id_token",
	} {
		ddl := tableDDL(t, db.Conn, table)
		if !strings.Contains(ddl, "PRIMARY KEY (`id`)") {
			t.Errorf("%s DDL missing id primary key:\n%s", table, ddl)
		}
		if strings.Contains(ddl, "AUTOINCREMENT") {
			t.Errorf("%s still has an auto-increment id:\n%s", table, ddl)
		}
		if !hasColumn(t, db.Conn, table, "user_id") {
			t.Errorf("%s is missing user_id", table)
		}
		if !hasIndex(t, db.Conn, table, index) {
			t.Errorf("%s missing unique user index %s", table, index)
		}
	}

	ddl = tableDDL(t, db.Conn, "prefill_job")
	if !strings.Contains(ddl, "PRIMARY KEY (`job_id`)") {
		t.Errorf("prefill_job DDL missing job_id primary key:\n%s", ddl)
	}
	if hasColumn(t, db.Conn, "prefill_job", "id") {
		t.Errorf("prefill_job still has an auto-increment id column:\n%s", ddl)
	}
	if !strings.Contains(ddl, "job_id") || strings.Contains(ddl, "`id`") {
		t.Errorf("prefill_job should key on job_id, not id:\n%s", ddl)
	}
	if !hasIndex(t, db.Conn, "prefill_job", "prefilljob_user_id_gallery_id_token") {
		t.Error("prefill_job lost the (gallery_id, token) lookup index")
	}
	if !hasIndex(t, db.Conn, "prefill_job", "prefilljob_user_id_status_created_at") {
		t.Error("prefill_job lost the (status, created_at) index")
	}

	// The tag tables use the tag itself as the natural primary key.
	for table, index := range map[string]string{
		"tag_preference":    "tag_preference_tag_key",
		"tag_sync_baseline": "tag_sync_baseline_tag_key",
	} {
		ddl := tableDDL(t, db.Conn, table)
		if !strings.Contains(ddl, "PRIMARY KEY (`user_id`, `tag`)") {
			t.Errorf("%s DDL missing (user_id, tag) primary key:\n%s", table, ddl)
		}
		if !hasColumn(t, db.Conn, table, "user_id") {
			t.Errorf("%s is missing user_id", table)
		}
		if hasIndex(t, db.Conn, table, index) {
			t.Errorf("%s still has the unique index that the primary key replaces", table)
		}
	}

	// Settings are keyed by their name.
	ddl = tableDDL(t, db.Conn, "sync_state")
	if !strings.Contains(ddl, "PRIMARY KEY (`key`)") {
		t.Errorf("sync_state DDL missing key primary key:\n%s", ddl)
	}
	if hasColumn(t, db.Conn, "sync_state", "id") {
		t.Errorf("sync_state still has an auto-increment id column:\n%s", ddl)
	}
	if hasIndex(t, db.Conn, "sync_state", "sync_state_key_key") {
		t.Error("sync_state still has the unique index that the primary key replaces")
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
