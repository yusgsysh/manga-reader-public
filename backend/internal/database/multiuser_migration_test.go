package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"manga-reader/internal/ident"

	"manga-reader/internal/config"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/readingprogress"
	"manga-reader/internal/ent/tagpreference"

	_ "modernc.org/sqlite"
)

// TestMultiTenantToMultiUserMigration verifies the ownership policy: each old
// tenant's rows move to that tenant's oldest administrator (or oldest user),
// duplicate business keys across tenants survive because they now belong to
// different users, and a tenant without accounts leaves its rows unowned
// instead of guessing an owner.
func TestMultiTenantToMultiUserMigration(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "multitenant.db")
	seed, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open seed: %v", err)
	}
	ddl := `
CREATE TABLE "tenant" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "name" text NOT NULL,
  "slug" text NOT NULL,
  "status" text NOT NULL DEFAULT 'active',
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "tenant_slug_key" ON "tenant"("slug");
INSERT INTO tenant (id, name, slug, status, created_at, updated_at) VALUES
 (1, 'A', 'a', 'active', '2024-01-01 00:00:00', '2024-01-01 00:00:00'),
 (2, 'B', 'b', 'active', '2024-01-01 00:00:00', '2024-01-01 00:00:00'),
 (3, 'C', 'c', 'active', '2024-01-01 00:00:00', '2024-01-01 00:00:00');

CREATE TABLE "users" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "tenant_id" bigint NOT NULL DEFAULT 1,
  "username" text NOT NULL,
  "password_hash" text NOT NULL,
  "role" text NOT NULL DEFAULT 'user',
  "status" text NOT NULL DEFAULT 'active',
  "token_version" integer NOT NULL DEFAULT 0,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "user_tenant_id_username" ON "users"("tenant_id", "username");
INSERT INTO users (id, tenant_id, username, password_hash, role, status, created_at, updated_at) VALUES
 (1, 1, 'admin-a', '$argon2id$a', 'platform_admin', 'active', '2024-01-01 00:00:00', '2024-01-01 00:00:00'),
 (2, 1, 'member-a', '$argon2id$m', 'user', 'active', '2024-01-02 00:00:00', '2024-01-02 00:00:00'),
 (3, 2, 'admin-b', '$argon2id$b', 'tenant_admin', 'active', '2024-01-01 00:00:00', '2024-01-01 00:00:00'),
 (4, 2, 'member-b', '$argon2id$n', 'user', 'active', '2024-01-03 00:00:00', '2024-01-03 00:00:00');

CREATE TABLE "bookshelf" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "tenant_id" bigint NOT NULL DEFAULT 1,
  "gallery_id" bigint NOT NULL,
  "token" text NOT NULL,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "bookshelf_tenant_id_gallery_id_token" ON "bookshelf"("tenant_id", "gallery_id", "token");
INSERT INTO bookshelf (tenant_id, gallery_id, token, created_at, updated_at) VALUES
 (1, 42, 'same', '2024-01-01 00:00:00', '2024-01-01 00:00:00'),
 (2, 42, 'same', '2024-01-01 00:00:00', '2024-01-01 00:00:00'),
 (3, 43, 'orphan', '2024-01-01 00:00:00', '2024-01-01 00:00:00');

CREATE TABLE "reading_progress" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "tenant_id" bigint NOT NULL DEFAULT 1,
  "gallery_id" bigint NOT NULL,
  "token" text NOT NULL,
  "current_page" integer NOT NULL DEFAULT 0,
  "progress" real NOT NULL DEFAULT 0,
  "completed" bool NOT NULL DEFAULT false,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "readingprogress_tenant_id_gallery_id_token" ON "reading_progress"("tenant_id", "gallery_id", "token");
INSERT INTO reading_progress (tenant_id, gallery_id, token, current_page, progress, completed, created_at, updated_at) VALUES
 (1, 42, 'same', 4, 0.4, false, '2024-01-01 00:00:00', '2024-01-01 00:00:00'),
 (2, 42, 'same', 8, 0.8, false, '2024-01-01 00:00:00', '2024-01-01 00:00:00');

CREATE TABLE "tag_preference" (
  "tenant_id" bigint NOT NULL DEFAULT 1,
  "tag" text NOT NULL,
  "action" text NOT NULL,
  "weight" integer NOT NULL DEFAULT 10,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("tenant_id", "tag")
);
INSERT INTO tag_preference (tenant_id, tag, action, weight, created_at, updated_at) VALUES
 (1, 'female:yuri', 'like', 10, '2024-01-01 00:00:00', '2024-01-01 00:00:00'),
 (2, 'female:yuri', 'dislike', -10, '2024-01-01 00:00:00', '2024-01-01 00:00:00');

CREATE TABLE "prefill_job" (
  "job_id" text NOT NULL PRIMARY KEY,
  "tenant_id" bigint NOT NULL DEFAULT 1,
  "gallery_id" integer NULL,
  "token" text NOT NULL DEFAULT '',
  "title" text NOT NULL DEFAULT '',
  "urls" json NOT NULL,
  "status" text NOT NULL DEFAULT 'queued',
  "total" integer NOT NULL DEFAULT 0,
  "failed_count" integer NOT NULL DEFAULT 0,
  "errors" json NULL,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  "finished_at" datetime NULL
);
INSERT INTO prefill_job (job_id, tenant_id, gallery_id, token, title, urls, status, total, created_at, updated_at) VALUES
 ('11111111-1111-1111-1111-111111111111', 2, 42, 'same', 'B job', '[]', 'completed', 0, '2024-01-01 00:00:00', '2024-01-01 00:00:00');

CREATE TABLE "sync_change" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "tenant_id" bigint NOT NULL DEFAULT 1,
  "entity" text NOT NULL,
  "gallery_id" bigint NOT NULL,
  "token" text NOT NULL,
  "op" text NOT NULL DEFAULT 'upsert',
  "changed_at" datetime NOT NULL
);
INSERT INTO sync_change (tenant_id, entity, gallery_id, token, op, changed_at) VALUES
 (2, 'reading_progress', 42, 'same', 'upsert', '2024-01-01 00:00:00');

CREATE TABLE "node" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "name" text NOT NULL,
  "tenant_id" bigint NOT NULL,
  "status" text NOT NULL DEFAULT 'active',
  "last_seen_at" datetime NULL,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "node_tenant_id_name" ON "node"("tenant_id", "name");
INSERT INTO node (name, tenant_id, status, created_at, updated_at) VALUES
 ('peer', 1, 'active', '2024-01-01 00:00:00', '2024-01-01 00:00:00'),
 ('peer', 2, 'active', '2024-01-01 00:00:00', '2024-01-01 00:00:00');
`
	if _, err := seed.Exec(ddl); err != nil {
		t.Fatalf("seed: %v", err)
	}
	seed.Close()

	db, err := NewDB(Options{Driver: DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	if ok, _ := tableExistsRaw(ctx, db.Conn, "tenant"); ok {
		t.Fatal("legacy tenant table survived")
	}
	if hasColumn(t, db.Conn, "users", "tenant_id") {
		t.Fatal("users.tenant_id survived")
	}
	for _, table := range []string{"bookshelf", "reading_progress", "prefill_job", "tag_preference", "sync_change", "node"} {
		if hasColumn(t, db.Conn, table, "tenant_id") {
			t.Errorf("%s.tenant_id survived", table)
		}
	}

	roles := map[string]string{}
	userIDs := map[string]ident.UUID{}
	rows, err := db.Client.User.Query().All(ctx)
	if err != nil || len(rows) != 4 {
		t.Fatalf("users = %d (err %v), want 4", len(rows), err)
	}
	for _, row := range rows {
		roles[row.Username] = row.Role
		userIDs[row.Username] = row.ID
	}
	if roles["admin-a"] != config.RoleAdmin || roles["admin-b"] != config.RoleAdmin ||
		roles["member-a"] != config.RoleUser || roles["member-b"] != config.RoleUser {
		t.Fatalf("roles after migration = %+v", roles)
	}

	adminA := userIDs["admin-a"]
	adminB := userIDs["admin-b"]

	assertShelf := func(userID ident.UUID, galleryID int64, token string) {
		t.Helper()
		if _, err := db.Client.Bookshelf.Query().
			Where(bookshelf.UserIDEQ(userID), bookshelf.GalleryID(galleryID), bookshelf.Token(token)).
			Only(ctx); err != nil {
			t.Fatalf("bookshelf user=%v %d/%s missing: %v", userID, galleryID, token, err)
		}
	}
	assertShelf(adminA, 42, "same")
	assertShelf(adminB, 42, "same")
	assertShelf(ident.Nil(), 43, "orphan")

	for userID, page := range map[ident.UUID]int{adminA: 4, adminB: 8} {
		p, err := db.Client.ReadingProgress.Query().
			Where(readingprogress.UserIDEQ(userID), readingprogress.GalleryID(42), readingprogress.Token("same")).
			Only(ctx)
		if err != nil || p.CurrentPage != page {
			t.Fatalf("progress user=%v = %+v (err %v), want page %d", userID, p, err, page)
		}
	}
	for userID, action := range map[ident.UUID]string{adminA: "like", adminB: "dislike"} {
		tag, err := db.Client.TagPreference.Query().
			Where(tagpreference.UserIDEQ(userID), tagpreference.IDEQ("female:yuri")).
			Only(ctx)
		if err != nil || tag.Action != action {
			t.Fatalf("tag user=%v = %+v (err %v), want %s", userID, tag, err, action)
		}
	}
	if n, err := db.Client.Node.Query().Count(ctx); err != nil || n != 2 {
		t.Fatalf("nodes = %d (err %v), want 2", n, err)
	}
	// Duplicate node names were deterministically made unique.
	if n, err := db.Client.Node.Query().Count(ctx); err != nil || n != 2 {
		t.Fatalf("node count = %d (err %v)", n, err)
	}
	names := map[string]bool{}
	nodes, _ := db.Client.Node.Query().All(ctx)
	for _, n := range nodes {
		names[n.Name] = true
	}
	if len(names) != 2 {
		t.Fatalf("node names not deduplicated: %v", names)
	}

	// Idempotent: a second startup keeps the same ownership and does not
	// reassign the deliberately unowned tenant-C row.
	if err := db.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}
	again, err := NewDB(Options{Driver: DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("second NewDB: %v", err)
	}
	defer again.Close()
	orphan, err := again.Client.Bookshelf.Query().
		Where(bookshelf.GalleryID(43), bookshelf.Token("orphan")).Only(context.Background())
	if err != nil || orphan.UserID != ident.Nil() {
		t.Fatalf("orphan row = %+v (err %v), want user_id nil", orphan, err)
	}
}

func tableExistsRaw(ctx context.Context, conn *sql.DB, table string) (bool, error) {
	var one int
	err := conn.QueryRowContext(ctx, `SELECT 1 FROM sqlite_master WHERE type='table' AND name=?`, table).Scan(&one)
	if err == sql.ErrNoRows {
		return false, nil
	}
	return err == nil, err
}
