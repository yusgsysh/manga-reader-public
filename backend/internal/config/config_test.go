package config

import "testing"

func setRequiredCookies(t *testing.T) {
	t.Helper()
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "1")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "hash")
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

func clearStorageEnv(t *testing.T) {
	t.Helper()
	for _, k := range []string{
		"MANGA_READER_STORAGE_DRIVER",
		"MANGA_READER_STORAGE_DIR",
		"MANGA_READER_S3_ENDPOINT",
		"MANGA_READER_S3_REGION",
		"MANGA_READER_S3_BUCKET",
		"MANGA_READER_S3_ACCESS_KEY",
		"MANGA_READER_S3_SECRET_KEY",
		"MANGA_READER_S3_USE_SSL",
		"MANGA_READER_S3_PATH_STYLE",
		"MINIO_ENDPOINT",
		"MINIO_REGION",
		"MINIO_BUCKET",
		"MINIO_ACCESS_KEY",
		"MINIO_SECRET_KEY",
		"MINIO_USE_SSL",
	} {
		t.Setenv(k, "")
	}
}

func TestLoad_StorageDefaultsToLocalWithoutS3Config(t *testing.T) {
	setRequiredCookies(t)
	clearStorageEnv(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.ResolvedDriver() != StorageDriverLocal {
		t.Fatalf("driver = %q, want local", cfg.Storage.ResolvedDriver())
	}
	if cfg.Storage.Dir != "data/cache" {
		t.Fatalf("dir = %q, want data/cache", cfg.Storage.Dir)
	}
}

func TestLoad_LegacyMinioEnvSelectsS3Driver(t *testing.T) {
	setRequiredCookies(t)
	clearStorageEnv(t)
	t.Setenv("MINIO_ENDPOINT", "minio:9000")
	t.Setenv("MINIO_ACCESS_KEY", "key")
	t.Setenv("MINIO_SECRET_KEY", "secret")
	t.Setenv("MINIO_BUCKET", "bucket")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.ResolvedDriver() != StorageDriverS3 {
		t.Fatalf("driver = %q, want s3", cfg.Storage.ResolvedDriver())
	}
	if cfg.Storage.S3.Endpoint != "minio:9000" || cfg.Storage.S3.Bucket != "bucket" {
		t.Fatalf("s3 config = %+v", cfg.Storage.S3)
	}
}

func TestLoad_NewS3EnvOverridesLegacyMinioEnv(t *testing.T) {
	setRequiredCookies(t)
	clearStorageEnv(t)
	t.Setenv("MINIO_ENDPOINT", "legacy:9000")
	t.Setenv("MINIO_BUCKET", "legacy-bucket")
	t.Setenv("MINIO_ACCESS_KEY", "legacy-key")
	t.Setenv("MINIO_SECRET_KEY", "legacy-secret")
	t.Setenv("MANGA_READER_S3_ENDPOINT", "s3.example.com")
	t.Setenv("MANGA_READER_S3_BUCKET", "new-bucket")
	t.Setenv("MANGA_READER_S3_ACCESS_KEY", "new-key")
	t.Setenv("MANGA_READER_S3_SECRET_KEY", "new-secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.S3.Endpoint != "s3.example.com" || cfg.Storage.S3.Bucket != "new-bucket" {
		t.Fatalf("s3 config = %+v, want new values", cfg.Storage.S3)
	}
	if cfg.Storage.S3.AccessKey != "new-key" || cfg.Storage.S3.SecretKey != "new-secret" {
		t.Fatalf("s3 credentials = %+v/%+v, want new values", cfg.Storage.S3.AccessKey, cfg.Storage.S3.SecretKey)
	}
}

func TestLoad_ExplicitLocalDriverIgnoresS3Config(t *testing.T) {
	setRequiredCookies(t)
	clearStorageEnv(t)
	t.Setenv("MANGA_READER_STORAGE_DRIVER", "local")
	t.Setenv("MANGA_READER_S3_ENDPOINT", "minio:9000")
	t.Setenv("MANGA_READER_S3_BUCKET", "bucket")
	t.Setenv("MANGA_READER_S3_ACCESS_KEY", "key")
	t.Setenv("MANGA_READER_S3_SECRET_KEY", "secret")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.Storage.ResolvedDriver() != StorageDriverLocal {
		t.Fatalf("driver = %q, want local", cfg.Storage.ResolvedDriver())
	}
}

func TestLoad_ExplicitS3DriverRequiresCompleteConfig(t *testing.T) {
	setRequiredCookies(t)
	clearStorageEnv(t)
	t.Setenv("MANGA_READER_STORAGE_DRIVER", "s3")
	t.Setenv("MANGA_READER_S3_ENDPOINT", "minio:9000")

	if _, err := Load(); err == nil {
		t.Fatal("Load succeeded with incomplete explicit s3 config, want error")
	}
}

func TestLoad_UnsupportedStorageDriver(t *testing.T) {
	setRequiredCookies(t)
	clearStorageEnv(t)
	t.Setenv("MANGA_READER_STORAGE_DRIVER", "ftp")

	if _, err := Load(); err == nil {
		t.Fatal("Load succeeded with unsupported storage driver, want error")
	}
}

func TestStorageConfig_ResolvedDriver(t *testing.T) {
	tests := []struct {
		name string
		cfg  StorageConfig
		want string
	}{
		{"empty is local", StorageConfig{}, StorageDriverLocal},
		{"auto without s3 config is local", StorageConfig{Driver: StorageDriverAuto}, StorageDriverLocal},
		{"auto with complete s3 config", StorageConfig{Driver: StorageDriverAuto, S3: S3Config{
			Endpoint: "e", Bucket: "b", AccessKey: "a", SecretKey: "s",
		}}, StorageDriverS3},
		{"explicit local wins over s3 config", StorageConfig{Driver: StorageDriverLocal, S3: S3Config{
			Endpoint: "e", Bucket: "b", AccessKey: "a", SecretKey: "s",
		}}, StorageDriverLocal},
		{"explicit s3", StorageConfig{Driver: StorageDriverS3}, StorageDriverS3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.cfg.ResolvedDriver(); got != tt.want {
				t.Fatalf("ResolvedDriver() = %q, want %q", got, tt.want)
			}
		})
	}
}
