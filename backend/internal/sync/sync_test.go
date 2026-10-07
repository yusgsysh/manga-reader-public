package sync

import (
	"bufio"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	entsql "entgo.io/ent/dialect/sql"
	_ "modernc.org/sqlite"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/gallerycache"
	"manga-reader/internal/ent/readingprogress"
)

func newTestClient(t *testing.T) *ent.Client {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	// A plain :memory: database is private to its connection; one connection
	// keeps every query on the same database.
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	if _, err := conn.Exec("PRAGMA busy_timeout=5000"); err != nil {
		t.Fatalf("set busy timeout: %v", err)
	}
	drv := entsql.OpenDB("sqlite3", conn)
	client := ent.NewClient(ent.Driver(drv))
	if err := client.Schema.Create(t.Context()); err != nil {
		client.Close()
		t.Fatalf("run migrations: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func newTestRouter(svc *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	svc.RegisterRoutes(r)
	return r
}

func seedProgress(t *testing.T, client *ent.Client, galleryID int64, token string, page int, updatedAt time.Time) {
	t.Helper()
	_, err := client.ReadingProgress.Create().
		SetGalleryID(galleryID).
		SetToken(token).
		SetCurrentPage(page).
		SetProgress(float64(page) / 10).
		SetCompleted(false).
		SetCreatedAt(updatedAt).
		SetUpdatedAt(updatedAt).
		Save(t.Context())
	if err != nil {
		t.Fatalf("seed progress: %v", err)
	}
}

func getProgress(t *testing.T, client *ent.Client, galleryID int64, token string) *ent.ReadingProgress {
	t.Helper()
	p, err := client.ReadingProgress.Query().
		Where(readingprogress.GalleryID(galleryID), readingprogress.Token(token)).
		Only(t.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		t.Fatalf("get progress: %v", err)
	}
	return p
}

func doPush(t *testing.T, r *gin.Engine, token string, req PushRequest) (*httptest.ResponseRecorder, PushResponse) {
	t.Helper()
	body, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal push: %v", err)
	}
	httpReq := httptest.NewRequest(http.MethodPost, "/api/sync/push", bytes.NewReader(body))
	httpReq.Header.Set("Content-Type", "application/json")
	if token != "" {
		httpReq.Header.Set(headerToken, token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httpReq)

	var resp PushResponse
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode push response: %v", err)
		}
	}
	return w, resp
}

func TestApplyChangesLastWriteWins(t *testing.T) {
	client := newTestClient(t)
	base := time.Now().UTC().Add(-time.Hour)
	seedProgress(t, client, 1, "tok", 1, base)

	older := Change{
		Entity: EntityReadingProgress, GalleryID: 1, Token: "tok", Op: OpUpsert,
		Row: &Row{CurrentPage: 3, Progress: 0.3, CreatedAt: base, UpdatedAt: base.Add(-time.Minute)},
	}
	applied, skipped, err := ApplyChanges(t.Context(), client, []Change{older})
	if err != nil {
		t.Fatalf("apply older: %v", err)
	}
	if applied != 0 || skipped != 1 {
		t.Fatalf("older apply = %d/%d, want 0/1", applied, skipped)
	}
	if p := getProgress(t, client, 1, "tok"); p.CurrentPage != 1 {
		t.Fatalf("current_page = %d after stale upsert, want 1", p.CurrentPage)
	}

	newer := Change{
		Entity: EntityReadingProgress, GalleryID: 1, Token: "tok", Op: OpUpsert,
		Row: &Row{CurrentPage: 7, Progress: 0.7, CreatedAt: base, UpdatedAt: base.Add(time.Minute)},
	}
	applied, skipped, err = ApplyChanges(t.Context(), client, []Change{newer})
	if err != nil {
		t.Fatalf("apply newer: %v", err)
	}
	if applied != 1 || skipped != 0 {
		t.Fatalf("newer apply = %d/%d, want 1/0", applied, skipped)
	}
	if p := getProgress(t, client, 1, "tok"); p.CurrentPage != 7 {
		t.Fatalf("current_page = %d after newer upsert, want 7", p.CurrentPage)
	}
}

