package handler

import (
	"bytes"
	json "encoding/json/v2"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ident"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/user"
	"manga-reader/internal/ent/usersetting"
	"manga-reader/internal/usercookie"
)

type muFixture struct {
	db     *ent.Client
	router *gin.Engine
	userA  *ent.User
	userB  *ent.User
	admin  *ent.User
	cookie *usercookie.Service
}

func newMUFixture(t *testing.T) *muFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := newTestDB(t)
	ctx := t.Context()
	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	create := func(name, role string) *ent.User {
		u, err := db.User.Create().
			SetUsername(name).SetPasswordHash(hash).
			SetRole(role).SetStatus(config.StatusActive).Save(ctx)
		if err != nil {
			t.Fatalf("create user %s: %v", name, err)
		}
		return u
	}
	userA := create("user-a", config.RoleUser)
	userB := create("user-b", config.RoleUser)
	admin := create("admin-a", config.RoleAdmin)

	secret := strings.Repeat("m", 32)
	authSvc := auth.New(db, config.AuthConfig{
		JWTSecret:     secret,
		JWTIssuer:     "mu-test",
		JWTExpiresIn:  time.Hour,
		AdminUsername: "ignored",
	})
	cookies := usercookie.New(db, secret)
	server := New(Config{DB: &database.DB{Client: db}, UserCookies: cookies})
	r := gin.New()
	r.Use(authSvc.Middleware())
	authSvc.RegisterRoutes(r)
	server.RegisterRoutes(r)
	t.Cleanup(server.StopPrefill)

	return &muFixture{db: db, router: r, userA: userA, userB: userB, admin: admin, cookie: cookies}
}

func (f *muFixture) do(t *testing.T, method, path string, body any, token string) *httptest.ResponseRecorder {
	t.Helper()
	var reader *bytes.Reader
	if body == nil {
		reader = bytes.NewReader(nil)
	} else {
		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("marshal body: %v", err)
		}
		reader = bytes.NewReader(data)
	}
	req := httptest.NewRequest(method, path, reader)
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	w := httptest.NewRecorder()
	f.router.ServeHTTP(w, req)
	return w
}

func (f *muFixture) login(t *testing.T, username string) string {
	t.Helper()
	w := f.do(t, http.MethodPost, "/api/auth/login", map[string]string{
		"username": username, "password": "password123",
	}, "")
	if w.Code != http.StatusOK {
		t.Fatalf("login %s = %d %s", username, w.Code, w.Body.String())
	}
	var res struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return res.AccessToken
}

