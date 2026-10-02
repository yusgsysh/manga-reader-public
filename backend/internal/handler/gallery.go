package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	json "encoding/json/v2"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/ent"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/model"
)

// galleryPagesHardCap bounds an entire /pages scrape (including the detached
// background work that continues after a client disconnects).
const galleryPagesHardCap = 10 * time.Minute

// galleryPagesHub coalesces concurrent page-list scrapes for the same gallery
// into a single upstream walk. Unlike a plain singleflight, subscribers receive
// the batches already scraped as they arrive instead of blocking until the
// whole list is ready, so opening a gallery while another scan of it is still
// running streams progressively instead of stalling on the loading skeleton.
// Shared by the streaming handler and the bookshelf prefetch.
var galleryPagesHub = newPagesHub()

// galleryCacheTTL bounds how long the online endpoints may serve a cached
// metadata / details / pages row before re-fetching upstream. Short enough to
// stay fresh, long enough that repeated views do not scrape ExHentai.
const galleryCacheTTL = 30 * time.Minute

func cacheFresh(at *time.Time, ttl time.Duration) bool {
	return at != nil && time.Since(*at) < ttl
}

// cacheDB returns the ent client when a database is configured. Some tests
// construct a Server without a DB; callers must tolerate a nil result.
func (s *Server) cacheDB() *ent.Client {
	if s.DB == nil {
		return nil
	}
	return s.DB.Client
}

func cacheMetaFromGallery(g *model.Gallery) model.GalleryCacheSnapshot {
	return model.GalleryCacheSnapshot{
		Title:       g.Title,
		TitleJPN:    g.TitleJPN,
		Category:    string(g.Category),
		Thumbnail:   g.Thumbnail,
		PageCount:   g.PageCount,
		Rating:      g.Rating,
		RatingCount: g.RatingCount,
		Uploader:    g.Uploader,
		PostedAt:    g.PostedAt,
		FileSize:    g.FileSize,
		Expunged:    g.Expunged,
		Tags:        g.Tags,
	}
}

func cacheMetaFromDetails(d model.GalleryDetail) model.GalleryCacheSnapshot {
	tags := make([]model.Tag, len(d.Tags))
	for i, t := range d.Tags {
		tags[i] = model.Tag{Namespace: t.Namespace, Name: t.Name}
	}
	return model.GalleryCacheSnapshot{
		Title:       d.Title,
		TitleJPN:    d.TitleJpn,
		Category:    string(model.MapCategory(d.Cat)),
		Thumbnail:   d.Cover,
		PageCount:   d.Length,
		Rating:      d.Rating,
		RatingCount: d.RatingCount,
		Uploader:    d.Uploader,
		Posted:      d.Posted,
		Language:    d.Language,
		Translated:  d.Translated == "TR",
		FileSize:    d.FileSize,
		Favorited:   d.Favorited,
		Tags:        tags,
	}
}

func galleryFromCache(row *ent.GalleryCache) *model.Gallery {
	return &model.Gallery{
		ID:          row.GalleryID,
		Token:       row.Token,
		Title:       row.Title,
		TitleJPN:    row.TitleJpn,
		Category:    model.GalleryCategory(row.Category),
		Thumbnail:   row.Thumbnail,
		PageCount:   row.PageCount,
		Rating:      row.Rating,
		RatingCount: row.RatingCount,
		Uploader:    row.Uploader,
		PostedAt:    row.PostedAt,
		Tags:        row.Tags,
		FileSize:    row.FileSize,
		Expunged:    row.Expunged,
	}
}

