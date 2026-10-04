package app

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"manga-reader/internal/config"
	"manga-reader/internal/logx"
	"manga-reader/internal/settings"
	"manga-reader/internal/storage"
)

// newSettingsTestConfig pins the database and cache paths so a second App can
// be built over the same state.
func newSettingsTestConfig(t *testing.T) *config.Config {
	t.Helper()
	dir := t.TempDir()
	for key, value := range map[string]string{
		"EHENTAI_COOKIE_IPB_MEMBER_ID": "1",
		"EHENTAI_COOKIE_IPB_PASS_HASH": "hash",
		"EHENTAI_COOKIE_IGNEOUS":       "",
		"EHENTAI_COOKIE_SK":            "",
		"MANGA_READER_DB_DRIVER":       "sqlite",
		"MANGA_READER_DB_PATH":         filepath.Join(dir, "test.db"),
		"MANGA_READER_STORAGE_DRIVER":  "local",
		"MANGA_READER_STORAGE_DIR":     filepath.Join(dir, "cache"),
		"MANGA_READER_S3_ENDPOINT":     "",
		"MANGA_READER_S3_REGION":       "",
		"MANGA_READER_S3_BUCKET":       "",
		"MANGA_READER_S3_ACCESS_KEY":   "",
		"MANGA_READER_S3_SECRET_KEY":   "",
		"MANGA_READER_S3_USE_SSL":      "",
		"MANGA_READER_S3_PATH_STYLE":   "",
		"MINIO_ENDPOINT":               "",
		"MINIO_REGION":                 "",
		"MINIO_BUCKET":                 "",
		"MINIO_ACCESS_KEY":             "",
		"MINIO_SECRET_KEY":             "",
		"MINIO_USE_SSL":                "",
		"LOG_LEVEL":                    "warn",
		"MANGA_READER_DEV_TOOLS":       "false",
	} {
		t.Setenv(key, value)
	}

	cfg, err := config.Load()
	if err != nil {
		t.Fatalf("config.Load: %v", err)
	}
	return cfg
}

func strPtr(s string) *string { return &s }
func boolPtr(b bool) *bool    { return &b }

// TestSettings_LogLevelChangeReachesTheLogger asserts the wiring the entrypoint
// uses: the settings service drives the setter passed through WithLogSetter.
func TestSettings_LogLevelChangeReachesTheLogger(t *testing.T) {
	cfg := newSettingsTestConfig(t)

	dyn := logx.New(slog.NewTextHandler(io.Discard, nil), slog.LevelWarn)
	a, err := New(cfg, dyn.Logger(), WithLogSetter(func(v string) {
		dyn.SetLevel(logx.ParseLevel(v))
	}))
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	if dyn.Level() != slog.LevelWarn {
		t.Fatalf("level = %s, want warn from the environment", dyn.Level())
	}

	if _, err := a.Settings.Update(context.Background(), settings.Update{
		LogLevel: strPtr("debug"),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if dyn.Level() != slog.LevelDebug {
		t.Errorf("level = %s, want debug after the save", dyn.Level())
	}
}

// Handlers hold one storage handle for the life of the process, so a settings
// save has to swap what is behind it rather than replace the handle.
func TestSettings_StorageChangeIsSwappedUnderTheHandlers(t *testing.T) {
	cfg := newSettingsTestConfig(t)
	a, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	newRoot := filepath.Join(t.TempDir(), "relocated")
	if _, err := a.Settings.Update(context.Background(), settings.Update{
		Storage: &settings.StorageUpdate{Dir: strPtr(newRoot)},
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}

	ctx := context.Background()
	if err := a.Storage.Put(ctx, "a/b.png", []byte("image"), "image/png", storage.PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}

	moved, err := storage.NewLocalStorage(storage.LocalConfig{Root: newRoot})
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}
	data, _, err := moved.Get(ctx, "a/b.png")
	if err != nil {
		t.Fatalf("object did not land in the new root: %v", err)
	}
	if string(data) != "image" {
		t.Errorf("data = %q, want image", data)
	}
}

func TestSettings_DevToolsRouteFollowsTheSavedFlag(t *testing.T) {
	cfg := newSettingsTestConfig(t)
	a, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	defer a.Close()

	get := func() int {
		w := httptest.NewRecorder()
		a.Router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/dev/upstream-down", nil))
		return w.Code
	}

	if code := get(); code != http.StatusNotFound {
		t.Fatalf("dev route at boot = %d, want 404", code)
	}
	if _, err := a.Settings.Update(context.Background(), settings.Update{DevTools: boolPtr(true)}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	if code := get(); code != http.StatusOK {
		t.Fatalf("dev route after enabling = %d, want 200", code)
	}
}

func TestSettings_RestartPicksUpPersistedValues(t *testing.T) {
	cfg := newSettingsTestConfig(t)

	first, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := first.Settings.Update(context.Background(), settings.Update{
		LogLevel: strPtr("error"),
		DevTools: boolPtr(true),
	}); err != nil {
		t.Fatalf("Update: %v", err)
	}
	first.Close()

	// A second process over the same database must pick the saved values up.
	second, err := New(cfg, testLogger())
	if err != nil {
		t.Fatalf("New (restart): %v", err)
	}
	defer second.Close()

	got := second.Settings.Get()
	if got.LogLevel != "error" || !got.DevTools {
		t.Errorf("settings after restart = %+v, want error/true", got)
	}
	if !got.Persisted {
		t.Error("Persisted = false, want true")
	}
}
