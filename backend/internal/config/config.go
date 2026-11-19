package config

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

type Config struct {
	Port     string
	Database DatabaseConfig
	// ImageCache configures the optional image cache object store (read-through
	// page-image/thumbnail cache). Enabled is derived from the presence of the
	// IMAGE_CACHE_* variables.
	ImageCache StorageConfig
	// ZipArchive configures the optional ZIP archive object store, fully
	// independent from ImageCache. Enabled is opt-in via ZIP_ENABLED.
	ZipArchive StorageConfig
	LogLevel   string
	// Sync configures the peer-facing sync host. User sync keys are stored in
	// the database, never in the environment.
	Sync SyncConfig
	// DevTools enables the /api/dev/* debug endpoints (off by default). They
	// exist so a deployment can simulate an ExHentai outage at runtime to
	// verify the frontend's offline fallback.
	DevTools bool
	// Metrics configures the internal Prometheus endpoint.
	Metrics MetricsConfig
	// Auth holds the local secret used to encrypt per-user ExHentai cookies
	// and the browser session-cookie attributes. User login itself is handled
	// by Kinde (see Kinde).
	Auth AuthConfig
	// Kinde configures OIDC access-token verification for every business API.
	Kinde KindeConfig
}

// AuthConfig holds local secrets that are NOT part of the user login flow.
// JWTSecret derives the AES-256-GCM key that seals each user's ExHentai
// cookie; it may be empty at load time and is then resolved from the database
// (generated and persisted on first boot) via auth.ResolveSecret.
type AuthConfig struct {
	JWTSecret string
	// CookieSecure selects the Secure flag for the HttpOnly media session
	// cookie. Auto (the default) sets it when the request arrived over TLS or
	// a trusted proxy reported X-Forwarded-Proto=https.
	CookieSecure     bool
	CookieSecureAuto bool
}

// KindeConfig identifies the Kinde tenant and the API audience that access
// tokens must be minted for. Issuer and audience are intentionally separate:
// the issuer is the tenant domain, the audience is the API resource.
type KindeConfig struct {
	Issuer   string
	Audience string
	// AdminEmails/AdminSubjects are optional operator allowlists. A user
	// whose verified email or Kinde subject matches gets the local "admin"
	// role (bootstrap for a fresh instance). They are not authentication
	// mechanisms; authorization still comes from users.role.
	AdminEmails   []string
	AdminSubjects []string
}

// MetricsConfig controls the internal metrics/readiness surface. When Token
// is empty, /metrics only answers loopback clients; with a token it requires
// "Authorization: Bearer <token>" (independent from JWT and sync keys).
type MetricsConfig struct {
	Enabled bool
	Token   string
}

