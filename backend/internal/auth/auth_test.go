package auth

import (
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"

	entsql "entgo.io/ent/dialect/sql"

	"manga-reader/internal/authtest"
	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/user"
)

const testAudience = "https://api.manga.example.com"

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

func newTestService(t *testing.T, adminEmails ...string) (*Service, *ent.Client, *authtest.Issuer) {
	t.Helper()
	client := newTestClient(t)
	iss := authtest.NewIssuer(t, testAudience)
	verifier, err := NewVerifier(t.Context(), iss.URL, testAudience)
	if err != nil {
		t.Fatalf("NewVerifier: %v", err)
	}
	return New(client, config.AuthConfig{}, verifier, adminEmails, nil), client, iss
}

func newTestRouter(svc *Service) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(svc.Middleware())
	svc.RegisterRoutes(r)
	r.GET("/healthz", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"status": "ok"}) })
	r.GET("/api/bookshelf", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"ok": true}) })
	r.POST("/api/sync/push", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"sync": true}) })
	r.GET("/api/image/page", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"image": true}) })
	r.GET("/api/events/changes", func(c *gin.Context) { c.JSON(http.StatusOK, gin.H{"events": true}) })
	return r
}

func doRequest(r http.Handler, method, path, token string, cookies ...*http.Cookie) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, nil)
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	for _, cookie := range cookies {
		req.AddCookie(cookie)
	}
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w
}

func TestMiddlewareRejectsMissingAndMalformedAuthorization(t *testing.T) {
	svc, _, _ := newTestService(t)
	r := newTestRouter(svc)

	cases := []struct {
		name   string
		header string
	}{
		{"missing", ""},
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"empty bearer", "Bearer "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/api/bookshelf", nil)
			if tc.header != "" {
				req.Header.Set("Authorization", tc.header)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", w.Code)
			}
			if !strings.Contains(w.Body.String(), `"error":"unauthorized"`) {
				t.Fatalf("body = %s", w.Body.String())
			}
		})
	}
}

func TestVerifierRejectsInvalidTokens(t *testing.T) {
	svc, _, iss := newTestService(t)
	r := newTestRouter(svc)

	otherKey := authtest.NewIssuer(t, testAudience)

	cases := []struct {
		name  string
		token func(t *testing.T) string
	}{
		{"garbage", func(t *testing.T) string { return "not.a.jwt" }},
		{"wrong signature", func(t *testing.T) string {
			return iss.Sign(t, authtest.Claims{Key: otherKey.Key})
		}},
		{"unknown key id", func(t *testing.T) string {
			return iss.Sign(t, authtest.Claims{KeyID: "missing-key"})
		}},
		{"wrong issuer", func(t *testing.T) string {
			return iss.Sign(t, authtest.Claims{Issuer: "https://evil.example.com"})
		}},
		{"wrong audience", func(t *testing.T) string {
			return iss.Sign(t, authtest.Claims{Audience: "https://other-api.example.com"})
		}},
		{"expired", func(t *testing.T) string {
			return iss.Sign(t, authtest.Claims{
				IssuedAt:  time.Now().Add(-2 * time.Hour),
				ExpiresAt: time.Now().Add(-time.Hour),
			})
		}},
		{"not yet valid", func(t *testing.T) string {
			return iss.Sign(t, authtest.Claims{
				NotBefore: time.Now().Add(time.Hour),
				ExpiresAt: time.Now().Add(2 * time.Hour),
			})
		}},
		{"missing expiry", func(t *testing.T) string {
			return iss.Sign(t, authtest.Claims{NoExpiry: true})
		}},
		{"unsigned", func(t *testing.T) string {
			return iss.Sign(t, authtest.Claims{Method: methodNoneForTest()})
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			w := doRequest(r, http.MethodGet, "/api/bookshelf", tc.token(t))
			if w.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401 (%s)", w.Code, w.Body.String())
			}
			if strings.Contains(w.Body.String(), "kid") || strings.Contains(w.Body.String(), "signature") {
				t.Fatalf("verification internals leaked: %s", w.Body.String())
			}
		})
	}
}