func galleryDetailsFromCache(row *ent.GalleryCache) gin.H {
	return gin.H{
		"id":           row.GalleryID,
		"token":        row.Token,
		"domain":       "",
		"title":        row.Title,
		"title_jpn":    row.TitleJpn,
		"cover":        row.Thumbnail,
		"category":     model.GalleryCategory(row.Category),
		"uploader":     row.Uploader,
		"posted":       row.Posted,
		"parent":       0,
		"visible":      "",
		"language":     row.Language,
		"translated":   row.Translated,
		"file_size":    row.FileSize,
		"page_count":   row.PageCount,
		"favorited":    row.Favorited,
		"rating_count": row.RatingCount,
		"rating":       row.Rating,
		"tags":         row.Tags,
	}
}

// parseGalleryIDToken reads and validates the :id/:token path params.
func parseGalleryIDToken(c *gin.Context) (int64, string, bool) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery id"})
		return 0, "", false
	}
	token := c.Param("token")
	if token == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid gallery token"})
		return 0, "", false
	}
	return id, token, true
}

// cacheUnavailable reports whether the offline cache backend is configured.
func (s *Server) cacheUnavailable(c *gin.Context) bool {
	if s.cacheDB() == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "gallery cache not configured"})
		return true
	}
	return false
}

func (s *Server) handleGetGallery(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}

	ctx := c.Request.Context()

	// Serve recent cache without touching upstream; stale/missing rows fall
	// through to the live fetch (and a failed fetch still returns 502 so the
	// frontend falls back to the cache endpoint).
	if db := s.cacheDB(); db != nil {
		if row, found, err := gallerycache.Get(ctx, db, id, token); err != nil {
			slog.Warn("gallery cache read failed", "id", id, "error", err)
		} else if found && row.Title != "" && cacheFresh(row.MetaFetchedAt, galleryCacheTTL) {
			c.JSON(http.StatusOK, galleryFromCache(row))
			return
		}
	}

	meta, err := exhentai.PostGalleryMetadata(ctx, s.Client, id, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("exhentai api failed: %v", err)})
		return
	}

	gallery := model.ConvertMetadataToGallery(meta)
	if db := s.cacheDB(); db != nil {
		if cacheErr := gallerycache.UpsertMeta(ctx, db, id, token, cacheMetaFromGallery(gallery)); cacheErr != nil {
			slog.Warn("gallery cache meta upsert failed", "id", id, "error", cacheErr)
		}
	}
	c.JSON(http.StatusOK, gallery)
}

func (s *Server) handleGalleryDetails(c *gin.Context) {
	galleryID, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}

	u := exhentai.GalleryURL(c.Param("id"), token)
	ctx := c.Request.Context()

	// Serve recent details cache; only rows written by a details scrape count
	// (details_fetched_at), so metadata-only rows still trigger the scrape.
	if db := s.cacheDB(); db != nil {
		if row, found, err := gallerycache.Get(ctx, db, galleryID, token); err != nil {
			slog.Warn("gallery cache read failed", "id", galleryID, "error", err)
		} else if found && row.Title != "" && cacheFresh(row.DetailsFetchedAt, galleryCacheTTL) {
			c.JSON(http.StatusOK, galleryDetailsFromCache(row))
			return
		}
	}

	details, err := exhentai.ScrapeGalleryDetails(ctx, s.Client, u)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery details failed: %v", err)})
		return
	}

	tags := make([]model.Tag, len(details.Tags))
	for i, t := range details.Tags {
		tags[i] = model.Tag{Namespace: t.Namespace, Name: t.Name}
	}

	if db := s.cacheDB(); db != nil {
		if cacheErr := gallerycache.UpsertDetails(ctx, db, galleryID, token, cacheMetaFromDetails(details)); cacheErr != nil {
			slog.Warn("gallery cache details upsert failed", "id", galleryID, "error", cacheErr)
		}
	}

	c.JSON(http.StatusOK, gin.H{
		"id":           details.GalleryID,
		"token":        details.Token,
		"domain":       details.Domain,
		"title":        details.Title,
		"title_jpn":    details.TitleJpn,
		"cover":        details.Cover,
		"category":     model.MapCategory(details.Cat),
		"uploader":     details.Uploader,
		"posted":       details.Posted,
		"parent":       details.Parent,
		"visible":      details.Visible,
		"language":     details.Language,
		"translated":   details.Translated == "TR",
		"file_size":    details.FileSize,
		"page_count":   details.Length,
		"favorited":    details.Favorited,
		"rating_count": details.RatingCount,
		"rating":       details.Rating,
		"tags":         tags,
	})
}

