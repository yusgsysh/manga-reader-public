package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

type Config struct {
	Port        string
	Database    DatabaseConfig
	Cookie      CookieConfig
	Storage     StorageConfig
	LogLevel    string
	Environment string
	// DevTools enables the /api/dev/* debug endpoints (off by default). They
	// exist so a deployment can simulate an ExHentai outage at runtime to
	// verify the frontend's offline fallback.
	DevTools bool
}

// DatabaseConfig selects the database backend. Driver defaults to "sqlite";
// "postgres" (alias "postgresql"/"pg") switches to PostgreSQL.
type DatabaseConfig struct {
	Driver string
	// Path is the SQLite database file path (used when Driver is sqlite).
	Path string
	// DSN is the PostgreSQL connection string (used when Driver is postgres).
	DSN string
}

func (c DatabaseConfig) IsPostgres() bool {
	return c.Driver == "postgres" || c.Driver == "postgresql" || c.Driver == "pg"
}

type CookieConfig struct {
	IpbMemberID string
	IpbPassHash string
	Igneous     string
	SK          string
}

func (c CookieConfig) IsValid() bool {
	return c.IpbMemberID != "" && c.IpbPassHash != ""
}

// Storage drivers supported by MANGA_READER_STORAGE_DRIVER.
const (
	// StorageDriverAuto picks S3 when a complete S3 configuration is present
	// (the behaviour of every deployment that already configured MinIO) and
	// falls back to local files otherwise.
	StorageDriverAuto = "auto"
	// StorageDriverLocal keeps cached objects in plain files on disk.
	StorageDriverLocal = "local"
	// StorageDriverS3 stores objects in any S3-compatible service.
	StorageDriverS3 = "s3"
)

// StorageConfig selects where cached images are stored. The business layer
// only ever sees storage.Storage; this config decides which implementation it
// gets.
type StorageConfig struct {
	// Driver is one of auto/local/s3. Empty is treated as auto.
	Driver string
	// Dir is the cache root used by the local driver.
	Dir string
	// S3 is used by the s3 driver (and to resolve auto).
	S3 S3Config
}

// ResolvedDriver returns the concrete driver ("local" or "s3") that will be
// used, applying the auto-detection rule.
func (c StorageConfig) ResolvedDriver() string {
	switch c.Driver {
	case StorageDriverS3:
		return StorageDriverS3
	case StorageDriverLocal:
		return StorageDriverLocal
	default:
		if c.S3.IsComplete() {
			return StorageDriverS3
		}
		return StorageDriverLocal
	}
}

// S3Config describes an S3-compatible object store. MinIO, AWS S3, Cloudflare
// R2 and friends all fit: only the endpoint/credentials differ.
type S3Config struct {
	// Endpoint is host:port (or a full https:// host). No scheme is required;
	// UseSSL decides the scheme. Examples: minio:9000, s3.amazonaws.com.
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	// PathStyle is "auto" (default), "path" (ForcePathStyle, needed by some
	// MinIO/R2 setups) or "dns" (virtual-hosted style, AWS default).
	PathStyle string
}

// IsComplete reports whether every required S3 field is present.
func (c S3Config) IsComplete() bool {
	return len(c.Missing()) == 0
}

// Missing lists the required S3 fields that are still empty.
func (c S3Config) Missing() []string {
	var missing []string
	if c.Endpoint == "" {
		missing = append(missing, "endpoint")
	}
	if c.Bucket == "" {
		missing = append(missing, "bucket")
	}
	if c.AccessKey == "" {
		missing = append(missing, "access key")
	}
	if c.SecretKey == "" {
		missing = append(missing, "secret key")
	}
	return missing
}

// EnvLookup returns the value of an environment-style configuration key. An
// empty result means "unset" and falls back to the built-in default.
type EnvLookup func(key string) string

// Load reads configuration from the process environment.
func Load() (*Config, error) {
	return LoadWith(os.Getenv)
}

// LoadWith reads configuration through lookup. Persisted settings are layered
// on top of the process environment by giving a lookup that consults the
// settings store first: a key the settings page has saved always wins, so a
// deployment can be reconfigured without touching .env or the compose file.
func LoadWith(lookup EnvLookup) (*Config, error) {
	l := envReader{lookup: lookup}

	dbDSN := l.get("MANGA_READER_DB_DSN", "")
	if dbDSN == "" {
		dbDSN = l.get("DATABASE_URL", "")
	}

	cfg := &Config{
		Port: l.get("EHENTAI_PORT", ":8080"),
		Database: DatabaseConfig{
			Driver: strings.ToLower(strings.TrimSpace(l.get("MANGA_READER_DB_DRIVER", "sqlite"))),
			Path:   l.get("MANGA_READER_DB_PATH", "data/manga-reader.db"),
			DSN:    dbDSN,
		},
		LogLevel:    l.get("LOG_LEVEL", "warn"),
		Environment: l.get("ENVIRONMENT", "production"),
		DevTools:    l.getBool("MANGA_READER_DEV_TOOLS", false),
		Storage:     l.storageConfig(),
	}

	// EHENTAI_COOKIE is a bootstrap shortcut that supplies the whole cookie
	// header at once; the individual fields are more specific, so they win
	// whenever they are set (the settings page only ever writes those).
	cfg.Cookie = parseCookieString(l.get("EHENTAI_COOKIE", ""))
	if v := l.get("EHENTAI_COOKIE_IPB_MEMBER_ID", ""); v != "" {
		cfg.Cookie.IpbMemberID = v
	}
	if v := l.get("EHENTAI_COOKIE_IPB_PASS_HASH", ""); v != "" {
		cfg.Cookie.IpbPassHash = v
	}
	if v := l.get("EHENTAI_COOKIE_IGNEOUS", ""); v != "" {
		cfg.Cookie.Igneous = v
	}
	if v := l.get("EHENTAI_COOKIE_SK", ""); v != "" {
		cfg.Cookie.SK = v
	}

	// Cookies are intentionally not validated here: the settings page can set
	// them at runtime, so a missing cookie must not stop the server from
	// booting (otherwise there would be no UI to fix it with).

	switch {
	case cfg.Database.IsPostgres():
		if cfg.Database.DSN == "" {
			return nil, fmt.Errorf("missing postgres DSN: set MANGA_READER_DB_DSN or DATABASE_URL")
		}
	case cfg.Database.Driver == "" || cfg.Database.Driver == "sqlite":
	default:
		return nil, fmt.Errorf("unsupported MANGA_READER_DB_DRIVER %q: use sqlite or postgres", cfg.Database.Driver)
	}

	switch cfg.Storage.Driver {
	case "", StorageDriverAuto, StorageDriverLocal, StorageDriverS3:
	default:
		return nil, fmt.Errorf("unsupported MANGA_READER_STORAGE_DRIVER %q: use auto, local or s3", cfg.Storage.Driver)
	}
	if cfg.Storage.ResolvedDriver() == StorageDriverS3 {
		if missing := cfg.Storage.S3.Missing(); len(missing) > 0 {
			return nil, fmt.Errorf("incomplete S3 storage configuration: missing %s (set MANGA_READER_S3_* or MINIO_* variables)",
				strings.Join(missing, ", "))
		}
	}

	return cfg, nil
}

