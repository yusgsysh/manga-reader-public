package handler

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/httpx"
	"manga-reader/internal/ident"
	"manga-reader/internal/metrics"
	"manga-reader/internal/model"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gin-gonic/gin"
	"golang.org/x/sync/singleflight"
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

// galleryPagesFillGroup coalesces cache-fill refreshes (read-through miss,
// page-count change, image-failure recovery) for the same gallery so a burst of
// triggers runs a single upstream walk and a single cache write.
var galleryPagesFillGroup singleflight.Group

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
	userID ident.UUID, galleryID int64,
	token string,
	emit func(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error,
) (int, error) {
	client, err := s.upstreamClient(ctx, userID)
	if err != nil {
		return 0, err
	}
	key := fmt.Sprintf("%s/%d:%s", userID, galleryID, token)
	stream, leader := galleryPagesHub.acquire(key)
	if leader {
		s.runGalleryPagesScrape(ctx, key, stream, userID, galleryID, token, client)
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
	userID ident.UUID, galleryID int64,
	token string,
	client *http.Client,
) {
	go func() {
		defer galleryPagesHub.release(key, stream)

		// Detached from the subscriber: cancelling one subscriber must not
		// abort the shared walk, so only values are inherited from ctx.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), galleryPagesHardCap)
		defer cancel()

		u := exhentai.GalleryURLAt(s.upstreamBase(ctx, userID), strconv.FormatInt(galleryID, 10), token)
		var pageURLs []string
		var thumbnails []model.GalleryPageThumb
		total := 0
		err := exhentai.StreamGalleryPages(ctx, client, u, func(t int, urls []string, thumbs []model.GalleryPageThumb) error {
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
	userID ident.UUID, galleryID int64,
	token string,
	emit func(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error,
) (int, error) {
	var pageURLs []string
	var thumbnails []model.GalleryPageThumb
	count, err := s.scrapeGalleryPages(ctx, userID, galleryID, token, func(total int, urls []string, thumbs []model.GalleryPageThumb) error {
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
func (s *Server) refreshPages(ctx context.Context, userID ident.UUID, galleryID int64, token string) {
	if s.cacheDB() == nil {
		return
	}
	if _, err := s.upstreamClient(ctx, userID); err != nil {
		return
	}
	key := fmt.Sprintf("%s/%d:%s", userID, galleryID, token)
	galleryPagesFillGroup.Do(key, func() (any, error) {
		// The refresh runs on the full background budget regardless of the
		// caller's lifetime; inherit values only, never cancellation.
		ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), galleryPagesHardCap)
		defer cancel()
		if _, err := s.scrapeAndCache(ctx, userID, galleryID, token, nil); err != nil {
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
func (s *Server) refreshPagesSync(ctx context.Context, userID ident.UUID, galleryID int64, token string) error {
	if s.cacheDB() == nil {
		return nil
	}
	if _, err := s.upstreamClient(ctx, userID); err != nil {
		return err
	}
	key := fmt.Sprintf("%s/%d:%s", userID, galleryID, token)
	// DoChan instead of Do: group.Do blocks duplicate callers with a bare
	// wg.Wait() that ignores their context, so a request could hang for the
	// leader's full budget after its client is gone. DoChan lets us honour
	// the caller's cancellation while the shared walk keeps running.
	ch := galleryPagesFillGroup.DoChan(key, func() (any, error) {
		ctx, cancel := context.WithTimeout(ctx, galleryPagesHardCap)
		defer cancel()
		_, err := s.scrapeAndCache(ctx, userID, galleryID, token, nil)
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
		httpx.JSON(c, http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch gallery pages failed: %v", err)})
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
	// NDJSON stream; duration is unbounded, so keep it out of the histogram.
	metrics.MarkLongRunning(c)
	galleryID, token, ok := parseGalleryIDToken(c)
	if !ok {
		return
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	idParam := c.Param("id")
	streamGalleryPagesNDJSON(c, idParam, token, func(ctx context.Context, emit func(int, []string, []model.GalleryPageThumb) error) (int, error) {
		return s.scrapeGalleryPages(ctx, uid, galleryID, token, emit)
	})
}

// ==================== Torrents (live, no cache) ====================
