package sync

import (
	"context"
	"fmt"

	"manga-reader/internal/ident"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/syncchange"
)

// RecordUpsert appends an outbox entry for a local mutation owned by userID.
// The sync engine replicates every user's rows; user_id travels with each
// change so peers keep the same isolation.
func (s *Service) RecordUpsert(ctx context.Context, userID ident.UUID, entity string, galleryID int64, token string) error {
	return s.record(ctx, userID, entity, OpUpsert, galleryID, token)
}

// RecordDelete appends a user-scoped delete tombstone.
func (s *Service) RecordDelete(ctx context.Context, userID ident.UUID, entity string, galleryID int64, token string) error {
	return s.record(ctx, userID, entity, OpDelete, galleryID, token)
}

// RecordDeletes appends tombstones for a batch of removed records.
func (s *Service) RecordDeletes(ctx context.Context, userID ident.UUID, entity string, keys []Key) error {
	if len(keys) == 0 || !s.recordEnabled() || !s.shouldRecord(userID) {
		return nil
	}
	builders := make([]*ent.SyncChangeCreate, 0, len(keys))
	for _, k := range keys {
		builders = append(builders, s.client.SyncChange.Create().
			SetUserID(userID).
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

func (s *Service) record(ctx context.Context, userID ident.UUID, entity, op string, galleryID int64, token string) error {
	if !s.recordEnabled() || !s.shouldRecord(userID) {
		return nil
	}
	if _, err := s.client.SyncChange.Create().
		SetUserID(userID).
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

// shouldRecord reports whether a write belongs to the account this instance
// syncs. A host always records every user (its outbox feeds every peer); a
// pure client drops other accounts so a user-scoped key never receives them.
func (s *Service) shouldRecord(userID ident.UUID) bool {
	if s.hostEnabled || userID == ident.Nil() {
		return true
	}
	owner := s.syncedUser()
	return owner == ident.Nil() || userID == owner
}

// maxOutboxID returns the current instance-wide outbox maximum (0 when empty).
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