func TestApplyChangesIdempotent(t *testing.T) {
	client := newTestClient(t)
	now := time.Now().UTC()
	ch := Change{
		Entity: EntityReadingProgress, GalleryID: 5, Token: "abc", Op: OpUpsert,
		Row: &Row{CurrentPage: 2, Progress: 0.2, CreatedAt: now, UpdatedAt: now},
	}
	applied, _, err := ApplyChanges(t.Context(), client, []Change{ch})
	if err != nil || applied != 1 {
		t.Fatalf("first apply = %d, err %v", applied, err)
	}
	// Echo: the same change re-applied must be a no-op (equal timestamps).
	applied, skipped, err := ApplyChanges(t.Context(), client, []Change{ch})
	if err != nil {
		t.Fatalf("second apply: %v", err)
	}
	if applied != 0 || skipped != 1 {
		t.Fatalf("echo apply = %d/%d, want 0/1", applied, skipped)
	}
}

func TestApplyDeleteTombstoneLWW(t *testing.T) {
	client := newTestClient(t)
	base := time.Now().UTC().Add(-time.Hour)
	seedProgress(t, client, 9, "tok", 5, base)

	// Tombstone older than the row: keep the row.
	old := base.Add(-time.Minute)
	delOld := Change{
		Entity: EntityReadingProgress, GalleryID: 9, Token: "tok",
		Op: OpDelete, DeletedAt: &old,
	}
	applied, _, err := ApplyChanges(t.Context(), client, []Change{delOld})
	if err != nil {
		t.Fatalf("apply old tombstone: %v", err)
	}
	if applied != 0 {
		t.Fatal("old tombstone must not delete a newer row")
	}
	if getProgress(t, client, 9, "tok") == nil {
		t.Fatal("row deleted by stale tombstone")
	}

	// Tombstone newer than the row: delete.
	fresh := base.Add(time.Hour)
	delNew := Change{
		Entity: EntityReadingProgress, GalleryID: 9, Token: "tok",
		Op: OpDelete, DeletedAt: &fresh,
	}
	applied, _, err = ApplyChanges(t.Context(), client, []Change{delNew})
	if err != nil {
		t.Fatalf("apply new tombstone: %v", err)
	}
	if applied != 1 {
		t.Fatal("new tombstone must delete the row")
	}
	if getProgress(t, client, 9, "tok") != nil {
		t.Fatal("row still present after fresh tombstone")
	}
}

func TestRecordNotifiesHub(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})

	peerCh, unsubPeer := svc.Hub().SubscribePeer()
	defer unsubPeer()
	localCh, unsubLocal := svc.Hub().SubscribeLocal()
	defer unsubLocal()

	if err := svc.RecordUpsert(t.Context(), EntityBookshelf, 1, "tok"); err != nil {
		t.Fatalf("record: %v", err)
	}
	assertSignaled(t, peerCh, "peer subscriber")
	assertSignaled(t, localCh, "local subscriber")

	// Remote-originated applies notify local subscribers only.
	svc.Hub().NotifyLocal()
	assertSignaled(t, localCh, "local subscriber after NotifyLocal")
	select {
	case <-peerCh:
		t.Fatal("peer subscriber must not wake on local-only notify")
	default:
	}
}

func assertSignaled(t *testing.T, ch <-chan struct{}, who string) {
	t.Helper()
	select {
	case <-ch:
	default:
		t.Fatalf("%s not signaled", who)
	}
}

