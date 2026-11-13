package node

import (
	"bytes"
	"database/sql"
	json "encoding/json/v2"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
)

func newTestDB(t *testing.T) *ent.Client {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("foreign keys: %v", err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", conn)))
	if err := client.Schema.Create(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func TestNodeAPIKeyAuthentication(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := newTestDB(t)
	tenantRow, err := client.Tenant.Create().SetName("T").SetSlug("t").SetStatus(config.StatusActive).Save(t.Context())
	if err != nil {
		t.Fatalf("tenant: %v", err)
	}
	nodeRow, err := client.Node.Create().
		SetName("node-a").SetTenantID(int64(tenantRow.ID)).SetStatus(config.StatusActive).Save(t.Context())
	if err != nil {
		t.Fatalf("node: %v", err)
	}
	material, err := GenerateKey()
	if err != nil {
		t.Fatalf("key: %v", err)
	}
	keyRow, err := client.NodeAPIKey.Create().
		SetNodeID(nodeRow.ID).SetKeyHash(material.Hash).SetKeyPrefix(material.Prefix).
		SetScopes([]string{ScopeSyncWrite}).SetStatus(config.StatusActive).Save(t.Context())
	if err != nil {
		t.Fatalf("api key: %v", err)
	}
	svc := New(client)
	r := gin.New()
	r.POST("/sync", svc.Middleware(ScopeSyncWrite), func(c *gin.Context) {
		id, ok := From(c)
		if !ok {
			c.Status(http.StatusInternalServerError)
			return
		}
		c.JSON(http.StatusOK, gin.H{"node_id": id.NodeID, "tenant_id": id.TenantID})
	})
	do := func(auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodPost, "/sync", nil)
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	if w := do("Bearer " + material.Plaintext); w.Code != http.StatusOK {
		t.Fatalf("valid key = %d %s", w.Code, w.Body.String())
	}
	used, err := client.NodeAPIKey.Get(t.Context(), keyRow.ID)
	if err != nil || used.LastUsedAt == nil {
		t.Fatalf("last_used_at not recorded: %+v err %v", used, err)
	}
	if w := do("Bearer wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong key = %d, want 401", w.Code)
	}
	if w := do("Basic YWRtaW46cGFzcw=="); w.Code != http.StatusUnauthorized {
		t.Fatalf("Basic header = %d, want 401", w.Code)
	}
	if w := do("Bearer eyJhbGciOiJIUzI1NiJ9.fake.jwt"); w.Code != http.StatusUnauthorized {
		t.Fatalf("JWT-like bearer = %d, want 401", w.Code)
	}

	// A key without the required scope is forbidden, not unauthenticated.
	readOnly, _ := GenerateKey()
	if _, err := client.NodeAPIKey.Create().
		SetNodeID(nodeRow.ID).SetKeyHash(readOnly.Hash).SetKeyPrefix(readOnly.Prefix).
		SetScopes([]string{ScopeSyncRead}).SetStatus(config.StatusActive).Save(t.Context()); err != nil {
		t.Fatalf("read key: %v", err)
	}
	if w := do("Bearer " + readOnly.Plaintext); w.Code != http.StatusForbidden {
		t.Fatalf("scope mismatch = %d, want 403", w.Code)
	}

	// Revocation is immediate.
	if _, err := client.NodeAPIKey.UpdateOneID(keyRow.ID).
		SetStatus("revoked").SetRevokedAt(time.Now()).Save(t.Context()); err != nil {
		t.Fatalf("revoke: %v", err)
	}
	if w := do("Bearer " + material.Plaintext); w.Code != http.StatusUnauthorized {
		t.Fatalf("revoked key = %d, want 401", w.Code)
	}

	// Disabled node is rejected even with a valid key.
	live, _ := GenerateKey()
	if _, err := client.NodeAPIKey.Create().
		SetNodeID(nodeRow.ID).SetKeyHash(live.Hash).SetKeyPrefix(live.Prefix).
		SetScopes([]string{ScopeSyncWrite}).SetStatus(config.StatusActive).Save(t.Context()); err != nil {
		t.Fatalf("live key: %v", err)
	}
	if _, err := client.Node.UpdateOneID(nodeRow.ID).SetStatus(config.StatusDisabled).Save(t.Context()); err != nil {
		t.Fatalf("disable node: %v", err)
	}
	if w := do("Bearer " + live.Plaintext); w.Code != http.StatusUnauthorized {
		t.Fatalf("disabled node = %d, want 401", w.Code)
	}
}

func TestNodeAPIKeyNeverLogged(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := newTestDB(t)
	tenantRow, _ := client.Tenant.Create().SetName("T").SetSlug("t").SetStatus(config.StatusActive).Save(t.Context())
	nodeRow, _ := client.Node.Create().SetName("n").SetTenantID(int64(tenantRow.ID)).SetStatus(config.StatusActive).Save(t.Context())
	material, _ := GenerateKey()
	var logs bytes.Buffer
	previous := slog.Default()
	slog.SetDefault(slog.New(slog.NewTextHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug})))
	defer slog.SetDefault(previous)

	r := gin.New()
	r.POST("/sync", New(client).Middleware(ScopeSyncWrite), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	req := httptest.NewRequest(http.MethodPost, "/sync", nil)
	req.Header.Set("Authorization", "Bearer "+material.Plaintext)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("unknown key = %d, want 401", w.Code)
	}
	if strings.Contains(logs.String(), material.Plaintext) {
		t.Fatalf("plaintext API key leaked into logs: %s", logs.String())
	}
	_ = nodeRow
}

