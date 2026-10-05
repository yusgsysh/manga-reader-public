package sync

import (
	"context"
	"fmt"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/readingprogress"
	"manga-reader/internal/ent/syncchange"
)

// ApplyChanges merges peer changes into the local database with per-row
// last-write-wins on updated_at.
//
// It is deliberately not integrated with Record: rows that arrive from a peer
// must not re-enter the local outbox, otherwise they would be pushed straight
// back (the receiving peer skips them by the same LWW comparison, but every
// cycle would cost an extra round trip). Callers must notify the hub
// themselves when applied > 0 so local frontends refresh.
//
// Applying the same change twice is a no-op (equal timestamps are skipped),
// which makes retries and echoes harmless.
func ApplyChanges(ctx context.Context, client *ent.Client, changes []Change) (applied, skipped int, err error) {
	for _, ch := range changes {
		ok, applyErr := applyChange(ctx, client, ch)
		if applyErr != nil {
			return applied, skipped, applyErr
		}
		if ok {
			applied++
		} else {
			skipped++
		}
	}
	return applied, skipped, nil
}

func applyChange(ctx context.Context, client *ent.Client, ch Change) (bool, error) {
	switch ch.Entity {
	case EntityReadingProgress:
		return applyReadingProgress(ctx, client, ch)
	case EntityBookshelf:
		return applyBookshelf(ctx, client, ch)
	default:
		// Unknown entities are skipped, not fatal: a newer peer may replicate
		// tables this instance does not know about yet.
		return false, nil
	}
}

func applyReadingProgress(ctx context.Context, client *ent.Client, ch Change) (bool, error) {
	existing, err := client.ReadingProgress.Query().
		Where(
			readingprogress.GalleryID(ch.GalleryID),
			readingprogress.Token(ch.Token),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return false, fmt.Errorf("read reading progress: %w", err)
	}

	if ch.Op == OpDelete {
		if existing == nil || ch.DeletedAt == nil {
			return false, nil
		}
		if !ch.DeletedAt.After(existing.UpdatedAt) {
			return false, nil
		}
		if _, err := client.ReadingProgress.Delete().
			Where(
				readingprogress.GalleryID(ch.GalleryID),
				readingprogress.Token(ch.Token),
			).
			Exec(ctx); err != nil {
			return false, fmt.Errorf("delete reading progress: %w", err)
		}
		return true, nil
	}

	if ch.Row == nil {
		return false, nil
	}
	if existing == nil {
		if _, err := client.ReadingProgress.Create().
			SetGalleryID(ch.GalleryID).
			SetToken(ch.Token).
			SetCurrentPage(ch.Row.CurrentPage).
			SetProgress(ch.Row.Progress).
			SetCompleted(ch.Row.Completed).
			SetCreatedAt(ch.Row.CreatedAt).
			SetUpdatedAt(ch.Row.UpdatedAt).
			Save(ctx); err != nil {
			return false, fmt.Errorf("create reading progress: %w", err)
		}
		return true, nil
	}
	if !ch.Row.UpdatedAt.After(existing.UpdatedAt) {
		return false, nil
	}
	// created_at is immutable on the schema and intentionally kept as-is:
	// only the replicated fields and updated_at decide the winner.
	if err := client.ReadingProgress.Update().
		Where(
			readingprogress.GalleryID(ch.GalleryID),
			readingprogress.Token(ch.Token),
		).
		SetCurrentPage(ch.Row.CurrentPage).
		SetProgress(ch.Row.Progress).
		SetCompleted(ch.Row.Completed).
		SetUpdatedAt(ch.Row.UpdatedAt).
		Exec(ctx); err != nil {
		return false, fmt.Errorf("update reading progress: %w", err)
	}
	return true, nil
}

func applyBookshelf(ctx context.Context, client *ent.Client, ch Change) (bool, error) {
	existing, err := client.Bookshelf.Query().
		Where(
			bookshelf.GalleryID(ch.GalleryID),
			bookshelf.Token(ch.Token),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return false, fmt.Errorf("read bookshelf: %w", err)
	}

	if ch.Op == OpDelete {
		if existing == nil || ch.DeletedAt == nil {
			return false, nil
		}
		if !ch.DeletedAt.After(existing.UpdatedAt) {
			return false, nil
		}
		if _, err := client.Bookshelf.Delete().
			Where(
				bookshelf.GalleryID(ch.GalleryID),
				bookshelf.Token(ch.Token),
			).
			Exec(ctx); err != nil {
			return false, fmt.Errorf("delete bookshelf: %w", err)
		}
		return true, nil
	}

	if ch.Row == nil {
		return false, nil
	}
	if existing == nil {
		if _, err := client.Bookshelf.Create().
			SetGalleryID(ch.GalleryID).
			SetToken(ch.Token).
			SetCreatedAt(ch.Row.CreatedAt).
			SetUpdatedAt(ch.Row.UpdatedAt).
			Save(ctx); err != nil {
			return false, fmt.Errorf("create bookshelf: %w", err)
		}
		return true, nil
	}
	if !ch.Row.UpdatedAt.After(existing.UpdatedAt) {
		return false, nil
	}
	if err := client.Bookshelf.Update().
		Where(
			bookshelf.GalleryID(ch.GalleryID),
			bookshelf.Token(ch.Token),
		).
		SetUpdatedAt(ch.Row.UpdatedAt).
		Exec(ctx); err != nil {
		return false, fmt.Errorf("update bookshelf: %w", err)
	}
	return true, nil
}

