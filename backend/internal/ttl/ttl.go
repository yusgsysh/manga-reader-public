// Package ttl centralizes every cache lifetime used by the backend so the
// freshness policy lives in one place instead of being scattered across
// handlers and clients. Values here are the single source of truth; call sites
// must reference them rather than re-declaring local durations.
package ttl

import (
	"strconv"
	"time"
)

const (
	// ListingCursor is how long an in-memory ExHentai next-page cursor stays
	// valid before it must be re-walked.
	ListingCursor = 10 * time.Minute

	// ImageThumbnailHTTP is the Cache-Control max-age for live image proxies.
	ImageThumbnailHTTP = time.Hour
	// ImageImmutableHTTP is the Cache-Control max-age for content-addressed
	// MinIO-backed image proxies.
	ImageImmutableHTTP = 365 * 24 * time.Hour
)

// PublicMaxAge renders a "public, max-age=<seconds>" Cache-Control value.
func PublicMaxAge(d time.Duration) string {
	return "public, max-age=" + seconds(d)
}

// PublicImmutableMaxAge renders a "public, max-age=<seconds>, immutable"
// Cache-Control value.
func PublicImmutableMaxAge(d time.Duration) string {
	return "public, max-age=" + seconds(d) + ", immutable"
}

func seconds(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Second), 10)
}
