package main

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"manga-reader/internal/database"
	"manga-reader/internal/model"
)

// insertProgressWithTimestamp 直接插入带自定义 updated_at 的阅读记录，用于测试清理边界。
func insertProgressWithTimestamp(t *testing.T, conn *sql.DB, galleryID int64, token string, updatedAt time.Time) {
	t.Helper()
	_, err := conn.ExecContext(t.Context(),
		`INSERT INTO reading_progress (gallery_id, token, current_page, progress, completed, started_at, updated_at)
		 VALUES (?, ?, 1, 0.1, 0, ?, ?)`,
		galleryID, token, updatedAt, updatedAt,
	)
	if err != nil {
		t.Fatalf("insert progress with timestamp: %v", err)
	}
}

func countReadingProgress(t *testing.T, conn *sql.DB) int {
	t.Helper()
	var n int
	if err := conn.QueryRowContext(t.Context(), `SELECT COUNT(*) FROM reading_progress`).Scan(&n); err != nil {
		t.Fatalf("count reading progress: %v", err)
	}
	return n
}

type cleanupResponse struct {
	Days    int   `json:"days"`
	Deleted int64 `json:"deleted"`
}

func performCleanup(t *testing.T, conn *sql.DB, query string) (int, cleanupResponse) {
	t.Helper()
	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}
	r.POST("/api/reading-progress/cleanup", app.handleReadingProgressCleanup)

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

// ==================== days 参数测试 ====================

func TestHandleReadingProgressCleanup_DefaultDays(t *testing.T) {
	conn := newTestDB(t)
	insertProgressWithTimestamp(t, conn, 111111, "old", time.Now().UTC().AddDate(0, 0, -31))

	code, resp := performCleanup(t, conn, "")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 30 {
		t.Errorf("days = %d, want 30 (default)", resp.Days)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", resp.Deleted)
	}
	if n := countReadingProgress(t, conn); n != 0 {
		t.Errorf("remaining records = %d, want 0", n)
	}
}

func TestHandleReadingProgressCleanup_Days7(t *testing.T) {
	conn := newTestDB(t)
	insertProgressWithTimestamp(t, conn, 111111, "six", time.Now().UTC().AddDate(0, 0, -6))
	insertProgressWithTimestamp(t, conn, 222222, "eight", time.Now().UTC().AddDate(0, 0, -8))

	code, resp := performCleanup(t, conn, "?days=7")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 7 {
		t.Errorf("days = %d, want 7", resp.Days)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (only the 8-day-old record)", resp.Deleted)
	}
	if n := countReadingProgress(t, conn); n != 1 {
		t.Errorf("remaining records = %d, want 1", n)
	}
}

func TestHandleReadingProgressCleanup_Days30(t *testing.T) {
	conn := newTestDB(t)
	insertProgressWithTimestamp(t, conn, 111111, "twenty-nine", time.Now().UTC().AddDate(0, 0, -29))
	insertProgressWithTimestamp(t, conn, 222222, "thirty-one", time.Now().UTC().AddDate(0, 0, -31))

	code, resp := performCleanup(t, conn, "?days=30")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 30 {
		t.Errorf("days = %d, want 30", resp.Days)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (only the 31-day-old record)", resp.Deleted)
	}
	if n := countReadingProgress(t, conn); n != 1 {
		t.Errorf("remaining records = %d, want 1 (29-day-old kept)", n)
	}
}

func TestHandleReadingProgressCleanup_Days0(t *testing.T) {
	conn := newTestDB(t)
	insertProgressWithTimestamp(t, conn, 111111, "fresh", time.Now().UTC())
	insertProgressWithTimestamp(t, conn, 222222, "old", time.Now().UTC().AddDate(0, 0, -60))

	code, resp := performCleanup(t, conn, "?days=0")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Days != 0 {
		t.Errorf("days = %d, want 0", resp.Days)
	}
	if resp.Deleted != 2 {
		t.Errorf("deleted = %d, want 2 (all records)", resp.Deleted)
	}
	if n := countReadingProgress(t, conn); n != 0 {
		t.Errorf("remaining records = %d, want 0", n)
	}
}