// galleryPagesLine is one NDJSON line of the streaming /pages response.
type galleryPagesLine struct {
	Type string `json:"type"`
	// meta fields
	ID    string `json:"id,omitempty"`
	Token string `json:"token,omitempty"`
	// page fields
	PageURL   string                  `json:"page_url,omitempty"`
	Index     *int                    `json:"index,omitempty"`
	Thumbnail *model.GalleryPageThumb `json:"thumbnail,omitempty"`
	// terminal fields
	Total *int   `json:"total,omitempty"`
	Error string `json:"error,omitempty"`
}

// galleryPagesStream is one in-flight page-list scrape that any number of
// requests can subscribe to. The leader appends batches under the lock and
// broadcasts; subscribers drain the accumulated log and then follow it live.
// A late subscriber is replayed the already-scraped prefix, so it never blocks
// until the whole list is ready.
type galleryPagesStream struct {
	mu       sync.Mutex
	cond     *sync.Cond
	pages    []model.CachedPage
	total    int
	done     bool
	err      error
	finished atomic.Bool
}

func newGalleryPagesStream() *galleryPagesStream {
	s := &galleryPagesStream{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *galleryPagesStream) append(total int, batch []model.CachedPage) {
	s.mu.Lock()
	if total > 0 {
		s.total = total
	}
	s.pages = append(s.pages, batch...)
	s.mu.Unlock()
	s.cond.Broadcast()
}

func (s *galleryPagesStream) finish(err error) {
	s.mu.Lock()
	s.done = true
	s.err = err
	s.mu.Unlock()
	s.finished.Store(true)
	s.cond.Broadcast()
}

// drain calls emit for every page appended after cursor, blocking until the
// scrape finishes or ctx is cancelled. It returns the number of pages the
// request received and the terminal error (ctx.Err() when cancelled). emit
// must not call back into the stream.
//
// A cancelled subscriber must be able to wake out of cond.Wait, so ctx
// cancellation broadcasts the condition variable; the walk itself keeps
// running detached for the remaining subscribers.
func (s *galleryPagesStream) drain(
	ctx context.Context,
	cursor int,
	emit func(total int, batch []model.CachedPage) error,
) (int, error) {
	stopWake := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer stopWake()

	for {
		s.mu.Lock()
		for cursor >= len(s.pages) && !s.done && ctx.Err() == nil {
			s.cond.Wait()
		}
		if ctx.Err() != nil {
			err := ctx.Err()
			s.mu.Unlock()
			return cursor, err
		}
		if cursor >= len(s.pages) {
			err := s.err
			s.mu.Unlock()
			return cursor, err
		}
		batch := append([]model.CachedPage(nil), s.pages[cursor:]...)
		cursor = len(s.pages)
		total := s.total
		s.mu.Unlock()

		if emit != nil {
			if err := emit(total, batch); err != nil {
				return cursor, err
			}
		}
	}
}

// pagesHub maps a gallery key to its in-flight scrape.
type pagesHub struct {
	mu      sync.Mutex
	streams map[string]*galleryPagesStream
}

func newPagesHub() *pagesHub {
	return &pagesHub{streams: make(map[string]*galleryPagesStream)}
}

// acquire returns the in-flight stream for key and whether this caller created
// it (the leader, which must start the upstream scrape). A finished stream is
// never handed out: a new request starts a fresh scrape instead of replaying a
// completed one that is about to be released.
func (h *pagesHub) acquire(key string) (*galleryPagesStream, bool) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if s, ok := h.streams[key]; ok && !s.finished.Load() {
		return s, false
	}
	s := newGalleryPagesStream()
	h.streams[key] = s
	return s, true
}

