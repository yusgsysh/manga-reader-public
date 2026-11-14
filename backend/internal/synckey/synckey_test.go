package synckey

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ident"
)

func newTestClient(t *testing.T) *ent.Client {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open test db: %v", err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
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

func seedKey(t *testing.T, client *ent.Client, userID ident.UUID, status string, expires *time.Time) string {
	t.Helper()
	material, err := GenerateKey()
	if err != nil {
		t.Fatalf("generate key: %v", err)
	}
	create := client.UserSyncKey.Create().
		SetUserID(userID).
		SetKeyHash(material.Hash).
		SetKeyPrefix(material.Prefix).
		SetStatus(status)
	if expires != nil {
		create.SetExpiresAt(*expires)
	}
	if _, err := create.Save(t.Context()); err != nil {
		t.Fatalf("create key: %v", err)
	}
	return material.Plaintext
}

func TestMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	client := newTestClient(t)
	user, err := client.User.Create().
		SetUsername("admin").SetPasswordHash("x").SetStatus(config.StatusActive).
		Save(t.Context())
	if err != nil {
		t.Fatalf("create user: %v", err)
	}

	valid := seedKey(t, client, user.ID, config.StatusActive, nil)
	revoked := seedKey(t, client, user.ID, "revoked", nil)
	expiredAt := time.Now().Add(-time.Hour)
	expired := seedKey(t, client, user.ID, config.StatusActive, &expiredAt)

	svc := New(client)
	r := gin.New()
	r.GET("/sync", svc.Middleware(ScopeSyncWrite), func(c *gin.Context) {
		identity, ok := From(c)
		if !ok || identity.UserID != user.ID {
			c.AbortWithStatusJSON(http.StatusInternalServerError, gin.H{"error": "identity missing"})
			return
		}
		c.Status(http.StatusNoContent)
	})

	do := func(token string) int {
		req := httptest.NewRequest(http.MethodGet, "/sync", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w.Code
	}

	if code := do(""); code != http.StatusUnauthorized {
		t.Fatalf("no key = %d, want 401", code)
	}
	if code := do("mrk_wrong"); code != http.StatusUnauthorized {
		t.Fatalf("wrong key = %d, want 401", code)
	}
	if code := do(revoked); code != http.StatusUnauthorized {
		t.Fatalf("revoked key = %d, want 401", code)
	}
	if code := do(expired); code != http.StatusUnauthorized {
		t.Fatalf("expired key = %d, want 401", code)
	}
	if code := do(valid); code != http.StatusNoContent {
		t.Fatalf("valid key = %d, want 204", code)
	}

	// A disabled owner invalidates the key even while the key row is active.
	if _, err := client.User.UpdateOneID(user.ID).SetStatus(config.StatusDisabled).Save(t.Context()); err != nil {
		t.Fatalf("disable user: %v", err)
	}
	if code := do(valid); code != http.StatusUnauthorized {
		t.Fatalf("disabled user key = %d, want 401", code)
	}
}

func TestGenerateKeyShape(t *testing.T) {
	material, err := GenerateKey()
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if len(material.Plaintext) < len(KeyPrefix)+32 {
		t.Fatalf("plaintext too short: %q", material.Plaintext)
	}
	if material.Prefix != material.Plaintext[:len(material.Prefix)] {
		t.Fatalf("prefix %q is not a prefix of the key", material.Prefix)
	}
	if material.Hash != HashKey(material.Plaintext) {
		t.Fatal("hash mismatch")
	}
}
