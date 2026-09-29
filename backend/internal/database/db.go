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
	return &DB{Client: client}, nil
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
