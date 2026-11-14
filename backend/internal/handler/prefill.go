package handler

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/archive"
	"manga-reader/internal/cache"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/predicate"
	"manga-reader/internal/ent/prefilljob"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/ident"
	"manga-reader/internal/metrics"
	"manga-reader/internal/model"
	"manga-reader/internal/storage"
	"manga-reader/internal/web"
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
	runs map[ident.UUID]*prefillRun
	wake chan struct{}
	stop chan struct{}
	// stopOnce guards closing stop so shutdown is idempotent.
	stopOnce sync.Once
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
			runs: make(map[ident.UUID]*prefillRun),
			wake: make(chan struct{}, 1),
			stop: make(chan struct{}),
		}
		s.prefillMu.Lock()
		s.prefillMgr = m
		s.prefillMu.Unlock()
		go m.worker(s)
		m.recoverPending(s)
	})
	return s.prefillMgr
}

// StopPrefill cancels active runs and stops the worker goroutine. It is safe to
// call multiple times and when the manager was never started.
func (s *Server) StopPrefill() {
	s.prefillMu.Lock()
	m := s.prefillMgr
	s.prefillMu.Unlock()
	if m == nil {
		return
	}
	m.mu.Lock()
	for _, run := range m.runs {
		if run.cancel != nil {
			run.cancel()
		}
	}
	m.mu.Unlock()
	m.stopOnce.Do(func() { close(m.stop) })
}

func (m *prefillManager) recoverPending(s *Server) {
	if s.Cache == nil && s.Archive == nil {
		slog.Warn("prefill recover skipped: no storage configured")
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
		slog.Warn("prefill recover attempt failed", "attempt", attempt+1, "error", err)
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
		id, uid, ok := m.nextQueued(s)
		if !ok {
			return
		}
		func() {
			defer func() {
				if r := recover(); r != nil {
					slog.Error("prefill job panic", "job_id", id, "panic", r)
					s.finishPrefillJob(id, uid, prefillStatusFailed, 0, nil, nil)
				}
			}()
			m.runJob(s, id, uid)
		}()
	}
}

// nextQueued claims the oldest queued job by atomically flipping it to
// running. Returns false when the queue is empty.
func (m *prefillManager) nextQueued(s *Server) (ident.UUID, ident.UUID, bool) {
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
			return ident.UUID{}, ident.Nil(), false
		}
		var lastErr error
		for attempt := range 3 {
			n, err := s.DB.Client.PrefillJob.Update().
				Where(
					prefilljob.ID(row.ID),
					prefilljob.UserIDEQ(row.UserID),
					prefilljob.Status(prefillStatusQueued),
				).
				SetStatus(prefillStatusRunning).
				SetUpdatedAt(time.Now()).
				Save(ctx)
			if err == nil {
				if n == 0 {
					break // cancelled between query and claim; take the next one
				}
				return row.ID, row.UserID, true
			}
			lastErr = err
			slog.Warn("prefill claim attempt failed", "attempt", attempt+1, "job_id", row.ID, "error", err)
			time.Sleep(50 * time.Millisecond)
		}
		slog.Error("prefill claim failed after retries", "job_id", row.ID, "error", lastErr)
		// Job was claimed by another worker or failed after retries;
		// continue outer loop to try the next queued job.
		continue
	}
}