// ManagedKeys lists the environment variables the settings page owns. They are
// the only keys persisted by the settings store, which keeps database/port
// settings firmly restart-only.
var ManagedKeys = []string{
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

// envReader resolves configuration keys through a single injectable lookup.
type envReader struct {
	lookup EnvLookup
}

func (l envReader) get(key, defaultVal string) string {
	if v := l.lookup(key); v != "" {
		return v
	}
	return defaultVal
}

func (l envReader) getBool(key string, defaultVal bool) bool {
	v := l.lookup(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
}

// boolWithFallback reads key, falling back to legacyKey when key is unset.
func (l envReader) boolWithFallback(key, legacyKey string, defaultVal bool) bool {
	if v := l.lookup(key); v != "" {
		if b, err := strconv.ParseBool(v); err == nil {
			return b
		}
		return defaultVal
	}
	return l.getBool(legacyKey, defaultVal)
}

// storageConfig reads MANGA_READER_STORAGE_*/MANGA_READER_S3_* with a
// fallback to the legacy MINIO_* names, so existing deployments keep working
// unchanged.
func (l envReader) storageConfig() StorageConfig {
	s3 := S3Config{
		Endpoint:  l.get("MANGA_READER_S3_ENDPOINT", l.get("MINIO_ENDPOINT", "")),
		Region:    l.get("MANGA_READER_S3_REGION", l.get("MINIO_REGION", "")),
		Bucket:    l.get("MANGA_READER_S3_BUCKET", l.get("MINIO_BUCKET", "")),
		AccessKey: l.get("MANGA_READER_S3_ACCESS_KEY", l.get("MINIO_ACCESS_KEY", "")),
		SecretKey: l.get("MANGA_READER_S3_SECRET_KEY", l.get("MINIO_SECRET_KEY", "")),
		UseSSL:    l.boolWithFallback("MANGA_READER_S3_USE_SSL", "MINIO_USE_SSL", false),
		PathStyle: l.get("MANGA_READER_S3_PATH_STYLE", "auto"),
	}
	return StorageConfig{
		Driver: normalizeDriver(l.get("MANGA_READER_STORAGE_DRIVER", StorageDriverAuto)),
		Dir:    l.get("MANGA_READER_STORAGE_DIR", "data/cache"),
		S3:     s3,
	}
}

func normalizeDriver(d string) string {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "", StorageDriverAuto:
		return StorageDriverAuto
	case StorageDriverLocal, "fs", "file":
		return StorageDriverLocal
	case StorageDriverS3, "minio":
		return StorageDriverS3
	default:
		return strings.ToLower(strings.TrimSpace(d))
	}
}

func parseCookieString(s string) CookieConfig {
	cfg := CookieConfig{}
	for _, pair := range splitSeq(s, ";") {
		pair = trimSpace(pair)
		if pair == "" {
			continue
		}
		parts := splitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := trimSpace(parts[0])
		value := trimSpace(parts[1])
		switch key {
		case "ipb_member_id":
			cfg.IpbMemberID = value
		case "ipb_pass_hash":
			cfg.IpbPassHash = value
		case "igneous":
			cfg.Igneous = value
		case "sk":
			cfg.SK = value
		}
	}
	return cfg
}

func splitSeq(s, sep string) []string {
	var result []string
	start := 0
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			result = append(result, s[start:i])
			start = i + len(sep)
		}
	}
	result = append(result, s[start:])
	return result
}

func splitN(s, sep string, n int) []string {
	if n <= 0 {
		return []string{s}
	}
	for i := 0; i <= len(s)-len(sep); i++ {
		if s[i:i+len(sep)] == sep {
			return []string{s[:i], s[i+len(sep):]}
		}
	}
	return []string{s}
}

func trimSpace(s string) string {
	start := 0
	for start < len(s) && (s[start] == ' ' || s[start] == '\t' || s[start] == '\n' || s[start] == '\r') {
		start++
	}
	end := len(s)
	for end > start && (s[end-1] == ' ' || s[end-1] == '\t' || s[end-1] == '\n' || s[end-1] == '\r') {
		end--
	}
	return s[start:end]
}
