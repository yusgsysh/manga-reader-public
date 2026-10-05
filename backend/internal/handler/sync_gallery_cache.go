package handler

import (
	"context"
	"log/slog"

	synclib "manga-reader/internal/sync"
)

// recordGalleryCacheChange queues a successful local gallery_cache write for
// sync so the peer receives the refreshed metadata/pages without polling.
func (s *Server) recordGalleryCacheChange(ctx context.Context, galleryID int64, token string) {
	if s.syncSvc == nil {
		return
	}
	synclib.LogRecordError(
		s.syncSvc.RecordUpsert(ctx, synclib.EntityGalleryCache, galleryID, token),
		synclib.EntityGalleryCache, galleryID, token)
}

// cleanupGalleryCache sweeps cache rows that no bookshelf or reading history
// references anymore and replicates the removal as tombstones, so the peer
// drops its copy too (both sides run the same sweep against their own
// references once those have converged through sync).
func (s *Server) cleanupGalleryCache(ctx context.Context) {
	if s.DB == nil {
		return
	}
	keys, err := s.DB.CleanupGalleryCache(ctx)
	if err != nil {
		slog.Warn("gallery cache cleanup failed", "error", err)
		return
	}
	if len(keys) == 0 {
		return
	}
	slog.Debug("gallery cache cleanup", "deleted", len(keys))

	if s.syncSvc == nil {
		return
	}
	syncKeys := make([]synclib.Key, len(keys))
	for i, k := range keys {
		syncKeys[i] = synclib.Key{GalleryID: k.GalleryID, Token: k.Token}
	}
	if recErr := s.syncSvc.RecordDeletes(ctx, synclib.EntityGalleryCache, syncKeys); recErr != nil {
		slog.Warn("record sync cache cleanup failed", "error", recErr)
	}
}