func (m *prefillManager) runJob(s *Server, id ident.UUID, userID ident.UUID) {
	ctx := context.Background()

	// At least one of the image cache or the ZIP archive must be enabled for a
	// prefill job to accomplish anything.
	if s.Cache == nil && s.Archive == nil {
		s.finishPrefillJob(id, ident.Nil(), prefillStatusFailed, 0, []model.PrefillItemError{
			{Index: 0, URL: "", Error: "no storage configured"},
		}, nil)
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

	row, err := s.DB.Client.PrefillJob.Query().
		Where(prefilljob.ID(id), prefilljob.UserIDEQ(userID)).
		Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			slog.Error("prefill load failed", "job_id", id, "error", err)
			// Non-NotFound error: mark as failed so the row doesn't stay running forever.
			s.finishPrefillJob(id, userID, prefillStatusFailed, 0, []model.PrefillItemError{
				{Index: 0, URL: "", Error: fmt.Sprintf("load failed: %v", err)},
			}, nil)
		}
		return
	}

	// If the job was cancelled (or otherwise terminal) before we started, exit.
	if row.Status != prefillStatusRunning {
		return
	}

	slog.Info("prefill job started", "job_id", id, "pages", len(row.Urls))
	status := prefillStatusCompleted

	// Phase 1: cache the page images. Only runs when the image cache is enabled.
	if s.Cache != nil {
		if st := m.processPages(s, jobCtx, run, row.UserID, row.Urls); st != prefillStatusCompleted {
			m.finishWithRun(s, id, row.UserID, st, nil)
			return
		}
		m.mu.Lock()
		allFailed := run.failed >= len(row.Urls)
		m.mu.Unlock()
		if len(row.Urls) > 0 && allFailed {
			m.finishWithRun(s, id, row.UserID, prefillStatusFailed, nil)
			return
		}
	}

	// Phase 2: build and upload the ZIP archive. Only runs when the archive is
	// enabled; the archive is built from the cache (phase 1) or, when the cache
	// is disabled, fetched straight from upstream.
	var archiveInfo *storage.ObjectInfo
	if s.Archive != nil {
		info, archiveErr := m.buildArchive(s, jobCtx, run, row)
		if archiveErr != nil {
			if s.Cache == nil {
				// Archive-only mode: the archive is the whole deliverable, so an
				// upload failure fails the job.
				m.mu.Lock()
				run.failed++
				if len(run.errs) < prefillMaxErrors {
					run.errs = append(run.errs, model.PrefillItemError{Index: 0, URL: "", Error: archiveErr.Error()})
				}
				m.mu.Unlock()
				m.finishWithRun(s, id, row.UserID, prefillStatusFailed, nil)
				return
			}
			// The cache succeeded; a failed archive is non-fatal here, and the
			// download endpoint falls back to on-the-fly streaming.
			slog.Error("prefill archive build failed", "job_id", id, "error", archiveErr)
		} else {
			archiveInfo = &info
		}
	}

	m.finishWithRun(s, id, row.UserID, status, archiveInfo)
	slog.Info("prefill job finished", "job_id", id, "status", status)
}

// finishWithRun snapshots the run's counters and writes the terminal state.
func (m *prefillManager) finishWithRun(s *Server, id, userID ident.UUID, status string, archiveInfo *storage.ObjectInfo) {
	m.mu.Lock()
	var failed int
	var errs []model.PrefillItemError
	if run := m.runs[id]; run != nil {
		failed = run.failed
		errs = append([]model.PrefillItemError(nil), run.errs...)
	}
	m.mu.Unlock()
	s.finishPrefillJob(id, userID, status, failed, errs, archiveInfo)
}

