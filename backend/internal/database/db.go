package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

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

	drv := entsql.OpenDB("sqlite3", conn)
	client := ent.NewClient(ent.Driver(drv))

	if err := client.Schema.Create(context.Background(),
		migrate.WithDropIndex(true),
		migrate.WithDropColumn(true),
	); err != nil {
		client.Close()
		return nil, fmt.Errorf("run migrations: %w", err)
	}

	log.Printf("database initialized: %s", dataSourceName)
	return &DB{Client: client}, nil
}

func (db *DB) Close() error {
	return db.Client.Close()
}