func TestReadChangesSinceCoalescesPerRecord(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})

	base := time.Now().UTC()
	seedProgress(t, client, 1, "tok", 1, base)
	if err := svc.RecordUpsert(t.Context(), EntityReadingProgress, 1, "tok"); err != nil {
		t.Fatalf("record upsert: %v", err)
	}
	if err := svc.RecordUpsert(t.Context(), EntityReadingProgress, 1, "tok"); err != nil {
		t.Fatalf("record upsert 2: %v", err)
	}
	if err := svc.RecordDelete(t.Context(), EntityReadingProgress, 1, "tok"); err != nil {
		t.Fatalf("record delete: %v", err)
	}
	if _, err := client.ReadingProgress.Delete().
		Where(readingprogress.GalleryID(1), readingprogress.Token("tok")).
		Exec(t.Context()); err != nil {
		t.Fatalf("delete row: %v", err)
	}

	changes, maxID, err := ReadChangesSince(t.Context(), client, 0)
	if err != nil {
		t.Fatalf("read changes: %v", err)
	}
	if maxID != 3 {
		t.Fatalf("maxID = %d, want 3", maxID)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1 coalesced entry", len(changes))
	}
	if changes[0].Op != OpDelete {
		t.Fatalf("op = %q, want delete (last entry wins)", changes[0].Op)
	}
	if changes[0].DeletedAt == nil {
		t.Fatal("delete tombstone missing deleted_at")
	}
}

func TestPushBootstrapAndDelta(t *testing.T) {
	serverClient := newTestClient(t)
	clientClient := newTestClient(t)
	serverSvc := NewService(serverClient, Options{HostToken: "s3cret"})
	router := newTestRouter(serverSvc)

	now := time.Now().UTC()
	seedProgress(t, serverClient, 100, "srv", 4, now.Add(-time.Hour))
	seedProgress(t, clientClient, 200, "cli", 6, now.Add(-time.Hour))

	// Bootstrap: client snapshot -> server, server snapshot -> client.
	snap, err := ExportSnapshot(t.Context(), clientClient)
	if err != nil {
		t.Fatalf("export client snapshot: %v", err)
	}
	w, resp := doPush(t, router, "s3cret", PushRequest{Cursor: 0, Snapshot: true, Changes: snap})
	if w.Code != http.StatusOK {
		t.Fatalf("bootstrap push status = %d, body %s", w.Code, w.Body.String())
	}
	if resp.Applied != 1 {
		t.Fatalf("server applied = %d, want 1", resp.Applied)
	}
	// The snapshot runs after the apply, so the response may echo the
	// client's own just-applied row; the server's row must be present.
	found := false
	for _, ch := range resp.Changes {
		if ch.Entity == EntityReadingProgress && ch.GalleryID == 100 {
			found = true
		}
	}
	if !found {
		t.Fatalf("bootstrap response changes = %+v, missing server row 100", resp.Changes)
	}
	if _, _, err := ApplyChanges(t.Context(), clientClient, resp.Changes); err != nil {
		t.Fatalf("client apply bootstrap: %v", err)
	}
	if getProgress(t, clientClient, 100, "srv") == nil {
		t.Fatal("server row missing on client after bootstrap")
	}
	if getProgress(t, serverClient, 200, "cli") == nil {
		t.Fatal("client row missing on server after bootstrap")
	}

	// Delta: client records a local change and pushes with its cursor.
	if err := NewService(clientClient, Options{}).RecordUpsert(t.Context(), EntityReadingProgress, 200, "cli"); err != nil {
		t.Fatalf("record delta: %v", err)
	}
	if err := clientClient.ReadingProgress.Update().
		Where(readingprogress.GalleryID(200), readingprogress.Token("cli")).
		SetCurrentPage(9).
		SetUpdatedAt(now.Add(time.Minute)).
		Exec(t.Context()); err != nil {
		t.Fatalf("update client row: %v", err)
	}
	delta, _, err := ReadChangesSince(t.Context(), clientClient, 0)
	if err != nil {
		t.Fatalf("read delta: %v", err)
	}

	w, resp = doPush(t, router, "s3cret", PushRequest{Cursor: resp.Cursor, Changes: delta})
	if w.Code != http.StatusOK {
		t.Fatalf("delta push status = %d, body %s", w.Code, w.Body.String())
	}
	if resp.Applied != 1 {
		t.Fatalf("server applied = %d, want 1", resp.Applied)
	}
	if p := getProgress(t, serverClient, 200, "cli"); p == nil || p.CurrentPage != 9 {
		t.Fatalf("server row = %+v, want current_page 9", p)
	}

	// Applies do not enter the outbox, and pushed entries were pruned.
	count, err := serverClient.SyncChange.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count server outbox: %v", err)
	}
	if count != 0 {
		t.Fatalf("server outbox = %d entries, want 0", count)
	}
}

