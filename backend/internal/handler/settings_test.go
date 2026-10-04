package handler

import (
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"

	"manga-reader/internal/config"
	"manga-reader/internal/database"
	"manga-reader/internal/settings"
)

// settingsTestEnv gives a complete, valid environment so that any setting a
// test sees come from the request payload.
func settingsTestEnv(t *testing.T) *config.Config {
	t.Helper()
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "member")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "hash")
	t.Setenv("EHENTAI_COOKIE_IGNEOUS", "igneous-value")
	t.Setenv("EHENTAI_COOKIE_SK", "sk-value")
	t.Setenv("MANGA_READER_STORAGE_DRIVER", "local")
	t.Setenv("MANGA_READER_STORAGE_DIR", t.TempDir())
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

func newSettingsServer(t *testing.T) *Server {
	t.Helper()
	client := newTestDB(t)
	svc := settings.New(client, slog.Default())
	if _, err := svc.Load(t.Context(), settingsTestEnv(t)); err != nil {
		t.Fatalf("settings.Load: %v", err)
	}
	return New(Config{
		Client:   &http.Client{},
		DB:       &database.DB{Client: client},
		Settings: svc,
	})
}

func TestSettingsRoutesAreAbsentWhenNotConfigured(t *testing.T) {
	r := setupMockRouter(New(Config{Client: &http.Client{}}))

	if code, _ := doJSON(t, r, "GET", "/api/settings", nil); code != http.StatusNotFound {
		t.Errorf("GET /api/settings = %d, want 404", code)
	}
	if code, _ := doJSON(t, r, "PUT", "/api/settings", json.RawMessage(`{"logLevel":"debug"}`)); code != http.StatusNotFound {
		t.Errorf("PUT /api/settings = %d, want 404", code)
	}
}

func TestSettingsGetMasksSecrets(t *testing.T) {
	r := setupMockRouter(newSettingsServer(t))

	code, body := doJSON(t, r, "GET", "/api/settings", nil)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", code, body)
	}

	var got struct {
		Persisted bool `json:"persisted"`
		Cookie    struct {
			MemberID   string `json:"memberId"`
			PassHash   string `json:"passHash"`
			Igneous    string `json:"igneous"`
			SK         string `json:"sk"`
			Configured bool   `json:"configured"`
		} `json:"cookie"`
		LogLevel string `json:"logLevel"`
		Storage  struct {
			Driver string `json:"driver"`
			Dir    string `json:"dir"`
		} `json:"storage"`
		DevTools bool `json:"devTools"`
	}
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if got.Cookie.PassHash != settings.Mask || got.Cookie.Igneous != settings.Mask || got.Cookie.SK != settings.Mask {
		t.Errorf("secrets = %+v, want them masked", got.Cookie)
	}
	if got.Cookie.MemberID != "member" {
		t.Errorf("memberId = %q, want it visible", got.Cookie.MemberID)
	}
	if !got.Cookie.Configured || got.LogLevel != "warn" || got.Storage.Driver != "local" {
		t.Errorf("payload = %+v, want the environment configuration", got)
	}
	if got.DevTools {
		t.Error("devTools = true, want false")
	}
}

func TestSettingsPutAppliesAndPersists(t *testing.T) {
	r := setupMockRouter(newSettingsServer(t))

	code, body := doJSON(t, r, "PUT", "/api/settings", json.RawMessage(`{"logLevel":"debug"}`))
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200. body: %s", code, body)
	}

	var updated struct {
		Persisted bool   `json:"persisted"`
		LogLevel  string `json:"logLevel"`
	}
	if err := json.Unmarshal(body, &updated); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if updated.LogLevel != "debug" || !updated.Persisted {
		t.Errorf("response = %+v, want debug persisted", updated)
	}

	// A fresh GET must agree with what was just saved.
	_, after := doJSON(t, r, "GET", "/api/settings", nil)
	if !strings.Contains(string(after), `"logLevel":"debug"`) {
		t.Errorf("GET after PUT = %s, want logLevel debug", after)
	}
}

func TestSettingsPutRejectsAnIncompleteCookie(t *testing.T) {
	r := setupMockRouter(newSettingsServer(t))

	code, body := doJSON(t, r, "PUT", "/api/settings", json.RawMessage(`{"cookie":{"memberId":"","passHash":""}}`))
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", code, body)
	}
	if !strings.Contains(string(body), "cookie") {
		t.Errorf("error = %s, want it to name the cookie problem", body)
	}

	// The rejected save must not have changed anything.
	_, after := doJSON(t, r, "GET", "/api/settings", nil)
	if !strings.Contains(string(after), `"memberId":"member"`) {
		t.Errorf("GET = %s, want the original configuration", after)
	}
}

func TestSettingsPutRejectsAnIncompleteStorageConfiguration(t *testing.T) {
	r := setupMockRouter(newSettingsServer(t))

	code, body := doJSON(t, r, "PUT", "/api/settings", json.RawMessage(`{"storage":{"driver":"s3"}}`))
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400. body: %s", code, body)
	}
	if !strings.Contains(string(body), "storage") && !strings.Contains(string(body), "S3") {
		t.Errorf("error = %s, want it to name the storage problem", body)
	}
}

func TestSettingsPutRejectsAMalformedPayload(t *testing.T) {
	r := setupMockRouter(newSettingsServer(t))

	code, _ := doJSON(t, r, "PUT", "/api/settings", `{"logLevel":`)
	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want 400", code)
	}
}
