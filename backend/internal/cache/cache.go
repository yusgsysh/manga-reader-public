package cache

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"manga-reader/internal/metrics"
)

const (
	ImageCachePrefix = "images/"
	CacheKeyLength   = sha256.Size * 2

	// maxObjectBytes caps a single cached object read. Writes are bounded
	// upstream (maxImageBytes / sprite fetch), so a larger object means
	// something foreign landed in the bucket; refuse to buffer it rather
	// than let io.ReadAll balloon the heap.
	maxObjectBytes = 40 << 20
)

type MinIOCache struct {
	client     *minio.Client
	bucket     string
	region     string
	autoCreate bool
}

type MinIOConfig struct {
	Endpoint  string
	AccessKey string
	SecretKey string
	Bucket    string
	UseSSL    bool
	Region    string
}

func (c *MinIOConfig) IsValid() bool {
	return c.Endpoint != "" && c.AccessKey != "" && c.SecretKey != "" && c.Bucket != ""
}

func NewMinIOCache(cfg *MinIOConfig) (*MinIOCache, error) {
	if !cfg.IsValid() {
		return nil, fmt.Errorf("minio config incomplete: endpoint, access_key, secret_key, and bucket are required")
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
		return nil, fmt.Errorf("minio client init failed: %w", err)
	}

	cache := &MinIOCache{
		client:     client,
		bucket:     cfg.Bucket,
		region:     cfg.Region,
		autoCreate: true,
	}

	// Bound startup: an unreachable MinIO must not hang boot forever with no
	// health endpoint up.
	bucketCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := cache.ensureBucket(bucketCtx); err != nil {
		return nil, err
	}

	slog.Info("minio cache initialized", "endpoint", cfg.Endpoint, "bucket", cfg.Bucket, "ssl", cfg.UseSSL)
	return cache, nil
}

func (c *MinIOCache) ensureBucket(ctx context.Context) error {
	exists, err := c.client.BucketExists(ctx, c.bucket)
	if err != nil {
		return fmt.Errorf("minio bucket check failed: %w", err)
	}
	if exists {
		return nil
	}
	if !c.autoCreate {
		return fmt.Errorf("minio bucket %q does not exist and auto-create is disabled", c.bucket)
	}

	err = c.client.MakeBucket(ctx, c.bucket, minio.MakeBucketOptions{Region: c.region})
	if err != nil {
		return fmt.Errorf("minio make bucket failed: %w", err)
	}
	slog.Info("minio bucket created", "bucket", c.bucket)
	return nil
}

func CacheKey(url string) string {
	sum := sha256.Sum256([]byte(url))
	return ImageCachePrefix + hex.EncodeToString(sum[:])
}

func IsNotFound(err error) bool {
	if err == nil {
		return false
	}
	// Filesystem-backed caches (FSCache) report a miss as fs.ErrNotExist,
	// possibly wrapped; MinIO reports it as an ErrorResponse code.
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if minioErr, ok := errors.AsType[minio.ErrorResponse](err); ok {
		return minioErr.Code == "NoSuchKey" || minioErr.Code == "NoSuchBucket" || minioErr.Code == "NotFound"
	}
	return false
}

// observe times a finished MinIO operation and classifies its error: a miss
// (object absent) is not an operation failure, everything else is.
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

func (c *MinIOCache) Get(ctx context.Context, key string) (data []byte, contentType string, err error) {
	start := time.Now()
	defer func() { observe("get", start, err) }()

	obj, err := c.client.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("minio get object: %w", err)
	}
	defer obj.Close()

	stat, err := obj.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("minio stat object: %w", err)
	}
	if stat.Size > maxObjectBytes {
		return nil, "", fmt.Errorf("cached object %q too large: %d bytes", key, stat.Size)
	}

	data, err = io.ReadAll(io.LimitReader(obj, maxObjectBytes+1))
	if err != nil {
		return nil, "", fmt.Errorf("minio read object: %w", err)
	}
	if len(data) > maxObjectBytes {
		return nil, "", fmt.Errorf("cached object %q too large: exceeds %d bytes", key, maxObjectBytes)
	}

	return data, stat.ContentType, nil
}

// GetStream opens a cached object for reading without buffering it, so a
// cache hit can be proxied to the client at constant memory. The caller
// must close the returned reader. Errors match Get, including IsNotFound
// for a missing key.
func (c *MinIOCache) GetStream(ctx context.Context, key string) (rc io.ReadCloser, contentType string, size int64, err error) {
	start := time.Now()
	defer func() { observe("get", start, err) }()

	obj, err := c.client.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", 0, fmt.Errorf("minio get object: %w", err)
	}

	stat, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, "", 0, fmt.Errorf("minio stat object: %w", err)
	}
	if stat.Size > maxObjectBytes {
		obj.Close()
		return nil, "", 0, fmt.Errorf("cached object %q too large: %d bytes", key, stat.Size)
	}
	return obj, stat.ContentType, stat.Size, nil
}

func (c *MinIOCache) Put(ctx context.Context, key string, data []byte, contentType string, cacheControl string) (err error) {
	start := time.Now()
	defer func() { observe("put", start, err) }()

	reader := bytes.NewReader(data)
	_, err = c.client.PutObject(ctx, c.bucket, key, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType:  contentType,
		CacheControl: cacheControl,
	})
	if err != nil {
		return fmt.Errorf("minio put object: %w", err)
	}
	return nil
}

func (c *MinIOCache) PutWithMeta(ctx context.Context, key string, data []byte, contentType string, meta map[string]string, cacheControl string) (err error) {
	start := time.Now()
	defer func() { observe("put", start, err) }()

	reader := bytes.NewReader(data)
	_, err = c.client.PutObject(ctx, c.bucket, key, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType:  contentType,
		CacheControl: cacheControl,
		UserMetadata: meta,
	})
	if err != nil {
		return fmt.Errorf("minio put object: %w", err)
	}
	return nil
}

func (c *MinIOCache) Head(ctx context.Context, key string) (info minio.ObjectInfo, err error) {
	start := time.Now()
	defer func() { observe("head", start, err) }()
	return c.client.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
}