func TestNodeAPIKeyBindsNodeAndTenant(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := newTestDB(t)
	t1, _ := client.Tenant.Create().SetName("T1").SetSlug("t1").SetStatus(config.StatusActive).Save(t.Context())
	t2, _ := client.Tenant.Create().SetName("T2").SetSlug("t2").SetStatus(config.StatusActive).Save(t.Context())
	n1, _ := client.Node.Create().SetName("n1").SetTenantID(int64(t1.ID)).SetStatus(config.StatusActive).Save(t.Context())
	n2, _ := client.Node.Create().SetName("n2").SetTenantID(int64(t2.ID)).SetStatus(config.StatusActive).Save(t.Context())
	makeKey := func(nodeID int) string {
		m, err := GenerateKey()
		if err != nil {
			t.Fatalf("key: %v", err)
		}
		if _, err := client.NodeAPIKey.Create().
			SetNodeID(nodeID).SetKeyHash(m.Hash).SetKeyPrefix(m.Prefix).
			SetScopes(DefaultScopes).SetStatus(config.StatusActive).Save(t.Context()); err != nil {
			t.Fatalf("key row: %v", err)
		}
		return m.Plaintext
	}
	k1, k2 := makeKey(n1.ID), makeKey(n2.ID)

	r := gin.New()
	r.POST("/sync", New(client).Middleware(ScopeSyncWrite), func(c *gin.Context) {
		id, _ := From(c)
		c.JSON(http.StatusOK, gin.H{"node_id": id.NodeID, "tenant_id": id.TenantID})
	})
	do := func(key string) (int, int64) {
		req := httptest.NewRequest(http.MethodPost, "/sync", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("auth = %d %s", w.Code, w.Body.String())
		}
		var body struct {
			NodeID   int   `json:"node_id"`
			TenantID int64 `json:"tenant_id"`
		}
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return body.NodeID, body.TenantID
	}
	if nodeID, tenantID := do(k1); nodeID != n1.ID || tenantID != int64(t1.ID) {
		t.Fatalf("key1 identity = %d/%d", nodeID, tenantID)
	}
	if nodeID, tenantID := do(k2); nodeID != n2.ID || tenantID != int64(t2.ID) {
		t.Fatalf("key2 identity = %d/%d", nodeID, tenantID)
	}
}

func TestGeneratedKeyPrefixMatchesPlaintext(t *testing.T) {
	for range 10 {
		m, err := GenerateKey()
		if err != nil {
			t.Fatalf("generate: %v", err)
		}
		if !strings.HasPrefix(m.Plaintext, m.Prefix) {
			t.Fatalf("prefix %q does not prefix plaintext %q", m.Prefix, m.Plaintext)
		}
		if len(m.Plaintext) < 32 {
			t.Fatalf("key too short: %d", len(m.Plaintext))
		}
	}
}

