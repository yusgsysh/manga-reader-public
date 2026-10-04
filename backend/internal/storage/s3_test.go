package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/minio/minio-go/v7"

	"manga-reader/internal/config"
)

func completeS3Config() config.S3Config {
	return config.S3Config{
		Endpoint:  "minio:9000",
		Region:    "us-east-1",
		Bucket:    "manga-cache",
		AccessKey: "minioadmin",
		SecretKey: "minioadmin",
		UseSSL:    false,
	}
}

func TestS3ConfigValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*config.S3Config)
		wantOK  bool
		missing int
	}{
		{"complete", func(*config.S3Config) {}, true, 0},
		{"missing endpoint", func(c *config.S3Config) { c.Endpoint = "" }, false, 1},
		{"missing bucket", func(c *config.S3Config) { c.Bucket = "" }, false, 1},
		{"missing access key", func(c *config.S3Config) { c.AccessKey = "" }, false, 1},
		{"missing secret key", func(c *config.S3Config) { c.SecretKey = "" }, false, 1},
		{"all empty", func(c *config.S3Config) { *c = config.S3Config{} }, false, 4},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := completeS3Config()
			tt.mutate(&cfg)
			if got := cfg.IsComplete(); got != tt.wantOK {
				t.Fatalf("IsComplete() = %v, want %v", got, tt.wantOK)
			}
			if got := len(cfg.Missing()); got != tt.missing {
				t.Fatalf("len(Missing()) = %d, want %d", got, tt.missing)
			}
		})
	}
}

func TestNewS3Storage_RejectsIncompleteConfig(t *testing.T) {
	cfg := completeS3Config()
	cfg.Bucket = ""
	if _, err := NewS3Storage(context.Background(), cfg); err == nil {
		t.Fatal("NewS3Storage succeeded with an empty bucket, want error")
	}
}

func TestBucketLookup(t *testing.T) {
	tests := []struct {
		in   string
		want minio.BucketLookupType
	}{
		{"", minio.BucketLookupAuto},
		{"auto", minio.BucketLookupAuto},
		{"path", minio.BucketLookupPath},
		{"Path", minio.BucketLookupPath},
		{"dns", minio.BucketLookupDNS},
		{"bogus", minio.BucketLookupAuto},
	}
	for _, tt := range tests {
		if got := bucketLookup(tt.in); got != tt.want {
			t.Errorf("bucketLookup(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}

func TestWrapS3MapsMissingObjectsToNotFound(t *testing.T) {
	tests := []struct {
		name string
		code string
		want bool
	}{
		{"nosuchkey", "NoSuchKey", true},
		{"nosuchbucket", "NoSuchBucket", true},
		{"nosuchobject", "NoSuchObject", true},
		{"notfound", "NotFound", true},
		{"accessdenied", "AccessDenied", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := wrapS3("get", "images/x", minio.ErrorResponse{Code: tt.code})
			if IsNotFound(err) != tt.want {
				t.Fatalf("IsNotFound(%v) = %v, want %v", err, IsNotFound(err), tt.want)
			}
			if IsNotFound(err) {
				// The original error must stay inspectable.
				resp, ok := errors.AsType[minio.ErrorResponse](err)
				if !ok || resp.Code != tt.code {
					t.Fatalf("original minio error lost: %v", err)
				}
			}
		})
	}
}

func TestWrapS3PreservesGenericErrors(t *testing.T) {
	err := wrapS3("put", "images/x", fmt.Errorf("connection refused"))
	if IsNotFound(err) {
		t.Fatal("IsNotFound = true, want false")
	}
}

func TestValidateKey(t *testing.T) {
	if err := validateKey(""); err == nil {
		t.Error("validateKey(\"\") = nil, want error")
	}
	if err := validateKey("images/\x00x"); err == nil {
		t.Error("validateKey with NUL = nil, want error")
	}
	if err := validateKey("images/abcdef"); err != nil {
		t.Errorf("validateKey(valid) = %v, want nil", err)
	}
}
