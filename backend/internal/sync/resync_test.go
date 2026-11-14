package sync

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/readingprogress"
)

// newEnginePair wires a host service (server role) to a client engine that
// dials it over real HTTP. The returned client service is the side whose
// engine performs the push cycles.
func newEnginePair(t *testing.T) (serverClient, clientClient *ent.Client, serverSvc, clientSvc *Service, cfg ClientConfig) {
	t.Helper()
	serverClient = newTestClient(t)
	clientClient = newTestClient(t)
	serverSvc = newHostService(serverClient, "tok")
	ts := httptest.NewServer(newTestRouter(serverSvc))
	t.Cleanup(ts.Close)
	clientSvc = NewService(clientClient, Options{})
	cfg = ClientConfig{Enabled: true, ServerURL: ts.URL, Token: "tok"}
	return serverClient, clientClient, serverSvc, clientSvc, cfg
}

func deleteProgress(t *testing.T, client *ent.Client, galleryID int64, token string) {
	t.Helper()
	if _, err := client.ReadingProgress.Delete().
		Where(readingprogress.GalleryID(galleryID), readingprogress.Token(token)).
		Exec(t.Context()); err != nil {
		t.Fatalf("delete progress: %v", err)
	}
}

// A delete recorded before the first sync must reach the peer: the live
// snapshot alone cannot express it and would otherwise resurrect the row.
func TestFullSyncPropagatesPreBootstrapDeletes(t *testing.T) {
	serverClient, clientClient, serverSvc, clientSvc, cfg := newEnginePair(t)
	old := time.Now().UTC().Add(-time.Hour)

	// Client deleted its copy before sync was ever enabled.
	seedProgress(t, clientClient, 1, "gone-on-client", 1, old)
	seedProgress(t, serverClient, 1, "gone-on-client", 1, old)
	deleteProgress(t, clientClient, 1, "gone-on-client")
	if err := clientSvc.RecordDelete(t.Context(), testID(1), EntityReadingProgress, 1, "gone-on-client"); err != nil {
		t.Fatalf("record client tombstone: %v", err)
	}

	// Server deleted its copy before the client ever synced.
	seedProgress(t, serverClient, 2, "gone-on-server", 1, old)
	seedProgress(t, clientClient, 2, "gone-on-server", 1, old)
	deleteProgress(t, serverClient, 2, "gone-on-server")
	if err := serverSvc.RecordDelete(t.Context(), testID(1), EntityReadingProgress, 2, "gone-on-server"); err != nil {
		t.Fatalf("record server tombstone: %v", err)
	}

	clientSvc.engine.pushCycle(t.Context(), cfg)

	if p := getProgress(t, serverClient, 1, "gone-on-client"); p != nil {
		t.Fatalf("client-side pre-sync delete did not reach the server: %+v", p)
	}
	if p := getProgress(t, clientClient, 2, "gone-on-server"); p != nil {
		t.Fatalf("server-side pre-sync delete did not reach the client: %+v", p)
	}
	if p := getProgress(t, clientClient, 1, "gone-on-client"); p != nil {
		t.Fatalf("client resurrected its own pre-sync delete: %+v", p)
	}
}

// BootstrapChanges carries tombstones but must not replay a tombstone whose
// row was re-created afterwards (the snapshot upsert is the current state).
func TestBootstrapChangesTombstones(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})
	now := time.Now().UTC().Add(-time.Hour)

	// Deleted and still gone: tombstone expected.
	seedProgress(t, client, 10, "gone", 1, now)
	if err := svc.RecordDelete(t.Context(), testID(1), EntityReadingProgress, 10, "gone"); err != nil {
		t.Fatalf("record delete: %v", err)
	}
	deleteProgress(t, client, 10, "gone")

	// Deleted then re-created: the snapshot upsert wins, no tombstone.
	seedProgress(t, client, 11, "alive", 1, now)
	if err := svc.RecordDelete(t.Context(), testID(1), EntityReadingProgress, 11, "alive"); err != nil {
		t.Fatalf("record delete: %v", err)
	}
	deleteProgress(t, client, 11, "alive")
	seedProgress(t, client, 11, "alive", 5, now.Add(time.Minute))
	if err := svc.RecordUpsert(t.Context(), testID(1), EntityReadingProgress, 11, "alive"); err != nil {
		t.Fatalf("record upsert: %v", err)
	}

	changes, watermark, err := BootstrapChanges(t.Context(), client)
	if err != nil {
		t.Fatalf("bootstrap changes: %v", err)
	}
	if watermark == 0 {
		t.Fatal("watermark = 0, want the current outbox maximum")
	}

	var goneDelete, aliveUpsert, aliveDelete bool
	for _, ch := range changes {
		if ch.GalleryID != 10 && ch.GalleryID != 11 {
			continue
		}
		switch {
		case ch.GalleryID == 10 && ch.Op == OpDelete:
			goneDelete = true
		case ch.GalleryID == 11 && ch.Op == OpUpsert:
			aliveUpsert = true
		case ch.GalleryID == 11 && ch.Op == OpDelete:
			aliveDelete = true
		}
	}
	if !goneDelete {
		t.Error("bootstrap is missing the tombstone for the deleted row")
	}
	if !aliveUpsert {
		t.Error("bootstrap is missing the re-created row's upsert")
	}
	if aliveDelete {
		t.Error("bootstrap sent a stale tombstone for a re-created row")
	}
}

