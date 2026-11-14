// Package storage defines a provider-agnostic object-store interface and a
// streaming, S3-compatible implementation backed by minio-go.
//
// It is deliberately independent from the image cache: the ZIP archive builder
// programs against ObjectStorage, so the archive can target a different
// provider, endpoint, bucket and credentials than the image cache, or reuse
// the same ones. The image cache keeps its own, narrower interface; the two
// are wired up separately from configuration.
package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"manga-reader/internal/metrics"
)

const (
	// DefaultPartSize is the multipart part size used for unknown-length
	// uploads when the operator does not configure one. minio-go's default is
	// 16 MiB; pinning it here makes the behaviour explicit and testable.
	DefaultPartSize = 16 << 20
	// MinimumPartSize is the smallest allowed multipart part (S3 requires parts
	// of at least 5 MiB, except the final part).
	MinimumPartSize = 5 << 20
)

// ObjectInfo is a minimal snapshot of a stored object.
type ObjectInfo struct {
	Key         string
	Size        int64
	ContentType string
	ETag        string
}

// ObjectStorage is the object-store surface the archive builder depends on.
// Implementations must stream: neither the full object nor a single part
// should ever be required to reside in memory, and an unknown object length
// must be uploadable via multipart.
type ObjectStorage interface {
	// Upload streams reader to key. A negative size means the length is
	// unknown, which forces a multipart upload so large archives never need a
	// single PUT or a materialized copy. contentType may be empty.
	Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (ObjectInfo, error)
	// Head returns the object's metadata without reading its body.
	Head(ctx context.Context, key string) (ObjectInfo, error)
	// Delete removes the object. Deleting a missing object is not an error.
	Delete(ctx context.Context, key string) error
	// Get opens the object for streaming reads. The caller must Close it.
	Get(ctx context.Context, key string) (io.ReadCloser, error)
	// GetStream additionally reports the stored content type and size so a
	// caller can proxy the object at constant memory.
	GetStream(ctx context.Context, key string) (rc io.ReadCloser, contentType string, size int64, err error)
	// PresignGet returns a time-limited signed URL that lets a client download
	// key directly from the object store, bypassing the backend. params may
	// override response headers (e.g. "response-content-disposition") and are
	// included in the signature. The URL is only meaningful when the store
	// endpoint is reachable by the client.
	PresignGet(ctx context.Context, key string, expiry time.Duration, params map[string]string) (string, error)
}

// IsNotFound reports whether err means the object (or bucket) does not exist.
func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	if minioErr, ok := errors.AsType[minio.ErrorResponse](err); ok {
		return minioErr.Code == "NoSuchKey" || minioErr.Code == "NoSuchBucket" || minioErr.Code == "NotFound"
	}
	return false
}

// Config is the connection configuration for one object-store backend.
type Config struct {
	Endpoint  string
	Region    string
	Bucket    string
	AccessKey string
	SecretKey string
	UseSSL    bool
	Prefix    string
	PartSize  int64
}

// Validate reports whether the required connection fields are present. Only a
// backend that is actually enabled should be validated; a disabled backend is
// allowed to be incomplete.
func (c Config) Validate() error {
	if c.Endpoint == "" || c.AccessKey == "" || c.SecretKey == "" || c.Bucket == "" {
		return fmt.Errorf("storage config incomplete: endpoint, access_key, secret_key and bucket are required")
	}
	return nil
}

// Minio is an S3-compatible ObjectStorage backed by minio-go. It serves MinIO,
// AWS S3 and any S3-compatible endpoint (including Aliyun OSS in its
// S3-compatible mode); the concrete provider is therefore invisible to the
// archive builder.
type Minio struct {
	client     *minio.Client
	bucket     string
	prefix     string
	region     string
	partSize   int64
	autoCreate bool
}

// New creates a Minio backend and ensures its bucket exists. Bucket creation is
// bounded so an unreachable store cannot hang startup; a missing bucket is
// auto-created (matching the image cache), so a fresh deployment only needs
// credentials and a bucket name.
func New(ctx context.Context, cfg Config) (*Minio, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}

	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""),
		Secure: cfg.UseSSL,
	}
	if cfg.Region != "" {
		opts.Region = cfg.Region
	}

	client, err := minio.New(cfg.Endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("storage client init failed: %w", err)
	}

	partSize := cfg.PartSize
	if partSize <= 0 {
		partSize = DefaultPartSize
	}

	s := &Minio{
		client:     client,
		bucket:     cfg.Bucket,
		prefix:     cfg.Prefix,
		region:     cfg.Region,
		partSize:   partSize,
		autoCreate: true,
	}

	bucketCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	if err := s.ensureBucket(bucketCtx); err != nil {
		return nil, err
	}

	slog.Info("object storage initialized", "endpoint", cfg.Endpoint, "bucket", cfg.Bucket, "prefix", cfg.Prefix, "ssl", cfg.UseSSL)
	return s, nil
}

