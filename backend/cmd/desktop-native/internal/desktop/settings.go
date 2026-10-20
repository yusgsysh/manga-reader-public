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
	"strconv"
	"strings"

	"manga-reader/internal/config"
)

const (
	appDirName      = "manga-reader"
	configFileName  = "config.json"
	databaseName    = "manga-reader.db"
	cacheDirName    = "images"
	fallbackDataDir = "data"

	// DefaultCacheMaxBytes bounds the image cache when the user has not set a
	// limit. Stored/compared as bytes.
	DefaultCacheMaxBytes int64 = 2 << 30 // 2 GiB
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
	// CacheMaxBytes bounds the image cache. nil means "use the default",
	// 0 means unlimited, and a positive value is the limit in bytes.
	CacheMaxBytes *int64 `json:"cache_max_bytes,omitempty"`
	// Appearance is "system" (the default), "light" or "dark".
	Appearance string `json:"appearance,omitempty"`
	// Language is the interface language tag. Only "zh-CN" is translated;
	// empty means the system's tag, which the UI shows as 简体中文 when the
	// system asks for Chinese.
	Language string `json:"language,omitempty"`
	// CookieInKeyring records that the cookie lives in the OS credential
	// store rather than this file, so Load knows to fetch it from there.
	CookieInKeyring bool `json:"cookie_in_keyring,omitempty"`
}

// AppearanceSource validates Appearance for mygo.Theme.SetSource: an unknown
// value falls back to following the system.
func (s Settings) AppearanceSource() string {
	switch s.Appearance {
	case "light", "dark":
		return s.Appearance
	}
	return "system"
}

// LanguageOrDefault returns the interface language tag: the saved one, or the
// system's, and "zh-CN" when the system asks for no language the UI has.
func (s Settings) LanguageOrDefault() string {
	return languageOrDefault(s.Language)
}

// languageOrDefault resolves an interface language tag to the one the UI is
// translated in. The system's own tag is kept when it asks for Chinese, so the
// settings page can show which variant the system wants; every other tag
// becomes "zh-CN", since Chinese is the only translation the UI has and
// falling back to English text it does not contain would be worse.
func languageOrDefault(tag string) string {
	if tag != "" {
		return tag
	}
	for _, v := range []string{os.Getenv("LC_ALL"), os.Getenv("LC_MESSAGES"), os.Getenv("LANG")} {
		lower := strings.ToLower(v)
		if strings.Contains(lower, "zh_tw") || strings.Contains(lower, "zh-tw") ||
			strings.Contains(lower, "zh_hk") || strings.Contains(lower, "zh-hk") {
			return "zh-TW"
		}
		if strings.Contains(lower, "zh") {
			return "zh-CN"
		}
	}
	return "zh-CN"
}

// CookieConfig parses Cookie into the structured cookie config.
func (s Settings) CookieConfig() config.CookieConfig {
	return config.ParseCookieString(s.Cookie)
}

// CacheLimit returns the effective image cache limit in bytes; 0 means
// unlimited.
func (s Settings) CacheLimit() int64 {
	if s.CacheMaxBytes == nil {
		return DefaultCacheMaxBytes
	}
	if *s.CacheMaxBytes < 0 {
		return 0
	}
	return *s.CacheMaxBytes
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
	// The cookie may live in the credential store rather than the file; the
	// file's flag decides. A store miss (the entry was deleted, a different
	// user, the store went away) falls back to whatever the file holds.
	if s.CookieInKeyring && s.Cookie == "" {
		if cookie, err := keyringLoad(); err == nil && cookie != "" {
			s.Cookie = cookie
		}
	}
	return settingsFromEnv(s), nil
}

// SaveSettings persists the settings. The cookie goes to the OS credential
// store when one answers (so it stops sitting in a plain-text file) and the
// rest to config.json; the file's cookie field is then cleared. When no
// store is available the cookie stays in config.json, written 0600 as
// before. Either way the file remains the source of the non-secret settings.
func SaveSettings(s Settings) error {
	dir, err := ConfigDir()
	if err != nil {
		return err
	}
	if s.Cookie != "" && KeyringAvailable() {
		if err := keyringSave(s.Cookie); err == nil {
			// Stored: keep the file free of the secret. A stale file copy
			// from before the keyring is overwritten with an empty field.
			s.Cookie = ""
			// Remember that the keyring holds it, so a load without the
			// file's cookie still reaches the engine.
			s.CookieInKeyring = true
		}
	} else if s.Cookie == "" && s.CookieInKeyring {
		// The caller cleared the cookie (a logout): drop it from the store.
		_ = keyringDelete()
		s.CookieInKeyring = false
	}
	return saveSettingsTo(dir, s)
}

func saveSettingsTo(dir string, s Settings) error {
	raw, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("encode settings: %w", err)
	}
	raw = append(raw, '\n')
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("create config dir: %w", err)
	}

	path := filepath.Join(dir, configFileName)
	tmp, err := os.CreateTemp(dir, ".config-*.json")
	if err != nil {
		return fmt.Errorf("create temp config: %w", err)
	}
	tmpName := tmp.Name()
	cleanup := func() { _ = os.Remove(tmpName) }

	if _, err := tmp.Write(raw); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("write temp config: %w", err)
	}
	if err := tmp.Chmod(0o600); err != nil {
		_ = tmp.Close()
		cleanup()
		return fmt.Errorf("chmod temp config: %w", err)
	}
	if err := tmp.Close(); err != nil {
		cleanup()
		return fmt.Errorf("close temp config: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		cleanup()
		return fmt.Errorf("rename temp config: %w", err)
	}
	return nil
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
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_CACHE_MAX_BYTES")); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil && n >= 0 {
			s.CacheMaxBytes = &n
		}
	}
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_SYNC_TOKEN")); v != "" {
		s.SyncToken = v
	}
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_DEV_TOOLS")); v != "" {
		s.DevTools = strings.EqualFold(v, "true") || v == "1" || strings.EqualFold(v, "yes")
	}
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_APPEARANCE")); v != "" {
		s.Appearance = v
	}
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_LANGUAGE")); v != "" {
		s.Language = v
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