func TestPushRequiresToken(t *testing.T) {
	serverClient := newTestClient(t)
	withToken := NewService(serverClient, Options{HostToken: "right"})
	router := newTestRouter(withToken)

	body, _ := json.Marshal(PushRequest{})
	do := func(token string) int {
		req := httptest.NewRequest(http.MethodPost, "/api/sync/push", bytes.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set(headerToken, token)
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w.Code
	}

	if code := do(""); code != http.StatusUnauthorized {
		t.Fatalf("no token = %d, want 401", code)
	}
	if code := do("wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", code)
	}
	if code := do("right"); code != http.StatusOK {
		t.Fatalf("right token = %d, want 200", code)
	}

	// Without a configured host token the peer routes must not exist.
	plain := NewService(newTestClient(t), Options{})
	plainRouter := newTestRouter(plain)
	req := httptest.NewRequest(http.MethodPost, "/api/sync/push", bytes.NewReader(body))
	w := httptest.NewRecorder()
	plainRouter.ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("disabled host = %d, want 404", w.Code)
	}
}

func TestConfigRoundTripMasksToken(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})
	router := newTestRouter(svc)

	put := func(body string) configResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/sync/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT config = %d, body %s", w.Code, w.Body.String())
		}
		var resp configResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return resp
	}
	get := func() configResponse {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/api/sync/config", nil)
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("GET config = %d", w.Code)
		}
		var resp configResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("decode config: %v", err)
		}
		return resp
	}

	resp := put(`{"enabled": true, "server_url": "https://example.com/", "token": "hunter2"}`)
	if resp.Token != Mask {
		t.Fatalf("PUT token = %q, want mask (response must not leak the token)", resp.Token)
	}
	if resp.ServerURL != "https://example.com" {
		t.Fatalf("PUT url = %q, want trimmed", resp.ServerURL)
	}

	resp = get()
	if resp.Token != Mask {
		t.Fatalf("GET token = %q, want mask", resp.Token)
	}
	cfg, err := svc.LoadClientConfig(t.Context())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.Token != "hunter2" {
		t.Fatalf("stored token = %q, want hunter2", cfg.Token)
	}
	if cfg.ServerURL != "https://example.com" {
		t.Fatalf("stored url = %q, want trimmed", cfg.ServerURL)
	}

	put(`{"token": "` + Mask + `"}`)
	cfg, _ = svc.LoadClientConfig(t.Context())
	if cfg.Token != "hunter2" {
		t.Fatalf("token after mask put = %q, want unchanged", cfg.Token)
	}

	put(`{"token": ""}`)
	cfg, _ = svc.LoadClientConfig(t.Context())
	if cfg.Token != "" {
		t.Fatalf("token after clear = %q, want empty", cfg.Token)
	}

	req := httptest.NewRequest(http.MethodPut, "/api/sync/config", strings.NewReader(`{"server_url": "ftp://x"}`))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("ftp url = %d, want 400", w.Code)
	}
}

func TestConfigPeerChangeResetsEngineState(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})
	router := newTestRouter(svc)

	put := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/sync/config", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT config = %d, body %s", w.Code, w.Body.String())
		}
	}
	loadState := func() EngineState {
		t.Helper()
		st, err := svc.LoadEngineState(t.Context())
		if err != nil {
			t.Fatalf("load engine state: %v", err)
		}
		return st
	}

	put(`{"server_url": "https://peer.example.com"}`)

	// An established replication state against the current peer.
	syncAt := time.Now().UTC().Truncate(time.Millisecond)
	for key, value := range map[string]string{
		keyCursor:       "42",
		keyLastPushedID: "42",
		keyBootstrapped: "true",
		keyLastSyncAt:   syncAt.Format(time.RFC3339Nano),
		keyLastError:    "boom",
	} {
		if err := svc.setState(t.Context(), key, value); err != nil {
			t.Fatalf("seed state %s: %v", key, err)
		}
	}

	// Re-saving the same peer (URL normalization, token rotation) keeps the
	// bookkeeping: only an actual peer change resets it.
	put(`{"server_url": "https://peer.example.com/", "token": "rotated"}`)
	st := loadState()
	if st.Cursor != 42 || st.LastPushedID != 42 || !st.Bootstrapped || st.LastError != "boom" || st.LastSyncAt == nil {
		t.Fatalf("same-peer save state = %+v, want unchanged", st)
	}

	// A different peer starts from scratch so the next exchange is a full
	// bidirectional snapshot instead of deltas against a foreign cursor.
	put(`{"server_url": "https://other.example.com"}`)
	st = loadState()
	if st.Cursor != 0 || st.LastPushedID != 0 || st.Bootstrapped || st.LastSyncAt != nil || st.LastError != "" {
		t.Fatalf("peer change state = %+v, want zeroed", st)
	}
}

