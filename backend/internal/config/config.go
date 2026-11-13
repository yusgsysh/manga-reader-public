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
	MinIO    MinIOConfig
	LogLevel string
	// Sync configures the peer-facing sync host. Node API keys are stored in
	// the database, never in the environment.
	Sync SyncConfig
	// DevTools enables the /api/dev/* debug endpoints (off by default). They
	// exist so a deployment can simulate an ExHentai outage at runtime to
	// verify the frontend's offline fallback.
	DevTools bool
	// Metrics configures the internal Prometheus endpoint.
	Metrics MetricsConfig
	// Auth is the JWT authentication setup used by the SPA login page. Every
	// request that touches user data is authenticated.
	Auth AuthConfig
}

// AuthConfig configures the JWT login flow. The administrator row itself is
// stored in the database; ADMIN_USERNAME/ADMIN_PASSWORD are only used once,
// when the users table is empty, so changing the environment later never
// overwrites an existing password.
type AuthConfig struct {
	// JWTSecret signs HS256 tokens. It may be empty at load time: main.go then
	// resolves it from the database (a generated secret persisted on first
	// boot) via auth.ResolveSecret, or from the environment when set. The same
	// secret derives the AES-GCM key that seals per-user ExHentai cookies in
	// the database.
	JWTSecret string
	JWTIssuer string
	// JWTExpiresIn is embedded in exp/iat and returned as expires_in.
	JWTExpiresIn time.Duration
	// AdminUsername / AdminPassword seed the first administrator. The
	// password is hashed with Argon2id before it reaches the database.
	AdminUsername string
	AdminPassword string
	// CookieSecure selects the Secure flag for the HttpOnly auth cookie.
	// Auto (the default) sets it when the request arrived over TLS or a
	// trusted proxy reported X-Forwarded-Proto=https.
	CookieSecure     bool
	CookieSecureAuto bool
}

// ExpiresInSeconds is the token lifetime in whole seconds for the login
// response (expires_in).
func (c AuthConfig) ExpiresInSeconds() int {
	return int(c.JWTExpiresIn / time.Second)
}

// MetricsConfig controls the internal metrics/readiness surface. When Token
// is empty, /metrics only answers loopback clients; with a token it requires
// "Authorization: Bearer <token>" (independent from JWT and node API keys).
type MetricsConfig struct {
	Enabled bool
	Token   string
}

// SyncConfig configures the node sync host. API keys remain database-managed.
type SyncConfig struct {
	Enabled         bool
	RequestTimeout  time.Duration
	APIKeyMinLength int
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

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
	Region    string
}

func (c MinIOConfig) IsValid() bool {
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
			Enabled:         parseBoolEnv("MANGA_READER_SYNC_ENABLED", true),
			RequestTimeout:  30 * time.Second,
			APIKeyMinLength: 32,
		},
		MinIO: MinIOConfig{
			Endpoint:  getEnv("MINIO_ENDPOINT", ""),
			AccessKey: getEnv("MINIO_ACCESS_KEY", ""),
			SecretKey: getEnv("MINIO_SECRET_KEY", ""),
			Bucket:    getEnv("MINIO_BUCKET", ""),
			UseSSL:    parseBoolEnv("MINIO_USE_SSL", false),
			Region:    getEnv("MINIO_REGION", ""),
		},
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
	if raw := strings.TrimSpace(getEnv("MANGA_READER_SYNC_API_KEY_MIN_LENGTH", "")); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < 32 {
			return nil, fmt.Errorf("MANGA_READER_SYNC_API_KEY_MIN_LENGTH must be at least 32")
		}
		cfg.Sync.APIKeyMinLength = n
	}

	authCfg, err := loadAuth()
	if err != nil {
		return nil, err
	}
	cfg.Auth = authCfg

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
// on its own and is never shared with JWT or node API keys.
func loadMetrics() (MetricsConfig, error) {
	cfg := MetricsConfig{Enabled: parseBoolEnv("MANGA_READER_METRICS_ENABLED", true)}
	token := strings.TrimSpace(getEnv("MANGA_READER_METRICS_TOKEN", ""))
	if token != "" && len(token) < 32 {
		return MetricsConfig{}, fmt.Errorf("MANGA_READER_METRICS_TOKEN must be at least 32 characters")
	}
	cfg.Token = token
	return cfg, nil
}

// loadAuth reads the JWT/admin settings. Authentication is mandatory: every
// user-private API needs an identity. JWT_SECRET may be empty here — main.go
// then resolves the signing key from the database (or generates and persists
// one on first boot) via auth.ResolveSecret.
func loadAuth() (AuthConfig, error) {
	secret := strings.TrimSpace(getEnv("JWT_SECRET", ""))
	adminPassword := getEnv("ADMIN_PASSWORD", "")

	// HMAC-SHA256 keys shorter than the 32-byte output weaken the signature.
	// Requiring 32 bytes also rejects placeholder values such as "change-me".
	// An empty secret is allowed: it is resolved from the database at startup.
	if secret != "" && len(secret) < 32 {
		return AuthConfig{}, fmt.Errorf("JWT_SECRET must be at least 32 bytes of random data (for example: openssl rand -hex 32)")
	}

	issuer := strings.TrimSpace(getEnv("JWT_ISSUER", "manga-reader"))
	if issuer == "" {
		return AuthConfig{}, fmt.Errorf("JWT_ISSUER must not be empty")
	}

	expires := time.Hour
	if raw := strings.TrimSpace(getEnv("JWT_EXPIRES_IN", "")); raw != "" {
		parsed, err := time.ParseDuration(raw)
		if err != nil {
			// Also accept a bare integer number of seconds (3600).
			if seconds, convErr := strconv.Atoi(raw); convErr == nil {
				parsed = time.Duration(seconds) * time.Second
				err = nil
			}
		}
		if err != nil {
			return AuthConfig{}, fmt.Errorf("invalid JWT_EXPIRES_IN %q: use a duration such as 1h or 30m", raw)
		}
		if parsed < time.Minute || parsed > 30*24*time.Hour {
			return AuthConfig{}, fmt.Errorf("JWT_EXPIRES_IN must be between 1m and 720h")
		}
		expires = parsed
	}

	username := strings.TrimSpace(getEnv("ADMIN_USERNAME", "admin"))
	if username == "" {
		return AuthConfig{}, fmt.Errorf("ADMIN_USERNAME must not be empty")
	}

	secureMode := strings.ToLower(strings.TrimSpace(getEnv("MANGA_READER_AUTH_COOKIE_SECURE", "auto")))
	cfg := AuthConfig{
		JWTSecret:     secret,
		JWTIssuer:     issuer,
		JWTExpiresIn:  expires,
		AdminUsername: username,
		AdminPassword: adminPassword,
	}
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
