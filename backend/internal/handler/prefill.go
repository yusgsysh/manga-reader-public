package handler

import (
	"archive/zip"
	"context"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/cache"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/prefilljob"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/model"
)

const (
	prefillStatusQueued    = "queued"
	prefillStatusRunning   = "running"
	prefillStatusCompleted = "completed"
	prefillStatusCancelled = "cancelled"
	prefillStatusFailed    = "failed"

	prefillMaxURIs            = 2000
	prefillMaxErrors          = 20
	prefillCleanupDaysDefault = 30
	prefillListLimit          = 200
)

// prefillRun holds the in-memory progress of an active prefill job. Counters
// are deliberately never persisted: the underlying image cache expires after
// ~30 days, so stored progress would quickly become a lie.
type prefillRun struct {
	cancel  context.CancelFunc
	done    int
	cached  int
	fetched int
	failed  int
	errs    []model.PrefillItemError
}

// prefillManager runs prefill jobs through a single worker goroutine, so jobs
// are executed strictly one at a time, in creation order, one page at a time.
type prefillManager struct {
	mu sync.Mutex
	// runs tracks in-memory progress for active jobs.
	runs map[int]*prefillRun
	// cancelPending records cancel requests that arrived after the worker
	// claimed a job but before its run was registered, so they are never lost.
	cancelPending map[int]struct{}
	wake          chan struct{}
}

func (m *prefillManager) signal() {
	select {
	case m.wake <- struct{}{}:
	default:
	}
}

// ensurePrefill lazily creates the manager, re-queues jobs that were active
// when the process last stopped and starts the single worker goroutine.
// Callers must have verified s.DB != nil.
func (s *Server) ensurePrefill() *prefillManager {
	s.prefillOnce.Do(func() {
		m := &prefillManager{
			runs:          make(map[int]*prefillRun),
			cancelPending: make(map[int]struct{}),
			wake:          make(chan struct{}, 1),
		}
		s.prefillMgr = m
		m.recoverPending(s)
		go m.worker(s)
	})
	return s.prefillMgr
}

func (m *prefillManager) recoverPending(s *Server) {
	ctx := context.Background()
	n, err := s.DB.Client.PrefillJob.Update().
		Where(prefilljob.Status(prefillStatusRunning)).
		SetStatus(prefillStatusQueued).
		SetUpdatedAt(time.Now()).
		Save(ctx)
	if err != nil {
		log.Printf("prefill recover failed: %v", err)
		return
	}
	if n > 0 {
		log.Printf("prefill re-queued %d interrupted job(s)", n)
	}
	m.signal()
}

func (m *prefillManager) worker(s *Server) {
	for range m.wake {
		m.drain(s)
	}
}

func (m *prefillManager) drain(s *Server) {
	for {
		id, ok := m.nextQueued(s)
		if !ok {
			return
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					log.Printf("prefill job %d panic: %v", id, r)
					s.finishPrefillJob(id, prefillStatusFailed, 0, nil)
				}
			}()
			m.runJob(s, id)
		}()
	}
}

// nextQueued claims the oldest queued job by atomically flipping it to
// running. Returns false when the queue is empty.
func (m *prefillManager) nextQueued(s *Server) (int, bool) {
	ctx := context.Background()
	for {
		row, err := s.DB.Client.PrefillJob.Query().
			Where(prefilljob.Status(prefillStatusQueued)).
			Order(ent.Asc(prefilljob.FieldCreatedAt)).
			First(ctx)
		if err != nil {
			if !ent.IsNotFound(err) {
				log.Printf("prefill queue query failed: %v", err)
			}
			return 0, false
		}
		n, err := s.DB.Client.PrefillJob.Update().
			Where(prefilljob.ID(row.ID), prefilljob.Status(prefillStatusQueued)).
			SetStatus(prefillStatusRunning).
			SetUpdatedAt(time.Now()).
			Save(ctx)
		if err != nil {
			log.Printf("prefill claim failed id=%d err=%v", row.ID, err)
			return 0, false
		}
		if n == 0 {
			continue // cancelled between query and claim; take the next one
		}
		return row.ID, true
	}
}

