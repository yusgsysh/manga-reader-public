package sync

import (
	"context"
	"fmt"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/gallerycache"
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
	case EntityGalleryCache:
		return applyGalleryCache(ctx, client, ch)
	default:
		// Unknown entities are skipped, not fatal: a newer peer may replicate
		// tables this instance does not know about yet.
		return false, nil
	}
}

// applyGalleryCache merges a replicated cache row. The gate is the same
// per-row LWW on updated_at as the other entities, but a winning row is
// merged field-wise: empty/zero/nil incoming values never blank a stored
// field. That mirrors gallerycache's own upsert rule (a metadata write must
// not erase details a richer source already persisted) and sidesteps JSON
// omitempty hiding legitimate empty values.
func applyGalleryCache(ctx context.Context, client *ent.Client, ch Change) (bool, error) {
	existing, err := client.GalleryCache.Query().
		Where(
			gallerycache.GalleryID(ch.GalleryID),
			gallerycache.Token(ch.Token),
		).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return false, fmt.Errorf("read gallery cache: %w", err)
	}

	if ch.Op == OpDelete {
		if existing == nil || ch.DeletedAt == nil {
			return false, nil
		}
		if !ch.DeletedAt.After(existing.UpdatedAt) {
			return false, nil
		}
		if _, err := client.GalleryCache.Delete().
			Where(
				gallerycache.GalleryID(ch.GalleryID),
				gallerycache.Token(ch.Token),
			).
			Exec(ctx); err != nil {
			return false, fmt.Errorf("delete gallery cache: %w", err)
		}
		return true, nil
	}

	if ch.Row == nil {
		return false, nil
	}
	r := ch.Row
	if existing == nil {
		create := client.GalleryCache.Create().
			SetGalleryID(ch.GalleryID).
			SetToken(ch.Token).
			SetCreatedAt(r.CreatedAt).
			SetUpdatedAt(r.UpdatedAt).
			SetTitle(r.Title).
			SetTitleJpn(r.TitleJPN).
			SetCategory(r.Category).
			SetThumbnail(r.Thumbnail).
			SetPageCount(r.PageCount).
			SetRating(r.Rating).
			SetRatingCount(r.RatingCount).
			SetUploader(r.Uploader).
			SetPosted(r.Posted).
			SetLanguage(r.Language).
			SetTranslated(r.Translated).
			SetFileSize(r.FileSize).
			SetFavorited(r.Favorited).
			SetExpunged(r.Expunged).
			SetTags(r.Tags).
			SetPages(r.Pages).
			SetThumbnails(r.Thumbnails)
		if r.PostedAt != nil {
			create.SetPostedAt(*r.PostedAt)
		}
		if r.MetaFetchedAt != nil {
			create.SetMetaFetchedAt(*r.MetaFetchedAt)
		}
		if r.DetailsFetchedAt != nil {
			create.SetDetailsFetchedAt(*r.DetailsFetchedAt)
		}
		if r.PagesFetchedAt != nil {
			create.SetPagesFetchedAt(*r.PagesFetchedAt)
		}
		if r.ThumbnailFetchedAt != nil {
			create.SetThumbnailFetchedAt(*r.ThumbnailFetchedAt)
		}
		if _, err := create.Save(ctx); err != nil {
			return false, fmt.Errorf("create gallery cache: %w", err)
		}
		return true, nil
	}
	if !r.UpdatedAt.After(existing.UpdatedAt) {
		return false, nil
	}

	update := client.GalleryCache.Update().
		Where(
			gallerycache.GalleryID(ch.GalleryID),
			gallerycache.Token(ch.Token),
		).
		SetUpdatedAt(r.UpdatedAt)
	if r.Title != "" {
		update.SetTitle(r.Title)
	}
	if r.TitleJPN != "" {
		update.SetTitleJpn(r.TitleJPN)
	}
	if r.Category != "" {
		update.SetCategory(r.Category)
	}
	if r.Thumbnail != "" {
		update.SetThumbnail(r.Thumbnail)
	}
	if r.PageCount != 0 {
		update.SetPageCount(r.PageCount)
	}
	if r.Rating != 0 {
		update.SetRating(r.Rating)
	}
	if r.RatingCount != 0 {
		update.SetRatingCount(r.RatingCount)
	}
	if r.Uploader != "" {
		update.SetUploader(r.Uploader)
	}
	if r.Posted != "" {
		update.SetPosted(r.Posted)
	}
	if r.PostedAt != nil {
		update.SetPostedAt(*r.PostedAt)
	}
	if r.Language != "" {
		update.SetLanguage(r.Language)
	}
	if r.Translated {
		update.SetTranslated(true)
	}
	if r.FileSize != "" {
		update.SetFileSize(r.FileSize)
	}
	if r.Favorited != 0 {
		update.SetFavorited(r.Favorited)
	}
	if r.Expunged {
		update.SetExpunged(true)
	}
	if len(r.Tags) > 0 {
		update.SetTags(r.Tags)
	}
	if len(r.Pages) > 0 {
		update.SetPages(r.Pages)
	}
	if len(r.Thumbnails) > 0 {
		update.SetThumbnails(r.Thumbnails)
	}
	if r.MetaFetchedAt != nil {
		update.SetMetaFetchedAt(*r.MetaFetchedAt)
	}
	if r.DetailsFetchedAt != nil {
		update.SetDetailsFetchedAt(*r.DetailsFetchedAt)
	}
	if r.PagesFetchedAt != nil {
		update.SetPagesFetchedAt(*r.PagesFetchedAt)
	}
	if r.ThumbnailFetchedAt != nil {
		update.SetThumbnailFetchedAt(*r.ThumbnailFetchedAt)
	}
	if err := update.Exec(ctx); err != nil {
		return false, fmt.Errorf("update gallery cache: %w", err)
	}
	return true, nil
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

