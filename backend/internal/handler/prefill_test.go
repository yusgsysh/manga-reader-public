package handler

import (
	"archive/zip"
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/cache"
	"manga-reader/internal/database"
	"manga-reader/internal/ent"
)

// ==================== Test Infrastructure ====================

// prefillUpstream is a configurable upstream server. Page requests (/s/...)
// return HTML pointing at a per-page image URL (/img/<page>.webp), so the
// exact request sequence can be asserted. Every request is recorded.
type prefillUpstream struct {
	mu    sync.Mutex
	order []string
	delay time.Duration
	// failPageNames makes matching /s/<name> page requests return HTTP 500.
	failPageNames []string
}

func (u *prefillUpstream) handler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u.mu.Lock()
		u.order = append(u.order, r.URL.Path)
		u.mu.Unlock()
		if u.delay > 0 {
			time.Sleep(u.delay)
		}
		if strings.Contains(r.URL.Path, "/s/") {
			for _, name := range u.failPageNames {
				if strings.Contains(r.URL.Path, "/s/"+name) {
					w.WriteHeader(http.StatusInternalServerError)
					return
				}
			}
			img := "https://example.com/img/" + path.Base(r.URL.Path) + ".webp"
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, mockPageHTML(img, ""))
			return
		}
		w.Header().Set("Content-Type", "image/webp")
		w.Write(mockImageBytes())
	}
}

func (u *prefillUpstream) paths() []string {
	u.mu.Lock()
	defer u.mu.Unlock()
	return append([]string(nil), u.order...)
}

func (u *prefillUpstream) count() int {
	u.mu.Lock()
	defer u.mu.Unlock()
	return len(u.order)
}

func pageURLs(names ...string) []string {
	urls := make([]string, 0, len(names))
	for _, n := range names {
		urls = append(urls, "https://exhentai.org/s/"+n)
	}
	return urls
}

func newPrefillTestServer(t *testing.T, up *prefillUpstream) (*gin.Engine, *Server, *mockImageCache) {
	t.Helper()
	client := newTestDB(t)
	upSrv := httptest.NewServer(up.handler())
	t.Cleanup(upSrv.Close)
	imgCache := newMockImageCache()
	server := &Server{
		Client: newMockClient(upSrv.URL),
		DB:     &database.DB{Client: client},
		Cache:  imgCache,
	}
	return setupMockRouter(server), server, imgCache
}

func doJSON(t *testing.T, r *gin.Engine, method, target string, body any) (int, []byte) {
	t.Helper()
	var rd io.Reader
	if body != nil {
		if raw, ok := body.(json.RawMessage); ok {
			rd = bytes.NewReader(raw)
		} else {
			b, err := json.Marshal(body)
			if err != nil {
				t.Fatalf("marshal body: %v", err)
			}
			rd = bytes.NewReader(b)
		}
	}
	req := httptest.NewRequest(method, target, rd)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w.Body.Bytes()
}

type prefillItemErrorResp struct {
	Index int    `json:"index"`
	URL   string `json:"url"`
	Error string `json:"error"`
}

type prefillJobResp struct {
	ID           int    `json:"id"`
	GalleryID    *int64 `json:"gallery_id"`
	GalleryToken string `json:"gallery_token"`
	Title        string `json:"title"`
	Status       string `json:"status"`
	Total        int    `json:"total"`
	Progress     *struct {
		Done    int `json:"done"`
		Cached  int `json:"cached"`
		Fetched int `json:"fetched"`
	} `json:"progress"`
	FailedCount int                    `json:"failed_count"`
	Errors      []prefillItemErrorResp `json:"errors"`
	CreatedAt   string                 `json:"created_at"`
	UpdatedAt   string                 `json:"updated_at"`
	FinishedAt  *string                `json:"finished_at"`
}

func getPrefillJob(t *testing.T, r *gin.Engine, id int) prefillJobResp {
	t.Helper()
	code, body := doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/prefill/%d", id), nil)
	if code != http.StatusOK {
		t.Fatalf("GET /api/prefill/%d = %d, body: %s", id, code, body)
	}
	var job prefillJobResp
	if err := json.Unmarshal(body, &job); err != nil {
		t.Fatalf("unmarshal job: %v, body: %s", err, body)
	}
	return job
}