func TestLocalEventsStreamsPayloads(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})
	ts := httptest.NewServer(newTestRouter(svc))
	defer ts.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/api/events/changes", nil)
	if err != nil {
		t.Fatalf("build request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("connect events: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("events status = %d", resp.StatusCode)
	}
	if ct := resp.Header.Get("Content-Type"); !strings.Contains(ct, "text/event-stream") {
		t.Fatalf("content type = %q, want text/event-stream", ct)
	}

	reader := bufio.NewReader(resp.Body)
	// Initial payload arrives without any change.
	if line := readEventLine(t, reader); !strings.HasPrefix(line, "data: ") {
		t.Fatalf("initial line = %q, want data payload", line)
	}

	// A notification produces another payload.
	svc.Hub().NotifyLocal()
	if line := readEventLine(t, reader); !strings.HasPrefix(line, "data: ") {
		t.Fatalf("notified line = %q, want data payload", line)
	}
}

// readEventLine reads from the stream until a data line arrives or the
// deadline passes. Comment lines (": ping" heartbeats) are skipped.
func readEventLine(t *testing.T, reader *bufio.Reader) string {
	t.Helper()
	deadline := time.After(3 * time.Second)
	type result struct {
		line string
		err  error
	}
	resCh := make(chan result, 1)
	go func() {
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				resCh <- result{err: err}
				return
			}
			line = strings.TrimRight(line, "\r\n")
			if strings.HasPrefix(line, "data:") {
				resCh <- result{line: line}
				return
			}
		}
	}()
	select {
	case <-deadline:
		t.Fatal("timed out waiting for event line")
	case res := <-resCh:
		if res.err != nil {
			t.Fatalf("read event: %v", res.err)
		}
		return res.line
	}
	return ""
}

func TestEngineSnapshotWatermark(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})
	engine := newEngine(svc)

	now := time.Now().UTC()
	seedProgress(t, client, 1, "a", 1, now)
	if err := svc.RecordUpsert(t.Context(), EntityReadingProgress, 1, "a"); err != nil {
		t.Fatalf("record: %v", err)
	}

	changes, watermark, err := engine.buildPushChanges(t.Context(), EngineState{Bootstrapped: false})
	if err != nil {
		t.Fatalf("bootstrap build: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("snapshot changes = %d, want 1", len(changes))
	}
	if watermark != 1 {
		t.Fatalf("watermark = %d, want 1 (outbox entries covered by the snapshot)", watermark)
	}

	changes, watermark, err = engine.buildPushChanges(t.Context(), EngineState{Bootstrapped: true, LastPushedID: 1})
	if err != nil {
		t.Fatalf("delta build: %v", err)
	}
	if len(changes) != 0 {
		t.Fatalf("delta changes = %d, want 0", len(changes))
	}
	if watermark != 1 {
		t.Fatalf("watermark = %d, want 1", watermark)
	}
}

// seedGalleryCacheRow inserts a gallery_cache row with the given updatedAt.
func seedGalleryCacheRow(t *testing.T, client *ent.Client, galleryID int64, token string, updatedAt time.Time) {
	t.Helper()
	_, err := client.GalleryCache.Create().
		SetGalleryID(galleryID).
		SetToken(token).
		SetTitle("Seed Title").
		SetCategory("Seed Category").
		SetPages([]string{"https://seed/page1.jpg"}).
		SetCreatedAt(updatedAt).
		SetUpdatedAt(updatedAt).
		Save(t.Context())
	if err != nil {
		t.Fatalf("seed gallery cache: %v", err)
	}
}