// release drops key once its scrape finished. A stream replaced by a newer one
// is left alone (only the current entry is removed).
func (h *pagesHub) release(key string, s *galleryPagesStream) {
	h.mu.Lock()
	if h.streams[key] == s {
		delete(h.streams, key)
	}
	h.mu.Unlock()
}

// scrapeGalleryPages publishes the page-list scrape for (galleryID, token) to
// the hub and streams it to the caller through emit. The first caller (leader)
// starts a detached background walk that appends every batch to the shared
// stream and caches the verified list; concurrent callers subscribe to the same
// stream and immediately receive the already-scraped pages. Cancelling ctx only
// detaches this subscriber — the walk keeps running for everyone else. It
// returns the number of pages streamed and the terminal error.
func (s *Server) scrapeGalleryPages(
	ctx context.Context,
	galleryID int64,
	token string,
	emit func(total int, batch []model.CachedPage) error,
) (int, error) {
	key := fmt.Sprintf("%d:%s", galleryID, token)
	stream, leader := galleryPagesHub.acquire(key)
	if leader {
		s.runGalleryPagesScrape(key, stream, galleryID, token)
	}
	return stream.drain(ctx, 0, emit)
}

// runGalleryPagesScrape drives one detached page-list walk and publishes it to
// stream. The walk is never aborted by an emit failure so it continues after
// every subscriber disconnects, letting the verified list still reach the
// cache. A complete scrape is the only thing ever persisted.
func (s *Server) runGalleryPagesScrape(
	key string,
	stream *galleryPagesStream,
	galleryID int64,
	token string,
) {
	go func() {
		defer galleryPagesHub.release(key, stream)

		ctx, cancel := context.WithTimeout(context.Background(), galleryPagesHardCap)
		defer cancel()

		u := exhentai.GalleryURL(strconv.FormatInt(galleryID, 10), token)
		var pages []model.CachedPage
		total := 0
		err := exhentai.StreamGalleryPages(ctx, s.Client, u, func(t int, batch []model.CachedPage) error {
			total = t
			pages = append(pages, batch...)
			stream.append(t, batch)
			return nil
		})
		if err == nil && (total <= 0 || len(pages) != total) {
			// StreamGalleryPages only returns nil once the received count equals
			// the ".gpc" total; re-check at the cache boundary so only a
			// verified, complete list is ever persisted.
			err = fmt.Errorf("incomplete page list: got %d pages, want %d", len(pages), total)
		}
		if err == nil {
			// UpsertPages stores the whole list in a single row write, so this
			// is the one and only cache write for the scrape: it either commits
			// in full or leaves the previous value untouched.
			if db := s.cacheDB(); db != nil {
				if cacheErr := gallerycache.UpsertPages(ctx, db, galleryID, token, pages); cacheErr != nil {
					slog.Warn("gallery cache pages upsert failed", "id", galleryID, "error", cacheErr)
				}
			}
		}
		stream.finish(err)
	}()
}

// writeNDJSONLine marshal line and writes it followed by a newline.
func writeNDJSONLine(c *gin.Context, line galleryPagesLine) error {
	data, err := json.Marshal(line)
	if err != nil {
		return err
	}
	if _, err := c.Writer.Write(append(data, '\n')); err != nil {
		return err
	}
	c.Writer.Flush()
	return nil
}

// replayGalleryPages writes a fully scraped list as NDJSON for a caller that
// joined an in-flight scrape instead of running its own.
func replayGalleryPages(c *gin.Context, id, token string, total int, pages []model.CachedPage) error {
	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	if err := writeNDJSONLine(c, galleryPagesLine{Type: "meta", ID: id, Token: token, Total: &total}); err != nil {
		return err
	}
	for _, p := range pages {
		index := p.Index
		if err := writeNDJSONLine(c, galleryPagesLine{
			Type:      "page",
			PageURL:   p.PageURL,
			Index:     &index,
			Thumbnail: p.Thumbnail,
		}); err != nil {
			return err
		}
	}
	done := len(pages)
	return writeNDJSONLine(c, galleryPagesLine{Type: "done", Total: &done})
}

