package cache

import (
	"crypto/sha256"
	"encoding/hex"
)

// Cache key helpers shared by every endpoint that reads or writes the image
// cache. The keys are derived from the upstream URL, so they are byte-stable
// no matter which storage backend (local files or S3) is configured.
const (
	// ImageCachePrefix namespaces every cached image object.
	ImageCachePrefix = "images/"
	// CacheKeyLength is the length of the hex encoded SHA-256 part of a key.
	CacheKeyLength = sha256.Size * 2
)

// CacheKey maps an upstream URL to its stable object key.
func CacheKey(url string) string {
	sum := sha256.Sum256([]byte(url))
	return ImageCachePrefix + hex.EncodeToString(sum[:])
}