func getGalleryCacheRow(t *testing.T, client *ent.Client, galleryID int64, token string) *ent.GalleryCache {
	t.Helper()
	row, err := client.GalleryCache.Query().
		Where(gallerycache.GalleryID(galleryID), gallerycache.Token(token)).
		Only(t.Context())
	if err != nil {
		if ent.IsNotFound(err) {
			return nil
		}
		t.Fatalf("get gallery cache: %v", err)
	}
	return row
}

func TestApplyGalleryCacheCreateAndMerge(t *testing.T) {
	client := newTestClient(t)
	base := time.Now().UTC().Add(-time.Hour)

	// Create a new row via incoming change.
	create := Change{
		Entity: EntityGalleryCache, GalleryID: 42, Token: "tok", Op: OpUpsert,
		Row: &Row{
			CreatedAt: base, UpdatedAt: base,
			Title:       "Full Metaller",
			Category:    "Manga",
			Pages:       []string{"https://a/1.jpg"},
			Rating:      4.5,
			RatingCount: 10,
		},
	}
	applied, skipped, err := ApplyChanges(t.Context(), client, []Change{create})
	if err != nil || applied != 1 || skipped != 0 {
		t.Fatalf("create apply = %d/%d, err %v", applied, skipped, err)
	}
	row := getGalleryCacheRow(t, client, 42, "tok")
	if row == nil {
		t.Fatal("row missing after create")
	}
	if row.Title != "Full Metaller" || row.Category != "Manga" || len(row.Pages) != 1 || row.Rating != 4.5 || row.RatingCount != 10 {
		t.Fatalf("row = %+v, want seeded fields", row)
	}

	// Newer row with only some fields set: Title empty (should preserve), Pages longer (should replace),
	// Rating updated, Category not provided (should preserve).
	metaAt := base.Add(10 * time.Minute)
	newer := Change{
		Entity: EntityGalleryCache, GalleryID: 42, Token: "tok", Op: OpUpsert,
		Row: &Row{
			CreatedAt: base, UpdatedAt: metaAt,
			Title:       "",                                             // empty -> preserve existing
			Category:    "",                                             // empty -> preserve existing
			Pages:       []string{"https://b/1.jpg", "https://b/2.jpg"}, // len>0 -> replace
			Rating:      4.8,                                            // non-zero -> update
			RatingCount: 0,                                              // zero -> preserve
			Uploader:    "NewUploader",
		},
	}
	applied, skipped, err = ApplyChanges(t.Context(), client, []Change{newer})
	if err != nil || applied != 1 || skipped != 0 {
		t.Fatalf("merge apply = %d/%d, err %v", applied, skipped, err)
	}
	row = getGalleryCacheRow(t, client, 42, "tok")
	if row == nil {
		t.Fatal("row missing after merge")
	}
	if row.Title != "Full Metaller" {
		t.Errorf("Title = %q, want preserved %q", row.Title, "Full Metaller")
	}
	if row.Category != "Manga" {
		t.Errorf("Category = %q, want preserved %q", row.Category, "Manga")
	}
	if len(row.Pages) != 2 {
		t.Errorf("Pages = %v, want 2 new pages", row.Pages)
	}
	if row.Rating != 4.8 {
		t.Errorf("Rating = %v, want 4.8", row.Rating)
	}
	if row.RatingCount != 10 {
		t.Errorf("RatingCount = %v, want preserved 10", row.RatingCount)
	}
	if row.Uploader != "NewUploader" {
		t.Errorf("Uploader = %q, want NewUploader", row.Uploader)
	}
	if !row.UpdatedAt.Equal(metaAt) {
		t.Errorf("UpdatedAt = %v, want %v", row.UpdatedAt, metaAt)
	}

	// Stale row (UpdatedAt older) should be skipped entirely.
	stale := Change{
		Entity: EntityGalleryCache, GalleryID: 42, Token: "tok", Op: OpUpsert,
		Row: &Row{
			CreatedAt: base, UpdatedAt: base.Add(-time.Minute),
			Title: "Stale Title",
		},
	}
	applied, skipped, err = ApplyChanges(t.Context(), client, []Change{stale})
	if err != nil {
		t.Fatalf("stale apply error: %v", err)
	}
	if applied != 0 || skipped != 1 {
		t.Fatalf("stale apply = %d/%d, want 0/1", applied, skipped)
	}
	row = getGalleryCacheRow(t, client, 42, "tok")
	if row.Title != "Full Metaller" {
		t.Errorf("Title = %q after stale, want preserved %q", row.Title, "Full Metaller")
	}
}