// handleGalleryPages streams the page list as NDJSON: a meta line (gallery
// id/token plus the total image count), one line per page as it is scraped,
// then a terminal done line or an error line. The response is committed only
// after the first upstream document loads, so a failure there still returns
// plain 502 JSON. The gallery cache is written only when the complete list
// was scraped — failures never cache partial data.
//
// The scrape is detached from the request context and given a hard cap, so a
// client disconnect no longer aborts the upstream work or loses an otherwise
// complete cache write. Concurrent requests for the same gallery share one
// scrape and each streams the already-scraped prefix immediately, so a request
// that joins mid-scrape renders progressively instead of blocking.
func (s *Server) handleGalleryPages(c *gin.Context) {
	galleryID, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}

	idParam := c.Param("id")

	// Serve a recently verified cached list immediately instead of re-scraping
	// every page. Stale or missing rows fall through to the live stream.
	if db := s.cacheDB(); db != nil {
		if row, found, err := gallerycache.Get(c.Request.Context(), db, galleryID, token); err != nil {
			slog.Warn("gallery cache read failed", "id", galleryID, "error", err)
		} else if found && len(row.Pages) > 0 && cacheFresh(row.PagesFetchedAt, galleryCacheTTL) {
			if replayErr := replayGalleryPages(c, idParam, token, len(row.Pages), row.Pages); replayErr != nil {
				slog.Warn("gallery pages cache replay failed", "id", galleryID, "error", replayErr)
			}
			return
		}
	}

	started := false
	clientGone := false

	writeLine := func(line galleryPagesLine) error {
		if clientGone {
			return nil
		}
		if err := writeNDJSONLine(c, line); err != nil {
			// The client went away; the shared scrape (and cache write)
			// continues for the other subscribers.
			clientGone = true
			return nil
		}
		return nil
	}

	emit := func(total int, batch []model.CachedPage) error {
		if !started {
			c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
			c.Header("Cache-Control", "no-store")
			// Defeat proxy buffering (nginx/Angie) even when the location's
			// proxy_buffering is on, so lines reach the browser as they are
			// scraped instead of after the whole response finishes.
			c.Header("X-Accel-Buffering", "no")
			c.Status(http.StatusOK)
			started = true
			if writeErr := writeLine(galleryPagesLine{
				Type:  "meta",
				ID:    idParam,
				Token: token,
				Total: &total,
			}); writeErr != nil {
				return writeErr
			}
		}
		for _, p := range batch {
			index := p.Index
			if writeErr := writeLine(galleryPagesLine{
				Type:      "page",
				PageURL:   p.PageURL,
				Index:     &index,
				Thumbnail: p.Thumbnail,
			}); writeErr != nil {
				return writeErr
			}
		}
		return nil
	}

	// Drain on a background context: a vanished client is handled by writeLine
	// swallowing write errors, and the subscriber must keep following the walk
	// so the terminal line is written and the detached scrape is never cut
	// loose early. (Thumbnail resolves, by contrast, drain on their request
	// context so an abandoned request stops waiting.)
	count, err := s.scrapeGalleryPages(context.Background(), galleryID, token, emit)

	if !started {
		// Nothing was streamed. Surface the scrape failure as a plain 502 so
		// the frontend can fall back to the cached endpoint.
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery pages failed: %v", err)})
		return
	}
	if err != nil {
		// Headers are already on the wire: report the failure as a terminal
		// error line (the client discards what it received) and cache nothing.
		slog.Warn("gallery pages stream failed", "id", galleryID, "error", err)
		_ = writeLine(galleryPagesLine{Type: "error", Error: fmt.Sprintf("fetch gallery pages failed: %v", err)})
		return
	}

	// The complete list has already been cached by the detached scrape; the
	// terminal line is written last so callers finalize immediately.
	total := count
	if writeErr := writeLine(galleryPagesLine{Type: "done", Total: &total}); writeErr != nil {
		slog.Warn("gallery pages done write failed", "id", galleryID, "error", writeErr)
	}
}

