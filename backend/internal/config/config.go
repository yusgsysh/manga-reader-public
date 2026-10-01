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
