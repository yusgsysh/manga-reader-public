package storage

import (
	"context"
	"fmt"

	"manga-reader/internal/config"
)

// New builds the Storage implementation selected by cfg. Driver "auto" (the
// default) resolves to S3 when a complete S3 configuration exists — which is
// what every pre-existing MinIO deployment has — and to local files otherwise.
func New(ctx context.Context, cfg config.StorageConfig) (Storage, error) {
	switch cfg.ResolvedDriver() {
	case config.StorageDriverS3:
		return NewS3Storage(ctx, cfg.S3)
	case config.StorageDriverLocal:
		return NewLocalStorage(LocalConfig{Root: cfg.Dir})
	default:
		return nil, fmt.Errorf("storage: unsupported driver %q", cfg.Driver)
	}
}
