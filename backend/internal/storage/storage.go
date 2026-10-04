// Package storage abstracts where cached objects live. The rest of the
// application only depends on Storage, so LocalStorage (plain files) and
// S3Storage (MinIO / AWS S3 / any S3-compatible service) are interchangeable.
package storage

import (
	"context"
	"errors"
)

// ErrNotFound is returned when the requested object does not exist.
var ErrNotFound = errors.New("storage: object not found")

// PutOptions carries optional metadata stored alongside an object.
type PutOptions struct {
	// CacheControl is stored with the object and replayed on Get.
	CacheControl string
	// Meta is extra key/value metadata (HTTP headers such as ETag aliases).
	Meta map[string]string
}

// ObjectInfo describes a stored object.
type ObjectInfo struct {
	ContentType  string
	CacheControl string
	Size         int64
	Meta         map[string]string
}

// Storage is the object store used by the image cache. Implementations must
// be safe for concurrent use.
type Storage interface {
	// Get returns the object bytes and its content type. It returns an error
	// matching ErrNotFound when the object does not exist.
	Get(ctx context.Context, key string) ([]byte, string, error)
	// Put stores data under key, overwriting any previous value.
	Put(ctx context.Context, key string, data []byte, contentType string, opts PutOptions) error
	// Delete removes key. Deleting a missing key is not an error.
	Delete(ctx context.Context, key string) error
	// Exists reports whether key is present.
	Exists(ctx context.Context, key string) (bool, error)
}

// IsNotFound reports whether err indicates a missing object.
func IsNotFound(err error) bool {
	return errors.Is(err, ErrNotFound)
}
