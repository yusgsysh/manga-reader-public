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

// PrivateMaxAge renders a "private, max-age=<seconds>" Cache-Control value.
// Image responses sit behind Basic Auth, so a shared cache must not store
// and re-serve them to another client.
func PrivateMaxAge(d time.Duration) string {
	return "private, max-age=" + seconds(d)
}

// PrivateImmutableMaxAge renders a "private, max-age=<seconds>, immutable"
// Cache-Control value.
func PrivateImmutableMaxAge(d time.Duration) string {
	return "private, max-age=" + seconds(d) + ", immutable"
}

func seconds(d time.Duration) string {
	return strconv.FormatInt(int64(d/time.Second), 10)
}