// ExportSnapshot returns the current state of both synced tables as upsert
// changes (bootstrap transfer).
func ExportSnapshot(ctx context.Context, client *ent.Client) ([]Change, error) {
	progressRows, err := client.ReadingProgress.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("export reading progress: %w", err)
	}
	bookshelfRows, err := client.Bookshelf.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("export bookshelf: %w", err)
	}

	changes := make([]Change, 0, len(progressRows)+len(bookshelfRows))
	for _, p := range progressRows {
		changes = append(changes, Change{
			Entity:    EntityReadingProgress,
			GalleryID: p.GalleryID,
			Token:     p.Token,
			Op:        OpUpsert,
			Row: &Row{
				CurrentPage: p.CurrentPage,
				Progress:    p.Progress,
				Completed:   p.Completed,
				CreatedAt:   p.CreatedAt,
				UpdatedAt:   p.UpdatedAt,
			},
		})
	}
	for _, b := range bookshelfRows {
		changes = append(changes, Change{
			Entity:    EntityBookshelf,
			GalleryID: b.GalleryID,
			Token:     b.Token,
			Op:        OpUpsert,
			Row: &Row{
				CreatedAt: b.CreatedAt,
				UpdatedAt: b.UpdatedAt,
			},
		})
	}
	return changes, nil
}

// ReadChangesSince converts outbox entries with id > cursor into wire
// changes. Upserts are resolved against the current row (so rapid updates are
// coalesced into one payload); an entry whose row has vanished is downgraded
// to a tombstone, which the peer applies only if the tombstone is newer than
// its own copy.
//
// The second return value is the current outbox maximum (the new cursor).
func ReadChangesSince(ctx context.Context, client *ent.Client, cursor int) ([]Change, int, error) {
	maxID, err := maxOutboxID(ctx, client)
	if err != nil {
		return nil, 0, err
	}

	entries, err := client.SyncChange.Query().
		Where(syncchange.IDGT(cursor)).
		Order(ent.Asc(syncchange.FieldID)).
		All(ctx)
	if err != nil {
		return nil, 0, fmt.Errorf("read outbox: %w", err)
	}
	if len(entries) == 0 {
		return []Change{}, maxID, nil
	}

	// Coalesce per record: the last entry wins (an upsert after a delete
	// resurrects the row, a delete after an upsert removes it).
	type dedupeKey struct {
		entity    string
		galleryID int64
		token     string
	}
	latest := make(map[dedupeKey]*ent.SyncChange, len(entries))
	order := make([]dedupeKey, 0, len(entries))
	for _, e := range entries {
		k := dedupeKey{entity: e.Entity, galleryID: e.GalleryID, token: e.Token}
		if _, seen := latest[k]; !seen {
			order = append(order, k)
		}
		latest[k] = e
	}

	changes := make([]Change, 0, len(order))
	for _, k := range order {
		e := latest[k]
		if e.Op == OpDelete {
			deletedAt := e.ChangedAt
			changes = append(changes, Change{
				Entity:    e.Entity,
				GalleryID: e.GalleryID,
				Token:     e.Token,
				Op:        OpDelete,
				DeletedAt: &deletedAt,
			})
			continue
		}
		row, err := fetchRow(ctx, client, e.Entity, e.GalleryID, e.Token)
		if err != nil {
			return nil, 0, err
		}
		if row == nil {
			deletedAt := e.ChangedAt
			changes = append(changes, Change{
				Entity:    e.Entity,
				GalleryID: e.GalleryID,
				Token:     e.Token,
				Op:        OpDelete,
				DeletedAt: &deletedAt,
			})
			continue
		}
		changes = append(changes, Change{
			Entity:    e.Entity,
			GalleryID: e.GalleryID,
			Token:     e.Token,
			Op:        OpUpsert,
			Row:       row,
		})
	}
	return changes, maxID, nil
}

func fetchRow(ctx context.Context, client *ent.Client, entity string, galleryID int64, token string) (*Row, error) {
	switch entity {
	case EntityReadingProgress:
		p, err := client.ReadingProgress.Query().
			Where(
				readingprogress.GalleryID(galleryID),
				readingprogress.Token(token),
			).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("fetch reading progress: %w", err)
		}
		return &Row{
			CurrentPage: p.CurrentPage,
			Progress:    p.Progress,
			Completed:   p.Completed,
			CreatedAt:   p.CreatedAt,
			UpdatedAt:   p.UpdatedAt,
		}, nil
	case EntityBookshelf:
		b, err := client.Bookshelf.Query().
			Where(
				bookshelf.GalleryID(galleryID),
				bookshelf.Token(token),
			).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("fetch bookshelf: %w", err)
		}
		return &Row{CreatedAt: b.CreatedAt, UpdatedAt: b.UpdatedAt}, nil
	default:
		return nil, nil
	}
}

// pruneOutbox removes pushed entries. Callers must only prune up to a cursor
// the peer has confirmed holding.
func pruneOutbox(ctx context.Context, client *ent.Client, cursor int) error {
	if cursor <= 0 {
		return nil
	}
	if _, err := client.SyncChange.Delete().
		Where(syncchange.IDLTE(cursor)).
		Exec(ctx); err != nil {
		return fmt.Errorf("prune outbox: %w", err)
	}
	return nil
}
