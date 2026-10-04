// Package settings persists the configuration the settings page owns and
// applies it to the running process.
//
// Everything else still comes from the environment: a key is only written to
// the database when its effective value differs from what the environment
// provides, so deployments that never touch the settings page behave exactly
// as before, and edits made to .env later still take effect for untouched keys.
package settings

import (
	"fmt"
	"strconv"
	"strings"

	"manga-reader/internal/config"
)

// Mask stands in for a stored secret in API responses. Sending it back in a
// PUT means "keep the value I already have", which is why it can never be a
// plausible credential.
const Mask = "********"

// Snapshot is the settings page's view of the current configuration. Secrets
// are masked; everything else is the effective value.
type Snapshot struct {
	// Persisted reports whether the settings page has overridden anything.
	Persisted bool        `json:"persisted"`
	Cookie    CookieView  `json:"cookie"`
	Storage   StorageView `json:"storage"`
	LogLevel  string      `json:"logLevel"`
	DevTools  bool        `json:"devTools"`
}

// CookieView is the ExHentai cookie configuration.
type CookieView struct {
	MemberID string `json:"memberId"`
	PassHash string `json:"passHash"`
	Igneous  string `json:"igneous"`
	SK       string `json:"sk"`
	// Configured is true once ipb_member_id and ipb_pass_hash are both set.
	Configured bool `json:"configured"`
}

// StorageView is the image cache storage configuration.
type StorageView struct {
	// Driver is the configured driver (auto/local/s3).
	Driver string `json:"driver"`
	// ResolvedDriver is what actually runs right now (local/s3).
	ResolvedDriver string `json:"resolvedDriver"`
	Dir            string `json:"dir"`
	S3             S3View `json:"s3"`
}

// S3View is the S3-compatible object store configuration.
type S3View struct {
	Endpoint  string `json:"endpoint"`
	Region    string `json:"region"`
	Bucket    string `json:"bucket"`
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
	UseSSL    bool   `json:"useSsl"`
	PathStyle string `json:"pathStyle"`
}

// Update is a PATCH-style payload: an absent field (JSON null or missing) means
// "leave this unchanged". Secret fields additionally treat Mask as unchanged,
// so a form that round-trips the masked value cannot overwrite a credential.
type Update struct {
	Cookie   *CookieUpdate  `json:"cookie"`
	Storage  *StorageUpdate `json:"storage"`
	LogLevel *string        `json:"logLevel"`
	DevTools *bool          `json:"devTools"`
}

// CookieUpdate is the ExHentai cookie section of an Update.
type CookieUpdate struct {
	MemberID *string `json:"memberId"`
	PassHash *string `json:"passHash"`
	Igneous  *string `json:"igneous"`
	SK       *string `json:"sk"`
}

// StorageUpdate is the storage section of an Update.
type StorageUpdate struct {
	Driver *string   `json:"driver"`
	Dir    *string   `json:"dir"`
	S3     *S3Update `json:"s3"`
}

// S3Update is the S3 section of an Update.
type S3Update struct {
	Endpoint  *string `json:"endpoint"`
	Region    *string `json:"region"`
	Bucket    *string `json:"bucket"`
	AccessKey *string `json:"accessKey"`
	SecretKey *string `json:"secretKey"`
	UseSSL    *bool   `json:"useSsl"`
	PathStyle *string `json:"pathStyle"`
}

// InvalidError marks a payload that failed validation; the API maps it to 400.
type InvalidError struct{ Err error }

func (e *InvalidError) Error() string { return e.Err.Error() }
func (e *InvalidError) Unwrap() error { return e.Err }

// IsInvalid reports whether err came from a rejected settings payload.
func IsInvalid(err error) bool {
	_, ok := err.(*InvalidError)
	return ok
}

// validate rejects a merged configuration that would not run.
func validate(c *config.Config) error {
	if !c.Cookie.IsValid() {
		return fmt.Errorf("ExHentai cookie is incomplete: ipb_member_id and ipb_pass_hash are both required")
	}
	switch c.Storage.Driver {
	case "", config.StorageDriverAuto, config.StorageDriverLocal, config.StorageDriverS3:
	default:
		return fmt.Errorf("unsupported storage driver %q: use auto, local or s3", c.Storage.Driver)
	}
	if c.Storage.ResolvedDriver() == config.StorageDriverS3 {
		if missing := c.Storage.S3.Missing(); len(missing) > 0 {
			return fmt.Errorf("incomplete S3 storage configuration: missing %s", strings.Join(missing, ", "))
		}
	}
	switch strings.ToLower(strings.TrimSpace(c.LogLevel)) {
	case "debug", "info", "warn", "warning", "error":
	default:
		return fmt.Errorf("unsupported log level %q: use debug, info, warn or error", c.LogLevel)
	}
	return nil
}

