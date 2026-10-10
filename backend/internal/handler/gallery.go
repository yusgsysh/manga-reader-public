package handler

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	json "encoding/json/v2"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"

	"manga-reader/internal/ent"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/metrics"
	"manga-reader/internal/model"
)

// galleryPagesHardCap bounds an entire /pages scrape (including the detached
// background work that continues after a client disconnects).
const galleryPagesHardCap = 10 * time.Minute

// maxListingPage bounds upstream listing pagination: a cold request for page N
// walks N sequential upstream fetches, so unbounded page numbers would let one
// request hammer the upstream for hours.
const maxListingPage = 200

// maxDBPage bounds DB-offset pagination so page*pageSize cannot overflow int.
const maxDBPage = 1_000_000

// gtidRe bounds the torrent id path parameter: it is echoed into a
// Content-Disposition filename and forwarded upstream as a form value.
var gtidRe = regexp.MustCompile(`^[0-9A-Za-z_-]{1,64}$`)

// galleryPagesHub coalesces concurrent page-list scrapes for the same gallery
// into a single upstream walk. Unlike a plain singleflight, subscribers receive
// the batches already scraped as they arrive instead of blocking until the
// whole list is ready, so opening a gallery while another scan of it is still
// running streams progressively instead of stalling on the loading skeleton.
// Shared by the streaming handler and the bookshelf prefetch.
var galleryPagesHub = newPagesHub()

// galleryPagesFillGroup coalesces cache-fill refreshes (read-through miss,
// page-count change, image-failure recovery) for the same gallery so a burst of
// triggers runs a single upstream walk and a single cache write.
var galleryPagesFillGroup singleflight.Group

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
		tags[i] = model.Tag(t)
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

// galleryDetailsJSON shapes a scraped gallery detail for the JSON response. It
// is shared by the online details endpoint and the cache read-through path.
func galleryDetailsJSON(details model.GalleryDetail) gin.H {
	tags := make([]model.Tag, len(details.Tags))
	for i, t := range details.Tags {
		tags[i] = model.Tag(t)
	}
	return gin.H{
		"id":            details.GalleryID,
		"token":         details.Token,
		"domain":        details.Domain,
		"title":         details.Title,
		"title_jpn":     details.TitleJpn,
		"cover":         details.Cover,
		"category":      model.MapCategory(details.Cat),
		"uploader":      details.Uploader,
		"posted":        details.Posted,
		"parent":        details.Parent,
		"visible":       details.Visible,
		"language":      details.Language,
		"translated":    details.Translated == "TR",
		"file_size":     details.FileSize,
		"page_count":    details.Length,
		"favorited":     details.Favorited,
		"rating_count":  details.RatingCount,
		"rating":        details.Rating,
		"torrent_count": details.TorrentCount,
		"tags":          tags,
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

// handleGetGallery is the online metadata endpoint. It always fetches upstream
// and never reads or writes the gallery cache; the cache endpoint is the
// read-through path and the frontend uses this one to stay fresh.
func (s *Server) handleGetGallery(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}

	meta, err := exhentai.PostGalleryMetadata(c.Request.Context(), s.Client, id, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("exhentai api failed: %v", err)})
		return
	}
	c.JSON(http.StatusOK, model.ConvertMetadataToGallery(meta))
}

// handleGalleryDetails is the online details endpoint. Like handleGetGallery it
// always scrapes upstream and never touches the cache.
func (s *Server) handleGalleryDetails(c *gin.Context) {
	_, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}

	u := exhentai.GalleryURL(c.Param("id"), token)
	ctx := c.Request.Context()

	details, err := exhentai.ScrapeGalleryDetails(ctx, s.Client, u)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery details failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, galleryDetailsJSON(details))
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

// thumbPtr returns a pointer to the thumbnail geometry, or nil when the page has
// no geometry, so the JSON field is omitted instead of serialized as a
// present-but-empty object.
func thumbPtr(t model.GalleryPageThumb) *model.GalleryPageThumb {
	if t.SpriteURL == "" {
		return nil
	}
	return &t
}

// galleryPagesStream is one in-flight page-list scrape that any number of
// requests can subscribe to. The leader appends batches under the lock and
// broadcasts; subscribers drain the accumulated log and then follow it live.
// A late subscriber is replayed the already-scraped prefix, so it never blocks
// until the whole list is ready.
type galleryPagesStream struct {
	mu         sync.Mutex
	cond       *sync.Cond
	pageURLs   []string
	thumbnails []model.GalleryPageThumb
	total      int
	done       bool
	err        error
	finished   atomic.Bool
}

