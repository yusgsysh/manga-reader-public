package database

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/migrate"
)

// Driver identifies a supported database backend.
type Driver string

const (
	DriverSQLite   Driver = "sqlite"
	DriverPostgres Driver = "postgres"
)

// Options configures the database connection. DSN is a SQLite file path when
// Driver is sqlite, or a libpq/pgx connection string when Driver is postgres.
type Options struct {
	Driver Driver
	DSN    string
}

type DB struct {
	Client *ent.Client
	Conn   *sql.DB
}

func NewDB(opts Options) (*DB, error) {
	switch opts.Driver {
	case "", DriverSQLite:
		return newSQLiteDB(opts.DSN)
	case DriverPostgres:
		return newPostgresDB(opts.DSN)
	default:
		return nil, fmt.Errorf("unsupported database driver %q", opts.Driver)
	}
}

func newSQLiteDB(dataSourceName string) (*DB, error) {
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

	return initClient(conn, dialect.SQLite)
}

func newPostgresDB(dsn string) (*DB, error) {
	conn, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, fmt.Errorf("open database: %w", err)
	}
	return initClient(conn, dialect.Postgres)
}

// initClient wires an ent client to an opened *sql.DB and runs migrations.
func initClient(conn *sql.DB, dialectName string) (*DB, error) {
	drv := entsql.OpenDB(dialectName, conn)
	client := ent.NewClient(ent.Driver(drv))

	if err := client.Schema.Create(context.Background(),
		migrate.WithDropIndex(true),
		migrate.WithDropColumn(true),
	); err != nil {
		client.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

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

func (db *DB) Close() error {
	return db.Client.Close()
}