func (m *prefillManager) runJob(s *Server, id int) {
	ctx := context.Background()
	row, err := s.DB.Client.PrefillJob.Get(ctx, id)
	if err != nil {
		if !ent.IsNotFound(err) {
			log.Printf("prefill load failed id=%d err=%v", id, err)
		}
		return
	}

	// nextQueued already flipped the row to "running". Register the run now so
	// that any cancel arriving from here on finds the cancel func; cancels that
	// landed in the claim/registration gap are held in cancelPending.
	jobCtx, cancel := context.WithCancel(context.Background())
	run := &prefillRun{cancel: cancel}
	m.mu.Lock()
	m.runs[id] = run
	_, pending := m.cancelPending[id]
	delete(m.cancelPending, id)
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.runs, id)
		delete(m.cancelPending, id)
		m.mu.Unlock()
	}()
	if pending {
		cancel()
	}

	log.Printf("prefill job %d started pages=%d", id, len(row.Urls))
	status := m.processPages(s, jobCtx, run, row.Urls)

	m.mu.Lock()
	failed := run.failed
	errs := append([]model.PrefillItemError(nil), run.errs...)
	m.mu.Unlock()

	// A job where every page failed is reported as failed, not completed.
	if status == prefillStatusCompleted && len(row.Urls) > 0 && failed >= len(row.Urls) {
		status = prefillStatusFailed
	}

	s.finishPrefillJob(id, status, failed, errs)
	log.Printf("prefill job %d finished status=%s failed=%d", id, status, failed)
}

// processPages fetches every page URL serially, in order, updating the
// in-memory counters after each page. It returns the terminal status.
func (m *prefillManager) processPages(s *Server, ctx context.Context, run *prefillRun, urls []string) string {
	status := prefillStatusCompleted
	for i, pageURL := range urls {
		if ctx.Err() != nil {
			return prefillStatusCancelled
		}
		key := cache.CacheKey(pageURL)

		if _, err := s.Cache.Head(ctx, key); err == nil {
			m.mu.Lock()
			run.done++
			run.cached++
			m.mu.Unlock()
			continue
		}

		_, err := s.fetchWithRetry(ctx, key, pageURL)
		m.mu.Lock()
		if err != nil {
			if ctx.Err() != nil {
				m.mu.Unlock()
				return prefillStatusCancelled
			}
			run.done++
			run.failed++
			if len(run.errs) < prefillMaxErrors {
				run.errs = append(run.errs, model.PrefillItemError{
					Index: i,
					URL:   pageURL,
					Error: err.Error(),
				})
			}
			m.mu.Unlock()
			continue
		}
		run.done++
		run.fetched++
		m.mu.Unlock()
	}
	return status
}

// finishPrefillJob writes the terminal state. The status predicate guarantees
// it never overwrites a row that was already cancelled or deleted.
func (s *Server) finishPrefillJob(id int, status string, failed int, errs []model.PrefillItemError) {
	if errs == nil {
		errs = []model.PrefillItemError{}
	}
	now := time.Now()
	n, err := s.DB.Client.PrefillJob.Update().
		Where(
			prefilljob.ID(id),
			prefilljob.StatusIn(prefillStatusQueued, prefillStatusRunning),
		).
		SetStatus(status).
		SetFailedCount(failed).
		SetErrors(errs).
		SetFinishedAt(now).
		SetUpdatedAt(now).
		Save(context.Background())
	if err != nil {
		log.Printf("prefill finish failed id=%d err=%v", id, err)
		return
	}
	if n == 0 {
		log.Printf("prefill job %d terminal state already set, counts discarded", id)
	}
	if m := s.prefillMgr; m != nil {
		m.mu.Lock()
		delete(m.cancelPending, id)
		m.mu.Unlock()
	}
}

// cancelRun stops an active run. When the worker has claimed a row but has
// not registered its run yet, the cancel is remembered and applied at
// registration time so it is never lost. Returns true when a live run was
// cancelled directly (as opposed to the pending path).
func (m *prefillManager) cancelRun(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run, ok := m.runs[id]; ok && run.cancel != nil {
		run.cancel()
		return true
	}
	m.cancelPending[id] = struct{}{}
	return false
}

func (m *prefillManager) clearPending(id int) {
	m.mu.Lock()
	delete(m.cancelPending, id)
	m.mu.Unlock()
}

// requirePrefill validates the database dependency and ensures the manager.
// It returns nil (after writing the error response) when unavailable.
func (s *Server) requirePrefill(c *gin.Context) *prefillManager {
	if s.DB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return nil
	}
	return s.ensurePrefill()
}

func prefillJobID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid job id"})
		return 0, false
	}
	return id, true
}