// flatten renders c as the managed configuration keys it corresponds to.
func flatten(c *config.Config) map[string]string {
	return map[string]string{
		"EHENTAI_COOKIE_IPB_MEMBER_ID": c.Cookie.IpbMemberID,
		"EHENTAI_COOKIE_IPB_PASS_HASH": c.Cookie.IpbPassHash,
		"EHENTAI_COOKIE_IGNEOUS":       c.Cookie.Igneous,
		"EHENTAI_COOKIE_SK":            c.Cookie.SK,
		"MANGA_READER_STORAGE_DRIVER":  c.Storage.Driver,
		"MANGA_READER_STORAGE_DIR":     c.Storage.Dir,
		"MANGA_READER_S3_ENDPOINT":     c.Storage.S3.Endpoint,
		"MANGA_READER_S3_REGION":       c.Storage.S3.Region,
		"MANGA_READER_S3_BUCKET":       c.Storage.S3.Bucket,
		"MANGA_READER_S3_ACCESS_KEY":   c.Storage.S3.AccessKey,
		"MANGA_READER_S3_SECRET_KEY":   c.Storage.S3.SecretKey,
		"MANGA_READER_S3_USE_SSL":      strconv.FormatBool(c.Storage.S3.UseSSL),
		"MANGA_READER_S3_PATH_STYLE":   c.Storage.S3.PathStyle,
		"LOG_LEVEL":                    c.LogLevel,
		"MANGA_READER_DEV_TOOLS":       strconv.FormatBool(c.DevTools),
	}
}

// applyOverlay mutates cur with the fields present in in. Absent pointer fields
// keep the current value; secret fields also keep it when the masked sentinel
// is echoed back.
func applyOverlay(cur *config.Config, in Update) {
	if in.Cookie != nil {
		if v, ok := plain(in.Cookie.MemberID); ok {
			cur.Cookie.IpbMemberID = v
		}
		if v, ok := secret(cur.Cookie.IpbPassHash, in.Cookie.PassHash); ok {
			cur.Cookie.IpbPassHash = v
		}
		if v, ok := secret(cur.Cookie.Igneous, in.Cookie.Igneous); ok {
			cur.Cookie.Igneous = v
		}
		if v, ok := secret(cur.Cookie.SK, in.Cookie.SK); ok {
			cur.Cookie.SK = v
		}
	}
	if in.Storage != nil {
		if v, ok := plain(in.Storage.Driver); ok {
			cur.Storage.Driver = normalizeDriver(v)
		}
		if v, ok := plain(in.Storage.Dir); ok {
			cur.Storage.Dir = v
		}
		if s := in.Storage.S3; s != nil {
			if v, ok := plain(s.Endpoint); ok {
				cur.Storage.S3.Endpoint = v
			}
			if v, ok := plain(s.Region); ok {
				cur.Storage.S3.Region = v
			}
			if v, ok := plain(s.Bucket); ok {
				cur.Storage.S3.Bucket = v
			}
			if v, ok := plain(s.AccessKey); ok {
				cur.Storage.S3.AccessKey = v
			}
			if v, ok := secret(cur.Storage.S3.SecretKey, s.SecretKey); ok {
				cur.Storage.S3.SecretKey = v
			}
			if s.UseSSL != nil {
				cur.Storage.S3.UseSSL = *s.UseSSL
			}
			if v, ok := plain(s.PathStyle); ok {
				cur.Storage.S3.PathStyle = normalizePathStyle(v)
			}
		}
	}
	if in.LogLevel != nil {
		if v, ok := plain(in.LogLevel); ok {
			cur.LogLevel = strings.ToLower(strings.TrimSpace(v))
		}
	}
	if in.DevTools != nil {
		cur.DevTools = *in.DevTools
	}
}

// plain reports whether ptr was sent and is not the masked sentinel.
func plain(ptr *string) (string, bool) {
	if ptr == nil || *ptr == Mask {
		return "", false
	}
	return *ptr, true
}

// secret is plain for credential fields.
func secret(current string, ptr *string) (string, bool) {
	v, ok := plain(ptr)
	if !ok {
		return current, false
	}
	return v, true
}

func normalizeDriver(d string) string {
	switch strings.ToLower(strings.TrimSpace(d)) {
	case "", config.StorageDriverAuto:
		return config.StorageDriverAuto
	case config.StorageDriverLocal, "fs", "file":
		return config.StorageDriverLocal
	case config.StorageDriverS3, "minio":
		return config.StorageDriverS3
	default:
		return strings.ToLower(strings.TrimSpace(d))
	}
}

func normalizePathStyle(p string) string {
	switch strings.ToLower(strings.TrimSpace(p)) {
	case "", "auto":
		return "auto"
	case "path":
		return "path"
	case "dns":
		return "dns"
	default:
		return strings.ToLower(strings.TrimSpace(p))
	}
}