func TestHandleReadingProgressCleanup_NoRecords(t *testing.T) {
	conn := newTestDB(t)

	code, resp := performCleanup(t, conn, "")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Deleted != 0 {
		t.Errorf("deleted = %d, want 0", resp.Deleted)
	}
}

// ==================== 非法参数测试 ====================

func TestHandleReadingProgressCleanup_NegativeDays(t *testing.T) {
	conn := newTestDB(t)
	insertProgressWithTimestamp(t, conn, 111111, "keep", time.Now().UTC().AddDate(0, 0, -31))

	code, _ := performCleanup(t, conn, "?days=-1")

	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", code, http.StatusBadRequest)
	}
	if n := countReadingProgress(t, conn); n != 1 {
		t.Errorf("records after failed cleanup = %d, want 1 (nothing deleted)", n)
	}
}

func TestHandleReadingProgressCleanup_NonNumericDays(t *testing.T) {
	conn := newTestDB(t)
	insertProgressWithTimestamp(t, conn, 111111, "keep", time.Now().UTC().AddDate(0, 0, -31))

	code, _ := performCleanup(t, conn, "?days=abc")

	if code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", code, http.StatusBadRequest)
	}
	if n := countReadingProgress(t, conn); n != 1 {
		t.Errorf("records after failed cleanup = %d, want 1 (nothing deleted)", n)
	}
}

// ==================== 回归测试 ====================

func TestHandleRecentlyRead_NoAutoCleanup(t *testing.T) {
	conn := newTestDB(t)
	insertProgressWithTimestamp(t, conn, 111111, "old", time.Now().UTC().AddDate(0, 0, -31))

	r := setupTestRouter()
	app := &App{DB: &DB{conn: conn}}
	r.GET("/api/recently-read", app.handleRecentlyRead)

	req := httptest.NewRequest("GET", "/api/recently-read", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.RecentlyReadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal recently-read: %v", err)
	}
	// 超过 30 天的记录仍然返回，recently-read 不触发清理
	if len(resp.Results) != 1 {
		t.Fatalf("Results length = %d, want 1 (recently-read must not auto-cleanup)", len(resp.Results))
	}
	if resp.Results[0].ID != 111111 {
		t.Errorf("result ID = %d, want 111111", resp.Results[0].ID)
	}
	if n := countReadingProgress(t, conn); n != 1 {
		t.Errorf("records after recently-read = %d, want 1 (no auto-cleanup)", n)
	}
}

func TestHandleReadingProgressCleanup_PreservesBookshelf(t *testing.T) {
	conn := newTestDB(t)
	bookshelfRepo := database.NewBookshelfRepository(conn)
	b := newTestBookshelf()
	if err := bookshelfRepo.Add(t.Context(), b); err != nil {
		t.Fatalf("add bookshelf: %v", err)
	}
	insertProgressWithTimestamp(t, conn, b.GalleryID, b.Token, time.Now().UTC().AddDate(0, 0, -31))

	code, resp := performCleanup(t, conn, "?days=30")

	if code != http.StatusOK {
		t.Fatalf("status = %d, want %d", code, http.StatusOK)
	}
	if resp.Deleted != 1 {
		t.Errorf("deleted = %d, want 1", resp.Deleted)
	}

	// Reading progress 已删除
	progressRepo := database.NewReadingProgressRepository(conn)
	p, err := progressRepo.Get(t.Context(), b.GalleryID, b.Token)
	if err != nil {
		t.Fatalf("get progress: %v", err)
	}
	if p.UpdatedAt != nil {
		t.Error("reading progress should be deleted after cleanup")
	}

	// Bookshelf 必须保留
	exists, _, err := bookshelfRepo.Exists(t.Context(), b.GalleryID, b.Token)
	if err != nil {
		t.Fatalf("check bookshelf: %v", err)
	}
	if !exists {
		t.Error("bookshelf should be preserved after cleanup")
	}
}
