package settings

import (
	"context"
	"database/sql"
	"log/slog"
	"path/filepath"
	"testing"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
)

func newTestService(t *testing.T) (*Service, *ent.Client) {
	t.Helper()
	// modernc.org/sqlite registers itself as "sqlite", not "sqlite3", so the
	// connection is opened here rather than through enttest.Open.
	conn, err := sql.Open("sqlite", "file:"+filepath.Join(t.TempDir(), "settings.db")+"?_pragma=foreign_keys=on")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	// A shared-cache :memory: pool still opens several connections; one
	// connection keeps the schema visible to every query.
	conn.SetMaxOpenConns(1)
	t.Cleanup(func() { _ = conn.Close() })

	client := ent.NewClient(ent.Driver(entsql.OpenDB(dialect.SQLite, conn)))
	if err := client.Schema.Create(context.Background()); err != nil {
		t.Fatalf("create schema: %v", err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return New(client, slog.Default()), client
}

// setBaseEnv gives a complete, valid environment so any change in a test comes
// from the persisted settings and not from a missing variable.
func setBaseEnv(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "member")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "hash")
	t.Setenv("EHENTAI_COOKIE_IGNEOUS", "igneous-value")
	t.Setenv("EHENTAI_COOKIE_SK", "sk-value")
	t.Setenv("MANGA_READER_STORAGE_DRIVER", "local")
	t.Setenv("MANGA_READER_STORAGE_DIR", "data/cache")
	t.Setenv("MANGA_READER_S3_ENDPOINT", "")
	t.Setenv("MANGA_READER_S3_REGION", "")
	t.Setenv("MANGA_READER_S3_BUCKET", "")
	t.Setenv("MANGA_READER_S3_ACCESS_KEY", "")
	t.Setenv("MANGA_READER_S3_SECRET_KEY", "")
	t.Setenv("MANGA_READER_S3_USE_SSL", "")
	t.Setenv("MANGA_READER_S3_PATH_STYLE", "")
	t.Setenv("MINIO_ENDPOINT", "")
	t.Setenv("MINIO_REGION", "")
	t.Setenv("MINIO_BUCKET", "")
	t.Setenv("MINIO_ACCESS_KEY", "")
	t.Setenv("MINIO_SECRET_KEY", "")
	t.Setenv("MINIO_USE_SSL", "")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("MANGA_READER_DEV_TOOLS", "false")

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func strPtr(s string) *string { return &s }

func TestLoad_UsesEnvironmentWhenNothingIsPersisted(t *testing.T) {
	svc, _ := newTestService(t)
	base := setBaseEnv(t)

	got, err := svc.Load(context.Background(), base)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Cookie.IpbMemberID != "member" || got.Cookie.IpbPassHash != "hash" {
		t.Errorf("cookie = %+v, want the environment cookie", got.Cookie)
	}
	if snap := svc.Get(); snap.Persisted {
		t.Error("Persisted = true, want false when nothing is saved")
	}
}

func TestLoad_AppliesPersistedOverridesOverEnvironment(t *testing.T) {
	svc, client := newTestService(t)
	base := setBaseEnv(t)

	if err := writeOverrides(context.Background(), client, map[string]string{
		"EHENTAI_COOKIE_IPB_MEMBER_ID": "from-db",
		"LOG_LEVEL":                    "debug",
	}); err != nil {
		t.Fatalf("seed overrides: %v", err)
	}

	got, err := svc.Load(context.Background(), base)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if got.Cookie.IpbMemberID != "from-db" {
		t.Errorf("member id = %q, want the persisted value", got.Cookie.IpbMemberID)
	}
	if got.Cookie.IpbPassHash != "hash" {
		t.Errorf("pass hash = %q, want the environment value for the untouched key", got.Cookie.IpbPassHash)
	}
	if got.LogLevel != "debug" {
		t.Errorf("log level = %q, want debug", got.LogLevel)
	}
	if snap := svc.Get(); !snap.Persisted {
		t.Error("Persisted = false, want true")
	}
}

func TestLoad_InvalidPersistedSettingsFallBackToEnvironment(t *testing.T) {
	svc, client := newTestService(t)
	base := setBaseEnv(t)

	// A driver of "s3" without any S3 credentials cannot run.
	if err := writeOverrides(context.Background(), client, map[string]string{
		"MANGA_READER_STORAGE_DRIVER": "s3",
	}); err != nil {
		t.Fatalf("seed overrides: %v", err)
	}

	got, err := svc.Load(context.Background(), base)
	if err == nil {
		t.Fatal("Load error = nil, want an error for the rejected settings")
	}
	if got.Storage.ResolvedDriver() != config.StorageDriverLocal {
		t.Errorf("resolved driver = %q, want local (the environment's)", got.Storage.ResolvedDriver())
	}
	// The bad rows stay put so the settings page can show and fix them.
	if len(svc.Overrides()) != 1 {
		t.Errorf("overrides = %v, want the rejected row retained", svc.Overrides())
	}
}

func TestUpdate_PersistsOnlyKeysThatDifferFromTheEnvironment(t *testing.T) {
	svc, client := newTestService(t)
	base := setBaseEnv(t)
	if _, err := svc.Load(context.Background(), base); err != nil {
		t.Fatalf("Load: %v", err)
	}

	snap, err := svc.Update(context.Background(), Update{
		Cookie:   &CookieUpdate{MemberID: strPtr("changed")},
		LogLevel: strPtr("info"),
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if snap.Cookie.MemberID != "changed" || snap.LogLevel != "info" {
		t.Errorf("snapshot = %+v, want the updated values", snap)
	}
	if !snap.Persisted {
		t.Error("Persisted = false, want true")
	}

	overrides := svc.Overrides()
	if len(overrides) != 2 {
		t.Fatalf("overrides = %v, want exactly the two changed keys", overrides)
	}
	if overrides["EHENTAI_COOKIE_IPB_MEMBER_ID"] != "changed" {
		t.Errorf("member id override = %q", overrides["EHENTAI_COOKIE_IPB_MEMBER_ID"])
	}
	if overrides["LOG_LEVEL"] != "info" {
		t.Errorf("log level override = %q", overrides["LOG_LEVEL"])
	}

	rows, err := client.Setting.Query().All(context.Background())
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if len(rows) != 2 {
		t.Errorf("rows = %d, want 2 persisted rows", len(rows))
	}
}

func TestUpdate_RevertingToTheEnvironmentDropsTheOverride(t *testing.T) {
	svc, client := newTestService(t)
	base := setBaseEnv(t)
	if _, err := svc.Load(context.Background(), base); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, err := svc.Update(context.Background(), Update{LogLevel: strPtr("debug")}); err != nil {
		t.Fatalf("first Update: %v", err)
	}
	if len(svc.Overrides()) != 1 {
		t.Fatalf("overrides = %v, want the log level stored", svc.Overrides())
	}

	snap, err := svc.Update(context.Background(), Update{LogLevel: strPtr("warn")})
	if err != nil {
		t.Fatalf("second Update: %v", err)
	}
	if len(svc.Overrides()) != 0 || snap.Persisted {
		t.Errorf("overrides = %v, persisted = %v; both want empty/false once the environment value is restored",
			svc.Overrides(), snap.Persisted)
	}
	rows, err := client.Setting.Query().All(context.Background())
	if err != nil {
		t.Fatalf("read settings: %v", err)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want the override deleted", len(rows))
	}
}

func TestGet_MasksSecrets(t *testing.T) {
	svc, _ := newTestService(t)
	base := setBaseEnv(t)
	if _, err := svc.Load(context.Background(), base); err != nil {
		t.Fatalf("Load: %v", err)
	}

	snap := svc.Get()
	if snap.Cookie.PassHash != Mask || snap.Cookie.SK != Mask || snap.Cookie.Igneous != Mask {
		t.Errorf("cookie secrets = %+v, want them masked", snap.Cookie)
	}
	if snap.Cookie.MemberID != "member" {
		t.Errorf("member id = %q, want it visible (it is an identifier)", snap.Cookie.MemberID)
	}
	if !snap.Cookie.Configured {
		t.Error("Configured = false, want true")
	}
}

func TestUpdate_KeepsSecretsWhenTheMaskIsEchoedBack(t *testing.T) {
	svc, _ := newTestService(t)
	base := setBaseEnv(t)
	if _, err := svc.Load(context.Background(), base); err != nil {
		t.Fatalf("Load: %v", err)
	}

	// A form that round-trips the masked value must not overwrite the secret.
	if _, err := svc.Update(context.Background(), Update{
		Cookie: &CookieUpdate{
			MemberID: strPtr("member"),
			PassHash: strPtr(Mask),
			Igneous:  strPtr(Mask),
			SK:       strPtr(Mask),
		},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	if len(svc.Overrides()) != 0 {
		t.Errorf("overrides = %v, want none: echoing the mask changes nothing", svc.Overrides())
	}
	if got := svc.Get().Cookie.PassHash; got != Mask {
		t.Errorf("pass hash = %q, want the mask back", got)
	}
}

func TestUpdate_ANewSecretReplacesTheOldOne(t *testing.T) {
	svc, _ := newTestService(t)
	base := setBaseEnv(t)
	if _, err := svc.Load(context.Background(), base); err != nil {
		t.Fatalf("Load: %v", err)
	}

	if _, err := svc.Update(context.Background(), Update{
		Cookie: &CookieUpdate{PassHash: strPtr("new-secret")},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if got := svc.Overrides()["EHENTAI_COOKIE_IPB_PASS_HASH"]; got != "new-secret" {
		t.Errorf("pass hash override = %q, want new-secret", got)
	}
}

func TestUpdate_RejectsIncompleteCookieWithoutPersisting(t *testing.T) {
	svc, client := newTestService(t)
	base := setBaseEnv(t)
	if _, err := svc.Load(context.Background(), base); err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, err := svc.Update(context.Background(), Update{
		Cookie: &CookieUpdate{MemberID: strPtr(""), PassHash: strPtr("")},
	})
	if err == nil {
		t.Fatal("Update error = nil, want a validation failure")
	}
	if !IsInvalid(err) {
		t.Errorf("error = %v, want an InvalidError so the API answers 400", err)
	}
	rows, rerr := client.Setting.Query().All(context.Background())
	if rerr != nil {
		t.Fatalf("read settings: %v", rerr)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want nothing persisted after a rejected save", len(rows))
	}
}

func TestUpdate_RejectsIncompleteS3WithoutPersisting(t *testing.T) {
	svc, client := newTestService(t)
	base := setBaseEnv(t)
	if _, err := svc.Load(context.Background(), base); err != nil {
		t.Fatalf("Load: %v", err)
	}

	_, err := svc.Update(context.Background(), Update{
		Storage: &StorageUpdate{Driver: strPtr("s3")},
	})
	if err == nil {
		t.Fatal("Update error = nil, want a validation failure")
	}
	if !IsInvalid(err) {
		t.Errorf("error = %v, want an InvalidError", err)
	}
	rows, rerr := client.Setting.Query().All(context.Background())
	if rerr != nil {
		t.Fatalf("read settings: %v", rerr)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want nothing persisted after a rejected save", len(rows))
	}
}

func TestUpdate_AppliesAndRollsBackThroughHooks(t *testing.T) {
	svc, _ := newTestService(t)
	base := setBaseEnv(t)
	if _, err := svc.Load(context.Background(), base); err != nil {
		t.Fatalf("Load: %v", err)
	}

	var cookies []config.CookieConfig
	var levels []string
	svc.Bind(Hooks{
		Storage: func(config.StorageConfig) error {
			return nil
		},
		Cookies: func(c config.CookieConfig) error {
			cookies = append(cookies, c)
			return nil
		},
		LogLevel: func(level string) { levels = append(levels, level) },
		DevTools: func(bool) {},
	})

	if _, err := svc.Update(context.Background(), Update{LogLevel: strPtr("debug")}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(cookies) != 0 {
		t.Errorf("cookie hook ran %d times, want 0 (nothing cookie-related changed)", len(cookies))
	}
	if len(levels) != 1 || levels[0] != "debug" {
		t.Errorf("log level hook = %v, want [debug]", levels)
	}
}

func TestUpdate_StorageHookFailureIsReportedAndNotSaved(t *testing.T) {
	svc, client := newTestService(t)
	base := setBaseEnv(t)
	if _, err := svc.Load(context.Background(), base); err != nil {
		t.Fatalf("Load: %v", err)
	}

	svc.Bind(Hooks{
		Storage: func(config.StorageConfig) error {
			return context.DeadlineExceeded
		},
	})

	_, err := svc.Update(context.Background(), Update{
		Storage: &StorageUpdate{Driver: strPtr("s3"), S3: &S3Update{
			Endpoint:  strPtr("minio:9000"),
			Bucket:    strPtr("cache"),
			AccessKey: strPtr("key"),
			SecretKey: strPtr("secret"),
		}},
	})
	if err == nil {
		t.Fatal("Update error = nil, want the storage failure surfaced")
	}
	if !IsInvalid(err) {
		t.Errorf("error = %v, want an InvalidError so the API answers 400", err)
	}
	rows, rerr := client.Setting.Query().All(context.Background())
	if rerr != nil {
		t.Fatalf("read settings: %v", rerr)
	}
	if len(rows) != 0 {
		t.Errorf("rows = %d, want nothing persisted when applying failed", len(rows))
	}
}

func TestUpdate_AppliesTheStorageDriverChange(t *testing.T) {
	svc, _ := newTestService(t)
	base := setBaseEnv(t)
	if _, err := svc.Load(context.Background(), base); err != nil {
		t.Fatalf("Load: %v", err)
	}

	var applied []config.StorageConfig
	svc.Bind(Hooks{Storage: func(c config.StorageConfig) error {
		applied = append(applied, c)
		return nil
	}})

	snap, err := svc.Update(context.Background(), Update{
		Storage: &StorageUpdate{Driver: strPtr("local"), Dir: strPtr("/tmp/cache")},
	})
	if err != nil {
		t.Fatalf("Update: %v", err)
	}
	if len(applied) != 1 {
		t.Fatalf("storage hook ran %d times, want 1", len(applied))
	}
	if applied[0].Dir != "/tmp/cache" {
		t.Errorf("applied dir = %q, want /tmp/cache", applied[0].Dir)
	}
	if snap.Storage.Dir != "/tmp/cache" || snap.Storage.ResolvedDriver != config.StorageDriverLocal {
		t.Errorf("snapshot storage = %+v, want the updated local config", snap.Storage)
	}
}
