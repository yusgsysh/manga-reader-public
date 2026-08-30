package database

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	_ "modernc.org/sqlite"
)

type DB struct {
	Conn *sql.DB
}

func NewDB(dataSourceName string) (*DB, error) {
	dir := filepath.Dir(dataSourceName)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}

	conn, err := sql.Open("sqlite", dataSourceName)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	if _, err := conn.Exec("PRAGMA journal_mode=WAL"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("set WAL mode: %w", err)
	}

	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		conn.Close()
		return nil, fmt.Errorf("enable foreign keys: %w", err)
	}

	if err := RunMigrations(conn); err != nil {
		conn.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	log.Printf("database initialized: %s", dataSourceName)
	return &DB{Conn: conn}, nil
}

func (db *DB) Close() error {
	return db.Conn.Close()
}

func RunMigrations(conn *sql.DB) error {
	migrations := []string{
		`CREATE TABLE IF NOT EXISTS bookshelf (
			gallery_id INTEGER NOT NULL,
			token TEXT NOT NULL,
			title TEXT NOT NULL DEFAULT '',
			title_jpn TEXT NOT NULL DEFAULT '',
			category TEXT NOT NULL DEFAULT '',
			thumbnail TEXT NOT NULL DEFAULT '',
			page_count INTEGER NOT NULL DEFAULT 0,
			added_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
			PRIMARY KEY (gallery_id, token)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_bookshelf_added_at ON bookshelf (added_at DESC)`,
		`CREATE TABLE IF NOT EXISTS reading_progress (
			gallery_id INTEGER NOT NULL,
			token TEXT NOT NULL,
			current_page INTEGER NOT NULL DEFAULT 0,
			progress REAL NOT NULL DEFAULT 0,
			completed INTEGER NOT NULL DEFAULT 0,
			started_at DATETIME,
			updated_at DATETIME,
			PRIMARY KEY (gallery_id, token),
			CHECK (current_page >= 0),
			CHECK (progress >= 0 AND progress <= 1)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_reading_progress_updated_at ON reading_progress (updated_at DESC)`,
	}

	for _, m := range migrations {
		if _, err := conn.Exec(m); err != nil {
			return fmt.Errorf("migration failed: %s: %w", truncateSQL(m), err)
		}
	}
	return nil
}

func truncateSQL(s string) string {
	if len(s) > 80 {
		return s[:80] + "..."
	}
	return s
}