// ==================== Offline cache endpoints ====================
//
// These read straight from gallery_cache. They never touch the upstream and
// never retry; the frontend orchestrates the fallback and retries.

func (s *Server) handleCachedGallery(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok || s.cacheUnavailable(c) {
		return
	}

	row, found, err := gallerycache.Get(c.Request.Context(), s.cacheDB(), id, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("read gallery cache failed: %v", err)})
		return
	}
	if !found || row.Title == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no cached gallery metadata"})
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, galleryFromCache(row))
}

func (s *Server) handleCachedGalleryDetails(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok || s.cacheUnavailable(c) {
		return
	}

	row, found, err := gallerycache.Get(c.Request.Context(), s.cacheDB(), id, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("read gallery cache failed: %v", err)})
		return
	}
	if !found || row.Title == "" {
		c.JSON(http.StatusNotFound, gin.H{"error": "no cached gallery details"})
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, galleryDetailsFromCache(row))
}

func (s *Server) handleCachedGalleryPages(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok || s.cacheUnavailable(c) {
		return
	}

	row, found, err := gallerycache.Get(c.Request.Context(), s.cacheDB(), id, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("read gallery cache failed: %v", err)})
		return
	}
	if !found || len(row.Pages) == 0 {
		c.JSON(http.StatusNotFound, gin.H{"error": "no cached pages"})
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{
		"id":    c.Param("id"),
		"token": token,
		"total": len(row.Pages),
		"pages": row.Pages,
	})
}

func (s *Server) handleSearch(c *gin.Context) {
	keyword := c.Query("q")
	categoryStr := c.Query("categories")
	site := c.DefaultQuery("site", "exhentai")
	pageStr := c.DefaultQuery("page", "0")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		page = 0
	}

	opts, err := parseSearchOptions(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	navOpts, err := parseListingNavOptions(c)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx := c.Request.Context()

	var siteURL string
	if site == "ehentai" {
		siteURL = exhentai.EhentaiURL
	} else {
		siteURL = exhentai.ExhentaiURL
	}

	var categories []string
	if categoryStr != "" {
		categories = strings.Split(categoryStr, ",")
	}

	total, results, nav, err := exhentai.ScrapeSearch(ctx, s.Client, siteURL, keyword, categories, page, opts, navOpts)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("search failed: %v", err)})
		return
	}

	type searchResult struct {
		ID       int64                 `json:"id"`
		Token    string                `json:"token"`
		Title    string                `json:"title"`
		Category model.GalleryCategory `json:"category"`
		Cover    string                `json:"cover"`
		Posted   string                `json:"posted"`
		Rating   float64               `json:"rating"`
		URL      string                `json:"url"`
		Tags     []string              `json:"tags"`
		Uploader string                `json:"uploader"`
		Pages    int                   `json:"pages"`
		Domain   string                `json:"domain"`
	}

	items := make([]searchResult, len(results))
	for i, r := range results {
		items[i] = searchResult{
			ID:       int64(r.GalleryID),
			Token:    r.Token,
			Title:    r.Title,
			Category: model.MapCategory(r.Cat),
			Cover:    r.Cover,
			Posted:   r.Posted,
			Rating:   r.Rating,
			URL:      r.URL,
			Tags:     r.Tags,
			Uploader: r.Uploader,
			Pages:    r.Pages,
			Domain:   r.Domain,
		}
	}

	const pageSize = 25
	totalPages := (total + pageSize - 1) / pageSize

	c.JSON(http.StatusOK, gin.H{
		"total":       total,
		"total_pages": totalPages,
		"page":        page,
		"page_size":   len(items),
		"results":     items,
		"nav":         nav,
	})
}

