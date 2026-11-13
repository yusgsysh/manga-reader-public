package cache

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

// TestCacheKey pins the two externally visible properties of the cache key:
// it is a stable SHA-256 of the URL and uses the package prefix/hex encoding.
func TestCacheKey(t *testing.T) {
	u := "https://exhentai.org/s/51d1aa689c/4153369-29"
	key := CacheKey(u)

	if !strings.HasPrefix(key, ImageCachePrefix) {
		t.Errorf("key should start with %q, got %q", ImageCachePrefix, key)
	}
	hexPart := strings.TrimPrefix(key, ImageCachePrefix)
	if len(hexPart) != CacheKeyLength {
		t.Errorf("hex part length = %d, want %d", len(hexPart), CacheKeyLength)
	}
	if _, err := hex.DecodeString(hexPart); err != nil {
		t.Errorf("hex part should be valid hex: %v", err)
	}

	sum := sha256.Sum256([]byte(u))
	if want := ImageCachePrefix + hex.EncodeToString(sum[:]); key != want {
		t.Errorf("key = %q, want %q", key, want)
	}
}

func TestIsNotFound(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"nosuchkey", minio.ErrorResponse{Code: "NoSuchKey"}, true},
		{"nosuchbucket", minio.ErrorResponse{Code: "NoSuchBucket"}, true},
		{"notfound", minio.ErrorResponse{Code: "NotFound"}, true},
		{"other", minio.ErrorResponse{Code: "AccessDenied"}, false},
		{"wrapped nosuchkey", fmt.Errorf("minio stat object: %w", minio.ErrorResponse{Code: "NoSuchKey"}), true},
		{"plain", errors.New("boom"), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := IsNotFound(tt.err); got != tt.want {
				t.Errorf("IsNotFound(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}
