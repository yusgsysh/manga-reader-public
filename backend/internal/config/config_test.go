package config

import (
	"testing"
	"time"
)

func setRequiredCookies(t *testing.T) {
	t.Helper()
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "1")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "hash")
	// Auth/metrics/sync settings must be opted into explicitly; shell
	// settings must not leak into unrelated tests.
	for _, key := range []string{
		"JWT_SECRET",
		"JWT_ISSUER",
		"JWT_EXPIRES_IN",
		"ADMIN_USERNAME",
		"ADMIN_PASSWORD",
		"MANGA_READER_AUTH_COOKIE_SECURE",
		"MANGA_READER_METRICS_ENABLED",
		"MANGA_READER_METRICS_TOKEN",
		"MANGA_READER_SYNC_ENABLED",
		"MANGA_READER_SYNC_REQUEST_TIMEOUT",
		"MANGA_READER_SYNC_API_KEY_MIN_LENGTH",
	} {
		t.Setenv(key, "")
	}
}

func TestLoad_AuthDisabledWithoutSecret(t *testing.T) {
	setRequiredCookies(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.Enabled {
		t.Fatal("Auth.Enabled = true, want false")
	}
}

func TestLoad_AuthConfig(t *testing.T) {
	setRequiredCookies(t)
	t.Setenv("JWT_SECRET", "0123456789abcdef0123456789abcdef")
	t.Setenv("JWT_ISSUER", "test-issuer")
	t.Setenv("JWT_EXPIRES_IN", "2h")
	t.Setenv("ADMIN_USERNAME", "root")
	t.Setenv("ADMIN_PASSWORD", "correct horse battery staple")
	t.Setenv("MANGA_READER_AUTH_COOKIE_SECURE", "true")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Auth.Enabled {
		t.Fatal("Auth.Enabled = false, want true")
	}
	if cfg.Auth.JWTIssuer != "test-issuer" {
		t.Fatalf("issuer = %q", cfg.Auth.JWTIssuer)
	}
	if cfg.Auth.ExpiresInSeconds() != 7200 {
		t.Fatalf("expires = %d, want 7200", cfg.Auth.ExpiresInSeconds())
	}
	if cfg.Auth.AdminUsername != "root" {
		t.Fatalf("admin username = %q", cfg.Auth.AdminUsername)
	}
	if cfg.Auth.CookieSecureAuto || !cfg.Auth.CookieSecure {
		t.Fatalf("cookie secure mode = %+v, want explicit true", cfg.Auth)
	}
}

func TestLoad_AuthRejectsWeakSecret(t *testing.T) {
	setRequiredCookies(t)
	t.Setenv("JWT_SECRET", "short")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for a short JWT_SECRET")
	}
}

func TestLoad_AdminPasswordWithoutSecretIsAnError(t *testing.T) {
	setRequiredCookies(t)
	t.Setenv("ADMIN_PASSWORD", "correct horse battery staple")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for ADMIN_PASSWORD without JWT_SECRET")
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

func TestLoad_MetricsAndSyncConfig(t *testing.T) {
	setRequiredCookies(t)
	t.Setenv("MANGA_READER_METRICS_ENABLED", "true")
	t.Setenv("MANGA_READER_METRICS_TOKEN", "mfxokrdp-ymjopne-3594-lyfsj-abcdef")
	t.Setenv("MANGA_READER_SYNC_ENABLED", "false")
	t.Setenv("MANGA_READER_SYNC_REQUEST_TIMEOUT", "45s")
	t.Setenv("MANGA_READER_SYNC_API_KEY_MIN_LENGTH", "48")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Metrics.Enabled || cfg.Metrics.Token == "" {
		t.Fatalf("metrics config = %+v", cfg.Metrics)
	}
	if cfg.Sync.Enabled {
		t.Fatal("Sync.Enabled = true, want false")
	}
	if cfg.Sync.RequestTimeout != 45*time.Second {
		t.Fatalf("timeout = %v", cfg.Sync.RequestTimeout)
	}
	if cfg.Sync.APIKeyMinLength != 48 {
		t.Fatalf("min key length = %d", cfg.Sync.APIKeyMinLength)
	}
}

func TestLoad_RejectsWeakMetricsToken(t *testing.T) {
	setRequiredCookies(t)
	t.Setenv("MANGA_READER_METRICS_TOKEN", "short")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for short metrics token")
	}
}