func waitPrefillStatus(t *testing.T, r *gin.Engine, id int, timeout time.Duration, want ...string) prefillJobResp {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last prefillJobResp
	for time.Now().Before(deadline) {
		code, body := doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/prefill/%d", id), nil)
		if code == http.StatusOK {
			var job prefillJobResp
			if json.Unmarshal(body, &job) == nil {
				last = job
				if slices.Contains(want, job.Status) {
					return job
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("job %d did not reach %v within %s (last status=%q progress=%+v)",
		id, want, timeout, last.Status, last.Progress)
	return last
}

func insertPrefillJob(t *testing.T, client *ent.Client, status string, urls []string, mut ...func(*ent.PrefillJobCreate)) *ent.PrefillJob {
	t.Helper()
	create := client.PrefillJob.Create().
		SetStatus(status).
		SetUrls(urls).
		SetGalleryToken("").
		SetTitle("job")
	for _, fn := range mut {
		fn(create)
	}
	row, err := create.Save(t.Context())
	if err != nil {
		t.Fatalf("insert prefill job: %v", err)
	}
	return row
}

// ==================== Validation ====================

func TestPrefillStart_Validation(t *testing.T) {
	r := setupMockRouter(&Server{Client: &http.Client{}})

	// Malformed body
	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", json.RawMessage(`{not json`))
	if code != http.StatusBadRequest {
		t.Errorf("malformed body: status = %d, want 400, body: %s", code, body)
	}

	// Empty urls
	code, body = doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{"urls": []string{}})
	if code != http.StatusBadRequest {
		t.Errorf("empty urls: status = %d, want 400, body: %s", code, body)
	}
	var errResp map[string]string
	json.Unmarshal(body, &errResp)
	if errResp["error"] != "urls must not be empty" {
		t.Errorf("error = %q, want %q", errResp["error"], "urls must not be empty")
	}

	// Invalid URL domain
	code, _ = doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"urls": []string{"https://example.com/s/abc/123-1"},
	})
	if code != http.StatusBadRequest {
		t.Errorf("bad domain: status = %d, want 400", code)
	}

	// Too many urls
	big := make([]string, prefillMaxURIs+1)
	for i := range big {
		big[i] = fmt.Sprintf("https://exhentai.org/s/p%d/1-%d", i, i)
	}
	code, _ = doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{"urls": big})
	if code != http.StatusBadRequest {
		t.Errorf("too many urls: status = %d, want 400", code)
	}

	// Valid body but no database
	code, body = doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"urls": pageURLs("v1"),
	})
	if code != http.StatusServiceUnavailable {
		t.Errorf("no db: status = %d, want 503, body: %s", code, body)
	}

	// Database configured but no cache
	client := newTestDB(t)
	r2 := setupMockRouter(&Server{Client: &http.Client{}, DB: &database.DB{Client: client}})
	code, body = doJSON(t, r2, http.MethodPost, "/api/prefill", map[string]any{
		"urls": pageURLs("v1"),
	})
	if code != http.StatusServiceUnavailable {
		t.Errorf("no cache: status = %d, want 503, body: %s", code, body)
	}
}

// ==================== Happy path ====================

func TestPrefillStart_CompletesSerially(t *testing.T) {
	up := &prefillUpstream{}
	r, _, imgCache := newPrefillTestServer(t, up)

	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"gallery_id":    4242,
		"gallery_token": "tok4242",
		"title":         "Serial Test",
		"urls":          pageURLs("s1", "s2", "s3"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body: %s", code, body)
	}
	var created prefillJobResp
	if err := json.Unmarshal(body, &created); err != nil {
		t.Fatalf("unmarshal: %v, body: %s", err, body)
	}
	if created.Status != prefillStatusQueued {
		t.Errorf("initial status = %q, want queued", created.Status)
	}
	if created.Total != 3 {
		t.Errorf("total = %d, want 3", created.Total)
	}
	if created.GalleryID == nil || *created.GalleryID != 4242 {
		t.Errorf("gallery_id = %v, want 4242", created.GalleryID)
	}

	job := waitPrefillStatus(t, r, created.ID, 10*time.Second, prefillStatusCompleted)
	if job.Progress != nil {
		t.Errorf("progress = %+v, want null for terminal job", job.Progress)
	}
	if job.FailedCount != 0 {
		t.Errorf("failed_count = %d, want 0", job.FailedCount)
	}
	if len(job.Errors) != 0 {
		t.Errorf("errors = %+v, want empty", job.Errors)
	}
	if job.FinishedAt == nil {
		t.Error("finished_at should be set")
	}
	if job.Total != 3 {
		t.Errorf("total = %d, want 3", job.Total)
	}

	// Pages must be fetched strictly one at a time, in input order:
	// page HTML then image, per page.
	want := []string{
		"/s/s1", "/img/s1.webp",
		"/s/s2", "/img/s2.webp",
		"/s/s3", "/img/s3.webp",
	}
	got := up.paths()
	if len(got) != len(want) {
		t.Fatalf("upstream order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("upstream order = %v, want %v", got, want)
		}
	}

	// Every image must have been written to the cache.
	for _, name := range []string{"s1", "s2", "s3"} {
		key := cache.CacheKey("https://exhentai.org/s/" + name)
		if _, err := imgCache.Head(t.Context(), key); err != nil {
			t.Errorf("cache missing for %s: %v", name, err)
		}
	}
}

