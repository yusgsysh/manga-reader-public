package sync

import (
	"bytes"
	js "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/syncchange"
	"manga-reader/internal/ident"
	"manga-reader/internal/synckey"
)

func seedUserSyncKey(t *testing.T, client *ent.Client, userID ident.UUID) string {
	t.Helper()
	material, err := synckey.GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	if _, err := client.UserSyncKey.Create().
		SetUserID(userID).
		SetKeyHash(material.Hash).
		SetKeyPrefix(material.Prefix).
		SetStatus(config.StatusActive).
		Save(t.Context()); err != nil {
		t.Fatalf("create user sync key: %v", err)
	}
	return material.Plaintext
}

// A user sync key scopes the whole exchange to its owner: the peer's rows land
// on that account, only that account's rows (plus the shared gallery cache)
// come back, and other accounts' outbox entries are never pruned.
func TestUserSyncKeyScopesExchange(t *testing.T) {
	serverClient := newTestClient(t)
	clientClient := newTestClient(t)
	serverUser := seedUser(t, serverClient, "server-owner")
	otherUser := seedUser(t, serverClient, "server-other")
	clientUser := seedUser(t, clientClient, "desktop-user")
	now := time.Now().UTC()

	serverSvc := NewService(serverClient, Options{
		HostEnabled: true,
		KeyAuth:     synckey.New(serverClient).Middleware,
	})
	seedProgressFor(t, serverClient, serverUser, 1, "mine", 5, now.Add(-time.Minute))
	seedProgressFor(t, serverClient, otherUser, 2, "other", 7, now.Add(-time.Minute))
	if err := serverSvc.RecordUpsert(t.Context(), otherUser, EntityReadingProgress, 2, "other"); err != nil {
		t.Fatalf("record other user: %v", err)
	}
	seedGalleryCacheRow(t, serverClient, 9, "shared", now)
	if err := serverSvc.RecordUpsert(t.Context(), ident.Nil(), EntityGalleryCache, 9, "shared"); err != nil {
		t.Fatalf("record gallery cache: %v", err)
	}

	key := seedUserSyncKey(t, serverClient, serverUser)
	ts := httptest.NewServer(newTestRouter(serverSvc))
	defer ts.Close()

	clientSvc := NewService(clientClient, Options{})
	cfg := ClientConfig{Enabled: true, ServerURL: ts.URL, Token: key, UserID: clientUser}
	seedProgressFor(t, clientClient, clientUser, 3, "desktop", 2, now)
	if err := clientSvc.RecordUpsert(t.Context(), clientUser, EntityReadingProgress, 3, "desktop"); err != nil {
		t.Fatalf("record client row: %v", err)
	}

	clientSvc.engine.pushCycle(t.Context(), cfg)

	// Push landed on the key's owner, not on the client's UUID.
	if p := getProgressForUser(t, serverClient, serverUser, 3, "desktop"); p == nil || p.CurrentPage != 2 {
		t.Fatalf("server owner row = %+v, want page 2", p)
	}
	if p := getProgressForUser(t, serverClient, clientUser, 3, "desktop"); p != nil {
		t.Fatalf("server stored a ghost-owner row: %+v", p)
	}

	// Pull returned the owner's row and the shared cache, and only those.
	if p := getProgressForUser(t, clientClient, clientUser, 1, "mine"); p == nil || p.CurrentPage != 5 {
		t.Fatalf("pulled owner row = %+v, want page 5", p)
	}
	if p := getProgressForUser(t, clientClient, clientUser, 2, "other"); p != nil {
		t.Fatalf("pulled another user's row: %+v", p)
	}
	if getGalleryCacheRow(t, clientClient, 9, "shared") == nil {
		t.Fatal("gallery cache row was not pulled")
	}

	// The next cycle prunes this key's entries and the shared stream, but
	// leaves the other account's pending change for its own peer.
	clientSvc.engine.pushCycle(t.Context(), cfg)
	n, err := serverClient.SyncChange.Query().
		Where(syncchange.UserIDEQ(otherUser)).
		Count(t.Context())
	if err != nil {
		t.Fatalf("count other outbox: %v", err)
	}
	if n != 1 {
		t.Fatalf("other user's outbox entries = %d, want 1", n)
	}
}

