package database

import (
	"database/sql"
	"path/filepath"
	"testing"
)

// Temporary smoke test: a legacy database that still has the old
// gallery_token column must migrate cleanly (token added, gallery_token
// dropped) without failing startup.
func TestNewDB_MigratesLegacyGalleryTokenColumn(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "legacy.db")

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := conn.Exec(`
		CREATE TABLE prefill_job (
			id INTEGER PRIMARY KEY,
			gallery_id INTEGER,
			gallery_token TEXT NOT NULL DEFAULT '',
			title TEXT NOT NULL DEFAULT '',
			urls JSON NOT NULL,
			status TEXT NOT NULL DEFAULT 'queued',
			total INTEGER NOT NULL DEFAULT 0,
			failed_count INTEGER NOT NULL DEFAULT 0,
			errors JSON,
			created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			finished_at DATETIME
		);
		INSERT INTO prefill_job (gallery_id, gallery_token, title, urls, status, total)
		VALUES (4242, 'tok4242', 'legacy', '[]', 'completed', 3);
	`); err != nil {
		t.Fatalf("create legacy schema: %v", err)
	}
	if err := conn.Close(); err != nil {
		t.Fatalf("close legacy: %v", err)
	}

	db, err := NewDB(Options{Driver: DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("NewDB on legacy database: %v", err)
	}
	defer db.Close()

	var cnt int
	if err := db.Conn.QueryRow(
		`SELECT COUNT(*) FROM pragma_table_info('prefill_job') WHERE name = 'gallery_token'`,
	).Scan(&cnt); err != nil {
		t.Fatalf("check old column: %v", err)
	}
	if cnt != 0 {
		t.Error("gallery_token column still exists after migration")
	}

	rows, err := db.Conn.Query(`SELECT gallery_id, token, title FROM prefill_job`)
	if err != nil {
		t.Fatalf("query migrated table: %v", err)
	}
	defer rows.Close()
	if !rows.Next() {
		t.Fatal("legacy row was lost during migration")
	}
	var galleryID int64
	var token, title string
	if err := rows.Scan(&galleryID, &token, &title); err != nil {
		t.Fatalf("scan: %v", err)
	}
	if galleryID != 4242 || title != "legacy" {
		t.Errorf("row = (%d, %q), want (4242, legacy)", galleryID, title)
	}
	if token != "" {
		t.Errorf("token = %q, want empty (old value intentionally discarded)", token)
	}
}
