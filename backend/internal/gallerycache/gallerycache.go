// Package gallerycache persists the latest known metadata and page list for a
// gallery so that bookshelf / reading history stay usable when the upstream
// ExHentai is unreachable.
package gallerycache

import (
	"context"
	"slices"
	"time"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/gallerycache"
	"manga-reader/internal/ent/predicate"
	"manga-reader/internal/model"
)

// Ref identifies a gallery in the cache.
type Ref struct {
	GalleryID int64
	Token     string
}

// Get returns the cached row for a gallery and whether it exists.
func Get(ctx context.Context, client *ent.Client, galleryID int64, token string) (*ent.GalleryCache, bool, error) {
	row, err := client.GalleryCache.Query().
		Where(
			gallerycache.GalleryID(galleryID),
			gallerycache.Token(token),
		).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return row, true, nil
}

// GetMany returns the cached rows for the given refs, keyed by ref.
func GetMany(ctx context.Context, client *ent.Client, refs []Ref) (map[Ref]*ent.GalleryCache, error) {
	result := make(map[Ref]*ent.GalleryCache, len(refs))
	if len(refs) == 0 {
		return result, nil
	}

	preds := make([]predicate.GalleryCache, 0, len(refs))
	for _, r := range refs {
		preds = append(preds, gallerycache.And(
			gallerycache.GalleryID(r.GalleryID),
			gallerycache.Token(r.Token),
		))
	}

	rows, err := client.GalleryCache.Query().
		Where(gallerycache.Or(preds...)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[Ref{GalleryID: row.GalleryID, Token: row.Token}] = row
	}
	return result, nil
}

// UpsertMeta writes metadata into the cache, keeping it in sync with the API:
// every incoming non-empty value overwrites the stored one, while empty/zero
// incoming values leave the existing value untouched. The latter matters
// because the two sources carry different subsets (metadata omits language /
// favorited / posted-relative fields that details provides), so a later
// metadata write must not blank out richer detail fields. The cache is the
// single source of truth for bookshelf and history metadata, so it tracks the
// latest upstream response rather than freezing the first one seen. The
// fetched-at timestamp is always refreshed, even when no field changed, so the
// online endpoints can treat a recent row as fresh.
func UpsertMeta(ctx context.Context, client *ent.Client, galleryID int64, token string, snap model.GalleryCacheSnapshot) error {
	return upsertMeta(ctx, client, galleryID, token, snap, false)
}

// UpsertDetails is UpsertMeta for a details scrape: it additionally stamps
// details_fetched_at so the details endpoint can serve the row within its TTL.
func UpsertDetails(ctx context.Context, client *ent.Client, galleryID int64, token string, snap model.GalleryCacheSnapshot) error {
	return upsertMeta(ctx, client, galleryID, token, snap, true)
}

func upsertMeta(ctx context.Context, client *ent.Client, galleryID int64, token string, snap model.GalleryCacheSnapshot, details bool) error {
	existing, found, err := Get(ctx, client, galleryID, token)
	if err != nil {
		return err
	}
	now := time.Now().UTC()

	if !found {
		create := client.GalleryCache.Create().
			SetGalleryID(galleryID).
			SetToken(token).
			SetMetaFetchedAt(now)
		if details {
			create.SetDetailsFetchedAt(now)
		}
		if snap.Title != "" {
			create.SetTitle(snap.Title)
		}
		if snap.TitleJPN != "" {
			create.SetTitleJpn(snap.TitleJPN)
		}
		if snap.Category != "" {
			create.SetCategory(snap.Category)
		}
		if snap.Thumbnail != "" {
			create.SetThumbnail(snap.Thumbnail)
		}
		if snap.PageCount != 0 {
			create.SetPageCount(snap.PageCount)
		}
		if snap.Rating != 0 {
			create.SetRating(snap.Rating)
		}
		if snap.RatingCount != 0 {
			create.SetRatingCount(snap.RatingCount)
		}
		if snap.Uploader != "" {
			create.SetUploader(snap.Uploader)
		}
		if snap.Posted != "" {
			create.SetPosted(snap.Posted)
		}
		if snap.PostedAt != nil {
			create.SetPostedAt(*snap.PostedAt)
		}
		if snap.Language != "" {
			create.SetLanguage(snap.Language)
		}
		if snap.Translated {
			create.SetTranslated(true)
		}
		if snap.FileSize != "" {
			create.SetFileSize(snap.FileSize)
		}
		if snap.Favorited != 0 {
			create.SetFavorited(snap.Favorited)
		}
		if snap.Expunged {
			create.SetExpunged(true)
		}
		if len(snap.Tags) > 0 {
			create.SetTags(snap.Tags)
		}
		return create.Exec(ctx)
	}

	update := client.GalleryCache.UpdateOneID(existing.ID)

	if snap.Title != "" {
		update.SetTitle(snap.Title)
	}
	if snap.TitleJPN != "" {
		update.SetTitleJpn(snap.TitleJPN)
	}
	if snap.Category != "" {
		update.SetCategory(snap.Category)
	}
	if snap.Thumbnail != "" {
		update.SetThumbnail(snap.Thumbnail)
	}
	if snap.PageCount != 0 {
		update.SetPageCount(snap.PageCount)
	}
	if snap.Rating != 0 {
		update.SetRating(snap.Rating)
	}
	if snap.RatingCount != 0 {
		update.SetRatingCount(snap.RatingCount)
	}
	if snap.Uploader != "" {
		update.SetUploader(snap.Uploader)
	}
	if snap.Posted != "" {
		update.SetPosted(snap.Posted)
	}
	if snap.PostedAt != nil {
		update.SetPostedAt(*snap.PostedAt)
	}
	if snap.Language != "" {
		update.SetLanguage(snap.Language)
	}
	if snap.Translated {
		update.SetTranslated(true)
	}
	if snap.FileSize != "" {
		update.SetFileSize(snap.FileSize)
	}
	if snap.Favorited != 0 {
		update.SetFavorited(snap.Favorited)
	}
	if snap.Expunged {
		update.SetExpunged(true)
	}
	if len(snap.Tags) > 0 {
		update.SetTags(snap.Tags)
	}

	update.SetMetaFetchedAt(now)
	if details {
		update.SetDetailsFetchedAt(now)
	}
	return update.Exec(ctx)
}

