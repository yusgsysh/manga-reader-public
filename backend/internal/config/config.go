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
	MinIO       MinIOConfig
	LogLevel    string
	Environment string
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
}

// BasicAuthConfig is the validated HTTP Basic Auth setup. Credentials come
// from exactly one source: a single user in the environment, or an htpasswd
// file (apr1 or bcrypt).
type BasicAuthConfig struct {
	Enabled bool
	// Username/Password hold the single environment-provided user.
	Username string
	Password string
	// File is a world-readable htpasswd file; empty when the credentials
	// came from the environment.
	File string
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
		Port: getEnv("EHENTAI_PORT", ":8080"),
		Database: DatabaseConfig{
			Driver: strings.ToLower(strings.TrimSpace(getEnv("MANGA_READER_DB_DRIVER", "sqlite"))),
			Path:   getEnv("MANGA_READER_DB_PATH", "data/manga-reader.db"),
			DSN:    dbDSN,
		},
		LogLevel:    getEnv("LOG_LEVEL", "warn"),
		Environment: getEnv("ENVIRONMENT", "production"),
		DevTools:    parseBoolEnv("MANGA_READER_DEV_TOOLS", false),
		SyncToken:   getEnv("MANGA_READER_SYNC_TOKEN", ""),
		Cookie: CookieConfig{
			IpbMemberID: getEnv("EHENTAI_COOKIE_IPB_MEMBER_ID", ""),
			IpbPassHash: getEnv("EHENTAI_COOKIE_IPB_PASS_HASH", ""),
			Igneous:     getEnv("EHENTAI_COOKIE_IGNEOUS", ""),
			SK:          getEnv("EHENTAI_COOKIE_SK", ""),
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

	if cookieStr := getEnv("EHENTAI_COOKIE", ""); cookieStr != "" {
		cfg.Cookie = parseCookieString(cookieStr)
	}

	basicAuth, err := loadBasicAuth()
	if err != nil {
		return nil, err
	}
	cfg.BasicAuth = basicAuth

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
		if st.Mode().Perm()&0o004 == 0 {
			return BasicAuthConfig{}, fmt.Errorf("MANGA_READER_BASIC_AUTH_FILE must be world-readable (chmod 644): %s", file)
		}
		cfg.File = file
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
