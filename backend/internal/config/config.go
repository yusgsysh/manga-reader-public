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
	Cookie   CookieConfig
	MinIO    MinIOConfig
	LogLevel string
	// SyncToken enables the peer-facing sync endpoints when non-empty
	// (/api/sync/push and /api/sync/events). Empty disables the sync host.
	SyncToken string
	// DevTools enables the /api/dev/* debug endpoints (off by default). They
	// exist so a deployment can simulate an ExHentai outage at runtime to
	// verify the frontend's offline fallback.
	DevTools bool
	// BasicAuth guards every route except /healthz with HTTP Basic Auth.
	// It replaced the reverse-proxy layer that used to sit in front of the
	// API, now that Gin serves the frontend too.
	BasicAuth BasicAuthConfig
	// Auth is the JWT authentication setup used by the SPA login page. It is
	// enabled by setting JWT_SECRET; without it the app keeps its legacy
	// anonymous single-user behaviour (and Basic Auth remains available).
	Auth AuthConfig
}

// AuthConfig configures the JWT login flow. The administrator row itself is
// stored in the database; ADMIN_USERNAME/ADMIN_PASSWORD are only used once,
// when the users table is empty, so changing the environment later never
// overwrites an existing password.
type AuthConfig struct {
	Enabled bool
	// JWTSecret signs HS256 tokens. It must be a high-entropy value of at
	// least 32 bytes; auth refuses to start with a weak/empty secret.
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

// BasicAuthConfig is the validated HTTP Basic Auth setup. Credentials come
// from exactly one source: a single user in the environment, or an htpasswd
// file (apr1 or bcrypt).
type BasicAuthConfig struct {
	Enabled bool
	// Username/Password hold the single environment-provided user.
	Username string
	Password string
	// File is the htpasswd path; empty when the credentials came from the
	// environment. It is kept for logging/diagnostics.
	File string
	// Hashes is the parsed htpasswd content, loaded once at startup so the
	// credential file does not have to be world-readable and every request
	// does not re-read it from disk.
	Hashes map[string]string
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
		LogLevel:  getEnv("LOG_LEVEL", "warn"),
		DevTools:  parseBoolEnv("MANGA_READER_DEV_TOOLS", false),
		SyncToken: getEnv("MANGA_READER_SYNC_TOKEN", ""),
		Cookie:    loadCookieConfig(),
		MinIO: MinIOConfig{
			Endpoint:  getEnv("MINIO_ENDPOINT", ""),
			AccessKey: getEnv("MINIO_ACCESS_KEY", ""),
			SecretKey: getEnv("MINIO_SECRET_KEY", ""),
			Bucket:    getEnv("MINIO_BUCKET", ""),
			UseSSL:    parseBoolEnv("MINIO_USE_SSL", false),
			Region:    getEnv("MINIO_REGION", ""),
		},
	}

	if cookieStr := getEnv("EHENTAI_COOKIE", ""); cookieStr != "" {
		cfg.Cookie = parseCookieString(cookieStr)
	}

	basicAuth, err := loadBasicAuth()
	if err != nil {
		return nil, err
	}
	cfg.BasicAuth = basicAuth

	authCfg, err := loadAuth()
	if err != nil {
		return nil, err
	}
	cfg.Auth = authCfg

	if !cfg.Cookie.IsValid() {
		return nil, fmt.Errorf("missing required cookies: set EHENTAI_COOKIE or EHENTAI_COOKIE_IPB_MEMBER_ID + EHENTAI_COOKIE_IPB_PASS_HASH")
	}

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

// loadBasicAuth reads and validates MANGA_READER_BASIC_AUTH_* the same way
// the old proxy layer did: an unparsable switch value or a missing credential
// source is a hard error, so a deployment can never come up anonymously by
// accident (fail closed).
func loadBasicAuth() (BasicAuthConfig, error) {
	enabled := strings.ToLower(strings.TrimSpace(getEnv("MANGA_READER_BASIC_AUTH_ENABLED", "false")))
	switch enabled {
	case "true", "1", "yes":
	case "false", "0", "no", "":
		return BasicAuthConfig{}, nil
	default:
		// Deliberately does not echo the raw value: mis-pasted secrets stay
		// out of the logs.
		return BasicAuthConfig{}, fmt.Errorf("invalid MANGA_READER_BASIC_AUTH_ENABLED value (want true/false)")
	}

	cfg := BasicAuthConfig{Enabled: true}

	if file := getEnv("MANGA_READER_BASIC_AUTH_FILE", ""); file != "" {
		st, err := os.Stat(file)
		if err != nil {
			return BasicAuthConfig{}, fmt.Errorf("MANGA_READER_BASIC_AUTH_FILE not found: %s", file)
		}
		if st.Size() == 0 {
			return BasicAuthConfig{}, fmt.Errorf("MANGA_READER_BASIC_AUTH_FILE is empty: %s", file)
		}
		// Least privilege: reject a credential file that group/other can
		// modify. Readability is proven by os.ReadFile below; the process UID
		// only needs read access (0400/0600/0644 are all fine).
		if st.Mode().Perm()&0o022 != 0 {
			return BasicAuthConfig{}, fmt.Errorf("MANGA_READER_BASIC_AUTH_FILE must not be writable by group or others (chmod 0400/0600/0644): %s", file)
		}
		data, err := os.ReadFile(file)
		if err != nil {
			return BasicAuthConfig{}, fmt.Errorf("MANGA_READER_BASIC_AUTH_FILE not readable: %s", file)
		}
		hashes := parseHtpasswd(data)
		if len(hashes) == 0 {
			return BasicAuthConfig{}, fmt.Errorf("MANGA_READER_BASIC_AUTH_FILE has no valid entries: %s", file)
		}
		cfg.File = file
		cfg.Hashes = hashes
		return cfg, nil
	}

	user := getEnv("MANGA_READER_BASIC_AUTH_USERNAME", "")
	if user == "" {
		return BasicAuthConfig{}, fmt.Errorf("basic auth enabled but MANGA_READER_BASIC_AUTH_USERNAME is not set")
	}
	if strings.Contains(user, ":") {
		return BasicAuthConfig{}, fmt.Errorf("MANGA_READER_BASIC_AUTH_USERNAME must not contain ':'")
	}

	pass := getEnv("MANGA_READER_BASIC_AUTH_PASSWORD", "")
	if pass == "" {
		passFile := getEnv("MANGA_READER_BASIC_AUTH_PASSWORD_FILE", "")
		if passFile == "" {
			return BasicAuthConfig{}, fmt.Errorf("basic auth enabled but no password (set MANGA_READER_BASIC_AUTH_PASSWORD or MANGA_READER_BASIC_AUTH_PASSWORD_FILE)")
		}
		data, err := os.ReadFile(passFile)
		if err != nil {
			return BasicAuthConfig{}, fmt.Errorf("MANGA_READER_BASIC_AUTH_PASSWORD_FILE not readable: %s", passFile)
		}
		line, _, _ := strings.Cut(string(data), "\n")
		line = strings.TrimRight(line, "\r")
		if line == "" {
			return BasicAuthConfig{}, fmt.Errorf("MANGA_READER_BASIC_AUTH_PASSWORD_FILE is empty: %s", passFile)
		}
		pass = line
	}
	if pass == "" {
		return BasicAuthConfig{}, fmt.Errorf("basic auth enabled but the password is empty")
	}

	cfg.Username = user
	cfg.Password = pass
	return cfg, nil
}

// loadAuth reads the JWT/admin settings. Auth is opt-in for backwards
// compatibility with existing anonymous deployments: a non-empty JWT_SECRET
// enables it. Setting ADMIN_PASSWORD without a secret is a configuration
// error rather than a silently anonymous deployment.
func loadAuth() (AuthConfig, error) {
	secret := strings.TrimSpace(getEnv("JWT_SECRET", ""))
	adminPassword := getEnv("ADMIN_PASSWORD", "")

	if secret == "" {
		if adminPassword != "" {
			return AuthConfig{}, fmt.Errorf("ADMIN_PASSWORD is set but JWT_SECRET is empty; set JWT_SECRET (for example: openssl rand -hex 32)")
		}
		// Keep the defaults available for diagnostics, but do not enable.
		return AuthConfig{}, nil
	}

	// HMAC-SHA256 keys shorter than the 32-byte output weaken the signature.
	// Requiring 32 bytes also rejects placeholder values such as "change-me".
	if len(secret) < 32 {
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
		Enabled:       true,
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

// parseHtpasswd turns an htpasswd file into username -> hash entries. Blank
// lines and comments are ignored; malformed lines are skipped.
func parseHtpasswd(data []byte) map[string]string {
	hashes := make(map[string]string)
	for line := range strings.SplitSeq(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, hash, ok := strings.Cut(line, ":")
		if !ok || name == "" || hash == "" {
			continue
		}
		hashes[name] = hash
	}
	return hashes
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

// loadCookieConfig reads the ExHentai session cookies from the environment.
// Both supported forms may be present: the individual variables take precedence,
// and a combined EHENTAI_COOKIE string only fills the fields they leave empty.
// Treating the string as a wholesale override (the old behaviour) silently
// dropped igneous/sk from an otherwise complete individual setup as soon as an
// older partial string survived in .env, and ExHentai answered with sad panda.
func loadCookieConfig() CookieConfig {
	cfg := CookieConfig{
		IpbMemberID: normalizeCookieValue(getEnv("EHENTAI_COOKIE_IPB_MEMBER_ID", "")),
		IpbPassHash: normalizeCookieValue(getEnv("EHENTAI_COOKIE_IPB_PASS_HASH", "")),
		Igneous:     normalizeCookieValue(getEnv("EHENTAI_COOKIE_IGNEOUS", "")),
		SK:          normalizeCookieValue(getEnv("EHENTAI_COOKIE_SK", "")),
	}
	combined := parseCookieString(getEnv("EHENTAI_COOKIE", ""))
	if cfg.IpbMemberID == "" {
		cfg.IpbMemberID = combined.IpbMemberID
	}
	if cfg.IpbPassHash == "" {
		cfg.IpbPassHash = combined.IpbPassHash
	}
	if cfg.Igneous == "" {
		cfg.Igneous = combined.Igneous
	}
	if cfg.SK == "" {
		cfg.SK = combined.SK
	}
	return cfg
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