func (s *Server) handlePageImage(c *gin.Context) {
	pageURL := c.Query("url")
	if pageURL == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "missing url parameter"})
		return
	}

	ctx := c.Request.Context()

	data, contentType, err := exhentai.FetchPageImage(ctx, s.Client, pageURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download image failed: %v", err)})
		return
	}

	c.Data(http.StatusOK, contentType, data)
}

func (s *Server) handleGalleryList(listURL string) gin.HandlerFunc {
	return func(c *gin.Context) {
		pageStr := c.DefaultQuery("page", "0")
		page, _ := strconv.Atoi(pageStr)
		if page < 0 {
			page = 0
		}

		opts, err := parseSearchOptions(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		navOpts, err := parseListingNavOptions(c)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}

		ctx := c.Request.Context()

		results, nav, err := exhentai.ScrapeGalleryList(ctx, s.Client, listURL, page, opts, navOpts)
		if err != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery list failed: %v", err)})
			return
		}

		type galleryResult struct {
			ID       int64                 `json:"id"`
			Token    string                `json:"token"`
			Title    string                `json:"title"`
			Category model.GalleryCategory `json:"category"`
			Cover    string                `json:"cover"`
			Posted   string                `json:"posted"`
			Rating   float64               `json:"rating"`
			URL      string                `json:"url"`
			Tags     []string              `json:"tags"`
			Uploader string                `json:"uploader"`
			Pages    int                   `json:"pages"`
			Domain   string                `json:"domain"`
		}

		items := make([]galleryResult, len(results))
		for i, r := range results {
			items[i] = galleryResult{
				ID:       int64(r.GalleryID),
				Token:    r.Token,
				Title:    r.Title,
				Category: model.MapCategory(r.Cat),
				Cover:    r.Cover,
				Posted:   r.Posted,
				Rating:   r.Rating,
				URL:      r.URL,
				Tags:     r.Tags,
				Uploader: r.Uploader,
				Pages:    r.Pages,
				Domain:   r.Domain,
			}
		}

		c.JSON(http.StatusOK, gin.H{
			"page":      page,
			"page_size": len(items),
			"results":   items,
			"nav":       nav,
		})
	}
}

func (s *Server) handleGalleries(c *gin.Context) {
	s.handleGalleryList(exhentai.ExhentaiURL + "/")(c)
}

func (s *Server) handleWatched(c *gin.Context) {
	s.handleGalleryList(exhentai.ExhentaiURL + "/watched")(c)
}

func (s *Server) handlePopular(c *gin.Context) {
	s.handleGalleryList(exhentai.ExhentaiURL + "/popular")(c)
}

var (
	listingSeekPattern = regexp.MustCompile(`^\d{2,4}(-\d{1,2}(-\d{1,2})?)?$`)
	listingJumpPattern = regexp.MustCompile(`^\d+[dwmy-]?$`)
)

// parseListingNavOptions reads the optional Jump/Seek query parameters. It
// returns (nil, nil) when neither is supplied.
func parseListingNavOptions(c *gin.Context) (*exhentai.ListingNavOptions, error) {
	seek := strings.TrimSpace(c.Query("seek"))
	jump := strings.TrimSpace(c.Query("jump"))
	if seek == "" && jump == "" {
		return nil, nil
	}
	if seek != "" && !listingSeekPattern.MatchString(seek) {
		return nil, fmt.Errorf("invalid seek")
	}
	if jump != "" && !listingJumpPattern.MatchString(jump) {
		return nil, fmt.Errorf("invalid jump")
	}
	return &exhentai.ListingNavOptions{Seek: seek, Jump: jump}, nil
}

func parseOptionalInt(c *gin.Context, key string) (*int, error) {
	v := c.Query(key)
	if v == "" {
		return nil, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return nil, err
	}
	return &n, nil
}

