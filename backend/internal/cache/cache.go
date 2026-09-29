package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

const (
	ImageCachePrefix = "images/"
	CacheKeyLength   = sha256.Size * 2
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

func LoadMinIOConfig() *MinIOConfig {
	return &MinIOConfig{
		Endpoint:  os.Getenv("MINIO_ENDPOINT"),
		AccessKey: os.Getenv("MINIO_ACCESS_KEY"),
		SecretKey: os.Getenv("MINIO_SECRET_KEY"),
		Bucket:    os.Getenv("MINIO_BUCKET"),
		UseSSL:    parseBoolEnv("MINIO_USE_SSL", false),
		Region:    os.Getenv("MINIO_REGION"),
	}
}

func (c *MinIOConfig) IsValid() bool {
	return c.Endpoint != "" && c.AccessKey != "" && c.SecretKey != "" && c.Bucket != ""
}

func parseBoolEnv(key string, defaultVal bool) bool {
	v := os.Getenv(key)
	if v == "" {
		return defaultVal
	}
	b, err := strconv.ParseBool(v)
	if err != nil {
		return defaultVal
	}
	return b
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

	if err := cache.ensureBucket(context.Background()); err != nil {
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
	if minioErr, ok := err.(minio.ErrorResponse); ok {
		return minioErr.Code == "NoSuchKey" || minioErr.Code == "NoSuchBucket" || minioErr.Code == "NotFound"
	}
	return false
}

func (c *MinIOCache) Get(ctx context.Context, key string) (data []byte, contentType string, err error) {
	obj, err := c.client.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, "", fmt.Errorf("minio get object: %w", err)
	}
	defer obj.Close()

	stat, err := obj.Stat()
	if err != nil {
		return nil, "", fmt.Errorf("minio stat object: %w", err)
	}

	data, err = io.ReadAll(obj)
	if err != nil {
		return nil, "", fmt.Errorf("minio read object: %w", err)
	}

	return data, stat.ContentType, nil
}

func (c *MinIOCache) Put(ctx context.Context, key string, data []byte, contentType string, cacheControl string) error {
	reader := strings.NewReader(string(data))
	_, err := c.client.PutObject(ctx, c.bucket, key, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType:  contentType,
		CacheControl: cacheControl,
	})
	if err != nil {
		return fmt.Errorf("minio put object: %w", err)
	}
	return nil
}

func (c *MinIOCache) PutWithMeta(ctx context.Context, key string, data []byte, contentType string, meta map[string]string, cacheControl string) error {
	reader := strings.NewReader(string(data))
	_, err := c.client.PutObject(ctx, c.bucket, key, reader, int64(len(data)), minio.PutObjectOptions{
		ContentType:  contentType,
		CacheControl: cacheControl,
		UserMetadata: meta,
	})
	if err != nil {
		return fmt.Errorf("minio put object: %w", err)
	}
	return nil
}

func (c *MinIOCache) Head(ctx context.Context, key string) (minio.ObjectInfo, error) {
	return c.client.StatObject(ctx, c.bucket, key, minio.StatObjectOptions{})
}