func TestApplyGalleryCacheTombstone(t *testing.T) {
	client := newTestClient(t)
	base := time.Now().UTC().Add(-time.Hour)
	seedGalleryCacheRow(t, client, 99, "tok", base)

	// Delete with newer tombstone.
	del := Change{
		Entity: EntityGalleryCache, GalleryID: 99, Token: "tok", Op: OpDelete,
		DeletedAt: &[]time.Time{base.Add(time.Minute)}[0],
	}
	applied, skipped, err := ApplyChanges(t.Context(), client, []Change{del})
	if err != nil || applied != 1 || skipped != 0 {
		t.Fatalf("delete apply = %d/%d, err %v", applied, skipped, err)
	}
	if getGalleryCacheRow(t, client, 99, "tok") != nil {
		t.Fatal("row still exists after tombstone")
	}

	// Re-applying same tombstone: row gone -> skipped.
	applied, skipped, err = ApplyChanges(t.Context(), client, []Change{del})
	if err != nil || applied != 0 || skipped != 1 {
		t.Fatalf("repeat delete = %d/%d, err %v", applied, skipped, err)
	}

	// Older tombstone should be skipped.
	olderDel := Change{
		Entity: EntityGalleryCache, GalleryID: 99, Token: "tok", Op: OpDelete,
		DeletedAt: &[]time.Time{base.Add(-time.Minute)}[0],
	}
	seedGalleryCacheRow(t, client, 99, "tok", base.Add(time.Hour)) // reseed with newer row
	applied, skipped, err = ApplyChanges(t.Context(), client, []Change{olderDel})
	if err != nil || applied != 0 || skipped != 1 {
		t.Fatalf("older tombstone = %d/%d, err %v", applied, skipped, err)
	}
	if getGalleryCacheRow(t, client, 99, "tok") == nil {
		t.Fatal("newer row incorrectly deleted by older tombstone")
	}
}

func TestExportSnapshotIncludesGalleryCache(t *testing.T) {
	client := newTestClient(t)
	now := time.Now().UTC()
	seedGalleryCacheRow(t, client, 77, "snapTok", now)

	snap, err := ExportSnapshot(t.Context(), client)
	if err != nil {
		t.Fatalf("export snapshot: %v", err)
	}
	found := false
	for _, ch := range snap {
		if ch.Entity == EntityGalleryCache && ch.GalleryID == 77 && ch.Token == "snapTok" {
			found = true
			if ch.Row == nil {
				t.Fatal("snapshot gallery cache row is nil")
			}
			if ch.Row.Title != "Seed Title" {
				t.Errorf("snapshot Title = %q, want Seed Title", ch.Row.Title)
			}
			if len(ch.Row.Pages) != 1 {
				t.Errorf("snapshot Pages = %v, want 1", ch.Row.Pages)
			}
		}
	}
	if !found {
		t.Fatalf("gallery_cache missing from snapshot; got %d entities", len(snap))
	}
}