func parseOptionalBool(c *gin.Context, key string) (bool, error) {
	v := c.Query(key)
	if v == "" {
		return false, nil
	}
	switch strings.ToLower(v) {
	case "true", "1":
		return true, nil
	case "false", "0":
		return false, nil
	default:
		return false, fmt.Errorf("invalid boolean value: %s", v)
	}
}

func parseSearchOptions(c *gin.Context) (*exhentai.SearchOptions, error) {
	minPages, err := parseOptionalInt(c, "min_pages")
	if err != nil {
		return nil, fmt.Errorf("invalid min_pages")
	}
	maxPages, err := parseOptionalInt(c, "max_pages")
	if err != nil {
		return nil, fmt.Errorf("invalid max_pages")
	}
	if minPages != nil && *minPages < 0 {
		return nil, fmt.Errorf("min_pages must be >= 0")
	}
	if maxPages != nil && *maxPages < 0 {
		return nil, fmt.Errorf("max_pages must be >= 0")
	}
	if minPages != nil && maxPages != nil && *minPages > *maxPages {
		return nil, fmt.Errorf("min_pages must be <= max_pages")
	}

	minRating, err := parseOptionalInt(c, "min_rating")
	if err != nil {
		return nil, fmt.Errorf("invalid min_rating")
	}
	if minRating != nil {
		switch *minRating {
		case 2, 3, 4, 5:
		default:
			return nil, fmt.Errorf("min_rating must be 2, 3, 4, or 5")
		}
	}

	hasTorrent, err := parseOptionalBool(c, "has_torrent")
	if err != nil {
		return nil, fmt.Errorf("invalid has_torrent")
	}
	includeExpunged, err := parseOptionalBool(c, "include_expunged")
	if err != nil {
		return nil, fmt.Errorf("invalid include_expunged")
	}
	searchName, err := parseOptionalBool(c, "search_name")
	if err != nil {
		return nil, fmt.Errorf("invalid search_name")
	}
	searchTags, err := parseOptionalBool(c, "search_tags")
	if err != nil {
		return nil, fmt.Errorf("invalid search_tags")
	}
	searchDescription, err := parseOptionalBool(c, "search_description")
	if err != nil {
		return nil, fmt.Errorf("invalid search_description")
	}
	includeLowPowerTags, err := parseOptionalBool(c, "include_low_power_tags")
	if err != nil {
		return nil, fmt.Errorf("invalid include_low_power_tags")
	}
	includeDownvotedTags, err := parseOptionalBool(c, "include_downvoted_tags")
	if err != nil {
		return nil, fmt.Errorf("invalid include_downvoted_tags")
	}
	disableLanguageFilter, err := parseOptionalBool(c, "disable_language_filter")
	if err != nil {
		return nil, fmt.Errorf("invalid disable_language_filter")
	}
	disableUploaderFilter, err := parseOptionalBool(c, "disable_uploader_filter")
	if err != nil {
		return nil, fmt.Errorf("invalid disable_uploader_filter")
	}
	disableTagFilter, err := parseOptionalBool(c, "disable_tag_filter")
	if err != nil {
		return nil, fmt.Errorf("invalid disable_tag_filter")
	}

	return &exhentai.SearchOptions{
		MinPages:              minPages,
		MaxPages:              maxPages,
		MinRating:             minRating,
		HasTorrent:            hasTorrent,
		IncludeExpunged:       includeExpunged,
		SearchName:            searchName,
		SearchTags:            searchTags,
		SearchDescription:     searchDescription,
		IncludeLowPowerTags:   includeLowPowerTags,
		IncludeDownvotedTags:  includeDownvotedTags,
		DisableLanguageFilter: disableLanguageFilter,
		DisableUploaderFilter: disableUploaderFilter,
		DisableTagFilter:      disableTagFilter,
	}, nil
}