func TestMultiUserBusinessDataIsolation(t *testing.T) {
	f := newMUFixture(t)
	ctx := t.Context()
	for _, u := range []ident.UUID{f.userA.ID, f.userB.ID} {
		if _, err := f.db.Bookshelf.Create().SetUserID(u).SetGalleryID(111).SetToken("same").Save(ctx); err != nil {
			t.Fatalf("seed bookshelf user %d: %v", u, err)
		}
	}
	if _, err := f.db.Bookshelf.Create().SetUserID(f.userA.ID).SetGalleryID(222).SetToken("a-only").Save(ctx); err != nil {
		t.Fatalf("seed A-only bookshelf: %v", err)
	}
	if _, err := f.db.ReadingProgress.Create().
		SetUserID(f.userA.ID).SetGalleryID(111).SetToken("same").
		SetCurrentPage(5).SetProgress(0.5).Save(ctx); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	job, err := f.db.PrefillJob.Create().
		SetUserID(f.userA.ID).SetGalleryID(111).SetToken("same").
		SetTitle("A job").SetUrls([]string{"https://exhentai.org/s/a/111-1"}).
		SetStatus("completed").SetTotal(1).Save(ctx)
	if err != nil {
		t.Fatalf("seed prefill: %v", err)
	}
	if _, err := f.db.TagPreference.Create().
		SetUserID(f.userA.ID).SetID("female:yuri").
		SetAction("like").SetWeight(10).Save(ctx); err != nil {
		t.Fatalf("seed tag: %v", err)
	}

	tokenA, tokenB := f.login(t, "user-a"), f.login(t, "user-b")

	var listA, listB struct {
		Total   int `json:"total"`
		Results []struct {
			ID int64 `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/bookshelf", nil, tokenA).Body.Bytes(), &listA); err != nil {
		t.Fatalf("decode A bookshelf: %v", err)
	}
	if listA.Total != 2 {
		t.Fatalf("A bookshelf total = %d, want 2", listA.Total)
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/bookshelf", nil, tokenB).Body.Bytes(), &listB); err != nil {
		t.Fatalf("decode B bookshelf: %v", err)
	}
	if listB.Total != 1 || listB.Results[0].ID != 111 {
		t.Fatalf("B bookshelf = %+v, want only its own row", listB)
	}

	// B cannot read or overwrite A's progress.
	if got := f.do(t, http.MethodGet, "/api/progress/111/same", nil, tokenB); got.Code != http.StatusOK ||
		!strings.Contains(got.Body.String(), `"current_page":0`) {
		t.Fatalf("B read A progress: %d %s", got.Code, got.Body.String())
	}
	if got := f.do(t, http.MethodPut, "/api/progress/111/same", map[string]any{
		"current_page": 99, "progress": 0.99, "user_id": f.userB.ID,
	}, tokenB); got.Code != http.StatusOK {
		t.Fatalf("B update own progress: %d %s", got.Code, got.Body.String())
	}
	var aProgress struct {
		CurrentPage int `json:"current_page"`
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/progress/111/same", nil, tokenA).Body.Bytes(), &aProgress); err != nil {
		t.Fatalf("decode A progress: %v", err)
	}
	if aProgress.CurrentPage != 5 {
		t.Fatalf("A progress changed to %d by B", aProgress.CurrentPage)
	}

	// B deleting the same gallery reference leaves A's row intact.
	if got := f.do(t, http.MethodDelete, "/api/bookshelf/111/same", nil, tokenB); got.Code != http.StatusOK {
		t.Fatalf("B delete bookshelf: %d", got.Code)
	}
	if n, err := f.db.Bookshelf.Query().Count(ctx); err != nil || n != 2 {
		t.Fatalf("bookshelf rows after B delete = %d (err %v), want A's 2 rows", n, err)
	}

	// Prefill jobs are user-private.
	if got := f.do(t, http.MethodGet, "/api/prefill/"+job.ID.String(), nil, tokenB); got.Code != http.StatusNotFound {
		t.Fatalf("B read A prefill = %d, want 404", got.Code)
	}
	if got := f.do(t, http.MethodDelete, "/api/prefill/"+job.ID.String(), nil, tokenB); got.Code != http.StatusNotFound {
		t.Fatalf("B delete A prefill = %d, want 404", got.Code)
	}

	// Tag preferences are user-private.
	var tagA, tagB struct {
		Tags []struct {
			Tag string `json:"tag"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/tag-settings", nil, tokenA).Body.Bytes(), &tagA); err != nil {
		t.Fatalf("decode A tags: %v", err)
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/tag-settings", nil, tokenB).Body.Bytes(), &tagB); err != nil {
		t.Fatalf("decode B tags: %v", err)
	}
	if len(tagA.Tags) != 1 || len(tagB.Tags) != 0 {
		t.Fatalf("tag isolation: A=%d B=%d", len(tagA.Tags), len(tagB.Tags))
	}
}

func TestMultiUserCookieAndSettingsIsolation(t *testing.T) {
	f := newMUFixture(t)
	tokenA, tokenB := f.login(t, "user-a"), f.login(t, "user-b")

	cookieA := "ipb_member_id=111; ipb_pass_hash=aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa; igneous=aaa"
	cookieB := "ipb_member_id=222; ipb_pass_hash=bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb; sk=bbb"

	for _, tc := range []struct {
		token, cookie, userID string
	}{
		{tokenA, cookieA, "111"},
		{tokenB, cookieB, "222"},
	} {
		w := f.do(t, http.MethodPut, "/api/settings/cookie", map[string]string{"cookie": tc.cookie}, tc.token)
		if w.Code != http.StatusOK {
			t.Fatalf("put cookie = %d %s", w.Code, w.Body.String())
		}
		if strings.Contains(w.Body.String(), "ipb_pass_hash") || strings.Contains(w.Body.String(), "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa") {
			t.Fatalf("cookie secret leaked in response: %s", w.Body.String())
		}
	}

	// Stored rows are encrypted, not the plaintext cookie.
	rowA, err := f.db.UserSetting.Query().Where(usersetting.IDEQ(f.userA.ID)).Only(t.Context())
	if err != nil {
		t.Fatalf("read A settings: %v", err)
	}
	if rowA.CookieCiphertext == "" || strings.Contains(rowA.CookieCiphertext, "ipb_pass_hash") {
		t.Fatalf("cookie not encrypted at rest: %q", rowA.CookieCiphertext)
	}
	clientA, err := f.cookie.Client(t.Context(), f.userA.ID)
	if err != nil {
		t.Fatalf("A client: %v", err)
	}
	clientB, err := f.cookie.Client(t.Context(), f.userB.ID)
	if err != nil {
		t.Fatalf("B client: %v", err)
	}
	if clientA == clientB {
		t.Fatal("two users share one HTTP client / cookie jar")
	}

	// Personal preferences are isolated too.
	prefsA := map[string]any{"preferences": map[string]any{"reader": map[string]any{"direction": "rtl"}}, "site": "e-hentai"}
	if w := f.do(t, http.MethodPut, "/api/settings", prefsA, tokenA); w.Code != http.StatusOK {
		t.Fatalf("put A settings = %d %s", w.Code, w.Body.String())
	}
	var settingsB struct {
		Preferences map[string]any `json:"preferences"`
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/settings", nil, tokenB).Body.Bytes(), &settingsB); err != nil {
		t.Fatalf("decode B settings: %v", err)
	}
	if len(settingsB.Preferences) != 0 {
		t.Fatalf("B saw A preferences: %+v", settingsB.Preferences)
	}

	// Clearing A's cookie leaves B's session intact.
	if w := f.do(t, http.MethodDelete, "/api/settings/cookie", nil, tokenA); w.Code != http.StatusOK {
		t.Fatalf("clear A cookie = %d", w.Code)
	}
	var statusB usercookie.Summary
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/settings/cookie", nil, tokenB).Body.Bytes(), &statusB); err != nil {
		t.Fatalf("decode B cookie: %v", err)
	}
	if !statusB.Configured || !statusB.HasIgneous && !statusB.HasSK {
		t.Fatalf("B cookie damaged by A clear: %+v", statusB)
	}
}