// ==================== Deduplication ====================

func TestPrefillStart_DedupeActiveGallery(t *testing.T) {
	up := &prefillUpstream{delay: 100 * time.Millisecond}
	r, server, _ := newPrefillTestServer(t, up)

	existing := insertPrefillJob(t, server.DB.Client, prefillStatusQueued, pageURLs("d1"), func(c *ent.PrefillJobCreate) {
		c.SetGalleryID(99)
		c.SetGalleryToken("tok99")
		c.SetTitle("existing")
	})

	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"gallery_id":    99,
		"gallery_token": "tok99",
		"title":         "again",
		"urls":          pageURLs("d1", "d2"),
	})
	if code != http.StatusOK {
		t.Fatalf("dedupe status = %d, want 200, body: %s", code, body)
	}
	var got prefillJobResp
	if err := json.Unmarshal(body, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if got.ID != existing.ID {
		t.Errorf("id = %d, want existing id %d", got.ID, existing.ID)
	}
	waitPrefillStatus(t, r, existing.ID, 10*time.Second, prefillStatusCompleted)
}

func TestPrefillStart_NoDedupeWithoutOrAfterGallery(t *testing.T) {
	up := &prefillUpstream{}
	r, server, _ := newPrefillTestServer(t, up)

	// Finished job for the gallery does not block a new job.
	insertPrefillJob(t, server.DB.Client, prefillStatusCompleted, pageURLs("old1"), func(c *ent.PrefillJobCreate) {
		c.SetGalleryID(7)
		c.SetGalleryToken("tok7")
		c.SetFinishedAt(time.Now())
	})

	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"gallery_id":    7,
		"gallery_token": "tok7",
		"urls":          pageURLs("n1"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body: %s", code, body)
	}
	var first prefillJobResp
	json.Unmarshal(body, &first)

	// Jobs without gallery identity never dedupe.
	code, body = doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"urls": pageURLs("n2"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, want 202, body: %s", code, body)
	}
	var second prefillJobResp
	json.Unmarshal(body, &second)
	if first.ID == second.ID {
		t.Errorf("both jobs have id %d, want distinct ids", first.ID)
	}

	waitPrefillStatus(t, r, first.ID, 10*time.Second, prefillStatusCompleted)
	waitPrefillStatus(t, r, second.ID, 10*time.Second, prefillStatusCompleted)
}

// ==================== Queue serialization ====================

func TestPrefillJobs_RunSeriallyInOrder(t *testing.T) {
	up := &prefillUpstream{}
	r, _, _ := newPrefillTestServer(t, up)

	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"title": "job-a",
		"urls":  pageURLs("ja1", "ja2"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("job a: status = %d, body: %s", code, body)
	}
	var jobA prefillJobResp
	json.Unmarshal(body, &jobA)

	code, body = doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"title": "job-b",
		"urls":  pageURLs("jb1", "jb2"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("job b: status = %d, body: %s", code, body)
	}
	var jobB prefillJobResp
	json.Unmarshal(body, &jobB)

	waitPrefillStatus(t, r, jobA.ID, 10*time.Second, prefillStatusCompleted)
	waitPrefillStatus(t, r, jobB.ID, 10*time.Second, prefillStatusCompleted)

	want := []string{
		"/s/ja1", "/img/ja1.webp",
		"/s/ja2", "/img/ja2.webp",
		"/s/jb1", "/img/jb1.webp",
		"/s/jb2", "/img/jb2.webp",
	}
	got := up.paths()
	if len(got) != len(want) {
		t.Fatalf("upstream order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("upstream order = %v, want %v", got, want)
		}
	}
}

