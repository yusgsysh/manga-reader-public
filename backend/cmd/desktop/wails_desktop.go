//go:build !android

package main

import (
	"os"
	"path/filepath"
)

// desktopDataDir is the per-user directory holding the SQLite database, the
// log file and the optional config.env.
func desktopDataDir() string {
	dir, err := os.UserConfigDir()
	if err != nil || dir == "" {
		return filepath.Join("data", "manga-reader")
	}
	return filepath.Join(dir, "manga-reader")
}

// desktopCacheDir is where LocalStorage keeps cached images.
func desktopCacheDir() string {
	dir, err := os.UserCacheDir()
	if err != nil || dir == "" {
		return filepath.Join(desktopDataDir(), "cache")
	}
	return filepath.Join(dir, "manga-reader", "cache")
}

// applyDesktopDefaults makes the desktop build behave the way a desktop app
// should: SQLite plus local file storage under the user's directories. Every
// value is opt-out, so explicit environment variables still win.
func applyDesktopDefaults() {
	setDefault("MANGA_READER_DB_DRIVER", "sqlite")
	setDefault("MANGA_READER_DB_PATH", filepath.Join(desktopDataDir(), "manga-reader.db"))
	setDefault("MANGA_READER_STORAGE_DRIVER", "local")
	setDefault("MANGA_READER_STORAGE_DIR", desktopCacheDir())
	setDefault("ENVIRONMENT", "desktop")
	setDefault("LOG_LEVEL", "info")
}

// dataDir returns the platform-specific data directory.
func dataDir() string {
	return desktopDataDir()
}

// cacheDir returns the platform-specific cache directory.
func cacheDir() string {
	return desktopCacheDir()
}

// applyPlatformDefaults applies platform-specific defaults.
func applyPlatformDefaults() {
	applyDesktopDefaults()
}

// envFilePath returns the platform-specific env file path.
func envFilePath() string {
	return filepath.Join(desktopDataDir(), "config.env")
}

// runtimeConfigPathValue returns the platform-specific runtime config path.
func runtimeConfigPathValue() string {
	return filepath.Join(os.TempDir(), "manga-reader-desktop.json")
}

// logFilePath returns the platform-specific log file path.
func logFilePath() string {
	return filepath.Join(desktopDataDir(), "manga-reader.log")
}