func TestMultiUserAdminIsolation(t *testing.T) {
	f := newMUFixture(t)
	userToken, adminToken := f.login(t, "user-a"), f.login(t, "admin-a")

	if got := f.do(t, http.MethodGet, "/api/admin/users", nil, userToken); got.Code != http.StatusForbidden {
		t.Fatalf("normal user admin list = %d, want 403", got.Code)
	}
	w := f.do(t, http.MethodGet, "/api/admin/users", nil, adminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("admin list = %d %s", w.Code, w.Body.String())
	}
	var body struct {
		Users []struct {
			ID       ident.UUID `json:"id"`
			Username string     `json:"username"`
		} `json:"users"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode users: %v", err)
	}
	if len(body.Users) != 3 {
		t.Fatalf("admin list = %+v, want 3 users", body.Users)
	}

	// Admin-created accounts default to the ordinary user role.
	if got := f.do(t, http.MethodPost, "/api/admin/users", map[string]string{
		"username": "new-user", "password": "new-password-1",
	}, adminToken); got.Code != http.StatusCreated {
		t.Fatalf("create user = %d %s", got.Code, got.Body.String())
	}
	created, err := f.db.User.Query().Where(user.UsernameEQ("new-user")).Only(t.Context())
	if err != nil || created.Role != config.RoleUser {
		t.Fatalf("created user = %+v err %v", created, err)
	}
}