// ==================== Restart recovery ====================

func TestPrefillRecoverPendingOnFirstRequest(t *testing.T) {
	up := &prefillUpstream{}
	r, server, _ := newPrefillTestServer(t, up)

	// Simulate a job that was running when the process stopped.
	interrupted := insertPrefillJob(t, server.DB.Client, prefillStatusRunning, pageURLs("rec1"), func(c *ent.PrefillJobCreate) {
		c.SetTitle("interrupted")
	})

	// Any prefill request bootstraps the manager, which re-queues the row.
	code, body := doJSON(t, r, http.MethodGet, "/api/prefill", nil)
	if code != http.StatusOK {
		t.Fatalf("list: status = %d, body: %s", code, body)
	}

	job := waitPrefillStatus(t, r, interrupted.ID, 10*time.Second, prefillStatusCompleted)
	if job.FinishedAt == nil {
		t.Error("finished_at should be set after recovery run")
	}
	if len(up.paths()) == 0 {
		t.Error("recovered job should have fetched its pages")
	}
}

// ==================== Cancel ====================

func TestPrefillCancel_Running(t *testing.T) {
	up := &prefillUpstream{delay: 150 * time.Millisecond}
	r, _, _ := newPrefillTestServer(t, up)

	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"title": "cancel-me",
		"urls":  pageURLs("c1", "c2", "c3"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, body: %s", code, body)
	}
	var created prefillJobResp
	json.Unmarshal(body, &created)

	// Wait until the job is actively running with at least one page done so
	// the cancel provably interrupts mid-run.
	deadline := time.Now().Add(10 * time.Second)
	cancelled := false
	for time.Now().Before(deadline) {
		job := getPrefillJob(t, r, created.ID)
		if job.Status == prefillStatusRunning && job.Progress != nil && job.Progress.Done >= 1 {
			if job.Progress.Done >= job.Total {
				t.Fatal("job finished before it could be cancelled")
			}
			code, body := doJSON(t, r, http.MethodPost,
				fmt.Sprintf("/api/prefill/%d/cancel", created.ID), nil)
			if code != http.StatusOK {
				t.Fatalf("cancel: status = %d, body: %s", code, body)
			}
			var resp prefillJobResp
			json.Unmarshal(body, &resp)
			if resp.Status != prefillStatusCancelled {
				t.Errorf("cancel response status = %q, want cancelled", resp.Status)
			}
			if resp.FinishedAt == nil {
				t.Error("cancel response finished_at should be set")
			}
			if resp.Progress != nil {
				t.Errorf("cancel response progress = %+v, want null", resp.Progress)
			}
			cancelled = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !cancelled {
		t.Fatal("job never became running with progress")
	}

	// The terminal state must stay cancelled and the worker must stop.
	job := getPrefillJob(t, r, created.ID)
	if job.Status != prefillStatusCancelled {
		t.Errorf("status = %q, want cancelled", job.Status)
	}
	time.Sleep(500 * time.Millisecond)
	settled := up.count()
	time.Sleep(300 * time.Millisecond)
	if later := up.count(); later != settled {
		t.Errorf("worker kept fetching after cancel: %d -> %d requests", settled, later)
	}
	if job := getPrefillJob(t, r, created.ID); job.Status != prefillStatusCancelled {
		t.Errorf("status = %q, want cancelled", job.Status)
	}
}

func TestPrefillCancel_IdempotentAndTerminal(t *testing.T) {
	up := &prefillUpstream{}
	r, server, _ := newPrefillTestServer(t, up)

	completed := insertPrefillJob(t, server.DB.Client, prefillStatusCompleted, pageURLs("x1"), func(c *ent.PrefillJobCreate) {
		c.SetFinishedAt(time.Now())
	})
	cancelled := insertPrefillJob(t, server.DB.Client, prefillStatusCancelled, pageURLs("x2"), func(c *ent.PrefillJobCreate) {
		c.SetFinishedAt(time.Now())
	})

	// Cancelling a finished job conflicts.
	code, body := doJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/prefill/%d/cancel", completed.ID), nil)
	if code != http.StatusConflict {
		t.Errorf("cancel completed: status = %d, want 409, body: %s", code, body)
	}

	// Cancelling an already cancelled job is idempotent.
	code, body = doJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/prefill/%d/cancel", cancelled.ID), nil)
	if code != http.StatusOK {
		t.Errorf("cancel cancelled: status = %d, want 200, body: %s", code, body)
	}
}

func TestPrefillCancel_StaleRunningRow(t *testing.T) {
	up := &prefillUpstream{}
	r, server, _ := newPrefillTestServer(t, up)

	// Bootstrap the manager first so recovery does not re-queue this row;
	// it simulates a running row that no worker currently owns.
	if code, _ := doJSON(t, r, http.MethodGet, "/api/prefill", nil); code != http.StatusOK {
		t.Fatal("list failed to bootstrap manager")
	}
	stale := insertPrefillJob(t, server.DB.Client, prefillStatusRunning, pageURLs("st1"))

	code, body := doJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/prefill/%d/cancel", stale.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("cancel stale: status = %d, body: %s", code, body)
	}
	var resp prefillJobResp
	json.Unmarshal(body, &resp)
	if resp.Status != prefillStatusCancelled {
		t.Errorf("status = %q, want cancelled", resp.Status)
	}
}

func TestPrefillCancel_InvalidID(t *testing.T) {
	up := &prefillUpstream{}
	r, _, _ := newPrefillTestServer(t, up)

	code, _ := doJSON(t, r, http.MethodPost, "/api/prefill/abc/cancel", nil)
	if code != http.StatusBadRequest {
		t.Errorf("invalid id: status = %d, want 400", code)
	}
	code, _ = doJSON(t, r, http.MethodPost, "/api/prefill/99999/cancel", nil)
	if code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404", code)
	}
}

