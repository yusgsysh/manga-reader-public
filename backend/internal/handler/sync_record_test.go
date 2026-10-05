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
	"manga-reader/internal/model"
	synclib "manga-reader/internal/sync"
)

func newSyncService(t *testing.T, client *ent.Client) *synclib.Service {
	t.Helper()
	return synclib.NewService(client, synclib.Options{})
}

func outboxEntries(t *testing.T, client *ent.Client) []*ent.SyncChange {
	t.Helper()
	entries, err := client.SyncChange.Query().All(t.Context())
	if err != nil {
		t.Fatalf("read outbox: %v", err)
	}
	return entries
}

func TestProgressUpdateRecordsSyncChange(t *testing.T) {
	client := newTestDB(t)
	syncSvc := newSyncService(t, client)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}, syncSvc: syncSvc}

	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{
		CurrentPage: 3,
		Progress:    0.25,
	})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}

	entries := outboxEntries(t, client)
	if len(entries) != 1 {
		t.Fatalf("outbox = %d entries, want 1", len(entries))
	}
	if entries[0].Entity != synclib.EntityReadingProgress || entries[0].Op != synclib.OpUpsert {
		t.Fatalf("entry = %+v, want reading_progress upsert", entries[0])
	}
	if entries[0].GalleryID != 123456 || entries[0].Token != "abcdef" {
		t.Fatalf("entry key = %d/%s, want 123456/abcdef", entries[0].GalleryID, entries[0].Token)
	}
}

func TestProgressUpdateBumpsBookshelfOutOfBand(t *testing.T) {
	client := newTestDB(t)
	// A bookshelf row for the same record: progress saves bump its updated_at
	// so the shelf orders by recent activity, which must replicate too.
	if _, err := client.Bookshelf.Create().
		SetGalleryID(123456).
		SetToken("abcdef").
		Save(t.Context()); err != nil {
		t.Fatalf("seed bookshelf: %v", err)
	}

	syncSvc := newSyncService(t, client)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}, syncSvc: syncSvc}
	r.PUT("/api/progress/:id/:token", server.handleUpdateProgress)

	body, _ := json.Marshal(model.UpdateReadingProgressRequest{CurrentPage: 1, Progress: 0.1})
	req := httptest.NewRequest("PUT", "/api/progress/123456/abcdef", bytes.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	entries := outboxEntries(t, client)
	seen := map[string]bool{}
	for _, e := range entries {
		seen[e.Entity] = true
	}
	if !seen[synclib.EntityReadingProgress] || !seen[synclib.EntityBookshelf] {
		t.Fatalf("outbox entities = %v, want both reading_progress and bookshelf", seen)
	}
}

func TestBookshelfRemoveRecordsTombstone(t *testing.T) {
	client := newTestDB(t)
	if _, err := client.Bookshelf.Create().
		SetGalleryID(123456).
		SetToken("abcdef").
		Save(t.Context()); err != nil {
		t.Fatalf("seed bookshelf: %v", err)
	}

	syncSvc := newSyncService(t, client)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}, syncSvc: syncSvc}
	r.DELETE("/api/bookshelf/:id/:token", server.handleBookshelfRemove)

	req := httptest.NewRequest("DELETE", "/api/bookshelf/123456/abcdef", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d", w.Code)
	}

	entries := outboxEntries(t, client)
	if len(entries) != 1 {
		t.Fatalf("outbox = %d entries, want 1", len(entries))
	}
	if entries[0].Entity != synclib.EntityBookshelf || entries[0].Op != synclib.OpDelete {
		t.Fatalf("entry = %+v, want bookshelf delete tombstone", entries[0])
	}
}

func TestReadingProgressCleanupRecordsTombstones(t *testing.T) {
	client := newTestDB(t)
	old := time.Now().UTC().AddDate(0, 0, -60)
	insertProgressWithTimestamp(t, client, 1, "a", old)
	insertProgressWithTimestamp(t, client, 2, "b", old)
	insertProgressWithTimestamp(t, client, 3, "c", time.Now().UTC())

	syncSvc := newSyncService(t, client)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}, syncSvc: syncSvc}
	r.POST("/api/reading-progress/cleanup", server.handleReadingProgressCleanup)

	req := httptest.NewRequest("POST", "/api/reading-progress/cleanup?days=30", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}

	entries := outboxEntries(t, client)
	if len(entries) != 2 {
		t.Fatalf("outbox = %d entries, want 2 tombstones (retention purge propagates)", len(entries))
	}
	for _, e := range entries {
		if e.Op != synclib.OpDelete || e.Entity != synclib.EntityReadingProgress {
			t.Fatalf("entry = %+v, want reading_progress delete", e)
		}
	}
}
