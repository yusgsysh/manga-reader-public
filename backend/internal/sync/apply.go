package sync

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"manga-reader/internal/ident"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/gallerycache"
	"manga-reader/internal/ent/readingprogress"
	"manga-reader/internal/ent/syncchange"
	"manga-reader/internal/ent/user"
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
	tombstones, err := pendingTombstones(ctx, client)
	if err != nil {
		return 0, 0, err
	}
	users, err := loadUserDirectory(ctx, client)
	if err != nil {
		return 0, 0, err
	}
	for _, ch := range changes {
		// Accounts are created per instance, so the sender's user UUID may not
		// exist here. Map it to the local account with the same username; a
		// private row that cannot be attributed is skipped instead of being
		// written under a ghost owner nobody can read.
		if ch.Entity != EntityGalleryCache {
			if ch.UserID == ident.Nil() {
				slog.Warn("skipping unowned sync change",
					"entity", ch.Entity, "gallery_id", ch.GalleryID, "token", ch.Token)
				skipped++
				continue
			}
			localID, ok := users.resolve(ch.UserID, ch.Username)
			if !ok {
				slog.Warn("skipping sync change for unknown user",
					"username", ch.Username, "user_id", ch.UserID.String(),
					"entity", ch.Entity, "gallery_id", ch.GalleryID, "token", ch.Token)
				skipped++
				continue
			}
			ch.UserID = localID
		}
		// A pending local deletion outranks an older incoming row. Rows have
		// no stored tombstone, so without this check a peer snapshot/delta
		// could resurrect a row this instance deleted but has not told the
		// peer about yet.
		if ch.Op == OpUpsert && ch.Row != nil {
			if deletedAt, ok := tombstones[changeKeyOf(ch)]; ok &&
				deletedAt.After(clampFutureTimestamp(ch.Row.UpdatedAt)) {
				skipped++
				continue
			}
		}
		ok, applyErr := applyChange(ctx, client, ch)
		if applyErr != nil {
			// A row that fails ent validation (e.g. progress > 1 from a
			// divergent peer) must not wedge replication: the cursor would
			// never advance and every cycle would resend the same batch.
			// Log it and count it as skipped; real DB failures still abort.
			if _, ok := errors.AsType[*ent.ValidationError](applyErr); ok {
				slog.Warn("skipping invalid sync change",
					"entity", ch.Entity, "gallery_id", ch.GalleryID, "token", ch.Token, "error", applyErr)
				skipped++
				continue
			}
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
	// A peer with a clock ahead of ours would otherwise win every future LWW
	// comparison and could permanently revert the user's newer local edits.
	// Clamp incoming timestamps to (approximately) now.
	if ch.Row != nil {
		ch.Row.UpdatedAt = clampFutureTimestamp(ch.Row.UpdatedAt)
		ch.Row.CreatedAt = clampFutureTimestamp(ch.Row.CreatedAt)
	}
	if ch.DeletedAt != nil {
		t := clampFutureTimestamp(*ch.DeletedAt)
		ch.DeletedAt = &t
	}
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

// clampFutureTimestamp caps a peer timestamp at now (plus a small tolerance
// for legitimate clock jitter between machines).
func clampFutureTimestamp(t time.Time) time.Time {
	if t.IsZero() {
		return t
	}
	if limit := time.Now().UTC().Add(time.Second); t.After(limit) {
		return limit
	}
	return t
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
			readingprogress.UserIDEQ(ch.UserID),
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
				readingprogress.UserIDEQ(ch.UserID),
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
			SetUserID(ch.UserID).
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
			readingprogress.UserIDEQ(ch.UserID),
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
			bookshelf.UserIDEQ(ch.UserID),
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
				bookshelf.UserIDEQ(ch.UserID),
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
			SetUserID(ch.UserID).
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
			bookshelf.UserIDEQ(ch.UserID),
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

	owners := make([]ident.UUID, 0, len(progressRows)+len(bookshelfRows))
	for _, p := range progressRows {
		owners = append(owners, p.UserID)
	}
	for _, b := range bookshelfRows {
		owners = append(owners, b.UserID)
	}
	usernames, err := loadUsernames(ctx, client, owners)
	if err != nil {
		return nil, err
	}

	changes := make([]Change, 0, len(progressRows)+len(bookshelfRows)+len(cacheRows))
	for _, p := range progressRows {
		changes = append(changes, Change{
			UserID:    p.UserID,
			Username:  usernames[p.UserID],
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
			UserID:    b.UserID,
			Username:  usernames[b.UserID],
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

// changeKey identifies one replicated row: per-user, per-entity, keyed by the
// gallery/token pair (gallery_cache rows carry the nil owner).
type changeKey struct {
	userID    ident.UUID
	entity    string
	galleryID int64
	token     string
}

func changeKeyOf(ch Change) changeKey {
	return changeKey{userID: ch.UserID, entity: ch.Entity, galleryID: ch.GalleryID, token: ch.Token}
}

// userDirectory resolves a peer-side owner onto a local account. Accounts are
// created independently on each instance (random UUIDv7), so the same person
// has a different user_id on the other side; the username is the stable link
// between them.
type userDirectory struct {
	byID   map[ident.UUID]ident.UUID
	byName map[string]ident.UUID
}

func loadUserDirectory(ctx context.Context, client *ent.Client) (*userDirectory, error) {
	users, err := client.User.Query().All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read sync users: %w", err)
	}
	d := &userDirectory{
		byID:   make(map[ident.UUID]ident.UUID, len(users)),
		byName: make(map[string]ident.UUID, len(users)),
	}
	for _, u := range users {
		d.byID[u.ID] = u.ID
		d.byName[u.Username] = u.ID
	}
	return d, nil
}

// resolve returns the local owner for a remote change. The sender's UUID wins
// when it names an account here (same database or an earlier mapping);
// otherwise the username identifies the same person. Changes from peers that
// predate username propagation keep their original UUID, matching the old
// behaviour.
func (d *userDirectory) resolve(id ident.UUID, username string) (ident.UUID, bool) {
	if local, ok := d.byID[id]; ok {
		return local, true
	}
	if username == "" {
		return id, true
	}
	if local, ok := d.byName[username]; ok {
		return local, true
	}
	return ident.Nil(), false
}

// loadUsernames maps owners to usernames for the wire payload so the receiver
// can match accounts whose UUIDs differ.
func loadUsernames(ctx context.Context, client *ent.Client, ids []ident.UUID) (map[ident.UUID]string, error) {
	names := make(map[ident.UUID]string, len(ids))
	unique := make([]ident.UUID, 0, len(ids))
	seen := make(map[ident.UUID]struct{}, len(ids))
	for _, id := range ids {
		if id == ident.Nil() {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return names, nil
	}
	users, err := client.User.Query().Where(user.IDIn(unique...)).All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read sync usernames: %w", err)
	}
	for _, u := range users {
		names[u.ID] = u.Username
	}
	return names, nil
}

// pendingTombstones returns the deletion time of every row whose newest outbox
// entry is a delete, i.e. a removal the peer has not acknowledged yet. An
// upsert entry after the delete clears the tombstone (the row exists again).
func pendingTombstones(ctx context.Context, client *ent.Client) (map[changeKey]time.Time, error) {
	entries, err := client.SyncChange.Query().
		Order(ent.Asc(syncchange.FieldID)).
		All(ctx)
	if err != nil {
		return nil, fmt.Errorf("read pending tombstones: %w", err)
	}
	tombstones := make(map[changeKey]time.Time, len(entries))
	for _, e := range entries {
		k := changeKey{userID: e.UserID, entity: e.Entity, galleryID: e.GalleryID, token: e.Token}
		if e.Op == OpDelete {
			tombstones[k] = e.ChangedAt
		} else {
			delete(tombstones, k)
		}
	}
	return tombstones, nil
}

// BootstrapChanges returns everything a peer needs on the first exchange of a
// sync epoch: the full live snapshot plus the delete tombstones still held by
// the outbox.
//
// A snapshot only carries rows that still exist, so it cannot express a
// deletion made before the first sync. Without the tombstones the peer would
// keep its own copy of the deleted row — and its snapshot would then resurrect
// the row on this side. Tombstones are LWW-guarded on the receiving end, so a
// genuinely newer peer row still wins.
//
// The watermark is read before the export: a row written while the snapshot is
// collected keeps an outbox id above it and is pushed as a delta next cycle.
func BootstrapChanges(ctx context.Context, client *ent.Client) ([]Change, int, error) {
	watermark, err := maxOutboxID(ctx, client)
	if err != nil {
		return nil, 0, err
	}
	changes, err := ExportSnapshot(ctx, client)
	if err != nil {
		return nil, 0, err
	}
	deltas, _, err := ReadChangesSince(ctx, client, 0)
	if err != nil {
		return nil, 0, err
	}

	live := make(map[changeKey]struct{}, len(changes))
	for _, ch := range changes {
		live[changeKeyOf(ch)] = struct{}{}
	}
	for _, ch := range deltas {
		if ch.Op != OpDelete {
			continue
		}
		if _, exists := live[changeKeyOf(ch)]; exists {
			// The row was re-created after the delete, so the snapshot is the
			// current state; replaying the older tombstone would erase it.
			continue
		}
		changes = append(changes, ch)
	}
	return changes, watermark, nil
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
	latest := make(map[changeKey]*ent.SyncChange, len(entries))
	order := make([]changeKey, 0, len(entries))
	for _, e := range entries {
		k := changeKey{userID: e.UserID, entity: e.Entity, galleryID: e.GalleryID, token: e.Token}
		if _, seen := latest[k]; !seen {
			order = append(order, k)
		}
		latest[k] = e
	}

	changes := make([]Change, 0, len(order))
	owners := make([]ident.UUID, 0, len(order))
	for _, k := range order {
		owners = append(owners, k.userID)
	}
	usernames, err := loadUsernames(ctx, client, owners)
	if err != nil {
		return nil, 0, err
	}
	for _, k := range order {
		e := latest[k]
		if e.Op == OpDelete {
			deletedAt := e.ChangedAt
			changes = append(changes, Change{
				UserID:    e.UserID,
				Username:  usernames[e.UserID],
				Entity:    e.Entity,
				GalleryID: e.GalleryID,
				Token:     e.Token,
				Op:        OpDelete,
				DeletedAt: &deletedAt,
			})
			continue
		}
		row, err := fetchRow(ctx, client, e.UserID, e.Entity, e.GalleryID, e.Token)
		if err != nil {
			return nil, 0, err
		}
		if row == nil {
			deletedAt := e.ChangedAt
			changes = append(changes, Change{
				UserID:    e.UserID,
				Username:  usernames[e.UserID],
				Entity:    e.Entity,
				GalleryID: e.GalleryID,
				Token:     e.Token,
				Op:        OpDelete,
				DeletedAt: &deletedAt,
			})
			continue
		}
		changes = append(changes, Change{
			UserID:    e.UserID,
			Username:  usernames[e.UserID],
			Entity:    e.Entity,
			GalleryID: e.GalleryID,
			Token:     e.Token,
			Op:        OpUpsert,
			Row:       row,
		})
	}
	return changes, maxID, nil
}

func fetchRow(ctx context.Context, client *ent.Client, userID ident.UUID, entity string, galleryID int64, token string) (*Row, error) {
	switch entity {
	case EntityReadingProgress:
		p, err := client.ReadingProgress.Query().
			Where(
				readingprogress.UserIDEQ(userID),
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
				bookshelf.UserIDEQ(userID),
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
