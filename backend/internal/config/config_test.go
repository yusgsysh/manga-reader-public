package config

import (
	"testing"
	"time"
)

func setTestEnv(t *testing.T) {
	t.Helper()
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
		"IMAGE_CACHE_ENDPOINT",
		"IMAGE_CACHE_ACCESS_KEY",
		"IMAGE_CACHE_SECRET_KEY",
		"IMAGE_CACHE_BUCKET",
		"IMAGE_CACHE_USE_SSL",
		"IMAGE_CACHE_REGION",
		"IMAGE_CACHE_PROVIDER",
		"ZIP_ENABLED",
		"ZIP_PROVIDER",
		"ZIP_ENDPOINT",
		"ZIP_ACCESS_KEY",
		"ZIP_SECRET_KEY",
		"ZIP_BUCKET",
		"ZIP_USE_SSL",
		"ZIP_REGION",
		"ZIP_PREFIX",
		"ZIP_CONCURRENCY",
		"ZIP_PART_SIZE",
		"ZIP_PRESIGN_DOWNLOAD",
		"ZIP_PRESIGN_EXPIRY",
	} {
		t.Setenv(key, "")
	}
}

func TestLoad_EmptyJWTSecretIsAllowed(t *testing.T) {
	setTestEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.JWTSecret != "" {
		t.Fatalf("JWTSecret = %q, want empty (resolved later from the database)", cfg.Auth.JWTSecret)
	}
}

func TestLoad_AuthConfig(t *testing.T) {
	setTestEnv(t)
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
	if cfg.Auth.JWTSecret != "0123456789abcdef0123456789abcdef" {
		t.Fatalf("JWTSecret = %q", cfg.Auth.JWTSecret)
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
	setTestEnv(t)
	t.Setenv("JWT_SECRET", "short")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for a short JWT_SECRET")
	}
}

func TestLoad_AdminPasswordWithoutSecretStillStarts(t *testing.T) {
	setTestEnv(t)
	t.Setenv("ADMIN_PASSWORD", "correct horse battery staple")
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Auth.JWTSecret != "" {
		t.Fatal("JWTSecret = non-empty, want empty when JWT_SECRET is unset")
	}
}

func TestLoad_DefaultDriverIsSQLite(t *testing.T) {
	setTestEnv(t)
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
	setTestEnv(t)
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
	setTestEnv(t)
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
	setTestEnv(t)
	t.Setenv("MANGA_READER_DB_DRIVER", "postgres")
	t.Setenv("MANGA_READER_DB_DSN", "")
	t.Setenv("DATABASE_URL", "")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for missing postgres DSN")
	}
}

func TestLoad_UnsupportedDriver(t *testing.T) {
	setTestEnv(t)
	t.Setenv("MANGA_READER_DB_DRIVER", "mysql")

	if _, err := Load(); err == nil {
		t.Fatal("expected error for unsupported driver")
	}
}

func TestLoad_MetricsAndSyncConfig(t *testing.T) {
	setTestEnv(t)
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
	setTestEnv(t)
	t.Setenv("MANGA_READER_METRICS_TOKEN", "short")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for short metrics token")
	}
}

func TestLoad_ImageCacheEnabledFromConfig(t *testing.T) {
	setTestEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ImageCache.Enabled {
		t.Fatalf("image cache enabled with no IMAGE_CACHE_* set: %+v", cfg.ImageCache)
	}

	t.Setenv("IMAGE_CACHE_ENDPOINT", "localhost:9000")
	t.Setenv("IMAGE_CACHE_ACCESS_KEY", "key")
	t.Setenv("IMAGE_CACHE_SECRET_KEY", "secret")
	t.Setenv("IMAGE_CACHE_BUCKET", "manga-cache")
	t.Setenv("IMAGE_CACHE_USE_SSL", "false")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.ImageCache.Enabled || !cfg.ImageCache.IsConfigured() {
		t.Fatalf("image cache not enabled: %+v", cfg.ImageCache)
	}
	if cfg.ImageCache.Bucket != "manga-cache" {
		t.Fatalf("image cache = %+v", cfg.ImageCache)
	}
}

func TestLoad_ZipArchiveDisabledByDefault(t *testing.T) {
	setTestEnv(t)
	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ZipArchive.Enabled {
		t.Fatalf("zip archive enabled by default: %+v", cfg.ZipArchive)
	}
	if cfg.ZipArchive.Prefix != "archives/" {
		t.Fatalf("default prefix = %q, want archives/", cfg.ZipArchive.Prefix)
	}
}

