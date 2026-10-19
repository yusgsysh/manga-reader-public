package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"manga-reader/internal/cache"
	"manga-reader/internal/config"
	"manga-reader/internal/handler"
)

// clearCookieEnv makes settings tests independent of the developer's shell.
func clearCookieEnv(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"EHENTAI_COOKIE",
		"EHENTAI_COOKIE_IPB_MEMBER_ID",
		"EHENTAI_COOKIE_IPB_PASS_HASH",
		"EHENTAI_COOKIE_IGNEOUS",
		"EHENTAI_COOKIE_SK",
		"MANGA_READER_DB_PATH",
		"MANGA_READER_CACHE_DIR",
		"MANGA_READER_SYNC_TOKEN",
		"MANGA_READER_DEV_TOOLS",
	} {
		t.Setenv(key, "")
	}
}

func TestLoadSettingsFromFile(t *testing.T) {
	clearCookieEnv(t)
	dir := t.TempDir()

	raw, err := json.Marshal(Settings{
		Cookie:       "ipb_member_id=42; ipb_pass_hash=deadbeef",
		DatabasePath: "/tmp/db.sqlite",
		CacheDir:     "/tmp/cache",
		SyncToken:    "secret",
		DevTools:     true,
	})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, configFileName), raw, 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}

	s, err := loadSettingsFrom(dir)
	if err != nil {
		t.Fatalf("loadSettingsFrom: %v", err)
	}
	if s.Cookie != "ipb_member_id=42; ipb_pass_hash=deadbeef" {
		t.Fatalf("Cookie = %q", s.Cookie)
	}
	if s.DatabasePath != "/tmp/db.sqlite" || s.CacheDir != "/tmp/cache" {
		t.Fatalf("paths = %q / %q", s.DatabasePath, s.CacheDir)
	}
	if s.SyncToken != "secret" || !s.DevTools {
		t.Fatalf("sync/dev = %q / %v", s.SyncToken, s.DevTools)
	}
}

func TestLoadSettingsEnvOverridesFile(t *testing.T) {
	clearCookieEnv(t)
	dir := t.TempDir()

	if err := os.WriteFile(filepath.Join(dir, configFileName), []byte(`{"database_path":"/from/file"}`), 0o644); err != nil {
		t.Fatalf("write config: %v", err)
	}
	t.Setenv("EHENTAI_COOKIE", "ipb_member_id=1; ipb_pass_hash=2")
	t.Setenv("MANGA_READER_DB_PATH", "/from/env")

	s, err := loadSettingsFrom(dir)
	if err != nil {
		t.Fatalf("loadSettingsFrom: %v", err)
	}
	if s.DatabasePath != "/from/env" {
		t.Fatalf("DatabasePath = %q, want env override", s.DatabasePath)
	}
	if !s.CookieConfig().IsValid() {
		t.Fatalf("CookieConfig = %+v, want valid", s.CookieConfig())
	}
}

func TestLoadSettingsMissingFileIsNotAnError(t *testing.T) {
	clearCookieEnv(t)
	s, err := loadSettingsFrom(t.TempDir())
	if err != nil {
		t.Fatalf("loadSettingsFrom: %v", err)
	}
	if s.Cookie != "" {
		t.Fatalf("Cookie = %q, want empty", s.Cookie)
	}
}

func TestLoadSettingsIndividualCookieEnv(t *testing.T) {
	clearCookieEnv(t)
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "7")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "hash")
	t.Setenv("EHENTAI_COOKIE_IGNEOUS", "ig")

	s, err := loadSettingsFrom(t.TempDir())
	if err != nil {
		t.Fatalf("loadSettingsFrom: %v", err)
	}
	cfg := s.CookieConfig()
	if cfg.IpbMemberID != "7" || cfg.IpbPassHash != "hash" || cfg.Igneous != "ig" {
		t.Fatalf("CookieConfig = %+v", cfg)
	}
}

func TestSettingsPathsHaveDefaults(t *testing.T) {
	var s Settings
	if s.DatabasePathOrDefault() == "" {
		t.Fatal("DatabasePathOrDefault is empty")
	}
	if s.CacheDirOrDefault() == "" {
		t.Fatal("CacheDirOrDefault is empty")
	}
}

func testOptions(t *testing.T) Options {
	t.Helper()
	return Options{
		Cookie:   config.CookieConfig{IpbMemberID: "1", IpbPassHash: "fake-hash"},
		DBPath:   filepath.Join(t.TempDir(), "test.db"),
		CacheDir: filepath.Join(t.TempDir(), "images"),
	}
}

func TestStartRejectsMissingCookie(t *testing.T) {
	_, err := Start(context.Background(), Options{DBPath: filepath.Join(t.TempDir(), "t.db")})
	if !errors.Is(err, ErrMissingCookie) {
		t.Fatalf("Start err = %v, want ErrMissingCookie", err)
	}
}

func TestStartServesHealthzAndCloses(t *testing.T) {
	srv, err := Start(context.Background(), testOptions(t))
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	if srv.BaseURL()[:7] != "http://" {
		t.Fatalf("BaseURL = %q, want http:// prefix", srv.BaseURL())
	}

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(srv.BaseURL() + "/healthz")
	if err != nil {
		t.Fatalf("GET /healthz: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != `{"status":"ok"}` {
		t.Fatalf("body = %q", body)
	}

	// The DB-backed routes must be mounted too; an empty bookshelf is fine.
	resp2, err := client.Get(srv.BaseURL() + "/api/bookshelf")
	if err != nil {
		t.Fatalf("GET /api/bookshelf: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("/api/bookshelf status = %d, want 200", resp2.StatusCode)
	}

	if err := srv.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// Closing twice must not panic or error.
	if err := srv.Close(); err != nil {
		t.Fatalf("second Close: %v", err)
	}
}

// TestCachedThumbnailServedFromFilesystem proves the embedded server reads
// image bytes back out of the local filesystem cache: pre-populate the cache
// exactly as the first fetch would, then request the cached endpoint and
// expect the stored bytes without any upstream call.
func TestCachedThumbnailServedFromFilesystem(t *testing.T) {
	opts := testOptions(t)
	ctx := context.Background()

	fc, err := cache.NewFSCache(opts.CacheDir)
	if err != nil {
		t.Fatalf("NewFSCache: %v", err)
	}
	rawURL := "https://s.exhentai.org/t/abc.jpg"
	payload := []byte("cached-thumbnail-bytes")
	if err := fc.Put(ctx, handler.ThumbnailCacheKey(rawURL), payload, "image/png", ""); err != nil {
		t.Fatalf("seed cache: %v", err)
	}

	srv, err := Start(ctx, opts)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(srv.BaseURL() + "/api/image-cache/thumbnail?url=" + url.QueryEscape(rawURL))
	if err != nil {
		t.Fatalf("GET cached thumbnail: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "image/png" {
		t.Fatalf("Content-Type = %q, want image/png", ct)
	}
	body, _ := io.ReadAll(resp.Body)
	if string(body) != string(payload) {
		t.Fatalf("body = %q, want %q", body, payload)
	}
}
