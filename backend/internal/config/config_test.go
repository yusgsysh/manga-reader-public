package config

import "testing"

func setRequiredCookies(t *testing.T) {
	t.Helper()
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "1")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "hash")
	// Basic Auth defaults to off; an enabled setup in the developer's shell
	// must not leak into tests that are about something else.
	for _, key := range []string{
		"MANGA_READER_BASIC_AUTH_ENABLED",
		"MANGA_READER_BASIC_AUTH_USERNAME",
		"MANGA_READER_BASIC_AUTH_PASSWORD",
		"MANGA_READER_BASIC_AUTH_PASSWORD_FILE",
		"MANGA_READER_BASIC_AUTH_FILE",
	} {
		t.Setenv(key, "")
	}
}

func TestLoad_DefaultDriverIsSQLite(t *testing.T) {
	setRequiredCookies(t)
	t.Setenv("MANGA_READER_DB_DRIVER", "")
	t.Setenv("MANGA_READER_DB_PATH", "")
	t.Setenv("MANGA_READER_DB_DSN", "")
	t.Setenv("DATABASE_URL", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Database.Driver != "sqlite" {
		t.Fatalf("driver = %q, want sqlite", cfg.Database.Driver)
	}
	if cfg.Database.IsPostgres() {
		t.Fatal("IsPostgres() = true, want false")
	}
	if cfg.Database.Path != "data/manga-reader.db" {
		t.Fatalf("path = %q, want default", cfg.Database.Path)
	}
}

func TestLoad_PostgresDriver(t *testing.T) {
	setRequiredCookies(t)
	t.Setenv("MANGA_READER_DB_DRIVER", "postgres")
	t.Setenv("MANGA_READER_DB_DSN", "postgres://user:pass@localhost:5432/db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Database.IsPostgres() {
		t.Fatal("IsPostgres() = false, want true")
	}
	if cfg.Database.DSN != "postgres://user:pass@localhost:5432/db" {
		t.Fatalf("dsn = %q", cfg.Database.DSN)
	}
}

func TestLoad_PostgresDriverFallsBackToDatabaseURL(t *testing.T) {
	setRequiredCookies(t)
	t.Setenv("MANGA_READER_DB_DRIVER", "pg")
	t.Setenv("MANGA_READER_DB_DSN", "")
	t.Setenv("DATABASE_URL", "postgres://user:pass@localhost:5432/db")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Database.IsPostgres() {
		t.Fatal("IsPostgres() = false, want true")
	}
}

func TestLoad_PostgresDriverRequiresDSN(t *testing.T) {
	setRequiredCookies(t)
	t.Setenv("MANGA_READER_DB_DRIVER", "postgres")
	t.Setenv("MANGA_READER_DB_DSN", "")
	t.Setenv("DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing postgres DSN")
	}
}

func TestLoad_UnsupportedDriver(t *testing.T) {
	setRequiredCookies(t)
	t.Setenv("MANGA_READER_DB_DRIVER", "mysql")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for unsupported driver")
	}
}