func TestFirstLoginCreatesLocalUserAndMeReturnsIt(t *testing.T) {
	svc, client, iss := newTestService(t)
	r := newTestRouter(svc)

	token := iss.Sign(t, authtest.Claims{
		Subject:       "kp_alice",
		Email:         "alice@example.com",
		EmailVerified: true,
		Name:          "Alice",
		Picture:       "https://cdn.example.com/alice.png",
	})

	w := doRequest(r, http.MethodGet, "/api/auth/me", token)
	if w.Code != http.StatusOK {
		t.Fatalf("me = %d %s", w.Code, w.Body.String())
	}
	var me struct {
		ID        string  `json:"id"`
		UserID    string  `json:"user_id"`
		Email     string  `json:"email"`
		Name      string  `json:"name"`
		AvatarURL *string `json:"avatar_url"`
		Status    string  `json:"status"`
		Role      string  `json:"role"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &me); err != nil {
		t.Fatalf("decode me: %v", err)
	}
	if me.ID == "" || me.UserID != "kp_alice" {
		t.Fatalf("identity = %+v, want local id and user_id=kp_alice", me)
	}
	if me.Email != "alice@example.com" || me.Name != "Alice" || me.AvatarURL == nil {
		t.Fatalf("profile = %+v", me)
	}
	if me.Status != config.StatusActive || me.Role != config.RoleUser {
		t.Fatalf("status/role = %q/%q", me.Status, me.Role)
	}

	rows, err := client.User.Query().Where(user.UserIDEQ("kp_alice")).All(t.Context())
	if err != nil {
		t.Fatalf("query users: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("users = %d, want 1", len(rows))
	}

	// A second login must reuse the row, not create another.
	doRequest(r, http.MethodGet, "/api/auth/me", token)
	count, _ := client.User.Query().Count(t.Context())
	if count != 1 {
		t.Fatalf("users after second login = %d, want 1", count)
	}
}

func TestConcurrentFirstLoginCreatesOneUser(t *testing.T) {
	svc, client, iss := newTestService(t)
	r := newTestRouter(svc)
	token := iss.Sign(t, authtest.Claims{Subject: "kp_concurrent"})

	const requests = 12
	var wg sync.WaitGroup
	codes := make([]int, requests)
	for i := 0; i < requests; i++ {
		wg.Add(1)
		go func(index int) {
			defer wg.Done()
			codes[index] = doRequest(r, http.MethodGet, "/api/auth/me", token).Code
		}(i)
	}
	wg.Wait()

	for _, code := range codes {
		if code != http.StatusOK {
			t.Fatalf("concurrent login status = %d, want 200", code)
		}
	}
	count, err := client.User.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count users: %v", err)
	}
	if count != 1 {
		t.Fatalf("users = %d, want 1", count)
	}
}

func TestDisabledUserGetsForbidden(t *testing.T) {
	svc, client, iss := newTestService(t)
	r := newTestRouter(svc)
	token := iss.Sign(t, authtest.Claims{Subject: "kp_disabled"})

	if w := doRequest(r, http.MethodGet, "/api/bookshelf", token); w.Code != http.StatusOK {
		t.Fatalf("initial login = %d %s", w.Code, w.Body.String())
	}
	if _, err := client.User.Update().
		Where(user.UserIDEQ("kp_disabled")).
		SetStatus(config.StatusDisabled).
		Save(t.Context()); err != nil {
		t.Fatalf("disable user: %v", err)
	}

	w := doRequest(r, http.MethodGet, "/api/bookshelf", token)
	if w.Code != http.StatusForbidden {
		t.Fatalf("disabled status = %d, want 403 (%s)", w.Code, w.Body.String())
	}
	if !strings.Contains(w.Body.String(), `"error":"forbidden"`) {
		t.Fatalf("body = %s", w.Body.String())
	}
}

func TestPublicRoutesNeedNoToken(t *testing.T) {
	svc, _, _ := newTestService(t)
	r := newTestRouter(svc)

	if w := doRequest(r, http.MethodGet, "/healthz", ""); w.Code != http.StatusOK {
		t.Fatalf("healthz = %d", w.Code)
	}
	if w := doRequest(r, http.MethodPost, "/api/sync/push", ""); w.Code != http.StatusOK {
		t.Fatalf("sync push = %d", w.Code)
	}
}

func TestMediaCookieIsLimitedToNativeGetRoutes(t *testing.T) {
	svc, _, iss := newTestService(t)
	r := newTestRouter(svc)
	token := iss.Sign(t, authtest.Claims{Subject: "kp_cookie"})

	// A normal bearer call sets the bridge cookie.
	w := doRequest(r, http.MethodGet, "/api/bookshelf", token)
	if w.Code != http.StatusOK {
		t.Fatalf("bearer call = %d", w.Code)
	}
	found := false
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == CookieName && cookie.Value == token {
			found = true
			if !cookie.HttpOnly {
				t.Fatal("bridge cookie is not HttpOnly")
			}
			if cookie.Path != "/api" {
				t.Fatalf("cookie path = %q, want /api", cookie.Path)
			}
		}
	}
	if !found {
		t.Fatal("bearer call did not set the bridge cookie")
	}
	cookie := &http.Cookie{Name: CookieName, Value: token}

	// Native image and SSE routes accept the cookie.
	if got := doRequest(r, http.MethodGet, "/api/image/page", "", cookie).Code; got != http.StatusOK {
		t.Fatalf("image via cookie = %d", got)
	}
	if got := doRequest(r, http.MethodGet, "/api/events/changes", "", cookie).Code; got != http.StatusOK {
		t.Fatalf("events via cookie = %d", got)
	}
	// Business and state-changing routes never accept the cookie.
	if got := doRequest(r, http.MethodGet, "/api/bookshelf", "", cookie).Code; got != http.StatusUnauthorized {
		t.Fatalf("bookshelf via cookie = %d, want 401", got)
	}
	if got := doRequest(r, http.MethodPost, "/api/auth/logout", "", cookie).Code; got != http.StatusUnauthorized {
		t.Fatalf("logout via cookie = %d, want 401", got)
	}
}

func TestLogoutBumpsTokenVersionAndClearsCookie(t *testing.T) {
	svc, client, iss := newTestService(t)
	r := newTestRouter(svc)
	token := iss.Sign(t, authtest.Claims{Subject: "kp_logout"})

	if w := doRequest(r, http.MethodGet, "/api/bookshelf", token); w.Code != http.StatusOK {
		t.Fatalf("login = %d", w.Code)
	}
	w := doRequest(r, http.MethodPost, "/api/auth/logout", token)
	if w.Code != http.StatusOK {
		t.Fatalf("logout = %d %s", w.Code, w.Body.String())
	}
	cleared := false
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == CookieName && cookie.MaxAge < 0 {
			cleared = true
		}
	}
	if !cleared {
		t.Fatal("logout did not clear the bridge cookie")
	}
	u, err := client.User.Query().Where(user.UserIDEQ("kp_logout")).Only(t.Context())
	if err != nil {
		t.Fatalf("query user: %v", err)
	}
	if u.TokenVersion != 1 {
		t.Fatalf("token_version = %d, want 1", u.TokenVersion)
	}
}

func TestAdminAllowlistPromotesVerifiedEmailOnly(t *testing.T) {
	svc, client, iss := newTestService(t, "boss@example.com")
	r := newTestRouter(svc)

	verified := iss.Sign(t, authtest.Claims{
		Subject:       "kp_boss",
		Email:         "boss@example.com",
		EmailVerified: true,
	})
	if w := doRequest(r, http.MethodGet, "/api/bookshelf", verified); w.Code != http.StatusOK {
		t.Fatalf("verified admin login = %d %s", w.Code, w.Body.String())
	}
	u, _ := client.User.Query().Where(user.UserIDEQ("kp_boss")).Only(t.Context())
	if u.Role != config.RoleAdmin {
		t.Fatalf("role = %q, want admin", u.Role)
	}

	unverified := iss.Sign(t, authtest.Claims{
		Subject:       "kp_unverified",
		Email:         "boss@example.com",
		EmailVerified: false,
	})
	if w := doRequest(r, http.MethodGet, "/api/bookshelf", unverified); w.Code != http.StatusOK {
		t.Fatalf("unverified login = %d %s", w.Code, w.Body.String())
	}
	u2, _ := client.User.Query().Where(user.UserIDEQ("kp_unverified")).Only(t.Context())
	if u2.Role != config.RoleUser {
		t.Fatalf("unverified role = %q, want user", u2.Role)
	}
}

func TestMeIgnoresClientSuppliedUserID(t *testing.T) {
	svc, _, iss := newTestService(t)
	r := newTestRouter(svc)
	token := iss.Sign(t, authtest.Claims{Subject: "kp_owner"})

	w := doRequest(r, http.MethodGet, "/api/auth/me?user_id=kp_someone_else", token)
	if w.Code != http.StatusOK {
		t.Fatalf("me = %d", w.Code)
	}
	var me struct {
		UserID string `json:"user_id"`
	}
	_ = json.Unmarshal(w.Body.Bytes(), &me)
	if me.UserID != "kp_owner" {
		t.Fatalf("user_id = %q, want kp_owner", me.UserID)
	}
}