func TestUserSyncKeyEndpoints(t *testing.T) {
	client := newTestClient(t)
	owner := seedUser(t, client, "admin")
	other := seedUser(t, client, "other")
	svc := NewService(client, Options{HostEnabled: true})

	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth.principal", auth.Principal{ID: owner, Username: "admin", Role: config.RoleAdmin})
		c.Next()
	})
	svc.RegisterRoutes(router)

	do := func(method, path string, body []byte) *httptest.ResponseRecorder {
		req := httptest.NewRequest(method, path, bytes.NewReader(body))
		if body != nil {
			req.Header.Set("Content-Type", "application/json")
		}
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		return w
	}

	w := do(http.MethodPost, "/api/sync/keys", []byte(`{"name":"laptop","expires_in_days":30}`))
	if w.Code != http.StatusCreated {
		t.Fatalf("create key = %d, body %s", w.Code, w.Body.String())
	}
	var created struct {
		Key    string          `json:"key"`
		APIKey userSyncKeyJSON `json:"api_key"`
	}
	if err := js.Unmarshal(w.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.APIKey.Name != "laptop" {
		t.Fatalf("name = %q, want laptop", created.APIKey.Name)
	}
	if created.APIKey.ExpiresAt == nil || !created.APIKey.ExpiresAt.After(time.Now().Add(29*24*time.Hour)) {
		t.Fatalf("expires_at = %v, want ~30 days out", created.APIKey.ExpiresAt)
	}
	if len(created.Key) < 20 {
		t.Fatalf("plaintext key = %q, want a full secret", created.Key)
	}

	w = do(http.MethodGet, "/api/sync/keys", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("list keys = %d", w.Code)
	}
	var list struct {
		Keys []userSyncKeyJSON `json:"keys"`
	}
	if err := js.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Keys) != 1 || list.Keys[0].ID != created.APIKey.ID {
		t.Fatalf("list = %+v, want the created key", list.Keys)
	}

	// Rotation replaces the key in place: same name/expiry, new id, old row gone.
	w = do(http.MethodPost, "/api/sync/keys/"+created.APIKey.ID.String()+"/rotate", nil)
	if w.Code != http.StatusOK {
		t.Fatalf("rotate key = %d, body %s", w.Code, w.Body.String())
	}
	var rotated struct {
		Key    string          `json:"key"`
		APIKey userSyncKeyJSON `json:"api_key"`
	}
	if err := js.Unmarshal(w.Body.Bytes(), &rotated); err != nil {
		t.Fatalf("decode rotate: %v", err)
	}
	if rotated.APIKey.ID == created.APIKey.ID {
		t.Fatal("rotate returned the same key id")
	}
	if rotated.APIKey.Name != "laptop" {
		t.Fatalf("rotated name = %q, want laptop", rotated.APIKey.Name)
	}
	if rotated.APIKey.ExpiresAt == nil || created.APIKey.ExpiresAt == nil ||
		!rotated.APIKey.ExpiresAt.Equal(*created.APIKey.ExpiresAt) {
		t.Fatalf("rotated expiry = %v, want %v", rotated.APIKey.ExpiresAt, created.APIKey.ExpiresAt)
	}
	if n, _ := client.UserSyncKey.Query().Count(t.Context()); n != 1 {
		t.Fatalf("keys after rotate = %d, want 1", n)
	}

	// Deleting removes the row entirely.
	w = do(http.MethodDelete, "/api/sync/keys/"+rotated.APIKey.ID.String(), nil)
	if w.Code != http.StatusOK {
		t.Fatalf("delete key = %d, body %s", w.Code, w.Body.String())
	}
	var deleted struct {
		Deleted bool `json:"deleted"`
	}
	if err := js.Unmarshal(w.Body.Bytes(), &deleted); err != nil {
		t.Fatalf("decode delete: %v", err)
	}
	if !deleted.Deleted {
		t.Fatal("delete did not report success")
	}
	if n, _ := client.UserSyncKey.Query().Count(t.Context()); n != 0 {
		t.Fatalf("keys after delete = %d, want 0", n)
	}

	// Another account's key cannot be rotated or deleted through this principal.
	otherKey := seedUserSyncKey(t, client, other)
	otherID := ""
	rows, _ := client.UserSyncKey.Query().All(t.Context())
	for _, row := range rows {
		if row.UserID == other {
			otherID = row.ID.String()
		}
	}
	if otherID == "" || otherKey == "" {
		t.Fatal("failed to seed other user's key")
	}
	if w := do(http.MethodDelete, "/api/sync/keys/"+otherID, nil); w.Code != http.StatusNotFound {
		t.Fatalf("delete another user's key = %d, want 404", w.Code)
	}
	if w := do(http.MethodPost, "/api/sync/keys/"+otherID+"/rotate", nil); w.Code != http.StatusNotFound {
		t.Fatalf("rotate another user's key = %d, want 404", w.Code)
	}

	// Bad lifetimes are rejected instead of silently creating a key.
	for _, body := range []string{
		`{"expires_in_days":0}`,
		`{"expires_in_days":3651}`,
		`{"expires_at":"2000-01-01T00:00:00Z"}`,
		`{"expires_at":"not-a-date"}`,
	} {
		if w := do(http.MethodPost, "/api/sync/keys", []byte(body)); w.Code != http.StatusBadRequest {
			t.Fatalf("create with %s = %d, want 400", body, w.Code)
		}
	}
}

