package handler

import (
	"archive/zip"
	"context"
	"fmt"
	"log/slog"
	"maps"
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

	prefillPageTimeout          = 90 * time.Second
	prefillConsecutiveFailLimit = 10
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
	wake chan struct{}
	stop chan struct{}
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
			runs: make(map[int]*prefillRun),
			wake: make(chan struct{}, 1),
			stop: make(chan struct{}),
		}
		s.prefillMgr = m
		go m.worker(s)
		m.recoverPending(s)
	})
	return s.prefillMgr
}

func (m *prefillManager) recoverPending(s *Server) {
	if s.Cache == nil {
		slog.Warn("prefill recover skipped: cache not configured")
		return
	}
	ctx := context.Background()
	var lastErr error
	for attempt := range 3 {
		n, err := s.DB.Client.PrefillJob.Update().
			Where(prefilljob.Status(prefillStatusRunning)).
			SetStatus(prefillStatusQueued).
			SetUpdatedAt(time.Now()).
			Save(ctx)
		if err == nil {
			if n > 0 {
				slog.Info("prefill re-queued interrupted jobs", "count", n)
			}
			m.signal()
			return
		}
		lastErr = err
		slog.Info("prefill recover attempt failed", "attempt", attempt+1, "error", err)
		time.Sleep(100 * time.Millisecond)
	}
	slog.Error("prefill recover failed after retries", "error", lastErr)
}

func (m *prefillManager) worker(s *Server) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-m.wake:
			m.drain(s)
		case <-ticker.C:
			m.drain(s)
		case <-m.stop:
			return
		}
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
					slog.Error("prefill job panic", "job_id", id, "panic", r)
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
				slog.Error("prefill queue query failed", "error", err)
			}
			return 0, false
		}
		var lastErr error
		for attempt := range 3 {
			n, err := s.DB.Client.PrefillJob.Update().
				Where(prefilljob.ID(row.ID), prefilljob.Status(prefillStatusQueued)).
				SetStatus(prefillStatusRunning).
				SetUpdatedAt(time.Now()).
				Save(ctx)
			if err == nil {
				if n == 0 {
					break // cancelled between query and claim; take the next one
				}
				return row.ID, true
			}
			lastErr = err
			slog.Error("prefill claim attempt failed", "attempt", attempt+1, "job_id", row.ID, "error", err)
			time.Sleep(50 * time.Millisecond)
		}
		slog.Error("prefill claim failed after retries", "job_id", row.ID, "error", lastErr)
		return 0, false
	}
}

func (m *prefillManager) runJob(s *Server, id int) {
	ctx := context.Background()

	// Guard against missing cache configuration.
	if s.Cache == nil {
		s.finishPrefillJob(id, prefillStatusFailed, 0, []model.PrefillItemError{
			{Index: 0, URL: "", Error: "cache not configured"},
		})
		return
	}

	// Register the run first so cancels arriving from here on find the cancel func.
	jobCtx, cancel := context.WithCancel(ctx)
	run := &prefillRun{cancel: cancel}
	m.mu.Lock()
	m.runs[id] = run
	m.mu.Unlock()
	defer func() {
		cancel()
		m.mu.Lock()
		delete(m.runs, id)
		m.mu.Unlock()
	}()

	row, err := s.DB.Client.PrefillJob.Get(ctx, id)
	if err != nil {
		if !ent.IsNotFound(err) {
			slog.Error("prefill load failed", "job_id", id, "error", err)
			// Non-NotFound error: mark as failed so the row doesn't stay running forever.
			s.finishPrefillJob(id, prefillStatusFailed, 0, []model.PrefillItemError{
				{Index: 0, URL: "", Error: fmt.Sprintf("load failed: %v", err)},
			})
		}
		return
	}

	// If the job was cancelled (or otherwise terminal) before we started, exit.
	if row.Status != prefillStatusRunning {
		return
	}

	slog.Info("prefill job started", "job_id", id, "pages", len(row.Urls))
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
	slog.Info("prefill job finished", "job_id", id, "status", status, "failed", failed)
}

