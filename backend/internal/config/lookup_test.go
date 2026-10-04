package config

import (
	"os"
	"testing"
)

// lookupOf returns a lookup where keys in overrides win and everything else
// still comes from the process environment — the arrangement the settings store
// produces in production.
func lookupOf(overrides map[string]string) EnvLookup {
	return func(key string) string {
		if v, ok := overrides[key]; ok {
			return v
		}
		return os.Getenv(key)
	}
}

func setPlainEnv(t *testing.T) {
	t.Helper()
	setRequiredCookies(t)
	clearStorageEnv(t)
	t.Setenv("EHENTAI_PORT", "")
	t.Setenv("MANGA_READER_DB_DRIVER", "")
	t.Setenv("MANGA_READER_DB_PATH", "")
	t.Setenv("MANGA_READER_DB_DSN", "")
	t.Setenv("DATABASE_URL", "")
	t.Setenv("ENVIRONMENT", "")
	t.Setenv("LOG_LEVEL", "warn")
	t.Setenv("MANGA_READER_DEV_TOOLS", "false")
}

func TestLoadWith_PersistedSettingsWinOverTheEnvironment(t *testing.T) {
	setPlainEnv(t)

	cfg, err := LoadWith(lookupOf(map[string]string{
		"EHENTAI_COOKIE_IPB_MEMBER_ID": "from-db",
		"LOG_LEVEL":                    "debug",
		"MANGA_READER_DEV_TOOLS":       "true",
	}))
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if cfg.Cookie.IpbMemberID != "from-db" {
		t.Errorf("member id = %q, want the persisted value", cfg.Cookie.IpbMemberID)
	}
	// Keys the settings page did not touch keep following the environment.
	if cfg.Cookie.IpbPassHash != "hash" {
		t.Errorf("pass hash = %q, want the environment value", cfg.Cookie.IpbPassHash)
	}
	if cfg.LogLevel != "debug" {
		t.Errorf("log level = %q, want debug", cfg.LogLevel)
	}
	if !cfg.DevTools {
		t.Error("DevTools = false, want true from the persisted value")
	}
	if cfg.Port != ":8080" || cfg.Database.Driver != "sqlite" {
		t.Errorf("port = %q driver = %q; restart-only settings must stay on the environment", cfg.Port, cfg.Database.Driver)
	}
}

// A key the settings page never saved is absent from the overlay entirely, so
// the lookup still sees the process environment for it.
func TestLoadWith_KeysThatWereNeverSavedFallBackToTheEnvironment(t *testing.T) {
	setPlainEnv(t)
	t.Setenv("LOG_LEVEL", "info")

	cfg, err := LoadWith(lookupOf(nil))
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if cfg.LogLevel != "info" {
		t.Errorf("log level = %q, want info from the environment", cfg.LogLevel)
	}
	if cfg.Cookie.IpbMemberID != "1" {
		t.Errorf("member id = %q, want the environment value", cfg.Cookie.IpbMemberID)
	}
}

// An empty EnvLookup value means "unset", which yields the built-in default
// rather than an empty string.
func TestLoadWith_EmptyLookupValueYieldsTheDefault(t *testing.T) {
	setPlainEnv(t)
	t.Setenv("LOG_LEVEL", "info")

	cfg, err := LoadWith(func(string) string { return "" })
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if cfg.LogLevel != "warn" {
		t.Errorf("log level = %q, want the warn default", cfg.LogLevel)
	}
	if cfg.Port != ":8080" {
		t.Errorf("port = %q, want the default", cfg.Port)
	}
}

func TestLoadWith_CookieHeaderIsTheBaseAndIndividualFieldsWin(t *testing.T) {
	setPlainEnv(t)
	t.Setenv("EHENTAI_COOKIE", "ipb_member_id=from-header; ipb_pass_hash=from-header; igneous=from-header")
	// Only the persisted member id and the header stand in for these; if the
	// environment also supplied them the test could not tell which won.
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "")
	t.Setenv("EHENTAI_COOKIE_IGNEOUS", "")

	cfg, err := LoadWith(lookupOf(map[string]string{
		"EHENTAI_COOKIE_IPB_MEMBER_ID": "from-db",
	}))
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if cfg.Cookie.IpbMemberID != "from-db" {
		t.Errorf("member id = %q, want the individual field to win", cfg.Cookie.IpbMemberID)
	}
	if cfg.Cookie.IpbPassHash != "from-header" {
		t.Errorf("pass hash = %q, want it from the bootstrap header", cfg.Cookie.IpbPassHash)
	}
	if cfg.Cookie.Igneous != "from-header" {
		t.Errorf("igneous = %q, want it from the bootstrap header", cfg.Cookie.Igneous)
	}
}

// The settings page is the only way to enter credentials at runtime, so an
// empty cookie must no longer be a boot-time error.
func TestLoad_BootsWithoutCookies(t *testing.T) {
	setPlainEnv(t)
	t.Setenv("EHENTAI_COOKIE", "")
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load = %v, want it to succeed so settings can fix the cookie", err)
	}
	if cfg.Cookie.IsValid() {
		t.Error("cookie is valid, want it incomplete")
	}
}

func TestLoadWith_MatchesLoadWhenTheLookupIsTheEnvironment(t *testing.T) {
	setPlainEnv(t)

	want, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	got, err := LoadWith(os.Getenv)
	if err != nil {
		t.Fatalf("LoadWith: %v", err)
	}
	if got.LogLevel != want.LogLevel || got.Cookie != want.Cookie || got.Storage != want.Storage || got.DevTools != want.DevTools {
		t.Errorf("LoadWith(os.Getenv) = %+v, want the same as Load: %+v", got, want)
	}
}

// A key missing from ManagedKeys would silently stay restart-only even though
// the settings page writes it, so the list is pinned.
func TestManagedKeysListEveryRuntimeSetting(t *testing.T) {
	want := []string{
		"EHENTAI_COOKIE_IPB_MEMBER_ID",
		"EHENTAI_COOKIE_IPB_PASS_HASH",
		"EHENTAI_COOKIE_IGNEOUS",
		"EHENTAI_COOKIE_SK",
		"MANGA_READER_STORAGE_DRIVER",
		"MANGA_READER_STORAGE_DIR",
		"MANGA_READER_S3_ENDPOINT",
		"MANGA_READER_S3_REGION",
		"MANGA_READER_S3_BUCKET",
		"MANGA_READER_S3_ACCESS_KEY",
		"MANGA_READER_S3_SECRET_KEY",
		"MANGA_READER_S3_USE_SSL",
		"MANGA_READER_S3_PATH_STYLE",
		"LOG_LEVEL",
		"MANGA_READER_DEV_TOOLS",
	}
	if len(ManagedKeys) != len(want) {
		t.Fatalf("ManagedKeys = %v, want %d keys", ManagedKeys, len(want))
	}
	seen := map[string]bool{}
	for i, k := range ManagedKeys {
		if k != want[i] {
			t.Errorf("ManagedKeys[%d] = %q, want %q", i, k, want[i])
		}
		if seen[k] {
			t.Errorf("ManagedKeys lists %q twice", k)
		}
		seen[k] = true
	}
}