// ==================== Get / list ====================

func TestPrefillGetAndList(t *testing.T) {
	up := &prefillUpstream{}
	r, server, _ := newPrefillTestServer(t, up)

	older := insertPrefillJob(t, server.DB.Client, prefillStatusCompleted, pageURLs("l1"), func(c *ent.PrefillJobCreate) {
		c.SetTitle("older")
		c.SetCreatedAt(time.Now().Add(-time.Hour))
		c.SetFinishedAt(time.Now().Add(-time.Hour))
	})
	newer := insertPrefillJob(t, server.DB.Client, prefillStatusCancelled, pageURLs("l2"), func(c *ent.PrefillJobCreate) {
		c.SetTitle("newer")
		c.SetCreatedAt(time.Now().Add(-time.Minute))
		c.SetFinishedAt(time.Now().Add(-time.Minute))
	})

	code, body := doJSON(t, r, http.MethodGet, "/api/prefill", nil)
	if code != http.StatusOK {
		t.Fatalf("list: status = %d, body: %s", code, body)
	}
	if strings.Contains(string(body), `"urls"`) {
		t.Error("list response must not leak the urls payload")
	}
	var list struct {
		Jobs []prefillJobResp `json:"jobs"`
	}
	if err := json.Unmarshal(body, &list); err != nil {
		t.Fatalf("unmarshal: %v, body: %s", err, body)
	}
	if len(list.Jobs) != 2 {
		t.Fatalf("jobs len = %d, want 2", len(list.Jobs))
	}
	if list.Jobs[0].ID != newer.ID || list.Jobs[1].ID != older.ID {
		t.Errorf("order = [%d %d], want newest first [%d %d]",
			list.Jobs[0].ID, list.Jobs[1].ID, newer.ID, older.ID)
	}
	for _, j := range list.Jobs {
		if j.Progress != nil {
			t.Errorf("job %d progress = %+v, want null for terminal job", j.ID, j.Progress)
		}
		if j.CreatedAt == "" || j.UpdatedAt == "" {
			t.Errorf("job %d missing timestamps", j.ID)
		}
	}

	// Single get
	got := getPrefillJob(t, r, newer.ID)
	if got.Title != "newer" || got.Status != prefillStatusCancelled {
		t.Errorf("get = %+v, want title newer / status cancelled", got)
	}

	// Invalid and unknown ids
	code, _ = doJSON(t, r, http.MethodGet, "/api/prefill/xyz", nil)
	if code != http.StatusBadRequest {
		t.Errorf("invalid id: status = %d, want 400", code)
	}
	code, _ = doJSON(t, r, http.MethodGet, "/api/prefill/99999", nil)
	if code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404", code)
	}
}