// prefillJobJSON renders a job row. progress is only populated while the job
// is running in this process; failed_count/errors come from memory while
// running and from the database afterwards.
func prefillJobJSON(m *prefillManager, row *ent.PrefillJob) gin.H {
	var progress gin.H
	failedCount := row.FailedCount
	errs := row.Errors

	m.mu.Lock()
	if run, ok := m.runs[row.ID]; ok && row.Status == prefillStatusRunning {
		progress = gin.H{
			"done":    run.done,
			"cached":  run.cached,
			"fetched": run.fetched,
		}
		failedCount = run.failed
		errs = run.errs
	}
	m.mu.Unlock()

	if errs == nil {
		errs = []model.PrefillItemError{}
	}
	var galleryID any
	if row.GalleryID != nil {
		galleryID = *row.GalleryID
	}
	var finishedAt any
	if row.FinishedAt != nil {
		finishedAt = row.FinishedAt.Format(time.RFC3339)
	}
	return gin.H{
		"id":            row.ID,
		"gallery_id":    galleryID,
		"gallery_token": row.GalleryToken,
		"title":         row.Title,
		"status":        row.Status,
		"total":         len(row.Urls),
		"progress":      progress,
		"failed_count":  failedCount,
		"errors":        errs,
		"created_at":    row.CreatedAt.Format(time.RFC3339),
		"updated_at":    row.UpdatedAt.Format(time.RFC3339),
		"finished_at":   finishedAt,
	}
}

// ==================== Handlers ====================

