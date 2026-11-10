package database

import (
	"path/filepath"
	"testing"
)

func TestNewDB_CreatesSchema(t *testing.T) {
	tests := []struct {
		name string
		opts Options
	}{
		{"explicit sqlite", Options{Driver: DriverSQLite, DSN: filepath.Join(t.TempDir(), "fresh.db")}},
		{"default driver", Options{DSN: filepath.Join(t.TempDir(), "default.db")}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db, err := NewDB(tt.opts)
			if err != nil {
				t.Fatalf("NewDB: %v", err)
			}
			defer db.Close()

			if db.Conn == nil || db.Client == nil {
				t.Fatal("expected client and connection to be set")
			}
		})
	}
}

func TestNewDB_UnsupportedDriver(t *testing.T) {
	if _, err := NewDB(Options{Driver: "mysql", DSN: "x"}); err == nil {
		t.Fatal("expected error for unsupported driver")
	}
}