// ==================== Delete / cleanup ====================

func TestPrefillDelete(t *testing.T) {
	up := &prefillUpstream{}
	r, server, _ := newPrefillTestServer(t, up)

	terminal := insertPrefillJob(t, server.DB.Client, prefillStatusCompleted, pageURLs("del1"), func(c *ent.PrefillJobCreate) {
		c.SetFinishedAt(time.Now())
	})

	code, body := doJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/prefill/%d", terminal.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("delete: status = %d, body: %s", code, body)
	}
	code, _ = doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/prefill/%d", terminal.ID), nil)
	if code != http.StatusNotFound {
		t.Errorf("after delete: status = %d, want 404", code)
	}

	// Active rows cannot be deleted until cancelled.
	if code, _ := doJSON(t, r, http.MethodGet, "/api/prefill", nil); code != http.StatusOK {
		t.Fatal("list failed")
	}
	active := insertPrefillJob(t, server.DB.Client, prefillStatusRunning, pageURLs("del2"))
	code, body = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/prefill/%d", active.ID), nil)
	if code != http.StatusConflict {
		t.Errorf("delete active: status = %d, want 409, body: %s", code, body)
	}

	code, body = doJSON(t, r, http.MethodPost,
		fmt.Sprintf("/api/prefill/%d/cancel", active.ID), nil)
	if code != http.StatusOK {
		t.Fatalf("cancel active: status = %d, body: %s", code, body)
	}
	code, body = doJSON(t, r, http.MethodDelete, fmt.Sprintf("/api/prefill/%d", active.ID), nil)
	if code != http.StatusOK {
		t.Errorf("delete after cancel: status = %d, body: %s", code, body)
	}

	code, _ = doJSON(t, r, http.MethodDelete, "/api/prefill/abc", nil)
	if code != http.StatusBadRequest {
		t.Errorf("invalid id: status = %d, want 400", code)
	}
}

func TestPrefillCleanup(t *testing.T) {
	up := &prefillUpstream{}
	r, server, _ := newPrefillTestServer(t, up)

	oldJob := insertPrefillJob(t, server.DB.Client, prefillStatusCompleted, pageURLs("cl1"), func(c *ent.PrefillJobCreate) {
		c.SetFinishedAt(time.Now().AddDate(0, 0, -40))
	})
	recentJob := insertPrefillJob(t, server.DB.Client, prefillStatusCompleted, pageURLs("cl2"), func(c *ent.PrefillJobCreate) {
		c.SetFinishedAt(time.Now().AddDate(0, 0, -1))
	})
	activeJob := insertPrefillJob(t, server.DB.Client, prefillStatusRunning, pageURLs("cl3"))

	// days=30 removes only the old terminal job.
	code, body := doJSON(t, r, http.MethodPost, "/api/prefill/cleanup?days=30", nil)
	if code != http.StatusOK {
		t.Fatalf("cleanup: status = %d, body: %s", code, body)
	}
	var resp struct {
		Days    int `json:"days"`
		Deleted int `json:"deleted"`
	}
	json.Unmarshal(body, &resp)
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", resp.Deleted)
	}
	if code, _ := doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/prefill/%d", oldJob.ID), nil); code != http.StatusNotFound {
		t.Errorf("old job should be deleted")
	}
	if code, _ := doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/prefill/%d", recentJob.ID), nil); code != http.StatusOK {
		t.Errorf("recent job should survive days=30")
	}
	if code, _ := doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/prefill/%d", activeJob.ID), nil); code != http.StatusOK {
		t.Errorf("active job must never be cleaned up")
	}

	// days=0 removes all remaining terminal jobs but not the active one.
	code, body = doJSON(t, r, http.MethodPost, "/api/prefill/cleanup?days=0", nil)
	if code != http.StatusOK {
		t.Fatalf("cleanup all: status = %d, body: %s", code, body)
	}
	json.Unmarshal(body, &resp)
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", resp.Deleted)
	}
	if code, _ := doJSON(t, r, http.MethodGet, fmt.Sprintf("/api/prefill/%d", activeJob.ID), nil); code != http.StatusOK {
		t.Errorf("active job must survive days=0")
	}

	// Invalid days
	code, _ = doJSON(t, r, http.MethodPost, "/api/prefill/cleanup?days=-1", nil)
	if code != http.StatusBadRequest {
		t.Errorf("days=-1: status = %d, want 400", code)
	}
}

