package handler

import (
	"context"
	"fmt"
	"log/slog"
	"manga-reader/internal/archive"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/predicate"
	"manga-reader/internal/ent/prefilljob"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/ident"
	"manga-reader/internal/imagecache"
	"manga-reader/internal/model"
	"manga-reader/internal/storage"
	"sync"
	"time"
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
		key := imagecache.CacheKey(pageURL)

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

// fetchPage returns one page's raw image bytes and content type. With the image
// cache enabled it reads through the cache (storing on miss); otherwise it
// fetches straight from upstream. Bytes are never decoded or re-encoded.
func (s *Server) fetchPage(ctx context.Context, userID ident.UUID, pageURL string) (data []byte, contentType string, err error) {
	if s.Cache != nil {
		key := imagecache.CacheKey(pageURL)
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
	// In archive-only mode (no image cache) the archive build is the only place
	// pages are fetched, so it must also drive the job's progress counters.
	// When the image cache is enabled, phase 1 (processPages) already accounts
	// for every page, so counting again here would over-report the progress.
	archiveOnly := s.Cache == nil
	fetch := func(fctx context.Context, pageURL string) ([]byte, string, error) {
		pageCtx, cancel := context.WithTimeout(fctx, prefillPageTimeout)
		defer cancel()
		data, ct, err := s.fetchPage(pageCtx, row.UserID, pageURL)
		if archiveOnly && err == nil {
			m.mu.Lock()
			run.done++
			run.fetched++
			m.mu.Unlock()
		}
		return data, ct, err
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
			if archiveOnly {
				run.done++
			}
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
