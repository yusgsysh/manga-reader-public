package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	json "encoding/json/v2"

	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/bookshelf"
	"manga-reader/internal/ent/readingprogress"
	"manga-reader/internal/model"
)

func TestHandleGetProgress_NotExists(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/progress/:id/:token", server.handleGetProgress)

	req := httptest.NewRequest("GET", "/api/progress/123456/abcdef", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.GalleryID != 123456 {
		t.Errorf("GalleryID = %d, want 123456", resp.GalleryID)
	}
	if resp.CurrentPage != 0 {
		t.Errorf("CurrentPage = %d, want 0", resp.CurrentPage)
	}
}

func TestHandleGetProgress_Exists(t *testing.T) {
	client := newTestDB(t)
	client.ReadingProgress.Create().
		SetGalleryID(123456).
		SetToken("abcdef").
		SetCurrentPage(10).
		SetProgress(0.416).
		SetCompleted(false).
		SetStartedAt(time.Now().UTC()).
		SetUpdatedAt(time.Now().UTC()).
		Save(t.Context())

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/progress/:id/:token", server.handleGetProgress)

	httpReq := httptest.NewRequest("GET", "/api/progress/123456/abcdef", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httpReq)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.CurrentPage != 10 {
		t.Errorf("CurrentPage = %d, want 10", resp.CurrentPage)
	}
	if resp.Progress != 0.416 {
		t.Errorf("Progress = %f, want 0.416", resp.Progress)
	}
}

func TestHandleUpdateProgress_First(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: 5,
		Progress:    0.208,
		Completed:   false,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp.CurrentPage != 5 {
		t.Errorf("CurrentPage = %d, want 5", resp.CurrentPage)
	}
	if resp.StartedAt == nil {
		t.Error("StartedAt should be set on first insert")
	}
}

func TestHandleUpdateProgress_Update(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{CurrentPage: 5, Progress: 0.2})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var first model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &first)

	time.Sleep(10 * time.Millisecond)

	body, _ = json.Marshal(model.UpdateReadingProgressRequest{CurrentPage: 10, Progress: 0.4})
	req = httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var second model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &second)

	if second.CurrentPage != 10 {
		t.Errorf("CurrentPage = %d, want 10", second.CurrentPage)
	}
	if first.StartedAt != nil && second.StartedAt != nil && !first.StartedAt.Equal(*second.StartedAt) {
		t.Error("started_at should not change")
	}
}

func TestHandleUpdateProgress_DoesNotWriteGalleryCache(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	save := func(page int, progress float64) {
		t.Helper()
		body, _ := json.Marshal(model.UpdateReadingProgressRequest{
			CurrentPage: page,
			Progress:    progress,
		})
		req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
		}
	}

	save(5, 0.2)
	save(12, 0.5)

	stored, err := client.ReadingProgress.Query().
		Where(
			readingprogress.GalleryID(123456),
			readingprogress.Token("abcdef"),
		).
		Only(t.Context())
	if err != nil {
		t.Fatalf("load progress: %v", err)
	}
	if stored.CurrentPage != 12 {
		t.Errorf("CurrentPage = %d, want 12", stored.CurrentPage)
	}

	// Progress saves must not populate gallery_cache (that is owned by the
	// online endpoints and the bookshelf prefetch).
	count, err := client.GalleryCache.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count gallery cache: %v", err)
	}
	if count != 0 {
		t.Errorf("gallery_cache rows = %d, want 0", count)
	}
}

func TestHandleUpdateProgress_Completed(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: 24,
		Progress:    0.5,
		Completed:   true,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d; body: %s", w.Code, http.StatusOK, w.Body.String())
	}

	var resp model.ReadingProgress
	json.Unmarshal(w.Body.Bytes(), &resp)
	if !resp.Completed {
		t.Error("Completed should be true")
	}
	if resp.Progress != 1 {
		t.Errorf("Progress = %f, want 1 (auto-set when completed=true)", resp.Progress)
	}
}