func newGalleryPagesStream() *galleryPagesStream {
	s := &galleryPagesStream{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *galleryPagesStream) append(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) {
	s.mu.Lock()
	if total > 0 {
		s.total = total
	}
	s.pageURLs = append(s.pageURLs, pageURLs...)
	s.thumbnails = append(s.thumbnails, thumbnails...)
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
	emit func(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error,
) (int, error) {
	stopWake := context.AfterFunc(ctx, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer stopWake()

	for {
		s.mu.Lock()
		for cursor >= len(s.pageURLs) && !s.done && ctx.Err() == nil {
			s.cond.Wait()
		}
		if ctx.Err() != nil {
			err := ctx.Err()
			s.mu.Unlock()
			return cursor, err
		}
		if cursor >= len(s.pageURLs) {
			err := s.err
			s.mu.Unlock()
			return cursor, err
		}
		batchURLs := append([]string(nil), s.pageURLs[cursor:]...)
		batchThumbs := append([]model.GalleryPageThumb(nil), s.thumbnails[cursor:]...)
		cursor = len(s.pageURLs)
		total := s.total
		s.mu.Unlock()

		if emit != nil {
			if err := emit(total, batchURLs, batchThumbs); err != nil {
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
	emit func(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error,
) (int, error) {
	key := fmt.Sprintf("%d:%s", galleryID, token)
	stream, leader := galleryPagesHub.acquire(key)
	if leader {
		s.runGalleryPagesScrape(ctx, key, stream, galleryID, token)
	}
	return stream.drain(ctx, 0, emit)
}

// runGalleryPagesScrape drives one detached page-list walk and publishes it to
// stream. It has no cache side effects: the online endpoint must not write the
// cache, and cache-filling subscribers persist the verified list themselves
// once the walk completes.
func (s *Server) runGalleryPagesScrape(
	ctx context.Context,
	key string,
	stream *galleryPagesStream,
	galleryID int64,
	token string,
) {
	go func() {
		defer galleryPagesHub.release(key, stream)

		// Detached from the subscriber: cancelling one subscriber must not
		// abort the shared walk, so only values are inherited from ctx.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), galleryPagesHardCap)
		defer cancel()

		u := exhentai.GalleryURL(strconv.FormatInt(galleryID, 10), token)
		var pageURLs []string
		var thumbnails []model.GalleryPageThumb
		total := 0
		err := exhentai.StreamGalleryPages(ctx, s.Client, u, func(t int, urls []string, thumbs []model.GalleryPageThumb) error {
			total = t
			pageURLs = append(pageURLs, urls...)
			thumbnails = append(thumbnails, thumbs...)
			stream.append(t, urls, thumbs)
			return nil
		})
		if err == nil && (total <= 0 || len(pageURLs) != total) {
			// StreamGalleryPages only returns nil once the received count equals
			// the ".gpc" total; re-check at the cache boundary so only a
			// verified, complete list is ever persisted.
			err = fmt.Errorf("incomplete page list: got %d pages, want %d", len(pageURLs), total)
		}
		stream.finish(err)
	}()
}

// scrapeAndCache streams the shared page walk to emit (when non-nil) and, on a
// complete verified walk, replaces the cached page list and thumbnail geometry
// wholesale. It is the only cache-writing page path: the cache endpoints and
// prefetch use it, while the online endpoint uses scrapeGalleryPages and never
// writes. The write happens after drain returns nil (a verified complete walk),
// so a failed or partial walk never touches the cache.
func (s *Server) scrapeAndCache(
	ctx context.Context,
	galleryID int64,
	token string,
	emit func(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error,
) (int, error) {
	var pageURLs []string
	var thumbnails []model.GalleryPageThumb
	count, err := s.scrapeGalleryPages(ctx, galleryID, token, func(total int, urls []string, thumbs []model.GalleryPageThumb) error {
		pageURLs = append(pageURLs, urls...)
		thumbnails = append(thumbnails, thumbs...)
		if emit != nil {
			return emit(total, urls, thumbs)
		}
		return nil
	})
	if err != nil {
		return count, err
	}
	if db := s.cacheDB(); db != nil {
		pagesErr := gallerycache.ReplacePages(ctx, db, galleryID, token, pageURLs)
		thumbErr := gallerycache.ReplaceThumbnails(ctx, db, galleryID, token, thumbnails)
		if pagesErr != nil {
			slog.Warn("gallery cache pages replace failed", "id", galleryID, "error", pagesErr)
		}
		if thumbErr != nil {
			slog.Warn("gallery cache thumbnails replace failed", "id", galleryID, "error", thumbErr)
		}
		if pagesErr == nil || thumbErr == nil {
			s.recordGalleryCacheChange(ctx, galleryID, token)
		}
	}
	return count, nil
}

// refreshPages runs a cache-filling walk for (galleryID, token) in the
// background. Concurrent triggers for the same gallery collapse into one walk
// and one write via galleryPagesFillGroup. It is used by the page-count check,
// image-failure recovery and thumbnail-resolve cache fill.
func (s *Server) refreshPages(ctx context.Context, galleryID int64, token string) {
	if s.cacheDB() == nil || s.Client == nil {
		return
	}
	key := fmt.Sprintf("%d:%s", galleryID, token)
	galleryPagesFillGroup.Do(key, func() (any, error) {
		// The refresh runs on the full background budget regardless of the
		// caller's lifetime; inherit values only, never cancellation.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), galleryPagesHardCap)
		defer cancel()
		if _, err := s.scrapeAndCache(ctx, galleryID, token, nil); err != nil {
			slog.Debug("gallery page cache refresh failed", "id", galleryID, "error", err)
		}
		return nil, nil
	})
}

// refreshPagesSync is refreshPages for callers that must observe the result:
// it runs (or joins) the same cache-filling walk and returns only once the
// cache write has completed. The caller's context bounds the walk (capped by
// galleryPagesHardCap), so a request-path recovery can time-box it instead of
// waiting on the full background budget.
func (s *Server) refreshPagesSync(ctx context.Context, galleryID int64, token string) error {
	if s.cacheDB() == nil || s.Client == nil {
		return nil
	}
	key := fmt.Sprintf("%d:%s", galleryID, token)
	// DoChan instead of Do: group.Do blocks duplicate callers with a bare
	// wg.Wait() that ignores their context, so a request could hang for the
	// leader's full budget after its client is gone. DoChan lets us honour
	// the caller's cancellation while the shared walk keeps running.
	ch := galleryPagesFillGroup.DoChan(key, func() (any, error) {
		ctx, cancel := context.WithTimeout(ctx, galleryPagesHardCap)
		defer cancel()
		_, err := s.scrapeAndCache(ctx, galleryID, token, nil)
		if err != nil {
			slog.Debug("gallery page cache refresh failed", "id", galleryID, "error", err)
		}
		return nil, err
	})
	select {
	case <-ctx.Done():
		return ctx.Err()
	case res := <-ch:
		return res.Err
	}
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
func replayGalleryPages(c *gin.Context, id, token string, total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error {
	c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
	c.Header("Cache-Control", "no-store")
	c.Header("X-Accel-Buffering", "no")
	c.Status(http.StatusOK)

	if err := writeNDJSONLine(c, galleryPagesLine{Type: "meta", ID: id, Token: token, Total: &total}); err != nil {
		return err
	}
	for i := range pageURLs {
		index := i
		thumb := model.GalleryPageThumb{}
		if i < len(thumbnails) {
			thumb = thumbnails[i]
		}
		if err := writeNDJSONLine(c, galleryPagesLine{
			Type:      "page",
			PageURL:   pageURLs[i],
			Index:     &index,
			Thumbnail: thumbPtr(thumb),
		}); err != nil {
			return err
		}
	}
	done := len(pageURLs)
	return writeNDJSONLine(c, galleryPagesLine{Type: "done", Total: &done})
}

// streamGalleryPagesNDJSON writes the NDJSON page stream to c using run to
// produce the pages. It is shared by the online endpoint and the cache
// read-through path; only run differs (the online run never writes the cache).
// The response is committed only after the first batch arrives, so a failure
// before that still returns a plain 502. A client disconnect is swallowed so a
// cache-filling drain still reaches completion and can write.
func streamGalleryPagesNDJSON(
	c *gin.Context,
	idParam, token string,
	run func(ctx context.Context, emit func(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error) (int, error),
) {
	started := false
	clientGone := false
	// written is the global page offset across all batches. The walk emits
	// pages in batches (the gallery document, then ?p=1, ?p=2, …), but the
	// NDJSON protocol requires a single contiguous index sequence, so each
	// page is numbered by its absolute position, not its position in the batch.
	written := 0

	writeLine := func(line galleryPagesLine) error {
		if clientGone {
			return nil
		}
		if err := writeNDJSONLine(c, line); err != nil {
			clientGone = true
			return nil
		}
		return nil
	}

	emit := func(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error {
		if !started {
			c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
			c.Header("Cache-Control", "no-store")
			// Defeat buffering in a reverse proxy sitting in front of the
			// app (nginx and friends honour this header even when
			// proxy_buffering is on), so lines reach the browser as they
			// are scraped instead of after the whole response finishes.
			c.Header("X-Accel-Buffering", "no")
			c.Status(http.StatusOK)
			started = true
			if writeErr := writeLine(galleryPagesLine{Type: "meta", ID: idParam, Token: token, Total: &total}); writeErr != nil {
				return writeErr
			}
		}
		for i := range pageURLs {
			index := written + i
			thumb := model.GalleryPageThumb{}
			if i < len(thumbnails) {
				thumb = thumbnails[i]
			}
			if writeErr := writeLine(galleryPagesLine{
				Type:      "page",
				PageURL:   pageURLs[i],
				Index:     &index,
				Thumbnail: thumbPtr(thumb),
			}); writeErr != nil {
				return writeErr
			}
		}
		written += len(pageURLs)
		return nil
	}

	// Drain on a background context: a vanished client is handled by writeLine
	// swallowing write errors, and a cache subscriber must keep following the
	// walk so the terminal line is written and the detached scrape is never cut
	// loose early.
	count, err := run(context.Background(), emit)

	if !started {
		// Nothing was streamed. Surface the scrape failure as a plain 502 so
		// the frontend can fall back to the cached endpoint.
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery pages failed: %v", err)})
		return
	}
	if err != nil {
		slog.Warn("gallery pages stream failed", "id", idParam, "error", err)
		_ = writeLine(galleryPagesLine{Type: "error", Error: fmt.Sprintf("fetch gallery pages failed: %v", err)})
		return
	}
	total := count
	if writeErr := writeLine(galleryPagesLine{Type: "done", Total: &total}); writeErr != nil {
		slog.Warn("gallery pages done write failed", "id", idParam, "error", writeErr)
	}
}

// handleGalleryPages is the online page endpoint: it always scrapes upstream
// (via the shared, side-effect-free walk) and never reads or writes the cache.
func (s *Server) handleGalleryPages(c *gin.Context) {
	galleryID, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}
	idParam := c.Param("id")
	streamGalleryPagesNDJSON(c, idParam, token, func(ctx context.Context, emit func(int, []string, []model.GalleryPageThumb) error) (int, error) {
		return s.scrapeGalleryPages(ctx, galleryID, token, emit)
	})
}

// ==================== Torrents (live, no cache) ====================

// handleGalleryTorrents lists the gallery's torrents. It always scrapes the
// upstream torrents page and never touches the gallery cache.
func (s *Server) handleGalleryTorrents(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}
	torrents, err := exhentai.ScrapeGalleryTorrents(c.Request.Context(), s.Client, strconv.FormatInt(id, 10), token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery torrents failed: %v", err)})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"torrents": torrents})
}

// handleGalleryTorrentInfo returns one torrent's expanded "Information" view
// (tracker stats plus the uploader comment). Live scrape, no cache.
func (s *Server) handleGalleryTorrentInfo(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}
	gtid := strings.TrimSpace(c.Param("gtid"))
	if !gtidRe.MatchString(gtid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid torrent id"})
		return
	}
	result, err := exhentai.ScrapeGalleryTorrentInfo(c.Request.Context(), s.Client, strconv.FormatInt(id, 10), token, gtid)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch torrent info failed: %v", err)})
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result.Info)
}

// handleGalleryTorrentDownload proxies the .torrent file through the backend so
// the session cookie is used (the browser has none). It re-resolves the link by
// torrent id and never caches.
func (s *Server) handleGalleryTorrentDownload(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}
	gtid := strings.TrimSpace(c.Param("gtid"))
	if !gtidRe.MatchString(gtid) {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid torrent id"})
		return
	}
	idParam := strconv.FormatInt(id, 10)
	ctx := c.Request.Context()

	torrents, err := exhentai.ScrapeGalleryTorrents(ctx, s.Client, idParam, token)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery torrents failed: %v", err)})
		return
	}
	var torrent *model.GalleryTorrent
	for i := range torrents {
		if torrents[i].GTID == gtid {
			torrent = &torrents[i]
			break
		}
	}
	if torrent == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "torrent not found"})
		return
	}

	rawURL := torrent.DownloadURL
	if c.Query("variant") == "personalized" {
		result, infoErr := exhentai.ScrapeGalleryTorrentInfo(ctx, s.Client, idParam, token, gtid)
		if infoErr != nil {
			c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch torrent info failed: %v", infoErr)})
			return
		}
		if result.PersonalizedURL == "" {
			c.JSON(http.StatusNotFound, gin.H{"error": "personalized torrent not available"})
			return
		}
		rawURL = result.PersonalizedURL
	}
	if rawURL == "" {
		c.JSON(http.StatusBadGateway, gin.H{"error": "torrent download link missing"})
		return
	}

	data, err := exhentai.FetchTorrent(ctx, s.Client, rawURL)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("download torrent failed: %v", err)})
		return
	}

	filename := exhentai.TorrentFilename(torrent.Name, gtid)
	ascii := "torrent-" + gtid + ".torrent"
	c.Header("Content-Type", "application/x-bittorrent")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`, ascii, url.PathEscape(filename)))
	c.Header("Cache-Control", "no-store")
	c.Data(http.StatusOK, "application/x-bittorrent", data)
}

// ==================== Cache (read-through) endpoints ====================
//
// These are the read-through cache: a hit is served from gallery_cache, a miss
// fetches upstream and backfills. The online endpoints stay fresh and never
// write, so every cache write flows through here or a background refresher.

func (s *Server) handleCachedGallery(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok || s.cacheUnavailable(c) {
		return
	}
	ctx := c.Request.Context()

	if row, found, err := gallerycache.Get(ctx, s.cacheDB(), id, token); err != nil {
		slog.Warn("gallery cache read failed", "id", id, "error", err)
	} else if found && row.Title != "" {
		metrics.ObserveGalleryCache("gallery", "hit")
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, galleryFromCache(row))
		return
	} else {
		metrics.ObserveGalleryCache("gallery", "miss")
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
		} else {
			s.recordGalleryCacheChange(ctx, id, token)
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gallery)
}

func (s *Server) handleCachedGalleryDetails(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok || s.cacheUnavailable(c) {
		return
	}
	ctx := c.Request.Context()

	// A details hit requires that details were actually scraped at least once
	// (DetailsFetchedAt is a completeness flag here, not a TTL); a metadata-only
	// row falls through and reads through upstream.
	if row, found, err := gallerycache.Get(ctx, s.cacheDB(), id, token); err != nil {
		slog.Warn("gallery cache read failed", "id", id, "error", err)
	} else if found && row.Title != "" && row.DetailsFetchedAt != nil {
		metrics.ObserveGalleryCache("details", "hit")
		c.Header("Cache-Control", "no-store")
		c.JSON(http.StatusOK, galleryDetailsFromCache(row))
		return
	} else {
		metrics.ObserveGalleryCache("details", "miss")
	}

	u := exhentai.GalleryURL(c.Param("id"), token)
	details, err := exhentai.ScrapeGalleryDetails(ctx, s.Client, u)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery details failed: %v", err)})
		return
	}
	if db := s.cacheDB(); db != nil {
		if cacheErr := gallerycache.UpsertDetails(ctx, db, id, token, cacheMetaFromDetails(details)); cacheErr != nil {
			slog.Warn("gallery cache details upsert failed", "id", id, "error", cacheErr)
		} else {
			s.recordGalleryCacheChange(ctx, id, token)
		}
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, galleryDetailsJSON(details))
}

func (s *Server) handleCachedGalleryPages(c *gin.Context) {
	id, token, ok := parseGalleryIDToken(c)
	if !ok || s.cacheUnavailable(c) {
		return
	}
	ctx := c.Request.Context()

	row, found, err := gallerycache.Get(ctx, s.cacheDB(), id, token)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("read gallery cache failed: %v", err)})
		return
	}
	if found && len(row.Pages) > 0 {
		metrics.ObserveGalleryCache("pages", "hit")
		if replayErr := replayGalleryPages(c, c.Param("id"), token, len(row.Pages), row.Pages, row.Thumbnails); replayErr != nil {
			slog.Warn("cached gallery pages replay failed", "id", id, "error", replayErr)
		}
		// Serve immediately, then verify the upstream page count in the
		// background (stale-while-revalidate): a gallery that gained or lost
		// pages is replaced without blocking the hit.
		go s.verifyCachedPageCount(id, token, len(row.Pages))
		return
	}

	// Miss: stream upstream and backfill on a complete walk.
	metrics.ObserveGalleryCache("pages", "miss")
	streamGalleryPagesNDJSON(c, c.Param("id"), token, func(ctx context.Context, emit func(int, []string, []model.GalleryPageThumb) error) (int, error) {
		return s.scrapeAndCache(ctx, id, token, emit)
	})
}

// verifyCachedPageCount is the background stale-while-revalidate probe: it
// fetches only the gallery's ".gpc" total and, when it no longer matches the
// cached page count, runs a full cache-fill walk. Concurrent probes for the
// same gallery collapse via galleryPagesFillGroup.
func (s *Server) verifyCachedPageCount(galleryID int64, token string, cached int) {
	if s.cacheDB() == nil || s.Client == nil {
		return
	}
	key := fmt.Sprintf("%d:%s", galleryID, token)
	galleryPagesFillGroup.Do(key, func() (any, error) {
		// The probe and the refresh walk get separate budgets: the walk can
		// take up to galleryPagesHardCap and must not consume the probe's
		// 30s window, or the detected page-count change would never persist.
		probeCtx, probeCancel := context.WithTimeout(context.Background(), 30*time.Second)
		u := exhentai.GalleryURL(strconv.FormatInt(galleryID, 10), token)
		total, err := exhentai.ScrapeGalleryTotal(probeCtx, s.Client, u)
		probeCancel()
		if err != nil {
			slog.Debug("cached page-count check failed", "id", galleryID, "error", err)
			return nil, nil
		}
		if total == cached {
			return nil, nil
		}
		slog.Info("cached page count changed; refreshing", "id", galleryID, "cached", cached, "upstream", total)
		walkCtx, walkCancel := context.WithTimeout(context.Background(), galleryPagesHardCap)
		defer walkCancel()
		if _, err := s.scrapeAndCache(walkCtx, galleryID, token, nil); err != nil {
			slog.Warn("cached page-count refresh failed", "id", galleryID, "error", err)
		}
		return nil, nil
	})
}

func (s *Server) handleSearch(c *gin.Context) {
	keyword := c.Query("q")
	categoryStr := c.Query("categories")
	pageStr := c.DefaultQuery("page", "0")
	page, _ := strconv.Atoi(pageStr)
	if page < 0 {
		page = 0
	}
	if page > maxListingPage {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("page too large (max %d)", maxListingPage)})
		return
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

	siteURL := exhentai.ExhentaiURL

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
	// Same allowlist as the cached-page endpoint: without it this handler
	// would fetch and return arbitrary caller-chosen URLs (SSRF) at the app
	// origin with no CSP in front.
	if err := exhentai.ValidatePageURL(pageURL); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(c.Request.Context(), 60*time.Second)
	defer cancel()

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
		if page > maxListingPage {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("page too large (max %d)", maxListingPage)})
			return
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

		var categories []string
		if categoryStr := c.Query("categories"); categoryStr != "" {
			categories = strings.Split(categoryStr, ",")
		}

		ctx := c.Request.Context()

		results, nav, err := exhentai.ScrapeGalleryList(ctx, s.Client, listURL, page, categories, opts, navOpts)
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

// parseTagsQuery splits a comma-separated tag list, trimming whitespace and
// dropping empty or duplicate entries while preserving order.
func parseTagsQuery(raw string) []string {
	if strings.TrimSpace(raw) == "" {
		return nil
	}
	seen := make(map[string]struct{})
	tags := make([]string, 0)
	for part := range strings.SplitSeq(raw, ",") {
		tag := strings.TrimSpace(part)
		if tag == "" {
			continue
		}
		if _, ok := seen[tag]; ok {
			continue
		}
		seen[tag] = struct{}{}
		tags = append(tags, tag)
	}
	if len(tags) == 0 {
		return nil
	}
	return tags
}

func parseSearchOptions(c *gin.Context) (*exhentai.SearchOptions, error) {
	tags := parseTagsQuery(c.Query("tags"))

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
		Tags:                  tags,
		MinPages:              minPages,
		MaxPages:              maxPages,
		MinRating:             minRating,
		HasTorrent:            hasTorrent,
		IncludeExpunged:       includeExpunged,
		DisableLanguageFilter: disableLanguageFilter,
		DisableUploaderFilter: disableUploaderFilter,
		DisableTagFilter:      disableTagFilter,
	}, nil
}
