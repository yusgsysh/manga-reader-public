package database

import (
	"context"
	"database/sql"
	"path/filepath"
	"testing"

	"manga-reader/internal/config"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/prefilljob"
	"manga-reader/internal/ent/readingprogress"
	"manga-reader/internal/ent/tagpreference"
	"manga-reader/internal/ent/tenant"
	"manga-reader/internal/ent/user"

	_ "modernc.org/sqlite"
)

// TestTenancyMigration_LegacySingleUser covers the upgrade path from the
// single-user schema: the default tenant is created, the old administrator
// becomes its platform admin, and every tenant-owned row keeps its data under
// tenant_id=1. Running the migration again must be idempotent.
func TestTenancyMigration_LegacySingleUser(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "legacy-tenancy.db")
	seed, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open seed: %v", err)
	}
	legacy := `
CREATE TABLE "users" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "username" text NOT NULL,
  "password_hash" text NOT NULL,
  "token_version" integer NOT NULL DEFAULT 0,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "users_username_key" ON "users"("username");
INSERT INTO users (username, password_hash, created_at, updated_at)
  VALUES ('legacy-admin', '$argon2id$legacy', '2024-01-01 00:00:00', '2024-01-01 00:00:00');

CREATE TABLE "bookshelf" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "gallery_id" bigint NOT NULL,
  "token" text NOT NULL,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "bookshelf_gallery_id_token" ON "bookshelf"("gallery_id", "token");
INSERT INTO bookshelf (gallery_id, token, created_at, updated_at)
  VALUES (111, 'aaa', '2024-01-01 00:00:00', '2024-01-01 00:00:00');

CREATE TABLE "reading_progress" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "gallery_id" bigint NOT NULL,
  "token" text NOT NULL,
  "current_page" integer NOT NULL DEFAULT 0,
  "progress" real NOT NULL DEFAULT 0,
  "completed" bool NOT NULL DEFAULT false,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "readingprogress_gallery_id_token" ON "reading_progress"("gallery_id", "token");
INSERT INTO reading_progress (gallery_id, token, current_page, progress, completed, created_at, updated_at)
  VALUES (111, 'aaa', 7, 0.7, false, '2024-01-01 00:00:00', '2024-01-01 00:00:00');

CREATE TABLE "tag_preference" (
  "id" integer NOT NULL PRIMARY KEY AUTOINCREMENT,
  "tag" text NOT NULL,
  "action" text NOT NULL,
  "weight" integer NOT NULL DEFAULT 10,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
CREATE UNIQUE INDEX "tag_preference_tag_key" ON "tag_preference"("tag");
INSERT INTO tag_preference (tag, action, weight, created_at, updated_at)
  VALUES ('female:yuri', 'like', 10, '2024-01-01 00:00:00', '2024-01-01 00:00:00');

CREATE TABLE "prefill_job" (
  "job_id" text NOT NULL PRIMARY KEY,
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
INSERT INTO prefill_job (job_id, gallery_id, token, title, urls, status, total, created_at, updated_at)
  VALUES ('11111111-1111-1111-1111-111111111111', 111, 'aaa', 'Legacy job', '["https://exhentai.org/s/a/111-1"]', 'completed', 1, '2024-01-01 00:00:00', '2024-01-01 00:00:00');

CREATE TABLE "gallery_cache" (
  "gallery_id" bigint NOT NULL,
  "token" text NOT NULL,
  "created_at" datetime NOT NULL,
  "title" text NOT NULL DEFAULT '',
  "title_jpn" text NOT NULL DEFAULT '',
  "category" text NOT NULL DEFAULT '',
  "thumbnail" text NOT NULL DEFAULT '',
  "page_count" integer NOT NULL DEFAULT 0,
  "rating" real NOT NULL DEFAULT 0,
  "rating_count" integer NOT NULL DEFAULT 0,
  "uploader" text NOT NULL DEFAULT '',
  "posted" text NOT NULL DEFAULT '',
  "posted_at" datetime NULL,
  "language" text NOT NULL DEFAULT '',
  "translated" bool NOT NULL DEFAULT false,
  "file_size" text NOT NULL DEFAULT '',
  "favorited" integer NOT NULL DEFAULT 0,
  "expunged" bool NOT NULL DEFAULT false,
  "tags" json NULL,
  "pages" json NULL,
  "thumbnails" json NULL,
  "meta_fetched_at" datetime NULL,
  "details_fetched_at" datetime NULL,
  "pages_fetched_at" datetime NULL,
  "thumbnail_fetched_at" datetime NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("gallery_id", "token")
);
INSERT INTO gallery_cache (gallery_id, token, title, created_at, updated_at)
  VALUES (111, 'aaa', 'Global Cache', '2024-01-01 00:00:00', '2024-01-01 00:00:00');
`
	if _, err := seed.Exec(legacy); err != nil {
		t.Fatalf("seed legacy: %v", err)
	}
	seed.Close()

	db, err := NewDB(Options{Driver: DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("NewDB: %v", err)
	}
	defer db.Close()
	ctx := context.Background()

	tenantRow, err := db.Client.Tenant.Query().
		Where(tenant.SlugEQ(config.DefaultTenantSlug)).Only(ctx)
	if err != nil {
		t.Fatalf("default tenant: %v", err)
	}
	if int64(tenantRow.ID) != config.DefaultTenantID {
		t.Fatalf("default tenant id = %d, want %d", tenantRow.ID, config.DefaultTenantID)
	}

	legacyUser, err := db.Client.User.Query().Where(user.UsernameEQ("legacy-admin")).Only(ctx)
	if err != nil {
		t.Fatalf("legacy user: %v", err)
	}
	if legacyUser.TenantID != config.DefaultTenantID || legacyUser.Role != config.RolePlatformAdmin {
		t.Fatalf("legacy user = tenant %d role %q", legacyUser.TenantID, legacyUser.Role)
	}
	if hasIndex(t, db.Conn, "users", "users_username_key") {
		t.Fatal("legacy global username index survived the migration")
	}
	if !hasIndex(t, db.Conn, "users", "user_tenant_id_username") {
		t.Fatal("(tenant_id, username) unique index missing")
	}

	shelf, err := db.Client.Bookshelf.Query().
		Where(bookshelf.TenantIDEQ(config.DefaultTenantID), bookshelf.GalleryID(111)).Only(ctx)
	if err != nil || shelf.Token != "aaa" {
		t.Fatalf("bookshelf not preserved: %+v err %v", shelf, err)
	}
	progress, err := db.Client.ReadingProgress.Query().
		Where(readingprogress.TenantIDEQ(config.DefaultTenantID), readingprogress.GalleryID(111)).Only(ctx)
	if err != nil || progress.CurrentPage != 7 {
		t.Fatalf("progress not preserved: %+v err %v", progress, err)
	}
	tag, err := db.Client.TagPreference.Query().
		Where(tagpreference.TenantIDEQ(config.DefaultTenantID), tagpreference.IDEQ("female:yuri")).Only(ctx)
	if err != nil || tag.Action != "like" {
		t.Fatalf("tag preference not preserved: %+v err %v", tag, err)
	}
	job, err := db.Client.PrefillJob.Query().
		Where(prefilljob.TenantIDEQ(config.DefaultTenantID), prefilljob.TitleEQ("Legacy job")).Only(ctx)
	if err != nil || job.Total != 1 {
		t.Fatalf("prefill job not preserved: %+v err %v", job, err)
	}

	// Second startup: idempotent (one default tenant, one user, same roles).
	db.Close()
	db2, err := NewDB(Options{Driver: DriverSQLite, DSN: dsn})
	if err != nil {
		t.Fatalf("second NewDB: %v", err)
	}
	defer db2.Close()
	count, err := db2.Client.Tenant.Query().Where(tenant.SlugEQ(config.DefaultTenantSlug)).Count(ctx)
	if err != nil || count != 1 {
		t.Fatalf("default tenant count after rerun = %d (err %v)", count, err)
	}
	u2, err := db2.Client.User.Query().Where(user.UsernameEQ("legacy-admin")).Only(ctx)
	if err != nil || u2.Role != config.RolePlatformAdmin {
		t.Fatalf("user changed after rerun: %+v err %v", u2, err)
	}
}

