package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"manga-reader/internal/exhentai"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/model"
)

// The bookshelf and the reading history build their cards from gallery_cache,
// which is the only source of a gallery's title, category, cover and page
// count once the upstream list entry is gone. A row there can hold nothing but
// the pages and thumbnails of a walk: the writes that store those create the
// row when none exists yet, and the metadata write meant to follow can fail
// (an upstream timeout) or be refused (a gallery the source reports no
// metadata for). The card then had nothing to draw. These helpers fill in such
// rows before a list is answered.

const (
	// galleryMetaFillTimeout bounds one upstream metadata fetch started for a
	// shelf row. It is longer than a list request would normally wait: the
	// row stays broken without it, and the fetch also benefits every later
	// request for the same gallery.
	galleryMetaFillTimeout = 60 * time.Second
	// galleryMetaFillCap bounds how many fetches one list request starts, so
	// a history full of old broken rows cannot turn one request into dozens.
	galleryMetaFillCap = 8
	// galleryMetaFillWorkers is how many of those run at once.
	galleryMetaFillWorkers = 4
)

// galleryMetaFillGroup collapses concurrent fetches for the same gallery, so
// the bookshelf, the history and a retry after a visible frame share one
// upstream request.
var galleryMetaFillGroup singleflight.Group

// fillShelfMeta fetches the metadata of the refs missing from meta and adds
// each fetched row to meta, so the caller's response shows it at once. It is
// best-effort: when the request ends the fetches stop, and a gallery that
// cannot be read keeps its card as it was, for a later request to retry.
//
// Rows already in the cache are never touched, so the common case makes no
// upstream request at all: only a row that cannot name its gallery is fetched.
func (s *Server) fillShelfMeta(ctx context.Context, refs []gallerycache.Ref, meta map[gallerycache.Ref]*model.Gallery) {
	if s.Client == nil || meta == nil {
		return
	}
	missing := make([]gallerycache.Ref, 0, len(refs))
	seen := make(map[gallerycache.Ref]bool, len(refs))
	for _, ref := range refs {
		if _, ok := meta[ref]; ok || seen[ref] {
			continue
		}
		seen[ref] = true
		missing = append(missing, ref)
	}
	if len(missing) == 0 {
		return
	}
	if len(missing) > galleryMetaFillCap {
		// The first entries are the newest ones in both lists, and the ones
		// the user is about to look at; the cap only keeps a page of history
		// from holding its response for a minute.
		missing = missing[:galleryMetaFillCap]
	}
	slog.Info("shelf entries have no metadata; fetching it", "count", len(missing))

	var wg sync.WaitGroup
	var mu sync.Mutex
	sem := make(chan struct{}, galleryMetaFillWorkers)
	for _, ref := range missing {
		wg.Add(1)
		go func() {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			g := s.fetchShelfMeta(ctx, ref.GalleryID, ref.Token)
			if g == nil {
				return
			}
			mu.Lock()
			// The card shows the metadata whether or not the write took: a
			// row the cache could not keep is not the list's problem.
			meta[ref] = g
			mu.Unlock()
		}()
	}
	wg.Wait()
}

// fetchShelfMeta reads one gallery's metadata and persists it, returning it
// for this response. The fetch gets its own budget but stops once the request
// is gone, so a list whose client left does not hold a walk open.
func (s *Server) fetchShelfMeta(ctx context.Context, galleryID int64, token string) *model.Gallery {
	key := fmt.Sprintf("%d:%s", galleryID, token)
	res, _, _ := galleryMetaFillGroup.Do(key, func() (any, error) {
		fillCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), galleryMetaFillTimeout)
		defer cancel()
		go func() {
			// Another caller waiting on this fetch keeps it alive: only
			// this request's own end cancels it, and the timeout bounds it.
			select {
			case <-ctx.Done():
				cancel()
			case <-fillCtx.Done():
			}
		}()

		meta, err := exhentai.PostGalleryMetadata(fillCtx, s.Client, galleryID, token)
		if err != nil {
			// "no metadata" is the source saying it has nothing for this
			// gallery: a withdrawn or expunged one that no list can name.
			if errors.Is(err, exhentai.ErrNoMetadata) {
				s.dropEmptyCacheRow(fillCtx, galleryID, token)
			}
			slog.Debug("shelf metadata fetch failed", "id", galleryID, "error", err)
			return (*model.Gallery)(nil), nil
		}
		gallery := model.ConvertMetadataToGallery(meta)
		if db := s.cacheDB(); db != nil {
			snap := cacheMetaFromGallery(gallery)
			var writeErr error
			if gallerycache.HasMetadata(snap) {
				writeErr = gallerycache.UpsertMeta(fillCtx, db, galleryID, token, snap)
			} else {
				s.dropEmptyCacheRow(fillCtx, galleryID, token)
			}
			if writeErr != nil {
				slog.Warn("shelf metadata cache write failed", "id", galleryID, "error", writeErr)
			} else {
				s.recordGalleryCacheChange(fillCtx, galleryID, token)
			}
		}
		return gallery, nil
	})
	g, _ := res.(*model.Gallery)
	return g
}

// dropEmptyCacheRow removes a row that holds nothing but pages and thumbnails,
// for a gallery the source could not name. Kept, it would make every list that
// references the gallery fetch it again and draw a blank card; the reading
// progress and the bookshelf entry that point at it are other tables and stay,
// so the record is still listed (with its Details unavailable note) and
// becomes whole again if the gallery ever comes back.
func (s *Server) dropEmptyCacheRow(ctx context.Context, galleryID int64, token string) {
	db := s.cacheDB()
	if db == nil {
		return
	}
	row, found, err := gallerycache.Get(ctx, db, galleryID, token)
	if err != nil || !found || !gallerycache.IsEmptyRow(row) {
		return
	}
	if err := gallerycache.Delete(ctx, db, galleryID, token); err != nil {
		slog.Warn("drop empty gallery cache row failed", "id", galleryID, "error", err)
		return
	}
	s.recordGalleryCacheChange(ctx, galleryID, token)
}