// processPages fetches every page URL serially, in order, updating the
// in-memory counters after each page. It returns the terminal status.
func (m *prefillManager) processPages(s *Server, ctx context.Context, run *prefillRun, userID ident.UUID, urls []string) string {
	status := prefillStatusCompleted
	consecutiveFails := 0
	for i, pageURL := range urls {
		if ctx.Err() != nil {
			return prefillStatusCancelled
		}
		key := cache.CacheKey(pageURL)

		pageCtx, pageCancel := context.WithTimeout(ctx, prefillPageTimeout)
		res, err := s.loadOrFetchImage(pageCtx, userID, key, pageURL)
		pageCancel()
		if err == nil {
			// Only the cache outcome is inspected below — the bytes are
			// never used — so return the budget reservation before taking
			// the job lock (a blocking resize must not run under it).
			res.release()
		}
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
		switch {
		case res.cacheHit:
			run.cached++
		case res.stored:
			run.fetched++
		default:
			run.failed++
			// A failed cache write is just as terminal as a fetch error: count
			// it so a MinIO outage aborts the job instead of grinding through
			// every remaining page.
			consecutiveFails++
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
// it never overwrites a row that was already cancelled or deleted. When
// archiveInfo is non-nil it also records the uploaded ZIP object so subsequent
// downloads can reuse it.
func (s *Server) finishPrefillJob(id ident.UUID, userID ident.UUID, status string, failed int, errs []model.PrefillItemError, archiveInfo *storage.ObjectInfo) {
	if errs == nil {
		errs = []model.PrefillItemError{}
	}
	now := time.Now()
	var lastErr error
	for attempt := range 3 {
		preds := []predicate.PrefillJob{
			prefilljob.ID(id),
			prefilljob.StatusIn(prefillStatusQueued, prefillStatusRunning),
		}
		if userID != ident.Nil() {
			preds = append(preds, prefilljob.UserIDEQ(userID))
		}
		upd := s.DB.Client.PrefillJob.Update().
			Where(preds...).
			SetStatus(status).
			SetFailedCount(failed).
			SetErrors(errs).
			SetFinishedAt(now).
			SetUpdatedAt(now)
		if archiveInfo != nil {
			upd.SetArchiveKey(archiveInfo.Key).
				SetArchiveSize(archiveInfo.Size).
				SetArchiveEtag(archiveInfo.ETag)
		}
		n, err := upd.Save(context.Background())
		if err == nil {
			if n == 0 {
				slog.Info("prefill job terminal state already set", "job_id", id)
			}
			return
		}
		lastErr = err
		slog.Warn("prefill finish attempt failed", "attempt", attempt+1, "job_id", id, "error", err)
		time.Sleep(100 * time.Millisecond)
	}
	slog.Error("prefill finish failed after retries", "job_id", id, "error", lastErr)
}

// cancelRun stops an active run by cancelling its context.
// Returns true when a live run was cancelled directly.
func (m *prefillManager) cancelRun(id ident.UUID) bool {
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
		web.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return nil
	}
	return s.ensurePrefill()
}

func prefillJobID(c *gin.Context) (ident.UUID, bool) {
	id, err := ident.Parse(c.Param("id"))
	if err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid job id"})
		return ident.UUID{}, false
	}
	return id, true
}

// prefillJobJSON renders a job row. progress is only populated while the job
// is running in this process; failed_count/errors come from memory while
// running and from the database afterwards.
func prefillJobJSON(m *prefillManager, row *ent.PrefillJob) gin.H {
	return prefillJobJSONWithRuns(row, m.snapshotRuns())
}

// prefillRunSnapshot is an immutable copy of a live run's counters, taken
// under prefillManager.mu so rendering never races the worker's updates
// (run.errs in particular is append-ed by the worker).
type prefillRunSnapshot struct {
	done    int
	cached  int
	fetched int
	failed  int
	errs    []model.PrefillItemError
}

func (m *prefillManager) snapshotRuns() map[ident.UUID]prefillRunSnapshot {
	m.mu.Lock()
	defer m.mu.Unlock()
	snap := make(map[ident.UUID]prefillRunSnapshot, len(m.runs))
	for id, run := range m.runs {
		snap[id] = prefillRunSnapshot{
			done:    run.done,
			cached:  run.cached,
			fetched: run.fetched,
			failed:  run.failed,
			errs:    append([]model.PrefillItemError(nil), run.errs...),
		}
	}
	return snap
}

func prefillJobJSONWithRuns(row *ent.PrefillJob, runs map[ident.UUID]prefillRunSnapshot) gin.H {
	var progress any
	failedCount := row.FailedCount
	errs := row.Errors

	if snap, ok := runs[row.ID]; ok && row.Status == prefillStatusRunning {
		progress = gin.H{
			"done":    snap.done,
			"cached":  snap.cached,
			"fetched": snap.fetched,
		}
		failedCount = snap.failed
		errs = snap.errs
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
		"id":            row.ID.String(),
		"gallery_id":    galleryID,
		"token":         row.Token,
		"title":         row.Title,
		"status":        row.Status,
		"total":         row.Total,
		"progress":      progress,
		"failed_count":  failedCount,
		"errors":        errs,
		"archive_ready": row.ArchiveKey != nil,
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
		GalleryID int64    `json:"gallery_id"`
		Token     string   `json:"token"`
		Title     string   `json:"title"`
		URLs      []string `json:"urls"`
	}
	// The urls array is materialized whole before any validation, so cap the
	// body; ReadTimeout bounds time, not size.
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<20)
	if err := web.BindJSON(c, &req); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			web.JSON(c, http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if len(req.URLs) == 0 {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "urls must not be empty"})
		return
	}
	if len(req.URLs) > prefillMaxURIs {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": fmt.Sprintf("too many urls (max %d)", prefillMaxURIs)})
		return
	}
	for _, pageURL := range req.URLs {
		if err := exhentai.ValidatePageURL(pageURL); err != nil {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if s.DB == nil {
		web.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return
	}
	if s.Cache == nil && s.Archive == nil {
		web.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "no storage configured"})
		return
	}
	m := s.ensurePrefill()
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	// Deduplicate: an already queued/running job for the same gallery wins.
	if req.GalleryID != 0 && req.Token != "" {
		existing, err := s.DB.Client.PrefillJob.Query().
			Where(
				prefilljob.UserIDEQ(uid),
				prefilljob.GalleryID(req.GalleryID),
				prefilljob.Token(req.Token),
				prefilljob.StatusIn(prefillStatusQueued, prefillStatusRunning),
			).
			Order(ent.Desc(prefilljob.FieldCreatedAt)).
			First(ctx)
		if err == nil {
			web.JSON(c, http.StatusOK, prefillJobJSON(m, existing))
			return
		}
		if !ent.IsNotFound(err) {
			slog.Error("prefill dedupe query failed", "error", err)
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
			return
		}
	}

	create := s.DB.Client.PrefillJob.Create().
		SetUserID(uid).
		SetUrls(req.URLs).
		SetToken(req.Token).
		SetTitle(req.Title).
		SetStatus(prefillStatusQueued).
		SetTotal(len(req.URLs))
	if req.GalleryID != 0 {
		create.SetGalleryID(req.GalleryID)
	}
	row, err := create.Save(ctx)
	if err != nil {
		slog.Error("prefill create failed", "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "create prefill job failed"})
		return
	}
	m.signal()
	web.JSON(c, http.StatusAccepted, prefillJobJSON(m, row))
}