// The immediately preceding release already used composite primary keys for
// bookshelf/reading_progress. Multi-tenancy switches those tables to an auto
// id plus a tenant-scoped unique index, so verify Ent's rebuild preserves the
// existing rows rather than recreating empty tables.
func TestTenancyMigration_CompositePKToTenantUnique(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "composite-legacy.db")
	seed, err := sql.Open("sqlite", dsn)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	ddl := `
CREATE TABLE "bookshelf" (
  "gallery_id" integer NOT NULL,
  "token" text NOT NULL,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("gallery_id", "token")
);
INSERT INTO bookshelf (gallery_id, token, created_at, updated_at)
  VALUES (333, 'bbb', '2024-01-01 00:00:00', '2024-01-01 00:00:00');
CREATE TABLE "reading_progress" (
  "gallery_id" integer NOT NULL,
  "token" text NOT NULL,
  "current_page" integer NOT NULL DEFAULT 0,
  "progress" real NOT NULL DEFAULT 0,
  "completed" bool NOT NULL DEFAULT false,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL,
  PRIMARY KEY ("gallery_id", "token")
);
INSERT INTO reading_progress (gallery_id, token, current_page, progress, completed, created_at, updated_at)
  VALUES (333, 'bbb', 9, 0.9, false, '2024-01-01 00:00:00', '2024-01-01 00:00:00');
CREATE TABLE "tag_preference" (
  "tag" text NOT NULL PRIMARY KEY,
  "action" text NOT NULL,
  "weight" integer NOT NULL DEFAULT 10,
  "created_at" datetime NOT NULL,
  "updated_at" datetime NOT NULL
);
INSERT INTO tag_preference (tag, action, weight, created_at, updated_at)
  VALUES ('male:sole male', 'like', 10, '2024-01-01 00:00:00', '2024-01-01 00:00:00');
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

	shelf, err := db.Client.Bookshelf.Query().
		Where(bookshelf.TenantIDEQ(config.DefaultTenantID), bookshelf.GalleryID(333)).Only(ctx)
	if err != nil || shelf.Token != "bbb" {
		t.Fatalf("bookshelf lost after composite migration: %+v err %v", shelf, err)
	}
	if shelf.ID == 0 {
		t.Fatal("bookshelf did not receive an auto id")
	}
	progress, err := db.Client.ReadingProgress.Query().
		Where(readingprogress.TenantIDEQ(config.DefaultTenantID), readingprogress.GalleryID(333)).Only(ctx)
	if err != nil || progress.CurrentPage != 9 {
		t.Fatalf("progress lost after composite migration: %+v err %v", progress, err)
	}
	tag, err := db.Client.TagPreference.Query().
		Where(tagpreference.TenantIDEQ(config.DefaultTenantID), tagpreference.IDEQ("male:sole male")).Only(ctx)
	if err != nil || tag.Action != "like" {
		t.Fatalf("tag lost after composite migration: %+v err %v", tag, err)
	}
}
