// Package desktop runs the Manga Reader API inside the native desktop
// process. The shipped binary is self-contained: no external server, no
// MinIO, no reverse proxy, no web frontend. It reuses the existing handlers,
// database and upstream client, and swaps the object store for a local
// filesystem cache.
package desktop

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"manga-reader/internal/config"
)

const (
	appDirName      = "manga-reader"
	configFileName  = "config.json"
	databaseName    = "manga-reader.db"
	cacheDirName    = "images"
	fallbackDataDir = "data"
)

// Settings is the on-disk desktop configuration. It lives in the user config
// directory (e.g. ~/.config/manga-reader/config.json) and is the single place
// the app reads cookies and storage paths from. Environment variables still
// override it, which keeps the CLI/CI story from the server working.
type Settings struct {
	// Cookie is a raw cookie header, e.g.
	// "ipb_member_id=1; ipb_pass_hash=abc; igneous=...; sk=...".
	Cookie       string `json:"cookie,omitempty"`
	DatabasePath string `json:"database_path,omitempty"`
	CacheDir     string `json:"cache_dir,omitempty"`
	SyncToken    string `json:"sync_token,omitempty"`
	DevTools     bool   `json:"dev_tools,omitempty"`
}

// CookieConfig parses Cookie into the structured cookie config.
func (s Settings) CookieConfig() config.CookieConfig {
	return config.ParseCookieString(s.Cookie)
}

// DatabasePathOrDefault returns the configured database path or the per-user
// default.
func (s Settings) DatabasePathOrDefault() string {
	if s.DatabasePath != "" {
		return s.DatabasePath
	}
	return defaultDatabasePath()
}

// CacheDirOrDefault returns the configured cache directory or the per-user
// default.
func (s Settings) CacheDirOrDefault() string {
	if s.CacheDir != "" {
		return s.CacheDir
	}
	return defaultCacheDir()
}

// ConfigDir returns the per-user config directory, creating it when needed.
func ConfigDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("resolve user config dir: %w", err)
	}
	dir := filepath.Join(base, appDirName)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("create config dir: %w", err)
	}
	return dir, nil
}

// SettingsPath returns the config.json path, or "" when the user config
// directory cannot be resolved.
func SettingsPath() string {
	dir, err := ConfigDir()
	if err != nil {
		return ""
	}
	return filepath.Join(dir, configFileName)
}

// LoadSettings reads config.json (when present) and applies environment
// overrides. A missing file is not an error: the app can still start and
// report that cookies are missing.
func LoadSettings() (Settings, error) {
	dir, err := ConfigDir()
	if err != nil {
		// Fall back to environment-only settings rather than refusing to boot.
		return settingsFromEnv(Settings{}), nil
	}
	return loadSettingsFrom(dir)
}

func loadSettingsFrom(dir string) (Settings, error) {
	var s Settings
	raw, err := os.ReadFile(filepath.Join(dir, configFileName))
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s); err != nil {
			return s, fmt.Errorf("parse %s: %w", filepath.Join(dir, configFileName), err)
		}
	case errors.Is(err, fs.ErrNotExist):
	default:
		return s, fmt.Errorf("read %s: %w", filepath.Join(dir, configFileName), err)
	}
	return settingsFromEnv(s), nil
}

// settingsFromEnv overlays environment variables on top of file settings.
func settingsFromEnv(s Settings) Settings {
	if v := strings.TrimSpace(os.Getenv("EHENTAI_COOKIE")); v != "" {
		s.Cookie = v
	} else {
		// Individual cookie variables are the other supported form.
		if id, hash := os.Getenv("EHENTAI_COOKIE_IPB_MEMBER_ID"), os.Getenv("EHENTAI_COOKIE_IPB_PASS_HASH"); id != "" && hash != "" {
			parts := []string{"ipb_member_id=" + id, "ipb_pass_hash=" + hash}
			if v := os.Getenv("EHENTAI_COOKIE_IGNEOUS"); v != "" {
				parts = append(parts, "igneous="+v)
			}
			if v := os.Getenv("EHENTAI_COOKIE_SK"); v != "" {
				parts = append(parts, "sk="+v)
			}
			s.Cookie = strings.Join(parts, "; ")
		}
	}
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_DB_PATH")); v != "" {
		s.DatabasePath = v
	}
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_CACHE_DIR")); v != "" {
		s.CacheDir = v
	}
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_SYNC_TOKEN")); v != "" {
		s.SyncToken = v
	}
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_DEV_TOOLS")); v != "" {
		s.DevTools = strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
	}
	return s
}

func defaultDatabasePath() string {
	dir, err := ConfigDir()
	if err != nil {
		return filepath.Join(fallbackDataDir, databaseName)
	}
	return filepath.Join(dir, databaseName)
}

func defaultCacheDir() string {
	base, err := os.UserCacheDir()
	if err != nil {
		return filepath.Join(fallbackDataDir, cacheDirName)
	}
	return filepath.Join(base, appDirName, cacheDirName)
}