// handlePrefillList returns jobs newest first. Progress fields are only
// present for jobs currently running in this process.
func (s *Server) handlePrefillList(c *gin.Context) {
	m := s.requirePrefill(c)
	if m == nil {
		return
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	// Snapshot active runs once to avoid per-row locking.
	runSnapshot := m.snapshotRuns()

	rows, err := s.DB.Client.PrefillJob.Query().
		Where(prefilljob.UserIDEQ(uid)).
		Select(
			prefilljob.FieldID,
			prefilljob.FieldGalleryID,
			prefilljob.FieldToken,
			prefilljob.FieldTitle,
			prefilljob.FieldStatus,
			prefilljob.FieldTotal,
			prefilljob.FieldFailedCount,
			prefilljob.FieldErrors,
			prefilljob.FieldArchiveKey,
			prefilljob.FieldCreatedAt,
			prefilljob.FieldUpdatedAt,
			prefilljob.FieldFinishedAt,
		).
		Order(ent.Desc(prefilljob.FieldCreatedAt)).
		Limit(prefillListLimit).
		All(c.Request.Context())
	if err != nil {
		slog.Error("prefill list failed", "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill jobs failed"})
		return
	}
	jobs := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, prefillJobJSONWithRuns(row, runSnapshot))
	}
	web.JSON(c, http.StatusOK, gin.H{"jobs": jobs})
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
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	row, err := s.getPrefillJob(c.Request.Context(), uid, id)
	if err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		slog.Error("prefill get failed", "job_id", id, "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}
	web.JSON(c, http.StatusOK, prefillJobJSON(m, row))
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
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()

	for range 3 {
		row, err := s.getPrefillJob(ctx, uid, id)
		if err != nil {
			if ent.IsNotFound(err) {
				web.JSON(c, http.StatusNotFound, gin.H{"error": "prefill job not found"})
				return
			}
			slog.Error("prefill cancel get failed", "job_id", id, "error", err)
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
			return
		}
		if row.Status != prefillStatusQueued && row.Status != prefillStatusRunning {
			if row.Status == prefillStatusCancelled {
				web.JSON(c, http.StatusOK, prefillJobJSON(m, row))
				return
			}
			web.JSON(c, http.StatusConflict, gin.H{"error": "job already finished"})
			return
		}

		now := time.Now()
		n, err := s.DB.Client.PrefillJob.Update().
			Where(prefilljob.ID(id), prefilljob.UserIDEQ(uid), prefilljob.Status(row.Status)).
			SetStatus(prefillStatusCancelled).
			SetFinishedAt(now).
			SetUpdatedAt(now).
			Save(ctx)
		if err != nil {
			slog.Error("prefill cancel failed", "job_id", id, "error", err)
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "cancel prefill job failed"})
			return
		}
		if n == 1 {
			// CAS succeeded. If the job was running, cancel the in-flight run.
			if row.Status == prefillStatusRunning {
				m.cancelRun(id)
			}
			row.Status = prefillStatusCancelled
			row.FinishedAt = &now
			web.JSON(c, http.StatusOK, prefillJobJSON(m, row))
			return
		}
		// Lost a race with the worker (it claimed or finished the row): retry.
	}
	web.JSON(c, http.StatusConflict, gin.H{"error": "cancel conflict, retry"})
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
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	ctx := c.Request.Context()
	row, err := s.getPrefillJob(ctx, uid, id)
	if err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		slog.Error("prefill get failed", "job_id", id, "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}
	if row.Status == prefillStatusQueued || row.Status == prefillStatusRunning {
		web.JSON(c, http.StatusConflict, gin.H{"error": "job is active, cancel it first"})
		return
	}
	n, err := s.DB.Client.PrefillJob.Delete().
		Where(
			prefilljob.ID(id),
			prefilljob.UserIDEQ(uid),
			prefilljob.StatusIn(prefillStatusCompleted, prefillStatusCancelled, prefillStatusFailed),
		).
		Exec(ctx)
	if err != nil {
		slog.Error("prefill delete failed", "job_id", id, "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete prefill job failed"})
		return
	}
	if n == 0 {
		web.JSON(c, http.StatusConflict, gin.H{"error": "job is active, cancel it first"})
		return
	}
	// Best-effort: drop the stored archive so the bucket doesn't accumulate
	// orphaned ZIP objects. The row is already gone, so a failed delete only
	// leaves a stale object for the bucket lifecycle to expire.
	if s.Archive != nil && row.ArchiveKey != nil {
		if err := s.Archive.Delete(ctx, *row.ArchiveKey); err != nil {
			slog.Warn("prefill archive delete failed", "job_id", id, "key", *row.ArchiveKey, "error", err)
		}
	}
	web.JSON(c, http.StatusOK, gin.H{"deleted": n})
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
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid days"})
		return
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	q := s.DB.Client.PrefillJob.Delete().
		Where(
			prefilljob.UserIDEQ(uid),
			prefilljob.StatusIn(prefillStatusCompleted, prefillStatusCancelled, prefillStatusFailed),
		)
	if days > 0 {
		cutoff := time.Now().AddDate(0, 0, -days)
		q.Where(prefilljob.FinishedAtLT(cutoff))
	}
	n, err := q.Exec(c.Request.Context())
	if err != nil {
		slog.Error("prefill cleanup failed", "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "cleanup prefill jobs failed"})
		return
	}
	web.JSON(c, http.StatusOK, gin.H{"days": days, "deleted": n})
}

