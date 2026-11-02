package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"io/fs"

	"github.com/minio/minio-go/v7"
)

const (
	ImageCachePrefix = "images/"
	CacheKeyLength   = sha256.Size * 2

	// maxObjectBytes caps a single cached object read. Writes are bounded
	// upstream (maxImageBytes / sprite fetch), so a larger object means
	// something foreign landed in the cache; refuse to buffer it rather than
	// let io.ReadAll balloon the heap.
	maxObjectBytes = 40 << 20
)

// CacheKey returns the object key for a source URL.
func CacheKey(url string) string {
	sum := sha256.Sum256([]byte(url))
	return ImageCachePrefix + hex.EncodeToString(sum[:])
}

// IsNotFound reports whether err means the cached object is absent. FSCache
// wraps fs.ErrNotExist, while cache implementations used by tests return a
// MinIO NoSuchKey error.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if minioErr, ok := errors.AsType[minio.ErrorResponse](err); ok {
		return minioErr.Code == "NoSuchKey" || minioErr.Code == "NoSuchBucket" || minioErr.Code == "NotFound"
	}
	return false
}