// ==================== Failures ====================

func TestPrefillPartialFailure(t *testing.T) {
	up := &prefillUpstream{failPageNames: []string{"bad"}}
	r, _, _ := newPrefillTestServer(t, up)

	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"title": "partial",
		"urls":  pageURLs("ok1", "bad1"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, body: %s", code, body)
	}
	var created prefillJobResp
	json.Unmarshal(body, &created)

	// One page failed after retries: the job still completes, with the error
	// recorded. Retries add ~1s of backoff.
	job := waitPrefillStatus(t, r, created.ID, 30*time.Second, prefillStatusCompleted)
	if job.FailedCount != 1 {
		t.Errorf("failed_count = %d, want 1", job.FailedCount)
	}
	if len(job.Errors) != 1 {
		t.Fatalf("errors len = %d, want 1", len(job.Errors))
	}
	if job.Errors[0].Index != 1 {
		t.Errorf("errors[0].index = %d, want 1", job.Errors[0].Index)
	}
	if !strings.Contains(job.Errors[0].URL, "/s/bad1") {
		t.Errorf("errors[0].url = %q, want the failing page", job.Errors[0].URL)
	}
	if job.Errors[0].Error == "" {
		t.Error("errors[0].error should not be empty")
	}
}

func TestPrefillAllPagesFailed(t *testing.T) {
	up := &prefillUpstream{failPageNames: []string{"allbad"}}
	r, _, _ := newPrefillTestServer(t, up)

	code, body := doJSON(t, r, http.MethodPost, "/api/prefill", map[string]any{
		"title": "all-fail",
		"urls":  pageURLs("allbad1"),
	})
	if code != http.StatusAccepted {
		t.Fatalf("status = %d, body: %s", code, body)
	}
	var created prefillJobResp
	json.Unmarshal(body, &created)

	job := waitPrefillStatus(t, r, created.ID, 30*time.Second, prefillStatusFailed)
	if job.FailedCount != 1 {
		t.Errorf("failed_count = %d, want 1", job.FailedCount)
	}
	if len(job.Errors) != 1 {
		t.Errorf("errors len = %d, want 1", len(job.Errors))
	}
}

// ==================== ZIP ====================

