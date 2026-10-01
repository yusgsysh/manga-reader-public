// Package gallerycache persists the latest known metadata and page list for a
// gallery so that bookshelf / reading history stay usable when the upstream
// ExHentai is unreachable.
package gallerycache

import (
	"context"
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

// UpsertMeta writes metadata into the cache on a "first value wins" basis: the
// row is created on first write and existing non-empty values are never
// overwritten. Only fields that are still empty are filled, because upstream
// metadata changes infrequently and we don't want repeated views to churn it.
func UpsertMeta(ctx context.Context, client *ent.Client, galleryID int64, token string, snap model.GalleryCacheSnapshot) error {
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
	changed := false

	if existing.Title == "" && snap.Title != "" {
		update.SetTitle(snap.Title)
		changed = true
	}
	if existing.TitleJpn == "" && snap.TitleJPN != "" {
		update.SetTitleJpn(snap.TitleJPN)
		changed = true
	}
	if existing.Category == "" && snap.Category != "" {
		update.SetCategory(snap.Category)
		changed = true
	}
	if existing.Thumbnail == "" && snap.Thumbnail != "" {
		update.SetThumbnail(snap.Thumbnail)
		changed = true
	}
	if existing.PageCount == 0 && snap.PageCount != 0 {
		update.SetPageCount(snap.PageCount)
		changed = true
	}
	if existing.Rating == 0 && snap.Rating != 0 {
		update.SetRating(snap.Rating)
		changed = true
	}
	if existing.RatingCount == 0 && snap.RatingCount != 0 {
		update.SetRatingCount(snap.RatingCount)
		changed = true
	}
	if existing.Uploader == "" && snap.Uploader != "" {
		update.SetUploader(snap.Uploader)
		changed = true
	}
	if existing.Posted == "" && snap.Posted != "" {
		update.SetPosted(snap.Posted)
		changed = true
	}
	if existing.PostedAt == nil && snap.PostedAt != nil {
		update.SetPostedAt(*snap.PostedAt)
		changed = true
	}
	if existing.Language == "" && snap.Language != "" {
		update.SetLanguage(snap.Language)
		changed = true
	}
	if !existing.Translated && snap.Translated {
		update.SetTranslated(true)
		changed = true
	}
	if existing.FileSize == "" && snap.FileSize != "" {
		update.SetFileSize(snap.FileSize)
		changed = true
	}
	if existing.Favorited == 0 && snap.Favorited != 0 {
		update.SetFavorited(snap.Favorited)
		changed = true
	}
	if !existing.Expunged && snap.Expunged {
		update.SetExpunged(true)
		changed = true
	}
	if len(existing.Tags) == 0 && len(snap.Tags) > 0 {
		update.SetTags(snap.Tags)
		changed = true
	}

	if !changed {
		return nil
	}
	return update.SetMetaFetchedAt(now).Exec(ctx)
}

func countPageThumbnails(pages []model.CachedPage) int {
	n := 0
	for _, p := range pages {
		if p.Thumbnail != nil {
			n++
		}
	}
	return n
}

// UpsertPages stores the page list on first write, with one exception: an
// existing list is replaced when the incoming one carries more page-thumbnail
// geometry. Page URLs are stable for a gallery, but the sprite geometry was
// added later, so rows cached before it existed (and locked in by the original
// first-write-wins rule) are refreshed here as soon as a scrape supplies the
// richer data. A list with equal or fewer thumbnails never overwrites, so
// existing data is not downgraded.
func UpsertPages(ctx context.Context, client *ent.Client, galleryID int64, token string, pages []model.CachedPage) error {
	existing, found, err := Get(ctx, client, galleryID, token)
	if err != nil {
		return err
	}
	now := time.Now().UTC()

	if !found {
		return client.GalleryCache.Create().
			SetGalleryID(galleryID).
			SetToken(token).
			SetPages(pages).
			SetPagesFetchedAt(now).
			Exec(ctx)
	}

	if len(existing.Pages) > 0 &&
		countPageThumbnails(pages) <= countPageThumbnails(existing.Pages) {
		return nil
	}

	return client.GalleryCache.UpdateOneID(existing.ID).
		SetPages(pages).
		SetPagesFetchedAt(now).
		Exec(ctx)
}