func TestReadChangesSinceFetchesGalleryCacheRow(t *testing.T) {
	client := newTestClient(t)
	svc := NewService(client, Options{})
	now := time.Now().UTC()

	// Seed a cache row and record its upsert so it appears in the outbox.
	seedGalleryCacheRow(t, client, 55, "tok", now)
	if err := svc.RecordUpsert(t.Context(), EntityGalleryCache, 55, "tok"); err != nil {
		t.Fatalf("record upsert: %v", err)
	}

	changes, maxID, err := ReadChangesSince(t.Context(), client, 0)
	if err != nil {
		t.Fatalf("read changes: %v", err)
	}
	if len(changes) != 1 {
		t.Fatalf("changes = %d, want 1", len(changes))
	}
	ch := changes[0]
	if ch.Entity != EntityGalleryCache || ch.GalleryID != 55 || ch.Token != "tok" {
		t.Fatalf("change = %+v, want gallery_cache 55/tok", ch)
	}
	if ch.Op != OpUpsert || ch.Row == nil {
		t.Fatalf("change op/row = %q/%v, want upsert with row", ch.Op, ch.Row)
	}
	if ch.Row.Title != "Seed Title" {
		t.Errorf("fetched row Title = %q, want Seed Title", ch.Row.Title)
	}
	if ch.Row.UpdatedAt.IsZero() {
		t.Error("fetched row UpdatedAt is zero")
	}
	if maxID != 1 {
		t.Fatalf("maxID = %d, want 1", maxID)
	}
}

// TestPushPruneSkipsRewoundCursor: a client cursor above the host outbox max
// means the host was reset (or the client is stale). Pruning with that raw
// value would delete host changes the client has never seen, destroying them
// permanently.
func TestPushPruneSkipsRewoundCursor(t *testing.T) {
	serverClient := newTestClient(t)
	svc := NewService(serverClient, Options{HostToken: "tok"})
	router := newTestRouter(svc)

	now := time.Now().UTC()
	seedProgress(t, serverClient, 1, "a", 1, now)
	for range 3 {
		if err := svc.RecordUpsert(t.Context(), EntityReadingProgress, 1, "a"); err != nil {
			t.Fatalf("record: %v", err)
		}
	}

	// Client holds a cursor from a previous incarnation of this host.
	w, resp := doPush(t, router, "tok", PushRequest{Cursor: 500})
	if w.Code != http.StatusOK {
		t.Fatalf("rewound push status = %d, body %s", w.Code, w.Body.String())
	}
	if resp.Cursor != 3 {
		t.Fatalf("response cursor = %d, want 3 (current outbox max)", resp.Cursor)
	}
	count, err := serverClient.SyncChange.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if count != 3 {
		t.Fatalf("outbox after rewind push = %d entries, want 3 (prune must be skipped)", count)
	}

	// A plausible cursor still prunes up to itself.
	if _, resp := doPush(t, router, "tok", PushRequest{Cursor: 1}); resp.Cursor != 3 {
		t.Fatalf("second push cursor = %d, want 3", resp.Cursor)
	}
	count, err = serverClient.SyncChange.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count outbox: %v", err)
	}
	if count != 2 {
		t.Fatalf("outbox after valid push = %d entries, want 2 (ids <= 1 pruned)", count)
	}
}

// TestApplyChangesSkipsInvalidRow: one row that fails ent validation must not
// abort the batch — the cursor would never advance and every cycle would
// resend the same batch, wedging replication.
func TestApplyChangesSkipsInvalidRow(t *testing.T) {
	client := newTestClient(t)
	base := time.Now().UTC()

	valid := Change{
		Entity: EntityReadingProgress, GalleryID: 1, Token: "tok", Op: OpUpsert,
		Row: &Row{CurrentPage: 2, Progress: 0.2, CreatedAt: base, UpdatedAt: base},
	}
	invalid := Change{
		Entity: EntityReadingProgress, GalleryID: 2, Token: "tok", Op: OpUpsert,
		Row: &Row{CurrentPage: 1, Progress: 1.5, CreatedAt: base, UpdatedAt: base},
	}

	applied, skipped, err := ApplyChanges(t.Context(), client, []Change{valid, invalid})
	if err != nil {
		t.Fatalf("invalid row must not abort the batch: %v", err)
	}
	if applied != 1 || skipped != 1 {
		t.Fatalf("applied/skipped = %d/%d, want 1/1", applied, skipped)
	}
	if p := getProgress(t, client, 1, "tok"); p == nil || p.CurrentPage != 2 {
		t.Fatalf("valid row not applied: %+v", p)
	}
}