// A pending local delete must not be undone by an older row arriving from the
// peer (e.g. in its bootstrap snapshot); a genuinely newer row still wins.
func TestApplyChangesKeepsPendingTombstone(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})
	now := time.Now().UTC()
	if err := svc.RecordDelete(t.Context(), testID(1), EntityReadingProgress, 3, "tok"); err != nil {
		t.Fatalf("record delete: %v", err)
	}

	older := Change{
		UserID: testID(1), Entity: EntityReadingProgress, GalleryID: 3, Token: "tok", Op: OpUpsert,
		Row: &Row{CurrentPage: 1, Progress: 0.1, CreatedAt: now.Add(-time.Hour), UpdatedAt: now.Add(-time.Hour)},
	}
	applied, skipped, err := ApplyChanges(t.Context(), client, []Change{older})
	if err != nil {
		t.Fatalf("apply older: %v", err)
	}
	if applied != 0 || skipped != 1 {
		t.Fatalf("older apply = %d/%d, want 0/1", applied, skipped)
	}
	if p := getProgress(t, client, 3, "tok"); p != nil {
		t.Fatalf("pending tombstone resurrected by an older row: %+v", p)
	}

	newer := Change{
		UserID: testID(1), Entity: EntityReadingProgress, GalleryID: 3, Token: "tok", Op: OpUpsert,
		Row: &Row{CurrentPage: 2, Progress: 0.2, CreatedAt: now, UpdatedAt: now.Add(time.Hour)},
	}
	applied, skipped, err = ApplyChanges(t.Context(), client, []Change{newer})
	if err != nil {
		t.Fatalf("apply newer: %v", err)
	}
	if applied != 1 || skipped != 0 {
		t.Fatalf("newer apply = %d/%d, want 1/0", applied, skipped)
	}
	if p := getProgress(t, client, 3, "tok"); p == nil || p.CurrentPage != 2 {
		t.Fatalf("newer row was not applied: %+v", p)
	}
}

// The manual resync endpoint clears the replication bookkeeping so the next
// exchange bootstraps both directions.
func TestResyncEndpointResetsState(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})
	router := newTestRouter(svc)

	enabled, serverURL, token := true, "https://peer.example.com", "node-key"
	if _, err := svc.SaveClientConfig(t.Context(), ConfigUpdate{
		Enabled: &enabled, ServerURL: &serverURL, Token: &token,
	}); err != nil {
		t.Fatalf("save config: %v", err)
	}
	syncAt := time.Now().UTC()
	for key, value := range map[string]string{
		keyCursor:       "42",
		keyLastPushedID: "17",
		keyBootstrapped: "true",
		keyLastSyncAt:   syncAt.Format(time.RFC3339Nano),
		keyLastError:    "boom",
	} {
		if err := svc.setState(t.Context(), key, value); err != nil {
			t.Fatalf("seed %s: %v", key, err)
		}
	}

	req := httptest.NewRequest(http.MethodPost, "/api/sync/resync", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("resync = %d, body %s", w.Code, w.Body.String())
	}

	st, err := svc.LoadEngineState(t.Context())
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if st.Cursor != 0 || st.LastPushedID != 0 || st.Bootstrapped || st.LastSyncAt != nil || st.LastError != "" {
		t.Fatalf("state after resync = %+v, want zeroed", st)
	}
}

func TestResyncEndpointRequiresConfiguration(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})
	router := newTestRouter(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/sync/resync", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("unconfigured resync = %d, want 409", w.Code)
	}
}
