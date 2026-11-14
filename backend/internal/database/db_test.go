package database

import (
	"path/filepath"
	"testing"

	"manga-reader/internal/ident"

	"manga-reader/internal/ent/bookshelf"
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

func TestEntTransactionRollbackAndCommit(t *testing.T) {
	db, err := NewDB(Options{Driver: DriverSQLite, DSN: filepath.Join(t.TempDir(), "tx.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := t.Context()

	tx, err := db.Client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := tx.Bookshelf.Create().SetUserID(ident.Nil()).SetGalleryID(1).SetToken("rolled-back").Save(ctx); err != nil {
		t.Fatalf("create in tx: %v", err)
	}
	if err := tx.Rollback(); err != nil {
		t.Fatalf("rollback: %v", err)
	}
	if n, err := db.Client.Bookshelf.Query().Count(ctx); err != nil || n != 0 {
		t.Fatalf("rows after rollback = %d (err %v), want 0", n, err)
	}

	tx, err = db.Client.Tx(ctx)
	if err != nil {
		t.Fatalf("begin tx: %v", err)
	}
	if _, err := tx.Bookshelf.Create().SetUserID(ident.Nil()).SetGalleryID(2).SetToken("committed").Save(ctx); err != nil {
		t.Fatalf("create in tx: %v", err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatalf("commit: %v", err)
	}
	if n, err := db.Client.Bookshelf.Query().Count(ctx); err != nil || n != 1 {
		t.Fatalf("rows after commit = %d (err %v), want 1", n, err)
	}
}

func TestEntUserScopedQueryIsolation(t *testing.T) {
	db, err := NewDB(Options{Driver: DriverSQLite, DSN: filepath.Join(t.TempDir(), "isolation.db")})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := t.Context()

	users := []ident.UUID{testUUID(1), testUUID(2)}
	for _, userID := range users {
		if _, err := db.Client.Bookshelf.Create().
			SetUserID(userID).SetGalleryID(42).SetToken("same").Save(ctx); err != nil {
			t.Fatalf("seed user %v: %v", userID, err)
		}
	}
	rows, err := db.Client.Bookshelf.Query().
		Where(bookshelf.UserIDEQ(users[0]), bookshelf.GalleryID(42)).
		All(ctx)
	if err != nil || len(rows) != 1 || rows[0].UserID != users[0] {
		t.Fatalf("user 1 query = %+v (err %v), want only its row", rows, err)
	}
	// The same natural key is allowed once per user, not globally.
	if _, err := db.Client.Bookshelf.Create().
		SetUserID(users[0]).SetGalleryID(42).SetToken("same").Save(ctx); err == nil {
		t.Fatal("duplicate key within one user was accepted")
	}
}

// testUUID returns a deterministic UUID whose last byte is n.
func testUUID(n byte) ident.UUID {
	var id ident.UUID
	id[15] = n
	return id
}
