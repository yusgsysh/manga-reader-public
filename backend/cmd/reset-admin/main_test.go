package main

import (
	"database/sql"
	"strings"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/ent"
)

func newTestClient(t *testing.T) *ent.Client {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("foreign keys: %v", err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", conn)))
	if err := client.Schema.Create(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestResetPassword(t *testing.T) {
	client := newTestClient(t)
	oldHash, err := auth.HashPassword("old-password")
	if err != nil {
		t.Fatalf("old hash: %v", err)
	}
	userRow, err := client.User.Create().
		SetUsername("admin").SetPasswordHash(oldHash).
		SetRole(config.RoleAdmin).SetStatus(config.StatusDisabled).Save(t.Context())
	if err != nil {
		t.Fatalf("user: %v", err)
	}

	if err := resetPassword(t.Context(), client, "admin", "new-password-123"); err != nil {
		t.Fatalf("reset: %v", err)
	}
	updated, err := client.User.Get(t.Context(), userRow.ID)
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if !auth.VerifyPassword(updated.PasswordHash, "new-password-123") {
		t.Fatal("new password does not verify")
	}
	if auth.VerifyPassword(updated.PasswordHash, "old-password") {
		t.Fatal("old password still verifies")
	}
	if updated.Status != config.StatusActive {
		t.Fatalf("status = %q, want active", updated.Status)
	}
	if updated.TokenVersion != 1 {
		t.Fatalf("token_version = %d, want 1", updated.TokenVersion)
	}
}

func TestResetPasswordErrors(t *testing.T) {
	client := newTestClient(t)
	if err := resetPassword(t.Context(), client, "missing", "new-password-123"); err == nil || !strings.Contains(err.Error(), "not found") {
		t.Fatalf("missing user err = %v", err)
	}
}