// countThumbnails returns the number of thumbnails with actual sprite geometry.
func countThumbnails(thumbnails []model.GalleryPageThumb) int {
	n := 0
	for _, t := range thumbnails {
		if t.SpriteURL != "" {
			n++
		}
	}
	return n
}

// mergeStrings merges incoming page URLs over the stored list index by index. A
// non-empty incoming URL replaces the stored one at the same index; empty
// entries leave the stored value untouched. New indices are appended. The
// result never shrinks, so a shorter (partial) scrape cannot truncate the
// cache.
func mergeStrings(stored, incoming []string) []string {
	n := len(stored)
	if len(incoming) > n {
		n = len(incoming)
	}
	merged := make([]string, n)
	copy(merged, stored)
	for i, v := range incoming {
		if v != "" {
			merged[i] = v
		}
	}
	return merged
}

// mergeThumbs merges incoming thumbnail geometry over the stored list index by
// index, capped at limit (the number of known pages). An entry with a non-empty
// SpriteURL replaces the stored geometry at the same index; empty entries leave
// the stored value untouched. The result never shrinks.
func mergeThumbs(stored, incoming []model.GalleryPageThumb, limit int) []model.GalleryPageThumb {
	n := len(incoming)
	if n > limit {
		n = limit
	}
	if n < len(stored) {
		n = len(stored)
	}
	merged := make([]model.GalleryPageThumb, n)
	copy(merged, stored)
	for i := 0; i < len(incoming) && i < n; i++ {
		if incoming[i].SpriteURL != "" {
			merged[i] = incoming[i]
		}
	}
	return merged
}

// UpsertPages stores the page URL list. Page URLs are stable for a gallery, so
// the stored list is merged index by index: non-empty incoming URLs overwrite,
// empty entries are ignored, and new indices are appended. The list never
// shrinks. pages_fetched_at is always refreshed so the online endpoint can treat
// a recently verified list as fresh; the JSON column is only rewritten when the
// list actually changed.
func UpsertPages(ctx context.Context, client *ent.Client, galleryID int64, token string, pageURLs []string) error {
	existing, found, err := Get(ctx, client, galleryID, token)
	if err != nil {
		return err
	}
	now := time.Now().UTC()

	if !found {
		return client.GalleryCache.Create().
			SetGalleryID(galleryID).
			SetToken(token).
			SetPages(pageURLs).
			SetPagesFetchedAt(now).
			Exec(ctx)
	}

	merged := mergeStrings(existing.Pages, pageURLs)
	update := client.GalleryCache.UpdateOneID(existing.ID).SetPagesFetchedAt(now)
	if !slices.Equal(merged, existing.Pages) {
		update.SetPages(merged)
	}
	return update.Exec(ctx)
}

// UpsertThumbnails stores page-thumbnail geometry independently of the page
// list. Geometry is index-aligned with pages, so it is merged index by index
// (non-empty SpriteURL overwrites, empty leaves the stored value, new indices
// are appended) and capped at the number of known pages. The result never
// shrinks, so a partial scrape cannot drop known geometry. thumbnail_fetched_at
// is always refreshed; the JSON column is only rewritten when the geometry
// changed. A gallery with no cached pages has nothing to align to, so the call
// is a no-op.
func UpsertThumbnails(ctx context.Context, client *ent.Client, galleryID int64, token string, thumbnails []model.GalleryPageThumb) error {
	existing, found, err := Get(ctx, client, galleryID, token)
	if err != nil {
		return err
	}
	if !found || len(existing.Pages) == 0 {
		return nil
	}
	now := time.Now().UTC()

	update := client.GalleryCache.UpdateOneID(existing.ID).SetThumbnailFetchedAt(now)
	if countThumbnails(thumbnails) > 0 {
		merged := mergeThumbs(existing.Thumbnails, thumbnails, len(existing.Pages))
		if !slices.Equal(merged, existing.Thumbnails) {
			update.SetThumbnails(merged)
		}
	}
	return update.Exec(ctx)
}
