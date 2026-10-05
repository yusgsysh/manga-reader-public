package sync

import (
	"context"
	"fmt"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/syncchange"
)

// RecordUpsert appends an upsert tombstone-free outbox entry for the given
// record. Handlers call it after every local mutation of a synced table.
//
// Failures are returned to the caller: a lost outbox entry would leave the
// peer permanently stale for that row.
func (s *Service) RecordUpsert(ctx context.Context, entity string, galleryID int64, token string) error {
	return s.record(ctx, entity, OpUpsert, galleryID, token)
}

// RecordDelete appends a delete tombstone for the given record.
func (s *Service) RecordDelete(ctx context.Context, entity string, galleryID int64, token string) error {
	return s.record(ctx, entity, OpDelete, galleryID, token)
}

// RecordDeletes appends tombstones for a batch of removed records and emits a
// single notification (used by retention purges).
func (s *Service) RecordDeletes(ctx context.Context, entity string, keys []Key) error {
	if len(keys) == 0 {
		return nil
	}
	builders := make([]*ent.SyncChangeCreate, 0, len(keys))
	for _, k := range keys {
		builders = append(builders, s.client.SyncChange.Create().
			SetEntity(entity).
			SetGalleryID(k.GalleryID).
			SetToken(k.Token).
			SetOp(OpDelete))
	}
	if _, err := s.client.SyncChange.CreateBulk(builders...).Save(ctx); err != nil {
		return fmt.Errorf("record sync deletes: %w", err)
	}
	s.hub.NotifyPeer()
	s.hub.NotifyLocal()
	return nil
}

func (s *Service) record(ctx context.Context, entity, op string, galleryID int64, token string) error {
	if _, err := s.client.SyncChange.Create().
		SetEntity(entity).
		SetGalleryID(galleryID).
		SetToken(token).
		SetOp(op).
		Save(ctx); err != nil {
		return fmt.Errorf("record sync change: %w", err)
	}
	s.hub.NotifyPeer()
	s.hub.NotifyLocal()
	return nil
}

// maxOutboxID returns the current outbox maximum (0 when empty).
func maxOutboxID(ctx context.Context, client *ent.Client) (int, error) {
	last, err := client.SyncChange.Query().
		Order(ent.Desc(syncchange.FieldID)).
		First(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return 0, nil
		}
		return 0, fmt.Errorf("read outbox max id: %w", err)
	}
	return last.ID, nil
}
