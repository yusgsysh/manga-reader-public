package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	_ "modernc.org/sqlite"
)

func openRawDB(t *testing.T, path string) *sql.DB {
	t.Helper()
	conn, err := sql.Open("sqlite", path)
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	t.Cleanup(func() { conn.Close() })
	return conn
}

func TestBackfillGalleryCacheFromProgress(t *testing.T) {
	ctx := context.Background()
	conn := openRawDB(t, filepath.Join(t.TempDir(), "old.db"))

	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE reading_progress (
			id INTEGER PRIMARY KEY,
			gallery_id INTEGER,
			token TEXT,
			current_page INTEGER,
			progress REAL,
			completed BOOLEAN,
			title TEXT,
			title_jpn TEXT,
			category TEXT,
			thumbnail TEXT,
			page_count INTEGER,
			started_at DATETIME,
			updated_at DATETIME
		)`); err != nil {
		t.Fatalf("create reading_progress: %v", err)
	}
	if _, err := conn.ExecContext(ctx, `
		CREATE TABLE gallery_cache (
			gallery_id INTEGER,
			token TEXT,
			title TEXT DEFAULT '',
			title_jpn TEXT DEFAULT '',
			category TEXT DEFAULT '',
			thumbnail TEXT DEFAULT '',
			page_count INTEGER DEFAULT 0,
			rating REAL DEFAULT 0,
			rating_count INTEGER DEFAULT 0,
			uploader TEXT DEFAULT '',
			posted TEXT DEFAULT '',
			posted_at DATETIME,
			language TEXT DEFAULT '',
			translated BOOLEAN DEFAULT false,
			file_size TEXT DEFAULT '',
			favorited INTEGER DEFAULT 0,
			expunged BOOLEAN DEFAULT false,
			tags TEXT,
			pages TEXT,
			meta_fetched_at DATETIME,
			pages_fetched_at DATETIME,
			updated_at DATETIME
		)`); err != nil {
		t.Fatalf("create gallery_cache: %v", err)
	}

	if _, err := conn.ExecContext(ctx, `
		INSERT INTO reading_progress
			(gallery_id, token, current_page, progress, completed, title, title_jpn, category, thumbnail, page_count, updated_at)
		VALUES
			(123456, 'abcdef', 5, 0.2, false, 'Test Gallery', 'テスト', 'doujinshi', 'https://example.com/t.webp', 24, '2026-01-01T00:00:00Z'),
			(999999, 'empty', 1, 0.1, false, '', '', '', '', 0, '2026-01-01T00:00:00Z')`); err != nil {
		t.Fatalf("insert reading_progress: %v", err)
	}

	if err := backfillGalleryCacheFromProgress(ctx, conn); err != nil {
		t.Fatalf("backfill: %v", err)
	}

	var title, thumbnail string
	var pageCount int
	if err := conn.QueryRowContext(ctx, `
		SELECT title, thumbnail, page_count FROM gallery_cache
		WHERE gallery_id=123456 AND token='abcdef'`).Scan(&title, &thumbnail, &pageCount); err != nil {
		t.Fatalf("query backfilled row: %v", err)
	}
	if title != "Test Gallery" {
		t.Errorf("title = %q, want Test Gallery", title)
	}
	if thumbnail != "https://example.com/t.webp" {
		t.Errorf("thumbnail = %q", thumbnail)
	}
	if pageCount != 24 {
		t.Errorf("page_count = %d, want 24", pageCount)
	}

	// Empty metadata rows are skipped.
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM gallery_cache WHERE gallery_id=999999`).Scan(&count); err != nil {
		t.Fatalf("count empty rows: %v", err)
	}
	if count != 0 {
		t.Errorf("empty metadata should not be backfilled, got %d rows", count)
	}

	// Idempotent: a second run must not duplicate.
	if err := backfillGalleryCacheFromProgress(ctx, conn); err != nil {
		t.Fatalf("second backfill: %v", err)
	}
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM gallery_cache`).Scan(&count); err != nil {
		t.Fatalf("count all: %v", err)
	}
	if count != 1 {
		t.Errorf("gallery_cache rows = %d, want 1 (idempotent)", count)
	}
}

func TestNewDB_FreshDatabase(t *testing.T) {
	db, err := NewDB(filepath.Join(t.TempDir(), "fresh.db"))
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	if db.Conn == nil || db.Client == nil {
		t.Fatal("expected client and connection to be set")
	}
}