// handlePrefillZip streams a ZIP archive for a job. When the job has a stored
// archive object it is served straight from the archive store; otherwise the
// ZIP is generated on the fly (from the image cache, fetching misses upstream,
// or straight from upstream when the cache is disabled).
func (s *Server) handlePrefillZip(c *gin.Context) {
	// ZIP download streams for as long as the client reads it; skip latency.
	metrics.MarkLongRunning(c)
	if s.DB == nil {
		web.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return
	}
	if s.Cache == nil && s.Archive == nil {
		web.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "no storage configured"})
		return
	}
	id, ok := prefillJobID(c)
	if !ok {
		return
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	row, err := s.getPrefillJob(c.Request.Context(), uid, id)
	if err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		slog.Error("prefill get failed", "job_id", id, "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}

	ctx := c.Request.Context()
	name := prefillZipFilename(row.Title)

	// A stored archive is the fast path: redirect to a signed URL (when presign
	// is enabled) or stream it back at constant memory.
	if s.Archive != nil && row.ArchiveKey != nil {
		if s.ArchivePresign {
			if loc, presignErr := s.Archive.PresignGet(ctx, *row.ArchiveKey, s.ArchivePresignExpiry, presignZipParams(name)); presignErr == nil {
				c.Redirect(http.StatusFound, loc)
				return
			} else {
				slog.Error("prefill archive presign failed; proxying instead", "job_id", id, "error", presignErr)
			}
		}
		rc, _, size, getErr := s.Archive.GetStream(ctx, *row.ArchiveKey)
		if getErr == nil {
			defer rc.Close()
			writeZipHeaders(c, name)
			c.Header("Content-Length", strconv.FormatInt(size, 10))
			c.Status(http.StatusOK)
			if _, err := io.Copy(c.Writer, rc); err != nil {
				slog.Error("prefill archive stream failed", "job_id", id, "error", err)
			}
			return
		}
		if !storage.IsNotFound(getErr) {
			slog.Error("prefill archive read failed; rebuilding on the fly", "job_id", id, "key", *row.ArchiveKey, "error", getErr)
		}
	}

	zw := zip.NewWriter(c.Writer)
	started := false
	missing := []string{}

	for i, pageURL := range row.Urls {
		if ctx.Err() != nil {
			break
		}

		pageCtx, pageCancel := context.WithTimeout(ctx, prefillPageTimeout)
		data, contentType, err := s.fetchPage(pageCtx, uid, pageURL)
		pageCancel()
		if err != nil {
			slog.Error("prefill zip fetch failed", "index", i, "error", err)
			missing = append(missing, fmt.Sprintf("%d: %s", i+1, err.Error()))
			continue
		}

		if !started {
			writeZipHeaders(c, name)
			c.Status(http.StatusOK)
			started = true
		}

		entryName := fmt.Sprintf("%03d%s", i+1, prefillImageExt(contentType, pageURL))
		w, err := zw.CreateHeader(&zip.FileHeader{Name: entryName, Method: zip.Store})
		if err != nil {
			slog.Error("prefill zip entry failed", "index", i, "error", err)
			break
		}
		if _, err := w.Write(data); err != nil {
			slog.Error("prefill zip write failed", "index", i, "error", err)
			break
		}
		c.Writer.Flush()
	}

	if !started {
		web.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "no pages could be fetched for ZIP"})
		return
	}

	if len(missing) > 0 {
		missingData := []byte(strings.Join(missing, "\n"))
		w, err := zw.CreateHeader(&zip.FileHeader{Name: "_missing.txt", Method: zip.Store})
		if err == nil {
			// Best effort: the ZIP is already streaming to the client and a
			// failed trailing entry cannot be reported back anymore.
			w.Write(missingData) //nolint:errcheck // request already committed
		}
	}

	if err := zw.Close(); err != nil {
		slog.Error("prefill zip close failed", "error", err)
	}
}

