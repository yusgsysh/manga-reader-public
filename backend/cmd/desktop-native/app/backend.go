package app

// Backend is the lifecycle of the API the UI talks to. The desktop app
// implements it around the in-process server so the first-run flow can start
// it once a cookie is entered. Tests and external-backend mode leave it nil,
// in which case the app assumes the client is already configured.
type Backend interface {
	// Configured reports whether the backend is running and can serve.
	Configured() bool
	// BaseURL is the address the UI should talk to; empty when not running.
	BaseURL() string
	// Start (re)starts the backend with the given raw cookie header and
	// returns the base URL to use.
	Start(cookie string) (string, error)
	// AuthToken is the per-run token the client must send. Empty when the
	// backend needs no token (external server).
	AuthToken() string
	// Storage reports display paths for the settings page.
	Storage() BackendStorage
	// ClearCache empties the on-disk image cache.
	ClearCache() error
	// CacheInfo reports the cache root, size and limit.
	CacheInfo() (CacheStats, error)
	// SetCacheMaxBytes changes the cache size limit in bytes; <= 0 disables
	// it, and the change is persisted.
	SetCacheMaxBytes(maxBytes int64) error
	// SetAppearance persists "system", "light" or "dark" and returns the
	// value in effect.
	SetAppearance(source string) error
	// Appearance is the saved appearance ("", "system", "light" or "dark").
	Appearance() string
}

// BackendStorage is the set of paths the settings page shows.
type BackendStorage struct {
	DBPath     string
	CacheDir   string
	ConfigPath string
}

// CacheStats is the image cache state shown on the settings page.
type CacheStats struct {
	Root     string
	Bytes    int64
	MaxBytes int64 // 0 means unlimited
}