// processPages fetches every page URL serially, in order, updating the
// in-memory counters after each page. It returns the terminal status.
func (m *prefillManager) processPages(s *Server, ctx context.Context, run *prefillRun, urls []string) string {
	status := prefillStatusCompleted
	consecutiveFails := 0
	for i, pageURL := range urls {
		if ctx.Err() != nil {
			return prefillStatusCancelled
		}
		key := cache.CacheKey(pageURL)

		pageCtx, pageCancel := context.WithTimeout(ctx, prefillPageTimeout)
		res, err := s.loadOrFetchImage(pageCtx, key, pageURL)
		pageCancel()
		m.mu.Lock()
		if err != nil {
			if ctx.Err() != nil {
				m.mu.Unlock()
				return prefillStatusCancelled
			}
			run.done++
			run.failed++
			consecutiveFails++
			if len(run.errs) < prefillMaxErrors {
				run.errs = append(run.errs, model.PrefillItemError{
					Index: i,
					URL:   pageURL,
					Error: err.Error(),
				})
			}
			if consecutiveFails >= prefillConsecutiveFailLimit {
				m.mu.Unlock()
				return prefillStatusFailed
			}
			m.mu.Unlock()
			continue
		}
		if res.cacheHit {
			run.cached++
		} else if res.stored {
			run.fetched++
		} else {
			run.failed++
			if len(run.errs) < prefillMaxErrors {
				run.errs = append(run.errs, model.PrefillItemError{
					Index: i,
					URL:   pageURL,
					Error: "cache store failed",
				})
			}
			if consecutiveFails >= prefillConsecutiveFailLimit {
				m.mu.Unlock()
				return prefillStatusFailed
			}
			m.mu.Unlock()
			continue
		}
		run.done++
		consecutiveFails = 0
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
	var lastErr error
	for attempt := range 3 {
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
		if err == nil {
			if n == 0 {
				slog.Info("prefill job terminal state already set", "job_id", id)
			}
			return
		}
		lastErr = err
		slog.Error("prefill finish attempt failed", "attempt", attempt+1, "job_id", id, "error", err)
		time.Sleep(100 * time.Millisecond)
	}
	slog.Error("prefill finish failed after retries", "job_id", id, "error", lastErr)
}

// cancelRun stops an active run by cancelling its context.
// Returns true when a live run was cancelled directly.
func (m *prefillManager) cancelRun(id int) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if run, ok := m.runs[id]; ok && run.cancel != nil {
		run.cancel()
		return true
	}
	return false
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
		"total":         row.Total,
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
			slog.Error("prefill dedupe query failed", "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
			return
		}
	}

	create := s.DB.Client.PrefillJob.Create().
		SetUrls(req.URLs).
		SetGalleryToken(req.GalleryToken).
		SetTitle(req.Title).
		SetStatus(prefillStatusQueued).
		SetTotal(len(req.URLs))
	if req.GalleryID != 0 {
		create.SetGalleryID(req.GalleryID)
	}
	row, err := create.Save(ctx)
	if err != nil {
		slog.Error("prefill create failed", "error", err)
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
	// Snapshot active runs once to avoid per-row locking.
	m.mu.Lock()
	runSnapshot := make(map[int]*prefillRun, len(m.runs))
	maps.Copy(runSnapshot, m.runs)
	m.mu.Unlock()

	rows, err := s.DB.Client.PrefillJob.Query().
		Select(
			prefilljob.FieldID,
			prefilljob.FieldGalleryID,
			prefilljob.FieldGalleryToken,
			prefilljob.FieldTitle,
			prefilljob.FieldStatus,
			prefilljob.FieldTotal,
			prefilljob.FieldFailedCount,
			prefilljob.FieldErrors,
			prefilljob.FieldCreatedAt,
			prefilljob.FieldUpdatedAt,
			prefilljob.FieldFinishedAt,
		).
		Order(ent.Desc(prefilljob.FieldCreatedAt)).
		Limit(prefillListLimit).
		All(c.Request.Context())
	if err != nil {
		slog.Error("prefill list failed", "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query prefill jobs failed"})
		return
	}
	jobs := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, prefillJobJSONWithRuns(row, runSnapshot))
	}
	c.JSON(http.StatusOK, gin.H{"jobs": jobs})
}