// SyncConfig configures the sync host. User sync keys remain database-managed.
type SyncConfig struct {
	Enabled        bool
	RequestTimeout time.Duration
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

// CookieConfig holds one user's ExHentai session. It is never read from the
// environment: each account submits its own cookie through the settings API
// and the server stores it encrypted (see internal/usercookie).
type CookieConfig struct {
	IpbMemberID string
	IpbPassHash string
	Igneous     string
	SK          string
}

func (c CookieConfig) IsValid() bool {
	return c.IpbMemberID != "" && c.IpbPassHash != ""
}

// MergedWith returns c with every non-empty field of other applied on top. It
// lets the settings API update individual cookies (e.g. a freshly rotated
// igneous) without resubmitting the credentials that are still valid.
func (c CookieConfig) MergedWith(other CookieConfig) CookieConfig {
	if other.IpbMemberID != "" {
		c.IpbMemberID = other.IpbMemberID
	}
	if other.IpbPassHash != "" {
		c.IpbPassHash = other.IpbPassHash
	}
	if other.Igneous != "" {
		c.Igneous = other.Igneous
	}
	if other.SK != "" {
		c.SK = other.SK
	}
	return c
}

// StorageConfig configures one object-storage backend. Both the image cache and
// the ZIP archive use it, but the two instances are fully independent: they can
// target different providers, endpoints, buckets and credentials, or share any
// subset of them.
type StorageConfig struct {
	// Enabled gates whether this backend is initialized at all. The image cache
	// derives it from IsConfigured(); the archive reads ZIP_ENABLED explicitly.
	Enabled   bool
	Provider  string
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Prefix    string

	// Concurrency bounds how many pages the archive fetches in parallel while
	// building a ZIP (order is still preserved). 0 or 1 means serial.
	Concurrency int
	// PartSize is the multipart upload part size in bytes. 0 selects the SDK
	// default (16 MiB); a positive value must be at least 5 MiB.
	PartSize int64

	// PresignDownload redirects ZIP archive downloads to a signed object-store
	// URL instead of proxying the bytes through the backend. Archive only.
	PresignDownload bool
	// PresignExpiry is the lifetime of a signed download URL.
	PresignExpiry time.Duration
}

// IsConfigured reports whether the required connection fields are all present.
// A disabled backend is not required to be configured.
func (c StorageConfig) IsConfigured() bool {
	return c.Endpoint != "" && c.AccessKey != "" && c.SecretKey != "" && c.Bucket != ""
}

func Load() (*Config, error) {
	dbDSN := getEnv("MANGA_READER_DB_DSN", "")
	if dbDSN == "" {
		dbDSN = getEnv("DATABASE_URL", "")
	}

	cfg := &Config{
		// Default to loopback: a bare `go run`/binary must not expose the app
		// on every interface. Containers explicitly set EHENTAI_PORT=:8080.
		Port: getEnv("EHENTAI_PORT", "127.0.0.1:8080"),
		Database: DatabaseConfig{
			Driver: strings.ToLower(strings.TrimSpace(getEnv("MANGA_READER_DB_DRIVER", "sqlite"))),
			Path:   getEnv("MANGA_READER_DB_PATH", "data/manga-reader.db"),
			DSN:    dbDSN,
		},
		LogLevel: getEnv("LOG_LEVEL", "warn"),
		DevTools: parseBoolEnv("MANGA_READER_DEV_TOOLS", false),
		Sync: SyncConfig{
			Enabled:        parseBoolEnv("MANGA_READER_SYNC_ENABLED", true),
			RequestTimeout: 30 * time.Second,
		},
		ImageCache: loadImageCache(),
	}

	metricsCfg, err := loadMetrics()
	if err != nil {
		return nil, err
	}
	cfg.Metrics = metricsCfg

	if raw := strings.TrimSpace(getEnv("MANGA_READER_SYNC_REQUEST_TIMEOUT", "")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < time.Second || d > 5*time.Minute {
			return nil, fmt.Errorf("MANGA_READER_SYNC_REQUEST_TIMEOUT must be a duration between 1s and 5m")
		}
		cfg.Sync.RequestTimeout = d
	}
	authCfg, err := loadAuth()
	if err != nil {
		return nil, err
	}
	cfg.Auth = authCfg
	cfg.Kinde = loadKinde()

	zipArchive, err := loadZipArchive()
	if err != nil {
		return nil, err
	}
	cfg.ZipArchive = zipArchive

	switch {
	case cfg.Database.IsPostgres():
		if cfg.Database.DSN == "" {
			return nil, fmt.Errorf("missing postgres DSN: set MANGA_READER_DB_DSN or DATABASE_URL")
		}
	case cfg.Database.Driver == "" || cfg.Database.Driver == "sqlite":
	default:
		return nil, fmt.Errorf("unsupported MANGA_READER_DB_DRIVER %q: use sqlite or postgres", cfg.Database.Driver)
	}

	return cfg, nil
}

// loadMetrics reads the independent metrics token. An empty token keeps the
// endpoint loopback-only; a configured token must be strong enough to stand
// on its own and is never shared with JWT or sync keys.
func loadMetrics() (MetricsConfig, error) {
	cfg := MetricsConfig{Enabled: parseBoolEnv("MANGA_READER_METRICS_ENABLED", true)}
	token := strings.TrimSpace(getEnv("MANGA_READER_METRICS_TOKEN", ""))
	if token != "" && len(token) < 32 {
		return MetricsConfig{}, fmt.Errorf("MANGA_READER_METRICS_TOKEN must be at least 32 characters")
	}
	cfg.Token = token
	return cfg, nil
}

// loadImageCache maps the IMAGE_CACHE_* environment variables onto the image
// cache storage backend. It stays implicit: the cache is enabled exactly when
// the required connection fields are present, so no explicit enable flag is
// needed. The image cache keeps its own content-addressed sub-prefixes
// (images/, thumbnail/, …), so it has no operator-configurable Prefix.
func loadImageCache() StorageConfig {
	c := StorageConfig{
		Provider:  strings.ToLower(strings.TrimSpace(getEnv("IMAGE_CACHE_PROVIDER", "minio"))),
		Endpoint:  getEnv("IMAGE_CACHE_ENDPOINT", ""),
		AccessKey: getEnv("IMAGE_CACHE_ACCESS_KEY", ""),
		SecretKey: getEnv("IMAGE_CACHE_SECRET_KEY", ""),
		Bucket:    getEnv("IMAGE_CACHE_BUCKET", ""),
		UseSSL:    parseBoolEnv("IMAGE_CACHE_USE_SSL", false),
		Region:    getEnv("IMAGE_CACHE_REGION", ""),
	}
	c.Enabled = c.IsConfigured()
	return c
}

// loadZipArchive reads the independent ZIP_* archive backend. Unlike the image
// cache it is opt-in (ZIP_ENABLED), and validation only applies when enabled:
// a disabled archive never requires credentials.
func loadZipArchive() (StorageConfig, error) {
	c := StorageConfig{
		Provider:  strings.ToLower(strings.TrimSpace(getEnv("ZIP_PROVIDER", "minio"))),
		Endpoint:  getEnv("ZIP_ENDPOINT", ""),
		AccessKey: getEnv("ZIP_ACCESS_KEY", ""),
		SecretKey: getEnv("ZIP_SECRET_KEY", ""),
		Bucket:    getEnv("ZIP_BUCKET", ""),
		UseSSL:    parseBoolEnv("ZIP_USE_SSL", false),
		Region:    getEnv("ZIP_REGION", ""),
		Prefix:    getEnv("ZIP_PREFIX", "archives/"),
	}
	c.Enabled = parseBoolEnv("ZIP_ENABLED", false)

	concurrency := 4
	if raw := strings.TrimSpace(getEnv("ZIP_CONCURRENCY", "")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 1 || n > 64 {
			return StorageConfig{}, fmt.Errorf("ZIP_CONCURRENCY must be an integer between 1 and 64")
		}
		concurrency = n
	}
	c.Concurrency = concurrency

	if raw := strings.TrimSpace(getEnv("ZIP_PART_SIZE", "")); raw != "" {
		n, err := strconv.ParseInt(raw, 10, 64)
		if err != nil || n < 0 {
			return StorageConfig{}, fmt.Errorf("ZIP_PART_SIZE must be a non-negative integer byte count")
		}
		// S3 multipart requires parts >= 5 MiB (except the final part).
		if n > 0 && n < 5<<20 {
			return StorageConfig{}, fmt.Errorf("ZIP_PART_SIZE must be at least 5 MiB (5242880)")
		}
		c.PartSize = n
	}

	if !c.Enabled {
		return c, nil
	}
	if !c.IsConfigured() {
		return StorageConfig{}, fmt.Errorf("ZIP_ENABLED requires ZIP_ENDPOINT, ZIP_ACCESS_KEY, ZIP_SECRET_KEY and ZIP_BUCKET")
	}

	c.PresignDownload = parseBoolEnv("ZIP_PRESIGN_DOWNLOAD", false)
	c.PresignExpiry = 15 * time.Minute
	if raw := strings.TrimSpace(getEnv("ZIP_PRESIGN_EXPIRY", "")); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil || d < time.Second || d > 7*24*time.Hour {
			return StorageConfig{}, fmt.Errorf("ZIP_PRESIGN_EXPIRY must be a duration between 1s and 168h")
		}
		c.PresignExpiry = d
	}
	return c, nil
}

