package database

import (
	"path/filepath"
	"testing"
)

func TestNewDB_FreshDatabase(t *testing.T) {
	db, err := NewDB(Options{Driver: DriverSQLite, DSN: filepath.Join(t.TempDir(), "fresh.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	if db.Conn == nil || db.Client == nil {
		t.Fatal("expected client and connection to be set")
	}
}

func TestNewDB_DefaultDriverIsSQLite(t *testing.T) {
	db, err := NewDB(Options{DSN: filepath.Join(t.TempDir(), "default.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()

	if db.Conn == nil || db.Client == nil {
		t.Fatal("expected client and connection to be set")
	}
}

func TestNewDB_UnsupportedDriver(t *testing.T) {
	if _, err := NewDB(Options{Driver: "mysql", DSN: "x"}); err == nil {
		t.Fatal("expected error for unsupported driver")
	}
}
