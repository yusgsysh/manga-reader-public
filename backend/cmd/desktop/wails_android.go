//go:build android

package main

import (
	"os"
	"path/filepath"
)

// androidDataDir returns the app-specific data directory on Android.
// This is the only writable location for the app.
func androidDataDir() string {
	// On Android, use the app's internal data directory
	dir := os.Getenv("ANDROID_DATA_DIR")
	if dir == "" {
		// Fallback to a reasonable default
		dir = "/data/data/com.manga.reader/files"
	}
	return dir
}

// androidCacheDir returns the cache directory on Android.
func androidCacheDir() string {
	dir := os.Getenv("ANDROID_CACHE_DIR")
	if dir == "" {
		dir = filepath.Join(androidDataDir(), "cache")
	}
	return dir
}

// applyAndroidDefaults sets Android-specific defaults.
func applyAndroidDefaults() {
	setDefault("MANGA_READER_DB_DRIVER", "sqlite")
	setDefault("MANGA_READER_DB_PATH", filepath.Join(androidDataDir(), "manga-reader.db"))
	setDefault("MANGA_READER_STORAGE_DRIVER", "local")
	setDefault("MANGA_READER_STORAGE_DIR", androidCacheDir())
	setDefault("ENVIRONMENT", "android")
	setDefault("LOG_LEVEL", "info")
}

// dataDir returns the platform-specific data directory.
func dataDir() string {
	return androidDataDir()
}

// cacheDir returns the platform-specific cache directory.
func cacheDir() string {
	return androidCacheDir()
}

// applyPlatformDefaults applies platform-specific defaults.
func applyPlatformDefaults() {
	applyAndroidDefaults()
}

// envFilePath returns the platform-specific env file path.
func envFilePath() string {
	return filepath.Join(androidDataDir(), "config.env")
}

// runtimeConfigPathValue returns the platform-specific runtime config path.
func runtimeConfigPathValue() string {
	return filepath.Join(androidDataDir(), "runtime-config.json")
}

// logFilePath returns the platform-specific log file path.
func logFilePath() string {
	return filepath.Join(androidDataDir(), "manga-reader.log")
}