func TestUserSyncKeyCreationRequiresHost(t *testing.T) {
	client := newTestClient(t)
	owner := seedUser(t, client, "admin")
	svc := NewService(client, Options{})
	router := gin.New()
	router.Use(func(c *gin.Context) {
		c.Set("auth.principal", auth.Principal{ID: owner, Username: "admin", Role: config.RoleAdmin})
		c.Next()
	})
	svc.RegisterRoutes(router)

	req := httptest.NewRequest(http.MethodPost, "/api/sync/keys", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	if w.Code != http.StatusConflict {
		t.Fatalf("create key without host = %d, want 409", w.Code)
	}
}

// Saving the client config binds it to the verified JWT account; switching
// accounts resets the cursor and drops the previous account's queued changes.
func TestConfigSaveBindsCurrentUser(t *testing.T) {
	client := newTestClient(t)
	first := seedUser(t, client, "first")
	second := seedUser(t, client, "second")
	svc := NewService(client, Options{})

	router := gin.New()
	current := first
	router.Use(func(c *gin.Context) {
		c.Set("auth.principal", auth.Principal{ID: current, Username: "u", Role: config.RoleAdmin})
		c.Next()
	})
	svc.RegisterRoutes(router)

	put := func(body string) {
		t.Helper()
		req := httptest.NewRequest(http.MethodPut, "/api/sync/config", bytes.NewReader([]byte(body)))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("PUT config = %d, body %s", w.Code, w.Body.String())
		}
	}

	put(`{"enabled":true,"server_url":"https://peer.example.com","token":"mrk_first"}`)
	cfg, err := svc.LoadClientConfig(t.Context())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.UserID != first {
		t.Fatalf("config user = %s, want %s", cfg.UserID, first)
	}

	if _, err := client.SyncChange.Create().
		SetUserID(first).SetEntity(EntityReadingProgress).
		SetGalleryID(1).SetToken("tok").SetOp(OpUpsert).Save(t.Context()); err != nil {
		t.Fatalf("seed outbox: %v", err)
	}
	if err := svc.setState(t.Context(), keyCursor, "7"); err != nil {
		t.Fatalf("seed cursor: %v", err)
	}

	current = second
	put(`{"token":"mrk_second"}`)
	cfg, err = svc.LoadClientConfig(t.Context())
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if cfg.UserID != second {
		t.Fatalf("config user = %s, want %s", cfg.UserID, second)
	}
	st, err := svc.LoadEngineState(t.Context())
	if err != nil {
		t.Fatalf("load state: %v", err)
	}
	if st.Cursor != 0 || st.Bootstrapped {
		t.Fatalf("state after account switch = %+v, want reset", st)
	}
	if n, _ := client.SyncChange.Query().Count(t.Context()); n != 0 {
		t.Fatalf("outbox after account switch = %d entries, want none", n)
	}
}