// loadAuth reads the local secret and cookie settings. Authentication is
// mandatory but delegated to Kinde; JWT_SECRET is still required because the
// same secret derives the encryption key for per-user ExHentai cookies.
func loadAuth() (AuthConfig, error) {
	secret := strings.TrimSpace(getEnv("JWT_SECRET", ""))

	// HMAC-SHA256 keys shorter than the 32-byte output weaken the signature.
	// Requiring 32 bytes also rejects placeholder values such as "change-me".
	// An empty secret is allowed: it is resolved from the database at startup.
	if secret != "" && len(secret) < 32 {
		return AuthConfig{}, fmt.Errorf("JWT_SECRET must be at least 32 bytes of random data (for example: openssl rand -hex 32)")
	}

	secureMode := strings.ToLower(strings.TrimSpace(getEnv("MANGA_READER_AUTH_COOKIE_SECURE", "auto")))
	cfg := AuthConfig{JWTSecret: secret}
	switch secureMode {
	case "", "auto":
		cfg.CookieSecureAuto = true
	case "true", "1", "yes":
		cfg.CookieSecure = true
	case "false", "0", "no":
		cfg.CookieSecure = false
	default:
		return AuthConfig{}, fmt.Errorf("invalid MANGA_READER_AUTH_COOKIE_SECURE value (want auto/true/false)")
	}

	return cfg, nil
}

