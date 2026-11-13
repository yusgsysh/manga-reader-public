package sync

import (
	"context"
	"fmt"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/syncchange"
)

// RecordUpsert appends an upsert tombstone-free outbox entry for the default
// tenant. Kept for compatibility; request handlers should call the ForTenant
// variant with the scope derived from the verified JWT.
func (s *Service) RecordUpsert(ctx context.Context, entity string, galleryID int64, token string) error {
	return s.RecordUpsertForTenant(ctx, s.tenantID, entity, galleryID, token)
}

// RecordUpsertForTenant records a local mutation only when it belongs to the
// tenant this sync instance serves. Other tenants are intentionally not
// replicated, so their rows can never enter another tenant's outbox.
func (s *Service) RecordUpsertForTenant(ctx context.Context, tenantID int64, entity string, galleryID int64, token string) error {
	return s.record(ctx, tenantID, entity, OpUpsert, galleryID, token)
}

// RecordDelete appends a delete tombstone for the default tenant.
func (s *Service) RecordDelete(ctx context.Context, entity string, galleryID int64, token string) error {
	return s.RecordDeleteForTenant(ctx, s.tenantID, entity, galleryID, token)
}

// RecordDeleteForTenant appends a tenant-scoped delete tombstone.
func (s *Service) RecordDeleteForTenant(ctx context.Context, tenantID int64, entity string, galleryID int64, token string) error {
	return s.record(ctx, tenantID, entity, OpDelete, galleryID, token)
}

// RecordDeletes appends tombstones for a batch of removed default-tenant
// records and emits a single notification (used by retention purges).
func (s *Service) RecordDeletes(ctx context.Context, entity string, keys []Key) error {
	return s.RecordDeletesForTenant(ctx, s.tenantID, entity, keys)
}

// RecordDeletesForTenant is the tenant-scoped batch form.
func (s *Service) RecordDeletesForTenant(ctx context.Context, tenantID int64, entity string, keys []Key) error {
	if len(keys) == 0 || !s.recordEnabled() || tenantID != s.tenantID {
		return nil
	}
	builders := make([]*ent.SyncChangeCreate, 0, len(keys))
	for _, k := range keys {
		builders = append(builders, s.client.SyncChange.Create().
			SetTenantID(tenantID).
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

func (s *Service) record(ctx context.Context, tenantID int64, entity, op string, galleryID int64, token string) error {
	if !s.recordEnabled() || tenantID != s.tenantID {
		return nil
	}
	if _, err := s.client.SyncChange.Create().
		SetTenantID(tenantID).
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

// maxOutboxID returns the current tenant-scoped outbox maximum (0 when empty).
func maxOutboxID(ctx context.Context, client *ent.Client, tenantID int64) (int, error) {
	last, err := client.SyncChange.Query().
		Where(syncchange.TenantIDEQ(tenantID)).
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