func TestNodeAPIKeyExpiryAndTenantStatus(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := newTestDB(t)
	tenantRow, _ := client.Tenant.Create().SetName("T").SetSlug("t").SetStatus(config.StatusActive).Save(t.Context())
	nodeRow, _ := client.Node.Create().SetName("n").SetTenantID(int64(tenantRow.ID)).SetStatus(config.StatusActive).Save(t.Context())

	newKey := func(expiresAt *time.Time, status string) string {
		t.Helper()
		m, err := GenerateKey()
		if err != nil {
			t.Fatalf("key: %v", err)
		}
		create := client.NodeAPIKey.Create().
			SetNodeID(nodeRow.ID).SetKeyHash(m.Hash).SetKeyPrefix(m.Prefix).
			SetScopes(DefaultScopes).SetStatus(status)
		if expiresAt != nil {
			create.SetExpiresAt(*expiresAt)
		}
		if _, err := create.Save(t.Context()); err != nil {
			t.Fatalf("key row: %v", err)
		}
		return m.Plaintext
	}

	r := gin.New()
	r.POST("/sync", New(client).Middleware(ScopeSyncWrite), func(c *gin.Context) { c.Status(http.StatusNoContent) })
	do := func(key string) int {
		req := httptest.NewRequest(http.MethodPost, "/sync", nil)
		req.Header.Set("Authorization", "Bearer "+key)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	past := time.Now().Add(-time.Hour)
	if code := do(newKey(&past, config.StatusActive)); code != http.StatusUnauthorized {
		t.Fatalf("expired key = %d, want 401", code)
	}
	future := time.Now().Add(time.Hour)
	if code := do(newKey(&future, config.StatusActive)); code != http.StatusNoContent {
		t.Fatalf("non-expired key = %d, want 204", code)
	}

	if _, err := client.Tenant.UpdateOneID(tenantRow.ID).SetStatus(config.StatusDisabled).Save(t.Context()); err != nil {
		t.Fatalf("disable tenant: %v", err)
	}
	if code := do(newKey(nil, config.StatusActive)); code != http.StatusUnauthorized {
		t.Fatalf("key for disabled tenant = %d, want 401", code)
	}
}

func TestNodeAPIKeyPersistsOnlyHashAndPrefix(t *testing.T) {
	client := newTestDB(t)
	tenantRow, _ := client.Tenant.Create().SetName("T").SetSlug("t").SetStatus(config.StatusActive).Save(t.Context())
	nodeRow, _ := client.Node.Create().SetName("n").SetTenantID(int64(tenantRow.ID)).SetStatus(config.StatusActive).Save(t.Context())
	m, err := GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	row, err := client.NodeAPIKey.Create().
		SetNodeID(nodeRow.ID).SetKeyHash(m.Hash).SetKeyPrefix(m.Prefix).
		SetScopes(DefaultScopes).SetStatus(config.StatusActive).Save(t.Context())
	if err != nil {
		t.Fatalf("create key: %v", err)
	}

	if row.KeyHash == m.Plaintext || row.KeyPrefix == m.Plaintext {
		t.Fatal("plaintext API key was persisted")
	}
	if row.KeyHash != HashKey(m.Plaintext) {
		t.Fatalf("stored hash = %q, want HashKey(plaintext)", row.KeyHash)
	}
	if len(row.KeyHash) != 64 {
		t.Fatalf("hash length = %d, want 64 hex chars", len(row.KeyHash))
	}
	if !strings.HasPrefix(m.Plaintext, row.KeyPrefix) || len(row.KeyPrefix) >= len(m.Plaintext) {
		t.Fatalf("prefix %q must be a short prefix of the plaintext", row.KeyPrefix)
	}
}

func TestNodeAPIKeyInvalidAttemptsAreRateLimited(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := newTestDB(t)
	r := gin.New()
	r.POST("/sync", New(client).Middleware(ScopeSyncWrite), func(c *gin.Context) { c.Status(http.StatusNoContent) })

	sawTooMany := false
	for range 30 {
		req := httptest.NewRequest(http.MethodPost, "/sync", nil)
		req.Header.Set("Authorization", "Bearer mrk_invalid")
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code == http.StatusTooManyRequests {
			sawTooMany = true
			break
		}
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("invalid key = %d, want 401 or 429", w.Code)
		}
	}
	if !sawTooMany {
		t.Fatal("repeated invalid keys were never rate limited")
	}
}