func TestHandleUpdateProgress_InvalidProgress(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: 5,
		Progress:    1.5,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateProgress_NegativeProgress(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: 5,
		Progress:    -0.1,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestHandleUpdateProgress_NegativePage(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: -1,
		Progress:    0.5,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

type cleanupResponse struct {
	Days    int   `json:"days"`
	Deleted int64 `json:"deleted"`
}

func performCleanup(t *testing.T, client *ent.Client, query string) (int, cleanupResponse) {
	t.Helper()
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	server.RegisterRoutes(r)

	req := httptest.NewRequest("POST", "/api/reading-progress/cleanup"+query, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	var resp cleanupResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal cleanup response: %v", err)
		}
	}
	return w.Code, resp
}

// ==================== days parameter tests ====================

func TestHandleReadingProgressCleanup_DefaultDays(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "old", time.Now().UTC().AddDate(0, 0, -31))

	code, resp := performCleanup(t, client, "")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 30 {
		t.Errorf("days = %d, want 30 (default)", resp.Days)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", resp.Deleted)
	}
	if n := countReadingProgress(t, client); n != 0 {
		t.Errorf("remaining records = %d, want 0", n)
	}
}

func TestHandleReadingProgressCleanup_Days7(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "six", time.Now().UTC().AddDate(0, 0, -6))
	insertProgressWithTimestamp(t, client, 222222, "eight", time.Now().UTC().AddDate(0, 0, -8))

	code, resp := performCleanup(t, client, "?days=7")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 7 {
		t.Errorf("days = %d, want 7", resp.Days)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (only the 8-day-old record)", resp.Deleted)
	}
	if n := countReadingProgress(t, client); n != 1 {
		t.Errorf("remaining records = %d, want 1", n)
	}
}

func TestHandleReadingProgressCleanup_Days30(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "twenty-nine", time.Now().UTC().AddDate(0, 0, -29))
	insertProgressWithTimestamp(t, client, 222222, "thirty-one", time.Now().UTC().AddDate(0, 0, -31))

	code, resp := performCleanup(t, client, "?days=30")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 30 {
		t.Errorf("days = %d, want 30", resp.Days)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (only the 31-day-old record)", resp.Deleted)
	}
	if n := countReadingProgress(t, client); n != 1 {
		t.Errorf("remaining records = %d, want 1 (29-day-old kept)", n)
	}
}

func TestHandleReadingProgressCleanup_Days0(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "fresh", time.Now().UTC())
	insertProgressWithTimestamp(t, client, 222222, "old", time.Now().UTC().AddDate(0, 0, -60))

	code, resp := performCleanup(t, client, "?days=0")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 0 {
		t.Errorf("days = %d, want 0", resp.Days)
	}
	if resp.Deleted != 2 {
		t.Errorf("deleted = %d, want 2 (all records)", resp.Deleted)
	}
	if n := countReadingProgress(t, client); n != 0 {
		t.Errorf("remaining records = %d, want 0", n)
	}
}

func TestHandleReadingProgressCleanup_NoRecords(t *testing.T) {
	client := newTestDB(t)

	code, resp := performCleanup(t, client, "")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Deleted != 0 {
		t.Errorf("deleted = %d, want 0", resp.Deleted)
	}
}

// ==================== invalid parameter tests ====================

func TestHandleReadingProgressCleanup_NegativeDays(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "keep", time.Now().UTC().AddDate(0, 0, -31))

	code, _ := performCleanup(t, client, "?days=-1")

	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", code, http.StatusBadRequest)
	}
	if n := countReadingProgress(t, client); n != 1 {
		t.Errorf("records after failed cleanup = %d, want 1 (nothing deleted)", n)
	}
}

func TestHandleReadingProgressCleanup_NonNumericDays(t *testing.T) {
	client := newTestDB(t)
	insertProgressWithTimestamp(t, client, 111111, "keep", time.Now().UTC().AddDate(0, 0, -31))

	code, _ := performCleanup(t, client, "?days=abc")

	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", code, http.StatusBadRequest)
	}
	if n := countReadingProgress(t, client); n != 1 {
		t.Errorf("records after failed cleanup = %d, want 1 (nothing deleted)", n)
	}
}

// ==================== regression tests ====================

func TestHandleReadingProgressCleanup_PreservesBookshelf(t *testing.T) {
	client := newTestDB(t)
	b := newTestBookshelf()
	_, err := client.Bookshelf.Create().
		SetGalleryID(b.GalleryID).
		SetToken(b.Token).
		Save(t.Context())
	if err != nil {
		t.Fatalf("add bookshelf: %v", err)
	}
	insertProgressWithTimestamp(t, client, b.GalleryID, b.Token, time.Now().UTC().AddDate(0, 0, -31))

	code, resp := performCleanup(t, client, "?days=30")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", resp.Deleted)
	}

	// Reading progress should be deleted
	exists, err := client.ReadingProgress.Query().
		Where(
			readingprogress.GalleryID(b.GalleryID),
			readingprogress.Token(b.Token),
		).
		Exist(t.Context())
	if err != nil {
		t.Fatalf("check progress: %v", err)
	}
	if exists {
		t.Error("reading progress should be deleted after cleanup")
	}

	// Bookshelf must be preserved
	exists, err = client.Bookshelf.Query().
		Where(
			bookshelf.GalleryID(b.GalleryID),
			bookshelf.Token(b.Token),
		).
		Exist(t.Context())
	if err != nil {
		t.Fatalf("check bookshelf: %v", err)
	}
	if !exists {
		t.Error("bookshelf should be preserved after cleanup")
	}
}
