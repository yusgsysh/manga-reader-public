package storage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"manga-reader/internal/config"
)

// S3Storage stores objects in any S3-compatible service (MinIO, AWS S3,
// Cloudflare R2, ...). Object keys are used verbatim, so switching between the
// local and S3 drivers never changes a cache key.
type S3Storage struct {
	client *minio.Client
	bucket string
	region string
}

var _ Storage = (*S3Storage)(nil)

// NewS3Storage connects to cfg's endpoint and makes sure the bucket exists.
func NewS3Storage(ctx context.Context, cfg config.S3Config) (*S3Storage, error) {
	if missing := cfg.Missing(); len(missing) > 0 {
		return nil, fmt.Errorf("storage: s3 configuration incomplete: missing %s (set MANGA_READER_S3_* or MINIO_*)",
			strings.Join(missing, ", "))
	}

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
		Region: cfg.Region,
		// BucketLookupAuto asks the SDK: path style for non-AWS endpoints
		// (self-hosted MinIO), virtual-hosted style for AWS/Google. "path" and
		// "dns" force one or the other for services the heuristic gets wrong.
		BucketLookup: bucketLookup(cfg.PathStyle),
	}

	client, err := minio.New(cfg.Endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("storage: s3 client init failed: %w", err)
	}

	s := &S3Storage{client: client, bucket: cfg.Bucket, region: cfg.Region}
	// Bound startup: an unreachable object store must not hang boot forever
	// with no health endpoint up.
	bucketCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := s.ensureBucket(bucketCtx); err != nil {
		return nil, err
	}

	slog.Info("s3 storage initialized", "endpoint", cfg.Endpoint, "bucket", cfg.Bucket, "ssl", cfg.UseSSL)
	return s, nil
}

func bucketLookup(style string) minio.BucketLookupType {
	switch strings.ToLower(strings.TrimSpace(style)) {
	case "path":
		return minio.BucketLookupPath
	case "dns":
		return minio.BucketLookupDNS
	default:
		return minio.BucketLookupAuto
	}
}

func (s *S3Storage) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("storage: bucket check failed: %w", err)
	}
	if exists {
		return nil
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: s.region}); err != nil {
		return fmt.Errorf("storage: make bucket failed: %w", err)
	}
	slog.Info("storage bucket created", "bucket", s.bucket)
	return nil
}

// Get downloads the object and returns its content type.
func (s *S3Storage) Get(ctx context.Context, key string) ([]byte, string, error) {
	if err := validateKey(key); err != nil {
		return nil, "", err
	}
	obj, err := s.client.GetObject(ctx, s.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", wrapS3("get", key, err)
	}
	defer obj.Close()

	stat, err := obj.Stat()
	if err != nil {
		return nil, "", wrapS3("get", key, err)
	}
	data, err := io.ReadAll(obj)
	if err != nil {
		return nil, "", fmt.Errorf("storage: read %q: %w", key, err)
	}
	return data, stat.ContentType, nil
}

// Put uploads the object with its content type and optional metadata.
func (s *S3Storage) Put(ctx context.Context, key string, data []byte, contentType string, opts PutOptions) error {
	if err := validateKey(key); err != nil {
		return err
	}
	_, err := s.client.PutObject(ctx, s.bucket, key, bytes.NewReader(data), int64(len(data)), minio.PutObjectOptions{
		ContentType:  contentType,
		CacheControl: opts.CacheControl,
		UserMetadata: opts.Meta,
	})
	if err != nil {
		return wrapS3("put", key, err)
	}
	return nil
}

// Delete removes the object. S3 deletes are idempotent.
func (s *S3Storage) Delete(ctx context.Context, key string) error {
	if err := validateKey(key); err != nil {
		return err
	}
	if err := s.client.RemoveObject(ctx, s.bucket, key, minio.RemoveObjectOptions{}); err != nil {
		return wrapS3("delete", key, err)
	}
	return nil
}

// Exists reports whether the object is present.
func (s *S3Storage) Exists(ctx context.Context, key string) (bool, error) {
	if err := validateKey(key); err != nil {
		return false, err
	}
	_, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err == nil {
		return true, nil
	}
	if isS3NotFound(err) {
		return false, nil
	}
	return false, wrapS3("stat", key, err)
}

// Stat returns the object metadata without downloading the payload.
func (s *S3Storage) Stat(ctx context.Context, key string) (ObjectInfo, error) {
	if err := validateKey(key); err != nil {
		return ObjectInfo{}, err
	}
	info, err := s.client.StatObject(ctx, s.bucket, key, minio.StatObjectOptions{})
	if err != nil {
		return ObjectInfo{}, wrapS3("stat", key, err)
	}
	meta := map[string]string{}
	for k, v := range info.UserMetadata {
		meta[k] = v
	}
	return ObjectInfo{
		ContentType:  info.ContentType,
		CacheControl: info.Metadata.Get("Cache-Control"),
		Size:         info.Size,
		Meta:         meta,
	}, nil
}

func validateKey(key string) error {
	if key == "" {
		return fmt.Errorf("storage: empty cache key")
	}
	if strings.ContainsRune(key, 0) {
		return fmt.Errorf("storage: invalid cache key %q", key)
	}
	return nil
}

// wrapS3 normalises SDK errors: missing objects become ErrNotFound so callers
// never need to know which backend they talk to.
func wrapS3(op, key string, err error) error {
	if err == nil {
		return nil
	}
	if isS3NotFound(err) {
		return fmt.Errorf("storage: %s %q: %w: %w", op, key, ErrNotFound, err)
	}
	return fmt.Errorf("storage: %s %q: %w", op, key, err)
}

func isS3NotFound(err error) bool {
	if resp, ok := errors.AsType[minio.ErrorResponse](err); ok {
		return resp.Code == "NoSuchKey" ||
			resp.Code == "NoSuchBucket" ||
			resp.Code == "NoSuchObject" ||
			resp.Code == "NotFound"
	}
	return false
}