// ExportSnapshot returns the current state of all synced tables as upsert
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
	cacheRows, err := client.GalleryCache.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("export gallery cache: %w", err)
	}

	changes := make([]Change, 0, len(progressRows)+len(bookshelfRows)+len(cacheRows))
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
	for _, g := range cacheRows {
		changes = append(changes, Change{
			Entity:    EntityGalleryCache,
			GalleryID: g.GalleryID,
			Token:     g.Token,
			Op:        OpUpsert,
			Row:       galleryCacheRow(g),
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
	case EntityGalleryCache:
		g, err := client.GalleryCache.Query().
			Where(
				gallerycache.GalleryID(galleryID),
				gallerycache.Token(token),
			).
			Only(ctx)
		if err != nil {
			if ent.IsNotFound(err) {
				return nil, nil
			}
			return nil, fmt.Errorf("fetch gallery cache: %w", err)
		}
		return galleryCacheRow(g), nil
	default:
		return nil, nil
	}
}

// galleryCacheRow converts a stored cache row into the wire representation.
func galleryCacheRow(g *ent.GalleryCache) *Row {
	return &Row{
		CreatedAt:          g.CreatedAt,
		UpdatedAt:          g.UpdatedAt,
		Title:              g.Title,
		TitleJPN:           g.TitleJpn,
		Category:           g.Category,
		Thumbnail:          g.Thumbnail,
		PageCount:          g.PageCount,
		Rating:             g.Rating,
		RatingCount:        g.RatingCount,
		Uploader:           g.Uploader,
		Posted:             g.Posted,
		PostedAt:           g.PostedAt,
		Language:           g.Language,
		Translated:         g.Translated,
		FileSize:           g.FileSize,
		Favorited:          g.Favorited,
		Expunged:           g.Expunged,
		Tags:               g.Tags,
		Pages:              g.Pages,
		Thumbnails:         g.Thumbnails,
		MetaFetchedAt:      g.MetaFetchedAt,
		DetailsFetchedAt:   g.DetailsFetchedAt,
		PagesFetchedAt:     g.PagesFetchedAt,
		ThumbnailFetchedAt: g.ThumbnailFetchedAt,
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
