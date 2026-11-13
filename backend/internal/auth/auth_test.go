package auth

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"

	entsql "entgo.io/ent/dialect/sql"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
)

func newTestClient(t *testing.T) *ent.Client {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("enable foreign keys: %v", err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", conn)))
	if err := client.Schema.Create(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

func testConfig(secret string) config.AuthConfig {
	return config.AuthConfig{
		JWTSecret:     secret,
		JWTIssuer:     "manga-reader-test",
		JWTExpiresIn:  time.Hour,
		AdminUsername: "admin",
		AdminPassword: "password123",
	}
}

func newTestService(t *testing.T) (*Service, *ent.Client) {
	t.Helper()
	client := newTestClient(t)
	svc := New(client, testConfig(strings.Repeat("k", 32)))
	if _, err := svc.EnsureAdmin(t.Context()); err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	return svc, client
}

func newTestRouter(svc *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(svc.Middleware())
	svc.RegisterRoutes(r)
	r.GET("/api/search", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"public": true}) })
	r.GET("/api/bookshelf", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"protected": true}) })
	return r
}

func doJSON(r http.Handler, method, path, body, token string) *httptest.ResponseRecorder {
	var reader *bytes.Reader
	if body == "" {
		reader = bytes.NewReader(nil)
	} else {
		reader = bytes.NewReader([]byte(body))
	}
	req := httptest.NewRequest(method, path, reader)
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func login(t *testing.T, r http.Handler, username, password string) (string, *httptest.ResponseRecorder) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": username, "password": password})
	w := doJSON(r, http.MethodPost, "/api/auth/login", string(body), "")
	if w.Code != http.StatusOK {
		return "", w
	}
	var res struct {
		AccessToken string `json:"access_token"`
		TokenType   string `json:"token_type"`
		ExpiresIn   int    `json:"expires_in"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	if res.TokenType != "Bearer" || res.AccessToken == "" || res.ExpiresIn <= 0 {
		t.Fatalf("unexpected login response: %s", w.Body.String())
	}
	return res.AccessToken, w
}

func TestHashPasswordAndVerify(t *testing.T) {
	hash, err := HashPassword("correct horse battery staple")
	if err != nil {
		t.Fatalf("HashPassword: %v", err)
	}
	if !strings.HasPrefix(hash, "$argon2id$v=19$") {
		t.Fatalf("hash = %q, want argon2id PHC", hash)
	}
	if strings.Contains(hash, "correct horse") {
		t.Fatal("hash contains the plaintext password")
	}
	if !VerifyPassword(hash, "correct horse battery staple") {
		t.Fatal("correct password did not verify")
	}
	if VerifyPassword(hash, "wrong") {
		t.Fatal("wrong password verified")
	}
	if VerifyPassword("not-a-hash", "anything") {
		t.Fatal("malformed hash verified")
	}
}

func TestLoginSuccessAndProtectedRoutes(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)

	token, w := login(t, r, "admin", "password123")
	if token == "" {
		t.Fatalf("login failed: %d %s", w.Code, w.Body.String())
	}
	cookies := w.Result().Cookies()
	if len(cookies) == 0 || cookies[0].Name != CookieName || !cookies[0].HttpOnly {
		t.Fatalf("login did not set an HttpOnly cookie: %+v", cookies)
	}

	if got := doJSON(r, http.MethodGet, "/api/auth/me", "", token); got.Code != http.StatusOK {
		t.Fatalf("/me = %d, want 200 (%s)", got.Code, got.Body.String())
	}
	if got := doJSON(r, http.MethodGet, "/api/bookshelf", "", token); got.Code != http.StatusOK {
		t.Fatalf("protected route with token = %d, want 200", got.Code)
	}
	if got := doJSON(r, http.MethodGet, "/api/bookshelf", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("protected route without token = %d, want 401", got.Code)
	}
	if got := doJSON(r, http.MethodGet, "/api/auth/status", "", ""); got.Code != http.StatusOK {
		t.Fatalf("public route = %d, want 200", got.Code)
	}
	if got := doJSON(r, http.MethodGet, "/api/search", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("user-private route without token = %d, want 401", got.Code)
	}
}

func TestLoginWrongPasswordAndUnknownUser(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)

	for _, tc := range []struct{ user, pass string }{
		{"admin", "wrong-password"},
		{"nobody", "password123"},
	} {
		body, _ := json.Marshal(map[string]string{"username": tc.user, "password": tc.pass})
		w := doJSON(r, http.MethodPost, "/api/auth/login", string(body), "")
		if w.Code != http.StatusUnauthorized {
			t.Fatalf("%s: status = %d, want 401", tc.user, w.Code)
		}
		if !strings.Contains(w.Body.String(), "invalid username or password") {
			t.Fatalf("%s: body = %s, want unified error", tc.user, w.Body.String())
		}
	}
}

// TestMiddlewareRejectsInvalidTokens covers every credential failure mode at
// the HTTP boundary: malformed, wrong signature, expired, wrong issuer and
// algorithm confusion. The valid case is the control so a failed setup cannot
// make the whole table pass.
func TestMiddlewareRejectsInvalidTokens(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)
	u, err := svc.db.User.Query().Only(t.Context())
	if err != nil {
		t.Fatalf("load user: %v", err)
	}

	valid, _ := login(t, r, "admin", "password123")
	if valid == "" {
		t.Fatal("control login failed")
	}

	wrongSigManager := newTokenManager(strings.Repeat("x", 32), svc.cfg.JWTIssuer, time.Hour)
	wrongSig, _, err := wrongSigManager.issue(u.ID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		t.Fatalf("issue wrong-signature token: %v", err)
	}
	expiredManager := newTokenManager(svc.cfg.JWTSecret, svc.cfg.JWTIssuer, time.Minute)
	expiredManager.now = func() time.Time { return time.Now().Add(-2 * time.Hour) }
	expired, _, err := expiredManager.issue(u.ID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		t.Fatalf("issue expired token: %v", err)
	}
	wrongIssuerManager := newTokenManager(svc.cfg.JWTSecret, "someone-else", time.Hour)
	wrongIssuer, _, err := wrongIssuerManager.issue(u.ID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		t.Fatalf("issue wrong-issuer token: %v", err)
	}

	tests := []struct {
		name  string
		token string
	}{
		{"missing", ""},
		{"not a jwt", "not-a-jwt"},
		{"wrong signature", wrongSig},
		{"expired", expired},
		{"wrong issuer", wrongIssuer},
		// Explicit non-HS256 variant signed with the same secret
		// (algorithm confusion / "none").
		{"alg none", "eyJhbGciOiJub25lIiwidHlwIjoiSldUIn0.eyJpc3MiOiJtYW5nYS1yZWFkZXItdGVzdCIsInN1YiI6IjEiLCJleHAiOjk5OTk5OTk5OTl9."},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := doJSON(r, http.MethodGet, "/api/bookshelf", "", tt.token); got.Code != http.StatusUnauthorized {
				t.Fatalf("token = %d, want 401 (%s)", got.Code, got.Body.String())
			}
		})
	}

	if got := doJSON(r, http.MethodGet, "/api/bookshelf", "", valid); got.Code != http.StatusOK {
		t.Fatalf("valid token = %d, want 200", got.Code)
	}
}

func TestLogoutRevokesOutstandingTokens(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)

	token, _ := login(t, r, "admin", "password123")
	if got := doJSON(r, http.MethodPost, "/api/auth/logout", "", token); got.Code != http.StatusOK {
		t.Fatalf("logout = %d, want 200 (%s)", got.Code, got.Body.String())
	}
	if got := doJSON(r, http.MethodGet, "/api/auth/me", "", token); got.Code != http.StatusUnauthorized {
		t.Fatalf("token after logout = %d, want 401", got.Code)
	}

	// A fresh login carries the incremented token_version and works again.
	newToken, _ := login(t, r, "admin", "password123")
	if newToken == "" {
		t.Fatal("login after logout failed")
	}
	if got := doJSON(r, http.MethodGet, "/api/auth/me", "", newToken); got.Code != http.StatusOK {
		t.Fatalf("new token = %d, want 200", got.Code)
	}
}

func TestEnsureAdminSkipsWhenNoPasswordConfigured(t *testing.T) {
	client := newTestClient(t)
	cfg := testConfig(strings.Repeat("k", 32))
	cfg.AdminPassword = ""
	svc := New(client, cfg)

	created, err := svc.EnsureAdmin(t.Context())
	if err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if created {
		t.Fatal("EnsureAdmin created an administrator without ADMIN_PASSWORD")
	}
	count, err := client.User.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("users = %d, want 0 (instance left uninitialized)", count)
	}
}

func TestRegisterCreatesFirstAdmin(t *testing.T) {
	client := newTestClient(t)
	svc := New(client, testConfig(strings.Repeat("k", 32)))
	r := newTestRouter(svc)

	w := doJSON(r, http.MethodGet, "/api/auth/status", "", "")
	var status struct {
		Initialized bool `json:"initialized"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.Initialized {
		t.Fatal("status = initialized, want uninitialized")
	}

	body, _ := json.Marshal(map[string]string{"username": "root", "password": "correct horse battery"})
	w = doJSON(r, http.MethodPost, "/api/auth/register", string(body), "")
	if w.Code != http.StatusOK {
		t.Fatalf("register = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var res struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil || res.AccessToken == "" {
		t.Fatalf("register response missing access_token: %s", w.Body.String())
	}

	u, err := client.User.Query().Only(t.Context())
	if err != nil {
		t.Fatalf("query user: %v", err)
	}
	if u.Username != "root" || u.Role != config.RoleAdmin || u.Status != config.StatusActive {
		t.Fatalf("unexpected admin: username=%q role=%q status=%q", u.Username, u.Role, u.Status)
	}
	if !VerifyPassword(u.PasswordHash, "correct horse battery") {
		t.Fatal("registered password does not verify")
	}

	// The new admin's token authenticates immediately.
	if got := doJSON(r, http.MethodGet, "/api/auth/me", "", res.AccessToken); got.Code != http.StatusOK {
		t.Fatalf("/me with registration token = %d, want 200", got.Code)
	}

	// A second registration is rejected once initialized.
	body2, _ := json.Marshal(map[string]string{"username": "other", "password": "another strong pass"})
	if got := doJSON(r, http.MethodPost, "/api/auth/register", string(body2), ""); got.Code != http.StatusConflict {
		t.Fatalf("second register = %d, want 409 (%s)", got.Code, got.Body.String())
	}
}

func TestRegisterValidatesInput(t *testing.T) {
	client := newTestClient(t)
	svc := New(client, testConfig(strings.Repeat("k", 32)))
	r := newTestRouter(svc)

	for _, tc := range []struct {
		name string
		body map[string]string
	}{
		{"empty username", map[string]string{"username": "  ", "password": "correct horse battery"}},
		{"short password", map[string]string{"username": "root", "password": "short"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload, _ := json.Marshal(tc.body)
			if got := doJSON(r, http.MethodPost, "/api/auth/register", string(payload), ""); got.Code != http.StatusBadRequest {
				t.Fatalf("register = %d, want 400 (%s)", got.Code, got.Body.String())
			}
		})
	}

	count, err := client.User.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 0 {
		t.Fatalf("users = %d, want 0 after rejected registrations", count)
	}
}

func TestEnsureAdminDoesNotOverwriteExistingPassword(t *testing.T) {
	svc, client := newTestService(t)

	changed := New(client, func() config.AuthConfig {
		cfg := testConfig(strings.Repeat("k", 32))
		cfg.AdminPassword = "another-password"
		return cfg
	}())
	created, err := changed.EnsureAdmin(t.Context())
	if err != nil {
		t.Fatalf("EnsureAdmin: %v", err)
	}
	if created {
		t.Fatal("EnsureAdmin created a second administrator")
	}
	u, err := client.User.Query().Only(t.Context())
	if err != nil {
		t.Fatalf("query user: %v", err)
	}
	if !VerifyPassword(u.PasswordHash, "password123") {
		t.Fatal("existing password was overwritten")
	}
	if VerifyPassword(u.PasswordHash, "another-password") {
		t.Fatal("new environment password replaced the stored hash")
	}
	_ = svc
}

func TestStatusIsPublicAndReportsInitialized(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)

	w := doJSON(r, http.MethodGet, "/api/auth/status", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	var res struct {
		Initialized bool `json:"initialized"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !res.Initialized {
		t.Fatalf("status = %+v, want initialized", res)
	}
}

func TestBasicAuthHeaderIsNotAccepted(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)

	req := httptest.NewRequest(http.MethodGet, "/api/bookshelf", nil)
	req.SetBasicAuth("admin", "password123")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("Basic auth header = %d, want 401", w.Code)
	}
}
