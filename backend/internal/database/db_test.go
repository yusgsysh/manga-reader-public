package database

import (
	"path/filepath"
	"testing"
)

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