// writeZipHeaders sets the response headers shared by the archive and on-the-fly
// ZIP paths.
func writeZipHeaders(c *gin.Context, name string) {
	c.Header("Content-Type", "application/zip")
	c.Header("Content-Disposition", zipContentDisposition(name))
	c.Header("Cache-Control", "no-store")
}

// zipContentDisposition renders the attachment filename header value (ASCII
// fallback + RFC 5987 encoded form).
func zipContentDisposition(name string) string {
	return fmt.Sprintf(`attachment; filename="%s"; filename*=UTF-8''%s`,
		asciiFallback(name), rfc5987Escape(name))
}

// presignZipParams returns the extra query parameters for a signed archive URL,
// so the object store sends the same Content-Disposition the proxy path would.
func presignZipParams(name string) map[string]string {
	return map[string]string{"response-content-disposition": zipContentDisposition(name)}
}

// handlePrefillZipHead handles HEAD requests for the ZIP endpoint.
// It returns 200 if the job exists and storage is configured, without streaming any data.
func (s *Server) handlePrefillZipHead(c *gin.Context) {
	if s.DB == nil {
		web.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return
	}
	if s.Cache == nil && s.Archive == nil {
		web.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "no storage configured"})
		return
	}
	id, ok := prefillJobID(c)
	if !ok {
		return
	}
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	_, err := s.getPrefillJob(c.Request.Context(), uid, id)
	if err != nil {
		if ent.IsNotFound(err) {
			web.JSON(c, http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		slog.Error("prefill zip head get failed", "job_id", id, "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}
	c.Status(http.StatusOK)
}

// ==================== Helpers ====================

// getPrefillJob is the only handler-side job lookup: ID alone is never enough,
// because job IDs from another user must look indistinguishable from 404.
func (s *Server) getPrefillJob(ctx context.Context, userID ident.UUID, id ident.UUID) (*ent.PrefillJob, error) {
	return s.DB.Client.PrefillJob.Query().
		Where(prefilljob.ID(id), prefilljob.UserIDEQ(userID)).
		Only(ctx)
}

func prefillImageExt(contentType, pageURL string) string {
	return archive.ExtForContentType(contentType, pageURL)
}

// prefillArchiveKey names a stored ZIP archive object. The final path segment
// keeps the human-readable gallery title (sanitized) as the object name, while
// the user ID and job ID live in the path so multi-user deployments stay
// isolated and two jobs that share a title never collide.
func prefillArchiveKey(userID, jobID ident.UUID, title string) string {
	return userID.String() + "/" + jobID.String() + "/" + prefillZipFilename(title) + ".zip"
}

// fetchPage returns one page's raw image bytes and content type. With the image
// cache enabled it reads through the cache (storing on miss); otherwise it
// fetches straight from upstream. Bytes are never decoded or re-encoded.
func (s *Server) fetchPage(ctx context.Context, userID ident.UUID, pageURL string) (data []byte, contentType string, err error) {
	if s.Cache != nil {
		key := cache.CacheKey(pageURL)
		res, ferr := s.loadOrFetchImage(ctx, userID, key, pageURL)
		if ferr != nil {
			return nil, "", ferr
		}
		data, contentType = res.data, res.contentType
		res.release()
		return data, contentType, nil
	}
	client, cerr := s.upstreamClient(ctx, userID)
	if cerr != nil {
		return nil, "", cerr
	}
	return exhentai.FetchPageImage(ctx, client, pageURL)
}

// buildArchive streams a ZIP of the job's pages straight into the archive
// object store. It returns the stored object info and any error. A page that
// fails to fetch is recorded (via the run's error list) and omitted, matching
// the on-the-fly download behaviour; a ZIP consisting of nothing but failures
// is not kept.
func (m *prefillManager) buildArchive(s *Server, ctx context.Context, run *prefillRun, row *ent.PrefillJob) (storage.ObjectInfo, error) {
	key := prefillArchiveKey(row.UserID, row.ID, row.Title)
	fetch := func(fctx context.Context, pageURL string) ([]byte, string, error) {
		pageCtx, cancel := context.WithTimeout(fctx, prefillPageTimeout)
		defer cancel()
		return s.fetchPage(pageCtx, row.UserID, pageURL)
	}
	info, res, err := archive.BuildAndUpload(ctx, s.Archive, key, row.Urls, fetch, archive.Options{
		Concurrency: s.ArchiveConcurrency,
		NameEntry: func(index int, pageURL, contentType string) string {
			return fmt.Sprintf("%03d%s", index+1, prefillImageExt(contentType, pageURL))
		},
		MissingName: "_missing.txt",
		OnMissing: func(index int, pageURL string, err error) {
			m.mu.Lock()
			run.failed++
			if len(run.errs) < prefillMaxErrors {
				run.errs = append(run.errs, model.PrefillItemError{Index: index, URL: pageURL, Error: err.Error()})
			}
			m.mu.Unlock()
		},
	})
	if err != nil {
		return storage.ObjectInfo{}, err
	}
	if len(row.Urls) > 0 && res.Missing >= len(row.Urls) {
		_ = s.Archive.Delete(ctx, key)
		return storage.ObjectInfo{}, fmt.Errorf("all %d pages failed to fetch", len(row.Urls))
	}
	slog.Info("prefill archive stored", "job_id", row.ID, "key", key, "size", info.Size, "missing", res.Missing)
	return info, nil
}

// prefillZipFilename mirrors the frontend's sanitizeFilename so archives are
// named consistently with the previous client-side ZIP downloads. It also
// strips "/" so the result is safe as a single object-key path segment.
func prefillZipFilename(title string) string {
	var b strings.Builder
	for _, r := range title {
		switch {
		case r == '"' || r == '\\' || strings.ContainsRune(`<>:|?*/`, r):
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