func (s *Minio) key(k string) string { return s.prefix + k }

func (s *Minio) ensureBucket(ctx context.Context) error {
	exists, err := s.client.BucketExists(ctx, s.bucket)
	if err != nil {
		return fmt.Errorf("storage bucket check failed: %w", err)
	}
	if exists {
		return nil
	}
	if !s.autoCreate {
		return fmt.Errorf("storage bucket %q does not exist and auto-create is disabled", s.bucket)
	}
	if err := s.client.MakeBucket(ctx, s.bucket, minio.MakeBucketOptions{Region: s.region}); err != nil {
		return fmt.Errorf("storage make bucket failed: %w", err)
	}
	slog.Info("storage bucket created", "bucket", s.bucket)
	return nil
}

// observe classifies a finished object-store operation for the metrics
// endpoint; a miss is not an operation failure.
func observe(op string, start time.Time, err error) {
	result := "ok"
	switch {
	case err == nil:
	case IsNotFound(err):
		result = "not_found"
	default:
		result = "error"
	}
	metrics.ObserveMinIOCache(op, result, time.Since(start))
}

// Upload streams reader to key. When size is negative the length is unknown and
// a multipart upload is used; minio-go aborts the incomplete upload on any
// failure (including context cancellation), so a half-written object is never
// left visible.
func (s *Minio) Upload(ctx context.Context, key string, reader io.Reader, size int64, contentType string) (ObjectInfo, error) {
	start := time.Now()
	info, err := s.client.PutObject(ctx, s.bucket, s.key(key), reader, size, minio.PutObjectOptions{
		ContentType: contentType,
		PartSize:    uint64(s.partSize),
	})
	observe("put", start, err)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("storage upload: %w", err)
	}
	return ObjectInfo{Key: key, Size: info.Size, ContentType: contentType, ETag: info.ETag}, nil
}

// Head returns the object's metadata without reading its body.
func (s *Minio) Head(ctx context.Context, key string) (ObjectInfo, error) {
	start := time.Now()
	stat, err := s.client.StatObject(ctx, s.bucket, s.key(key), minio.StatObjectOptions{})
	observe("head", start, err)
	if err != nil {
		return ObjectInfo{}, fmt.Errorf("storage head: %w", err)
	}
	return ObjectInfo{Key: key, Size: stat.Size, ContentType: stat.ContentType, ETag: stat.ETag}, nil
}

// Delete removes the object. A missing object is not an error.
func (s *Minio) Delete(ctx context.Context, key string) error {
	start := time.Now()
	err := s.client.RemoveObject(ctx, s.bucket, s.key(key), minio.RemoveObjectOptions{})
	observe("delete", start, err)
	if err != nil && !IsNotFound(err) {
		return fmt.Errorf("storage delete: %w", err)
	}
	return nil
}

// Get opens the object for streaming reads. The caller must Close the reader.
func (s *Minio) Get(ctx context.Context, key string) (io.ReadCloser, error) {
	rc, _, _, err := s.GetStream(ctx, key)
	return rc, err
}

// GetStream opens the object without buffering it, reporting content type and
// size so a caller can proxy it at constant memory. Errors satisfy IsNotFound
// for a missing key.
func (s *Minio) GetStream(ctx context.Context, key string) (rc io.ReadCloser, contentType string, size int64, err error) {
	start := time.Now()
	defer func() { observe("get", start, err) }()

	obj, err := s.client.GetObject(ctx, s.bucket, s.key(key), minio.GetObjectOptions{})
	if err != nil {
		return nil, "", 0, fmt.Errorf("storage get: %w", err)
	}
	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, "", 0, fmt.Errorf("storage stat: %w", err)
	}
	return obj, stat.ContentType, stat.Size, nil
}

// PresignGet returns a signed GET URL for key. extra query parameters (such as
// response-content-disposition) are merged into the signed URL. The URL is
// valid for expiry (bounded by the provider, typically 1s–7 days).
func (s *Minio) PresignGet(ctx context.Context, key string, expiry time.Duration, params map[string]string) (string, error) {
	q := make(url.Values, len(params))
	for k, v := range params {
		q.Set(k, v)
	}
	u, err := s.client.PresignedGetObject(ctx, s.bucket, s.key(key), expiry, q)
	if err != nil {
		return "", fmt.Errorf("storage presign: %w", err)
	}
	return u.String(), nil
}