// handlePrefillStart validates the request and enqueues a background job.
// Progress is polled separately; no image data is returned.
func (s *Server) handlePrefillStart(c *gin.Context) {
	var req struct {
		GalleryID    int64    `json:"gallery_id"`
		GalleryToken string   `json:"gallery_token"`
		Title        string   `json:"title"`
		URLs         []string `json:"urls"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if len(req.URLs) == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "urls must not be empty"})
		return
	}
	if len(req.URLs) > prefillMaxURIs {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("too many urls (max %d)", prefillMaxURIs)})
		return
	}
	for _, pageURL := range req.URLs {
		if err := exhentai.ValidatePageURL(pageURL); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if s.DB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return
	}
	if s.Cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cache not configured"})
		return
	}
	m := s.ensurePrefill()
	ctx := c.Request.Context()

	// Deduplicate: an already queued/running job for the same gallery wins.
	if req.GalleryID != 0 && req.GalleryToken != "" {
		existing, err := s.DB.Client.PrefillJob.Query().
			Where(
				prefilljob.GalleryID(req.GalleryID),
				prefilljob.GalleryToken(req.GalleryToken),
				prefilljob.StatusIn(prefillStatusQueued, prefillStatusRunning),
			).
			Order(ent.Desc(prefilljob.FieldCreatedAt)).
			First(ctx)
		if err == nil {
			c.JSON(http.StatusOK, prefillJobJSON(m, existing))
			return
		}
		if !ent.IsNotFound(err) {
			log.Printf("prefill dedupe query failed: %v", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
			return
		}
	}

	create := s.DB.Client.PrefillJob.Create().
		SetUrls(req.URLs).
		SetGalleryToken(req.GalleryToken).
		SetTitle(req.Title).
		SetStatus(prefillStatusQueued)
	if req.GalleryID != 0 {
		create.SetGalleryID(req.GalleryID)
	}
	row, err := create.Save(ctx)
	if err != nil {
		log.Printf("prefill create failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "create prefill job failed"})
		return
	}
	m.signal()
	c.JSON(http.StatusAccepted, prefillJobJSON(m, row))
}

// handlePrefillList returns jobs newest first. Progress fields are only
// present for jobs currently running in this process.
func (s *Server) handlePrefillList(c *gin.Context) {
	m := s.requirePrefill(c)
	if m == nil {
		return
	}
	rows, err := s.DB.Client.PrefillJob.Query().
		Order(ent.Desc(prefilljob.FieldCreatedAt)).
		Limit(prefillListLimit).
		All(c.Request.Context())
	if err != nil {
		log.Printf("prefill list failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query prefill jobs failed"})
		return
	}
	jobs := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, prefillJobJSON(m, row))
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// handlePrefillGet returns a single job snapshot.
func (s *Server) handlePrefillGet(c *gin.Context) {
	m := s.requirePrefill(c)
	if m == nil {
		return
	}
	id, ok := prefillJobID(c)
	if !ok {
		return
	}
	row, err := s.DB.Client.PrefillJob.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		log.Printf("prefill get failed id=%d err=%v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}
	c.JSON(http.StatusOK, prefillJobJSON(m, row))
}

// handlePrefillCancel cancels a job. The status flip is always a compare-and-
// swap against the state we observed, so it never overwrites a row that
// finished concurrently. Cancels take effect immediately in the response;
// an in-flight worker is signalled to stop.
func (s *Server) handlePrefillCancel(c *gin.Context) {
	m := s.requirePrefill(c)
	if m == nil {
		return
	}
	id, ok := prefillJobID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	for attempt := 0; attempt < 3; attempt++ {
		row, err := s.DB.Client.PrefillJob.Get(ctx, id)
		if err != nil {
			if ent.IsNotFound(err) {
				c.JSON(http.StatusNotFound, gin.H{"error": "prefill job not found"})
				return
			}
			log.Printf("prefill cancel get failed id=%d err=%v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
			return
		}
		if row.Status != prefillStatusQueued && row.Status != prefillStatusRunning {
			if row.Status == prefillStatusCancelled {
				c.JSON(http.StatusOK, prefillJobJSON(m, row))
				return
			}
			c.JSON(http.StatusConflict, gin.H{"error": "job already finished"})
			return
		}

		if row.Status == prefillStatusRunning {
			m.cancelRun(id)
		}
		now := time.Now()
		n, err := s.DB.Client.PrefillJob.Update().
			Where(prefilljob.ID(id), prefilljob.Status(row.Status)).
			SetStatus(prefillStatusCancelled).
			SetFinishedAt(now).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			log.Printf("prefill cancel failed id=%d err=%v", id, err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "cancel prefill job failed"})
			return
		}
		if n == 1 {
			m.clearPending(id)
			row.Status = prefillStatusCancelled
			row.FinishedAt = &now
			c.JSON(http.StatusOK, prefillJobJSON(m, row))
			return
		}
		// Lost a race with the worker (it claimed or finished the row): retry.
	}
	c.JSON(http.StatusConflict, gin.H{"error": "cancel conflict, retry"})
}

// handlePrefillDelete removes a terminal job record.
func (s *Server) handlePrefillDelete(c *gin.Context) {
	m := s.requirePrefill(c)
	if m == nil {
		return
	}
	id, ok := prefillJobID(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	row, err := s.DB.Client.PrefillJob.Get(ctx, id)
	if err != nil {
		if ent.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		log.Printf("prefill get failed id=%d err=%v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}
	if row.Status == prefillStatusQueued || row.Status == prefillStatusRunning {
		c.JSON(http.StatusConflict, gin.H{"error": "job is active, cancel it first"})
		return
	}
	n, err := s.DB.Client.PrefillJob.Delete().
		Where(
			prefilljob.ID(id),
			prefilljob.StatusIn(prefillStatusCompleted, prefillStatusCancelled, prefillStatusFailed),
		).
		Exec(ctx)
	if err != nil {
		log.Printf("prefill delete failed id=%d err=%v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "delete prefill job failed"})
		return
	}
	if n == 0 {
		c.JSON(http.StatusConflict, gin.H{"error": "job is active, cancel it first"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"deleted": n})
}

// handlePrefillCleanup bulk-deletes terminal jobs. days=0 removes all
// terminal records; active jobs are never touched.
func (s *Server) handlePrefillCleanup(c *gin.Context) {
	m := s.requirePrefill(c)
	if m == nil {
		return
	}
	daysStr := c.DefaultQuery("days", strconv.Itoa(prefillCleanupDaysDefault))
	days, err := strconv.Atoi(daysStr)
	if err != nil || days < 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid days"})
		return
	}
	q := s.DB.Client.PrefillJob.Delete().
		Where(prefilljob.StatusIn(prefillStatusCompleted, prefillStatusCancelled, prefillStatusFailed))
	if days > 0 {
		cutoff := time.Now().AddDate(0, 0, -days)
		q.Where(prefilljob.FinishedAtLT(cutoff))
	}
	n, err := q.Exec(c.Request.Context())
	if err != nil {
		log.Printf("prefill cleanup failed: %v", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "cleanup prefill jobs failed"})
		return
	}
	c.JSON(http.StatusOK, gin.H{"days": days, "deleted": n})
}

// handlePrefillZip streams a ZIP archive for a job. Every entry is written
// straight from the MinIO cache (fetching misses on the fly, deduplicated
// through singleflight but not queued behind the worker) so the browser
// receives a chunked, streaming download instead of an in-memory archive.
func (s *Server) handlePrefillZip(c *gin.Context) {
	if s.DB == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return
	}
	if s.Cache == nil {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "cache not configured"})
		return
	}
	id, ok := prefillJobID(c)
	if !ok {
		return
	}
	row, err := s.DB.Client.PrefillJob.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		log.Printf("prefill get failed id=%d err=%v", id, err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}

	ctx := c.Request.Context()
	name := prefillZipFilename(row.Title)
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", fmt.Sprintf(
		`attachment; filename="%s"; filename*=UTF-8''%s`,
		asciiFallback(name), rfc5987Escape(name),
	))
	c.Header("Cache-Control", "no-store")
	c.Status(http.StatusOK)

	zw := zip.NewWriter(c.Writer)
	for i, pageURL := range row.Urls {
		if ctx.Err() != nil {
			break
		}
		key := cache.CacheKey(pageURL)
		var data []byte
		var contentType string
		if _, headErr := s.Cache.Head(ctx, key); headErr == nil {
			d, ct, getErr := s.Cache.Get(ctx, key)
			if getErr != nil {
				log.Printf("prefill zip cache read failed index=%d err=%v", i, getErr)
				continue
			}
			data, contentType = d, ct
		} else {
			res, fetchErr := s.fetchWithRetry(ctx, key, pageURL)
			if fetchErr != nil {
				log.Printf("prefill zip fetch failed index=%d err=%v", i, fetchErr)
				continue
			}
			data, contentType = res.data, res.contentType
		}

		entryName := fmt.Sprintf("%03d%s", i+1, prefillImageExt(contentType, pageURL))
		w, err := zw.CreateHeader(&zip.FileHeader{Name: entryName, Method: zip.Store})
		if err != nil {
			log.Printf("prefill zip entry failed index=%d err=%v", i, err)
			break
		}
		if _, err := w.Write(data); err != nil {
			log.Printf("prefill zip write failed index=%d err=%v", i, err)
			break
		}
		c.Writer.Flush()
	}
	if err := zw.Close(); err != nil {
		log.Printf("prefill zip close failed: %v", err)
	}
}

// ==================== Helpers ====================

func prefillImageExt(contentType, pageURL string) string {
	ct := strings.ToLower(strings.TrimSpace(strings.Split(contentType, ";")[0]))
	switch ct {
	case "image/jpeg", "image/jpg":
		return ".jpg"
	case "image/png":
		return ".png"
	case "image/webp":
		return ".webp"
	case "image/gif":
		return ".gif"
	}
	if u, err := url.Parse(pageURL); err == nil {
		switch strings.ToLower(path.Ext(u.Path)) {
		case ".jpg", ".jpeg":
			return ".jpg"
		case ".png":
			return ".png"
		case ".webp":
			return ".webp"
		case ".gif":
			return ".gif"
		}
	}
	return ".jpg"
}

// prefillZipFilename mirrors the frontend's sanitizeFilename so archives are
// named consistently with the previous client-side ZIP downloads.
func prefillZipFilename(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r == '"' || r == '\\' || strings.ContainsRune(`<>:|?*`, r):
			b.WriteRune(' ')
		case r < 0x20 || r == 0x7f:
			b.WriteRune(' ')
		default:
			b.WriteRune(r)
		}
	}
	runes := []rune(strings.Join(strings.Fields(b.String()), " "))
	if len(runes) > 100 {
		runes = []rune(strings.TrimSpace(string(runes[:100])))
	}
	name := string(runes)
	if name == "" {
		name = "download"
	}
	return name
}

func asciiFallback(name string) string {
	var b strings.Builder
	for _, r := range name {
		if r >= 0x20 && r < 0x7f && r != '"' && r != '\\' {
			b.WriteRune(r)
		} else {
			b.WriteByte('_')
		}
	}
	if b.Len() == 0 {
		return "download"
	}
	return b.String()
}

// rfc5987Escape percent-encodes a string for the filename* parameter.
func rfc5987Escape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		ch := s[i]
		if isAttrChar(ch) {
			b.WriteByte(ch)
		} else {
			fmt.Fprintf(&b, "%%%02X", ch)
		}
	}
	return b.String()
}

func isAttrChar(c byte) bool {
	if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' {
		return true
	}
	return strings.IndexByte("!#$&+-.^_`|~", c) >= 0
}