// loadKinde reads the OIDC tenant configuration. Empty values are allowed at
// load time so tooling that only inspects the config (tests, maintenance
// commands) keeps working; main refuses to start without them because every
// business API is protected by Kinde verification.
func loadKinde() KindeConfig {
	issuer := strings.TrimRight(strings.TrimSpace(getEnv("KINDE_ISSUER", "")), "/")
	audience := strings.TrimSpace(getEnv("KINDE_API_AUDIENCE", ""))

	var admins []string
	for _, email := range strings.Split(getEnv("KINDE_ADMIN_EMAILS", ""), ",") {
		if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
			admins = append(admins, email)
		}
	}
	var subjects []string
	for _, subject := range strings.Split(getEnv("KINDE_ADMIN_SUBJECTS", ""), ",") {
		if subject = strings.TrimSpace(subject); subject != "" {
			subjects = append(subjects, subject)
		}
	}

	return KindeConfig{
		Issuer:        issuer,
		Audience:      audience,
		AdminEmails:   admins,
		AdminSubjects: subjects,
	}
}

func getEnv(key, defaultVal string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return defaultVal
}

func parseBoolEnv(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
}

// ParseCookieString parses a browser-style "k=v; k=v" ExHentai cookie header.
func ParseCookieString(s string) CookieConfig { return parseCookieString(s) }

// Encode renders the session as a browser-style cookie header. Secrets are
// only ever handled in memory; callers must not log the result.
func (c CookieConfig) Encode() string {
	parts := make([]string, 0, 4)
	if c.IpbMemberID != "" {
		parts = append(parts, "ipb_member_id="+c.IpbMemberID)
	}
	if c.IpbPassHash != "" {
		parts = append(parts, "ipb_pass_hash="+c.IpbPassHash)
	}
	if c.Igneous != "" {
		parts = append(parts, "igneous="+c.Igneous)
	}
	if c.SK != "" {
		parts = append(parts, "sk="+c.SK)
	}
	return strings.Join(parts, "; ")
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
		value := normalizeCookieValue(parts[1])
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

// normalizeCookieValue removes what copying a cookie out of browser devtools or
// a shell wrapper leaves around the value: whitespace, surrounding quotes and a
// trailing separator. A stray space in ipb_pass_hash or igneous is enough for
// ExHentai to reject the session.
func normalizeCookieValue(v string) string {
	v = strings.TrimSpace(v)
	if len(v) >= 2 && (v[0] == '"' || v[0] == '\'') && v[len(v)-1] == v[0] {
		v = strings.TrimSpace(v[1 : len(v)-1])
	}
	return strings.TrimRight(strings.TrimSpace(v), ";,")
}
