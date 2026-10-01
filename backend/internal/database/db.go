package database

import (
	"context"
	"database/sql"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	_ "modernc.org/sqlite"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/migrate"

	entsql "entgo.io/ent/dialect/sql"
)

type DB struct {
	Client *ent.Client
	Conn   *sql.DB
}

func NewDB(dataSourceName string) (*DB, error) {
	dir := filepath.Dir(dataSourceName)
	if dir != "." && dir != "" {
		if err := os.MkdirAll(dir, 0755); err != nil {
			return nil, fmt.Errorf("create db directory: %w", err)
		}
	}

	// Use DSN parameters to apply pragmas to every connection in the pool.
	// modernc.org/sqlite supports _pragma=... for per-connection settings.
	dsn := dataSourceName
	if strings.Contains(dsn, "?") {
		dsn += "&"
	} else {
		dsn += "?"
	}
	dsn += "_pragma=journal_mode=wal&_pragma=foreign_keys=on&_pragma=busy_timeout=5000" +
		"&_pragma=synchronous=NORMAL"

	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}

	// SQLite handles one writer at a time; limit connections to avoid contention.
	conn.SetMaxOpenConns(1)

	drv := entsql.OpenDB("sqlite3", conn)
	client := ent.NewClient(ent.Driver(drv))

	// Copy reading-progress metadata into gallery_cache before the migration
	// drops those columns from reading_progress.
	if err := backfillGalleryCacheFromProgress(context.Background(), conn); err != nil {
		client.Close()
		return nil, fmt.Errorf("backfill gallery cache: %w", err)
	}

	if err := client.Schema.Create(context.Background(),
		migrate.WithDropIndex(true),
		migrate.WithDropColumn(true),
	); err != nil {
		client.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	// Backfill total for existing prefill_job rows where total=0.
	// Use raw SQL to avoid loading the full urls JSON.
	if err := backfillPrefillTotal(context.Background(), conn); err != nil {
		client.Close()
		return nil, fmt.Errorf("backfill prefill total: %w", err)
	}

	slog.Info("database initialized", "path", dataSourceName)
	return &DB{Client: client, Conn: conn}, nil
}

// CleanupGalleryCache deletes cached gallery metadata/pages that are no longer
// referenced by either the bookshelf or the reading history.
func (db *DB) CleanupGalleryCache(ctx context.Context) (int, error) {
	if db.Conn == nil {
		return 0, nil
	}

	res, err := db.Conn.ExecContext(ctx, `
		DELETE FROM gallery_cache
		WHERE NOT EXISTS (
			SELECT 1 FROM bookshelf b
			WHERE b.gallery_id = gallery_cache.gallery_id
			  AND b.token = gallery_cache.token
		) AND NOT EXISTS (
			SELECT 1 FROM reading_progress r
			WHERE r.gallery_id = gallery_cache.gallery_id
			  AND r.token = gallery_cache.token
		)`)
	if err != nil {
		return 0, fmt.Errorf("cleanup gallery cache: %w", err)
	}
	affected, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("cleanup gallery cache rows affected: %w", err)
	}
	return int(affected), nil
}

func tableExists(ctx context.Context, conn *sql.DB, name string) (bool, error) {
	var count int
	if err := conn.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name,
	).Scan(&count); err != nil {
		return false, err
	}
	return count > 0, nil
}

func tableHasColumn(ctx context.Context, conn *sql.DB, table, column string) (bool, error) {
	rows, err := conn.QueryContext(ctx, fmt.Sprintf("PRAGMA table_info(%s)", table))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	for rows.Next() {
		var (
			cid       int
			name      string
			ctype     string
			notnull   int
			dfltValue sql.NullString
			pk        int
		)
		if err := rows.Scan(&cid, &name, &ctype, &notnull, &dfltValue, &pk); err != nil {
			return false, err
		}
		if name == column {
			return true, nil
		}
	}
	return false, rows.Err()
}

// backfillGalleryCacheFromProgress migrates metadata that used to live on the
// reading_progress row into gallery_cache. It runs before the schema migration
// removes those columns, and is a no-op on fresh databases or after the columns
// are already gone.
func backfillGalleryCacheFromProgress(ctx context.Context, conn *sql.DB) error {
	hasReading, err := tableExists(ctx, conn, "reading_progress")
	if err != nil || !hasReading {
		return err
	}

	hasTitleColumn, err := tableHasColumn(ctx, conn, "reading_progress", "title")
	if err != nil || !hasTitleColumn {
		return err
	}

	hasCache, err := tableExists(ctx, conn, "gallery_cache")
	if err != nil {
		return err
	}
	if !hasCache {
		slog.Warn("gallery_cache missing; skipping reading-progress metadata backfill")
		return nil
	}

	if _, err := conn.ExecContext(ctx, `
		INSERT INTO gallery_cache (
			gallery_id, token, title, title_jpn, category, thumbnail, page_count,
			rating, rating_count, uploader, posted, language, translated, file_size,
			favorited, expunged, tags, pages, meta_fetched_at, updated_at
		)
		SELECT
			rp.gallery_id, rp.token, rp.title, rp.title_jpn, rp.category, rp.thumbnail, rp.page_count,
			0, 0, '', '', '', 0, '', 0, 0, '[]', '[]', rp.updated_at, rp.updated_at
		FROM reading_progress rp
		WHERE (rp.title <> '' OR rp.thumbnail <> '' OR rp.category <> '' OR rp.page_count <> 0)
		  AND NOT EXISTS (
			SELECT 1 FROM gallery_cache gc
			WHERE gc.gallery_id = rp.gallery_id AND gc.token = rp.token
		  )`); err != nil {
		return fmt.Errorf("insert gallery cache from progress: %w", err)
	}

	slog.Info("backfilled gallery cache from reading progress")
	return nil
}

func backfillPrefillTotal(ctx context.Context, conn *sql.DB) error {
	// First check if the prefill_job table exists and has the total column.
	var count int
	if err := conn.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name='prefill_job'`).Scan(&count); err != nil {
		return err
	}
	if count == 0 {
		return nil
	}

	// Use JSON1 extension to get array length of urls column.
	_, err := conn.ExecContext(ctx, `
		UPDATE prefill_job
		SET total = json_array_length(urls)
		WHERE total = 0 AND urls IS NOT NULL
	`)
	return err
}

func (db *DB) Close() error {
	return db.Client.Close()
}