// prefillJobJSONWithRuns is like prefillJobJSON but takes a pre-snapshot of runs.
func prefillJobJSONWithRuns(row *ent.PrefillJob, runs map[int]*prefillRun) gin.H {
	var progress gin.H
	failedCount := row.FailedCount
	errs := row.Errors

	if run, ok := runs[row.ID]; ok && row.Status == prefillStatusRunning {
		progress = gin.H{
			"done":    run.done,
			"cached":  run.cached,
			"fetched": run.fetched,
		}
		failedCount = run.failed
		errs = run.errs
	}

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
		"total":         row.Total,
		"progress":      progress,
		"failed_count":  failedCount,
		"errors":        errs,
		"created_at":    row.CreatedAt.Format(time.RFC3339),
		"updated_at":    row.UpdatedAt.Format(time.RFC3339),
		"finished_at":   finishedAt,
	}
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
		slog.Error("prefill get failed", "job_id", id, "error", err)
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

	for range 3 {
		row, err := s.DB.Client.PrefillJob.Get(ctx, id)
		if err != nil {
			if ent.IsNotFound(err) {
				c.JSON(http.StatusNotFound, gin.H{"error": "prefill job not found"})
				return
			}
			slog.Error("prefill cancel get failed", "job_id", id, "error", err)
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

		now := time.Now()
		n, err := s.DB.Client.PrefillJob.Update().
			Where(prefilljob.ID(id), prefilljob.Status(row.Status)).
			SetStatus(prefillStatusCancelled).
			SetFinishedAt(now).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			slog.Error("prefill cancel failed", "job_id", id, "error", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "cancel prefill job failed"})
			return
		}
		if n == 1 {
			// CAS succeeded. If the job was running, cancel the in-flight run.
			if row.Status == prefillStatusRunning {
				m.cancelRun(id)
			}
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
		slog.Error("prefill get failed", "job_id", id, "error", err)
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
		slog.Error("prefill delete failed", "job_id", id, "error", err)
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
		slog.Error("prefill cleanup failed", "error", err)
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
		slog.Error("prefill get failed", "job_id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}

	ctx := c.Request.Context()
	name := prefillZipFilename(row.Title)
	zw := zip.NewWriter(c.Writer)
	started := false
	written := 0
	missing := []string{}

	for i, pageURL := range row.Urls {
		if ctx.Err() != nil {
			break
		}
		key := cache.CacheKey(pageURL)

		pageCtx, pageCancel := context.WithTimeout(ctx, prefillPageTimeout)
		res, err := s.loadOrFetchImage(pageCtx, key, pageURL)
		pageCancel()
		if err != nil {
			slog.Error("prefill zip fetch failed", "index", i, "error", err)
			missing = append(missing, fmt.Sprintf("%d: %s", i+1, err.Error()))
			continue
		}

		if !started {
			c.Header("Content-Type", "application/zip")
			c.Header("Content-Disposition", fmt.Sprintf(
				`attachment; filename="%s"; filename*=UTF-8''%s`,
				asciiFallback(name), rfc5987Escape(name),
			))
			c.Header("Cache-Control", "no-store")
			c.Status(http.StatusOK)
			started = true
		}

		entryName := fmt.Sprintf("%03d%s", i+1, prefillImageExt(res.contentType, pageURL))
		w, err := zw.CreateHeader(&zip.FileHeader{Name: entryName, Method: zip.Store})
		if err != nil {
			slog.Error("prefill zip entry failed", "index", i, "error", err)
			break
		}
		if _, err := w.Write(res.data); err != nil {
			slog.Error("prefill zip write failed", "index", i, "error", err)
			break
		}
		c.Writer.Flush()
		written++
	}

	if !started {
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no pages could be fetched for ZIP"})
		return
	}

	if len(missing) > 0 {
		missingData := []byte(strings.Join(missing, "\n"))
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "_missing.txt", Method: zip.Store})
		if err == nil {
			w.Write(missingData)
		}
	}

	if err := zw.Close(); err != nil {
		slog.Error("prefill zip close failed", "error", err)
	}
}

// handlePrefillZipHead handles HEAD requests for the ZIP endpoint.
// It returns 200 if the job exists and cache is configured, without streaming any data.
func (s *Server) handlePrefillZipHead(c *gin.Context) {
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
	_, err := s.DB.Client.PrefillJob.Get(c.Request.Context(), id)
	if err != nil {
		if ent.IsNotFound(err) {
			c.JSON(http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		slog.Error("prefill zip head get failed", "job_id", id, "error", err)
		c.JSON(http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}
	c.Status(http.StatusOK)
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
