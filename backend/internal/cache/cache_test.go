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

func TestCacheKey_SameURL(t *testing.T) {
	u := "https://exhentai.org/s/51d1aa689c/4153369-29"
	key1 := CacheKey(u)
	key2 := CacheKey(u)
	if key1 != key2 {
		t.Errorf("same URL should produce same key: %q != %q", key1, key2)
	}
}

func TestCacheKey_DifferentURL(t *testing.T) {
	key1 := CacheKey("https://exhentai.org/s/aaa/111-1")
	key2 := CacheKey("https://exhentai.org/s/bbb/222-2")
	if key1 == key2 {
		t.Error("different URLs should produce different keys")
	}
}

func TestCacheKey_Format(t *testing.T) {
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
}

func TestCacheKey_MatchesSHA256(t *testing.T) {
	u := "https://exhentai.org/s/51d1aa689c/4153369-29"
	key := CacheKey(u)

	sum := sha256.Sum256([]byte(u))
	expected := ImageCachePrefix + hex.EncodeToString(sum[:])

	if key != expected {
		t.Errorf("key = %q, want %q", key, expected)
	}
}

func TestCacheKey_DifferentPartsProduceDifferentKeys(t *testing.T) {
	urls := []string{
		"https://exhentai.org/s/aaa/111-1",
		"https://exhentai.org/s/bbb/111-1",
		"https://exhentai.org/s/aaa/222-2",
		"https://e-hentai.org/s/aaa/111-1",
		"https://exhentai.org/s/aaa/111-1?nl=XYZ",
	}

	seen := make(map[string]string)
	for _, u := range urls {
		key := CacheKey(u)
		if prev, ok := seen[key]; ok {
			t.Errorf("URL %q and %q produced same key %q", u, prev, key)
		}
		seen[key] = u
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