func TestPrefillZip(t *testing.T) {
	up := &prefillUpstream{}
	r, server, imgCache := newPrefillTestServer(t, up)

	urls := pageURLs("z1", "z2", "z3")
	job := insertPrefillJob(t, server.DB.Client, prefillStatusCompleted, urls, func(c *ent.PrefillJobCreate) {
		c.SetTitle("测试 Gallery: v1?")
		c.SetFinishedAt(time.Now())
	})

	// First two pages come from the cache, the third is fetched on the fly.
	jpegData := []byte("jpeg-bytes")
	pngData := []byte("png-bytes")
	if err := imgCache.Put(t.Context(), cache.CacheKey(urls[0]), jpegData, "image/jpeg", ""); err != nil {
		t.Fatalf("put cache 1: %v", err)
	}
	if err := imgCache.Put(t.Context(), cache.CacheKey(urls[1]), pngData, "image/png", ""); err != nil {
		t.Fatalf("put cache 2: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, fmt.Sprintf("/api/prefill/%d/zip", job.ID), nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("zip: status = %d, body: %s", w.Code, w.Body.String())
	}
	if ct := w.Header().Get("Content-Type"); ct != "application/zip" {
		t.Errorf("Content-Type = %q, want application/zip", ct)
	}
	if cc := w.Header().Get("Cache-Control"); cc != "no-store" {
		t.Errorf("Cache-Control = %q, want no-store", cc)
	}
	cd := w.Header().Get("Content-Disposition")
	if !strings.HasPrefix(cd, "attachment;") {
		t.Errorf("Content-Disposition = %q, want attachment", cd)
	}
	if !strings.Contains(cd, `filename="`) || !strings.Contains(cd, `filename*=UTF-8''`) {
		t.Errorf("Content-Disposition = %q, want ascii + rfc5987 filenames", cd)
	}
	if strings.Contains(cd, "测试") {
		t.Errorf("ascii filename fallback must be ASCII: %q", cd)
	}
	if !strings.Contains(cd, "filename*=UTF-8''%E6%B5%8B%E8%AF%95%20Gallery%20v1") {
		t.Errorf("rfc5987 filename missing/wrong: %q", cd)
	}

	zr, err := zip.NewReader(bytes.NewReader(w.Body.Bytes()), int64(w.Body.Len()))
	if err != nil {
		t.Fatalf("open zip: %v", err)
	}
	wantNames := []string{"001.jpg", "002.png", "003.webp"}
	if len(zr.File) != len(wantNames) {
		t.Fatalf("entries = %d, want %d", len(zr.File), len(wantNames))
	}
	for i, want := range wantNames {
		if zr.File[i].Name != want {
			t.Errorf("entries[%d] = %q, want %q", i, zr.File[i].Name, want)
		}
	}

	readEntry := func(i int) []byte {
		t.Helper()
		rc, err := zr.File[i].Open()
		if err != nil {
			t.Fatalf("open entry %d: %v", i, err)
		}
		defer rc.Close()
		b, err := io.ReadAll(rc)
		if err != nil {
			t.Fatalf("read entry %d: %v", i, err)
		}
		return b
	}
	if got := readEntry(0); !bytes.Equal(got, jpegData) {
		t.Errorf("entry 1 contents = %q, want %q", got, jpegData)
	}
	if got := readEntry(1); !bytes.Equal(got, pngData) {
		t.Errorf("entry 2 contents = %q, want %q", got, pngData)
	}
	if got := readEntry(2); !bytes.Equal(got, mockImageBytes()) {
		t.Errorf("entry 3 contents = %v, want upstream image", got)
	}

	// Cached pages must not hit the upstream; only page 3 is fetched.
	want := []string{"/s/z3", "/img/z3.webp"}
	got := up.paths()
	if len(got) != len(want) {
		t.Fatalf("upstream order = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("upstream order = %v, want %v", got, want)
		}
	}
}

func TestPrefillZip_Errors(t *testing.T) {
	// No database
	r := setupMockRouter(&Server{Client: &http.Client{}})
	code, _ := doJSON(t, r, http.MethodGet, "/api/prefill/1/zip", nil)
	if code != http.StatusServiceUnavailable {
		t.Errorf("no db: status = %d, want 503", code)
	}

	// Invalid id
	up := &prefillUpstream{}
	r2, _, _ := newPrefillTestServer(t, up)
	code, _ = doJSON(t, r2, http.MethodGet, "/api/prefill/abc/zip", nil)
	if code != http.StatusBadRequest {
		t.Errorf("invalid id: status = %d, want 400", code)
	}
	code, _ = doJSON(t, r2, http.MethodGet, "/api/prefill/99999/zip", nil)
	if code != http.StatusNotFound {
		t.Errorf("unknown id: status = %d, want 404", code)
	}

	// Missing cache
	client := newTestDB(t)
	r3 := setupMockRouter(&Server{Client: &http.Client{}, DB: &database.DB{Client: client}})
	code, _ = doJSON(t, r3, http.MethodGet, "/api/prefill/1/zip", nil)
	if code != http.StatusServiceUnavailable {
		t.Errorf("no cache: status = %d, want 503", code)
	}
}

// ==================== Dead code guards ====================

func TestPrefillHelpers(t *testing.T) {
	if got := prefillZipFilename(""); got != "download" {
		t.Errorf("empty title = %q, want download", got)
	}
	if got := prefillZipFilename(`a<b>c:d|e?f*g"h\i`); strings.ContainsAny(got, `<>:|?*"\`) {
		t.Errorf("sanitized = %q still contains forbidden chars", got)
	}
	long := strings.Repeat("x", 200)
	if got := prefillZipFilename(long); len([]rune(got)) > 100 {
		t.Errorf("long title len = %d, want <= 100", len([]rune(got)))
	}
	if got := asciiFallback("测试"); got == "测试" || !isASCIIStr(got) {
		t.Errorf("asciiFallback(测试) = %q, want ASCII underscores", got)
	}
	if got := rfc5987Escape("a b"); got != "a%20b" {
		t.Errorf("rfc5987Escape = %q, want a%%20b", got)
	}
	if got := rfc5987Escape("Az09!#$&+-.^_`|~"); got != "Az09!#$&+-.^_`|~" {
		t.Errorf("attr-char escape mangled safe chars: %q", got)
	}
}

func isASCIIStr(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] > 0x7f {
			return false
		}
	}
	return true
}