func TestLoad_ZipArchiveEnabled(t *testing.T) {
	setTestEnv(t)
	t.Setenv("ZIP_ENABLED", "true")
	t.Setenv("ZIP_ENDPOINT", "localhost:9000")
	t.Setenv("ZIP_ACCESS_KEY", "zip-key")
	t.Setenv("ZIP_SECRET_KEY", "zip-secret")
	t.Setenv("ZIP_BUCKET", "manga-archives")
	t.Setenv("ZIP_CONCURRENCY", "6")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.ZipArchive.Enabled {
		t.Fatal("zip archive not enabled")
	}
	if cfg.ZipArchive.Bucket != "manga-archives" {
		t.Fatalf("bucket = %q", cfg.ZipArchive.Bucket)
	}
	if cfg.ZipArchive.Concurrency != 6 {
		t.Fatalf("concurrency = %d, want 6", cfg.ZipArchive.Concurrency)
	}
}

func TestLoad_ZipArchiveEnabledRequiresConfig(t *testing.T) {
	setTestEnv(t)
	t.Setenv("ZIP_ENABLED", "true")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for ZIP_ENABLED without connection fields")
	}
}

func TestLoad_ZipArchiveRejectsInvalidConcurrency(t *testing.T) {
	setTestEnv(t)
	t.Setenv("ZIP_ENABLED", "true")
	t.Setenv("ZIP_ENDPOINT", "localhost:9000")
	t.Setenv("ZIP_ACCESS_KEY", "k")
	t.Setenv("ZIP_SECRET_KEY", "s")
	t.Setenv("ZIP_BUCKET", "b")
	t.Setenv("ZIP_CONCURRENCY", "0")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for ZIP_CONCURRENCY=0")
	}
}

func TestLoad_ZipArchiveRejectsSmallPartSize(t *testing.T) {
	setTestEnv(t)
	t.Setenv("ZIP_ENABLED", "true")
	t.Setenv("ZIP_ENDPOINT", "localhost:9000")
	t.Setenv("ZIP_ACCESS_KEY", "k")
	t.Setenv("ZIP_SECRET_KEY", "s")
	t.Setenv("ZIP_BUCKET", "b")
	t.Setenv("ZIP_PART_SIZE", "1024")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for a part size below 5 MiB")
	}
}

func TestLoad_ZipArchivePresign(t *testing.T) {
	setTestEnv(t)
	t.Setenv("ZIP_ENABLED", "true")
	t.Setenv("ZIP_ENDPOINT", "localhost:9000")
	t.Setenv("ZIP_ACCESS_KEY", "k")
	t.Setenv("ZIP_SECRET_KEY", "s")
	t.Setenv("ZIP_BUCKET", "b")
	t.Setenv("ZIP_PRESIGN_DOWNLOAD", "true")
	t.Setenv("ZIP_PRESIGN_EXPIRY", "1h")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.ZipArchive.PresignDownload {
		t.Fatal("presign not enabled")
	}
	if cfg.ZipArchive.PresignExpiry != time.Hour {
		t.Fatalf("expiry = %v, want 1h", cfg.ZipArchive.PresignExpiry)
	}

	// Defaults: presign off, 15m expiry.
	setTestEnv(t)
	t.Setenv("ZIP_ENABLED", "true")
	t.Setenv("ZIP_ENDPOINT", "localhost:9000")
	t.Setenv("ZIP_ACCESS_KEY", "k")
	t.Setenv("ZIP_SECRET_KEY", "s")
	t.Setenv("ZIP_BUCKET", "b")
	cfg, err = Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.ZipArchive.PresignDownload || cfg.ZipArchive.PresignExpiry != 15*time.Minute {
		t.Fatalf("defaults = %+v", cfg.ZipArchive)
	}
}

func TestLoad_ZipArchiveRejectsInvalidPresignExpiry(t *testing.T) {
	setTestEnv(t)
	t.Setenv("ZIP_ENABLED", "true")
	t.Setenv("ZIP_ENDPOINT", "localhost:9000")
	t.Setenv("ZIP_ACCESS_KEY", "k")
	t.Setenv("ZIP_SECRET_KEY", "s")
	t.Setenv("ZIP_BUCKET", "b")
	t.Setenv("ZIP_PRESIGN_EXPIRY", "0s")
	if _, err := Load(); err == nil {
		t.Fatal("expected error for ZIP_PRESIGN_EXPIRY=0s")
	}
}
