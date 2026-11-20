package handler

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/prefilljob"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/httpx"
	"manga-reader/internal/ident"
	"manga-reader/internal/model"
	"net/http"
	"strconv"
	"time"

	"github.com/gin-gonic/gin"
)

// requirePrefill validates the database dependency and ensures the manager.
// It returns nil (after writing the error response) when unavailable.
func (s *Server) requirePrefill(c *gin.Context) *prefillManager {
	if s.DB == nil {
		httpx.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return nil
	}
	return s.ensurePrefill()
}

func prefillJobID(c *gin.Context) (ident.UUID, bool) {
	id, err := ident.Parse(c.Param("id"))
	if err != nil {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid job id"})
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
	if err := httpx.BindJSON(c, &req); err != nil {
		if _, ok := errors.AsType[*http.MaxBytesError](err); ok {
			httpx.JSON(c, http.StatusRequestEntityTooLarge, gin.H{"error": "request body too large"})
			return
		}
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if len(req.URLs) == 0 {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "urls must not be empty"})
		return
	}
	if len(req.URLs) > prefillMaxURIs {
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": fmt.Sprintf("too many urls (max %d)", prefillMaxURIs)})
		return
	}
	for _, pageURL := range req.URLs {
		if err := exhentai.ValidatePageURL(pageURL); err != nil {
			httpx.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if s.DB == nil {
		httpx.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "database not configured"})
		return
	}
	if s.Cache == nil && s.Archive == nil {
		httpx.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "no storage configured"})
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
			httpx.JSON(c, http.StatusOK, prefillJobJSON(m, existing))
			return
		}
		if !ent.IsNotFound(err) {
			slog.Error("prefill dedupe query failed", "error", err)
			httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
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
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "create prefill job failed"})
		return
	}
	m.signal()
	httpx.JSON(c, http.StatusAccepted, prefillJobJSON(m, row))
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
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill jobs failed"})
		return
	}
	jobs := make([]gin.H, 0, len(rows))
	for _, row := range rows {
		jobs = append(jobs, prefillJobJSONWithRuns(row, runSnapshot))
	}
	httpx.JSON(c, http.StatusOK, gin.H{"jobs": jobs})
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
			httpx.JSON(c, http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		slog.Error("prefill get failed", "job_id", id, "error", err)
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}
	httpx.JSON(c, http.StatusOK, prefillJobJSON(m, row))
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
				httpx.JSON(c, http.StatusNotFound, gin.H{"error": "prefill job not found"})
				return
			}
			slog.Error("prefill cancel get failed", "job_id", id, "error", err)
			httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
			return
		}
		if row.Status != prefillStatusQueued && row.Status != prefillStatusRunning {
			if row.Status == prefillStatusCancelled {
				httpx.JSON(c, http.StatusOK, prefillJobJSON(m, row))
				return
			}
			httpx.JSON(c, http.StatusConflict, gin.H{"error": "job already finished"})
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
			httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "cancel prefill job failed"})
			return
		}
		if n == 1 {
			// CAS succeeded. If the job was running, cancel the in-flight run.
			if row.Status == prefillStatusRunning {
				m.cancelRun(id)
			}
			row.Status = prefillStatusCancelled
			row.FinishedAt = &now
			httpx.JSON(c, http.StatusOK, prefillJobJSON(m, row))
			return
		}
		// Lost a race with the worker (it claimed or finished the row): retry.
	}
	httpx.JSON(c, http.StatusConflict, gin.H{"error": "cancel conflict, retry"})
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
			httpx.JSON(c, http.StatusNotFound, gin.H{"error": "prefill job not found"})
			return
		}
		slog.Error("prefill get failed", "job_id", id, "error", err)
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "query prefill job failed"})
		return
	}
	if row.Status == prefillStatusQueued || row.Status == prefillStatusRunning {
		httpx.JSON(c, http.StatusConflict, gin.H{"error": "job is active, cancel it first"})
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
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete prefill job failed"})
		return
	}
	if n == 0 {
		httpx.JSON(c, http.StatusConflict, gin.H{"error": "job is active, cancel it first"})
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
	httpx.JSON(c, http.StatusOK, gin.H{"deleted": n})
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
		httpx.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid days"})
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
		httpx.JSON(c, http.StatusInternalServerError, gin.H{"error": "cleanup prefill jobs failed"})
		return
	}
	httpx.JSON(c, http.StatusOK, gin.H{"days": days, "deleted": n})
}

// getPrefillJob is the only handler-side job lookup: ID alone is never enough,
// because job IDs from another user must look indistinguishable from 404.
func (s *Server) getPrefillJob(ctx context.Context, userID ident.UUID, id ident.UUID) (*ent.PrefillJob, error) {
	return s.DB.Client.PrefillJob.Query().
		Where(prefilljob.ID(id), prefilljob.UserIDEQ(userID)).
		Only(ctx)
}
