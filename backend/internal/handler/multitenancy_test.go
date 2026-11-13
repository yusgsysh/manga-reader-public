package handler

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	"manga-reader/internal/tenant"
)

type mtFixture struct {
	db       *database.DB
	server   *Server
	router   *gin.Engine
	tenantA  *ent.Tenant
	tenantB  *ent.Tenant
	userA    *ent.User
	userB    *ent.User
	adminA   *ent.User
	normalA  *ent.User
	tenantAA *ent.User
}

func newMTFixture(t *testing.T) *mtFixture {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := newTestDatabase(t)
	ctx := t.Context()

	tenantA, err := db.Client.Tenant.Create().
		SetName("Tenant A").SetSlug("tenant-a").SetStatus(config.StatusActive).Save(ctx)
	if err != nil {
		t.Fatalf("create tenant A: %v", err)
	}
	tenantB, err := db.Client.Tenant.Create().
		SetName("Tenant B").SetSlug("tenant-b").SetStatus(config.StatusActive).Save(ctx)
	if err != nil {
		t.Fatalf("create tenant B: %v", err)
	}

	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("hash password: %v", err)
	}
	createUser := func(tid int64, name, role string) *ent.User {
		u, err := db.Client.User.Create().
			SetTenantID(tid).SetUsername(name).SetPasswordHash(hash).
			SetRole(role).SetStatus(config.StatusActive).Save(ctx)
		if err != nil {
			t.Fatalf("create user %s: %v", name, err)
		}
		return u
	}
	adminA := createUser(int64(tenantA.ID), "admin-a", config.RolePlatformAdmin)
	tenantAA := createUser(int64(tenantA.ID), "tenant-admin-a", config.RoleTenantAdmin)
	normalA := createUser(int64(tenantA.ID), "user-a", config.RoleUser)
	userB := createUser(int64(tenantB.ID), "admin-b", config.RoleTenantAdmin)

	authSvc := auth.New(db.Client, config.AuthConfig{
		Enabled:       true,
		JWTSecret:     strings.Repeat("m", 32),
		JWTIssuer:     "mt-test",
		JWTExpiresIn:  time.Hour,
		AdminUsername: "ignored",
	})
	server := New(Config{DB: db, AuthEnabled: true})
	r := gin.New()
	r.Use(authSvc.Middleware())
	r.Use(tenant.New(db.Client, true).Middleware())
	authSvc.RegisterRoutes(r)
	server.RegisterRoutes(r)
	t.Cleanup(server.StopPrefill)

	return &mtFixture{
		db: db, server: server, router: r,
		tenantA: tenantA, tenantB: tenantB,
		userA: userB, userB: userB, adminA: adminA, normalA: normalA, tenantAA: tenantAA,
	}
}

func (f *mtFixture) do(t *testing.T, method, path string, body any, token string) *httptest.ResponseRecorder {
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

func (f *mtFixture) login(t *testing.T, tenantSlug, username, password string) (string, int) {
	t.Helper()
	w := f.do(t, http.MethodPost, "/api/auth/login", map[string]string{
		"tenant": tenantSlug, "username": username, "password": password,
	}, "")
	if w.Code != http.StatusOK {
		return "", w.Code
	}
	var res struct {
		AccessToken string `json:"access_token"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return res.AccessToken, w.Code
}

func TestMultiTenant_DataIsolation(t *testing.T) {
	f := newMTFixture(t)
	ctx := t.Context()

	// Same gallery reference exists in both tenants; each gets its own row.
	for _, tc := range []struct {
		tid int64
	}{
		{int64(f.tenantA.ID)},
		{int64(f.tenantB.ID)},
	} {
		if _, err := f.db.Client.Bookshelf.Create().
			SetTenantID(tc.tid).SetGalleryID(111).SetToken("same").Save(ctx); err != nil {
			t.Fatalf("seed bookshelf tenant %d: %v", tc.tid, err)
		}
	}
	// A-only row: tenant B must never see it.
	if _, err := f.db.Client.Bookshelf.Create().
		SetTenantID(int64(f.tenantA.ID)).SetGalleryID(222).SetToken("a-only").Save(ctx); err != nil {
		t.Fatalf("seed A-only bookshelf: %v", err)
	}
	if _, err := f.db.Client.ReadingProgress.Create().
		SetTenantID(int64(f.tenantA.ID)).SetGalleryID(111).SetToken("same").
		SetCurrentPage(5).SetProgress(0.5).Save(ctx); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	job, err := f.db.Client.PrefillJob.Create().
		SetTenantID(int64(f.tenantA.ID)).SetGalleryID(111).SetToken("same").
		SetTitle("A job").SetUrls([]string{"https://exhentai.org/s/a/111-1"}).
		SetStatus("completed").SetTotal(1).Save(ctx)
	if err != nil {
		t.Fatalf("seed prefill: %v", err)
	}

	tokenA, _ := f.login(t, "tenant-a", "admin-a", "password123")
	tokenB, _ := f.login(t, "tenant-b", "admin-b", "password123")
	if tokenA == "" || tokenB == "" {
		t.Fatal("tenant logins failed")
	}

	// A sees its bookshelf row; B sees only its own row (not A's extra data).
	var listA, listB struct {
		Total   int `json:"total"`
		Results []struct {
			ID int64 `json:"id"`
		} `json:"results"`
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/bookshelf", nil, tokenA).Body.Bytes(), &listA); err != nil {
		t.Fatalf("decode bookshelf A: %v", err)
	}
	if listA.Total != 2 {
		t.Fatalf("tenant A bookshelf = %+v, want 2 rows", listA)
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/bookshelf", nil, tokenB).Body.Bytes(), &listB); err != nil {
		t.Fatalf("decode bookshelf B: %v", err)
	}
	if listB.Total != 1 || listB.Results[0].ID != 111 {
		t.Fatalf("tenant B bookshelf = %+v, want only its own 1 row", listB)
	}

	// A progress is invisible to B and B cannot overwrite it.
	if got := f.do(t, http.MethodGet, "/api/progress/111/same", nil, tokenB); got.Code != http.StatusOK ||
		!strings.Contains(got.Body.String(), `"current_page":0`) {
		t.Fatalf("B read A progress: %d %s", got.Code, got.Body.String())
	}
	if got := f.do(t, http.MethodPut, "/api/progress/111/same", map[string]any{
		"current_page": 99, "progress": 0.99, "tenant_id": f.tenantB.ID,
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
		t.Fatalf("A progress changed to %d by tenant B", aProgress.CurrentPage)
	}

	// B deleting the same gallery reference must not touch A's row.
	if got := f.do(t, http.MethodDelete, "/api/bookshelf/111/same", nil, tokenB); got.Code != http.StatusOK {
		t.Fatalf("B delete bookshelf: %d", got.Code)
	}
	n, err := f.db.Client.Bookshelf.Query().Count(ctx)
	if err != nil || n != 2 {
		t.Fatalf("bookshelf rows after B delete = %d (err %v), want both A rows intact", n, err)
	}

	// Prefill jobs are tenant-private.
	if got := f.do(t, http.MethodGet, "/api/prefill/"+job.ID.String(), nil, tokenB); got.Code != http.StatusNotFound {
		t.Fatalf("B read A prefill = %d, want 404", got.Code)
	}
	if got := f.do(t, http.MethodDelete, "/api/prefill/"+job.ID.String(), nil, tokenB); got.Code != http.StatusNotFound {
		t.Fatalf("B delete A prefill = %d, want 404", got.Code)
	}
	var jobsA, jobsB struct {
		Jobs []any `json:"jobs"`
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/prefill", nil, tokenA).Body.Bytes(), &jobsA); err != nil {
		t.Fatalf("decode A prefill: %v", err)
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/prefill", nil, tokenB).Body.Bytes(), &jobsB); err != nil {
		t.Fatalf("decode B prefill: %v", err)
	}
	if len(jobsA.Jobs) != 1 || len(jobsB.Jobs) != 0 {
		t.Fatalf("prefill isolation: A=%d B=%d", len(jobsA.Jobs), len(jobsB.Jobs))
	}

	// A client-supplied tenant_id is ignored.
	got := f.do(t, http.MethodGet, "/api/bookshelf?tenant_id=2", nil, tokenA)
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"total":2`) {
		t.Fatalf("tenant_id query override leaked: %d %s", got.Code, got.Body.String())
	}
}

func TestMultiTenant_SameUsernameAndRoles(t *testing.T) {
	f := newMTFixture(t)
	ctx := t.Context()
	hash, err := auth.HashPassword("password123")
	if err != nil {
		t.Fatalf("hash: %v", err)
	}
	for _, tid := range []int64{int64(f.tenantA.ID), int64(f.tenantB.ID)} {
		if _, err := f.db.Client.User.Create().
			SetTenantID(tid).SetUsername("same-name").SetPasswordHash(hash).
			SetRole(config.RoleUser).SetStatus(config.StatusActive).Save(ctx); err != nil {
			t.Fatalf("create same-name user: %v", err)
		}
	}
	tokenA, _ := f.login(t, "tenant-a", "same-name", "password123")
	tokenB, _ := f.login(t, "tenant-b", "same-name", "password123")
	if tokenA == "" || tokenB == "" {
		t.Fatal("same username should be loginable in both tenants")
	}
	var meA, meB struct {
		TenantID int64 `json:"tenant_id"`
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/auth/me", nil, tokenA).Body.Bytes(), &meA); err != nil {
		t.Fatalf("decode meA: %v", err)
	}
	if err := json.Unmarshal(f.do(t, http.MethodGet, "/api/auth/me", nil, tokenB).Body.Bytes(), &meB); err != nil {
		t.Fatalf("decode meB: %v", err)
	}
	if meA.TenantID != int64(f.tenantA.ID) || meB.TenantID != int64(f.tenantB.ID) {
		t.Fatalf("tenant claim mismatch: A=%d B=%d", meA.TenantID, meB.TenantID)
	}

	normalToken, _ := f.login(t, "tenant-a", "user-a", "password123")
	tenantAdminToken, _ := f.login(t, "tenant-a", "tenant-admin-a", "password123")
	platformToken, _ := f.login(t, "tenant-a", "admin-a", "password123")
	if got := f.do(t, http.MethodGet, "/api/admin/tenants", nil, normalToken); got.Code != http.StatusForbidden {
		t.Fatalf("normal user admin tenants = %d, want 403", got.Code)
	}
	if got := f.do(t, http.MethodGet, "/api/tenant/users", nil, normalToken); got.Code != http.StatusForbidden {
		t.Fatalf("normal user tenant users = %d, want 403", got.Code)
	}
	if got := f.do(t, http.MethodGet, "/api/admin/tenants", nil, tenantAdminToken); got.Code != http.StatusForbidden {
		t.Fatalf("tenant admin platform API = %d, want 403", got.Code)
	}
	if got := f.do(t, http.MethodGet, "/api/admin/tenants", nil, platformToken); got.Code != http.StatusOK {
		t.Fatalf("platform admin tenants = %d, want 200", got.Code)
	}
	if got := f.do(t, http.MethodGet, "/api/tenant/users", nil, tenantAdminToken); got.Code != http.StatusOK {
		t.Fatalf("tenant admin users = %d, want 200", got.Code)
	}
	// Tenant admin A cannot touch tenant B's user through the path id.
	if got := f.do(t, http.MethodPut, "/api/tenant/users/"+strconv.Itoa(f.userB.ID), map[string]any{
		"username": "hijacked",
	}, tenantAdminToken); got.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant user update = %d, want 404", got.Code)
	}
	if got := f.do(t, http.MethodPatch, "/api/tenant/users/"+strconv.Itoa(f.userB.ID)+"/status", map[string]any{
		"status": "disabled",
	}, tenantAdminToken); got.Code != http.StatusNotFound {
		t.Fatalf("cross-tenant user status = %d, want 404", got.Code)
	}
}

func TestMultiTenant_JWTTamperAndDisabledTenant(t *testing.T) {
	f := newMTFixture(t)
	token, _ := f.login(t, "tenant-b", "admin-b", "password123")
	if token == "" {
		t.Fatal("login failed")
	}

	// Flip one byte in the payload: the HS256 signature must fail.
	parts := strings.Split(token, ".")
	payload := []byte(parts[1])
	if payload[0] == 'e' {
		payload[0] = 'f'
	} else {
		payload[0] = 'e'
	}
	tampered := parts[0] + "." + string(payload) + "." + parts[2]
	if got := f.do(t, http.MethodGet, "/api/bookshelf", nil, tampered); got.Code != http.StatusUnauthorized {
		t.Fatalf("tampered JWT = %d, want 401", got.Code)
	}

	platformToken, _ := f.login(t, "tenant-a", "admin-a", "password123")
	if got := f.do(t, http.MethodPatch, "/api/admin/tenants/"+strconv.Itoa(f.tenantB.ID)+"/status", map[string]any{
		"status": "disabled",
	}, platformToken); got.Code != http.StatusOK {
		t.Fatalf("disable tenant: %d %s", got.Code, got.Body.String())
	}
	if got := f.do(t, http.MethodGet, "/api/bookshelf", nil, token); got.Code != http.StatusForbidden {
		t.Fatalf("disabled tenant token = %d, want 403", got.Code)
	}
	if _, status := f.login(t, "tenant-b", "admin-b", "password123"); status != http.StatusUnauthorized {
		t.Fatalf("disabled tenant login = %d, want 401", status)
	}
}

func TestMultiTenant_AdminFlows(t *testing.T) {
	f := newMTFixture(t)
	platformToken, _ := f.login(t, "tenant-a", "admin-a", "password123")
	if platformToken == "" {
		t.Fatal("platform login failed")
	}

	w := f.do(t, http.MethodPost, "/api/admin/tenants", map[string]any{
		"name":           "Tenant C",
		"slug":           "tenant-c",
		"admin_username": "c-admin",
		"admin_password": "password123",
	}, platformToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("create tenant = %d %s", w.Code, w.Body.String())
	}
	var created struct {
		ID   int64  `json:"id"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &created); err != nil || created.Slug != "tenant-c" {
		t.Fatalf("decode created tenant: %v %s", err, w.Body.String())
	}

	adminToken, status := f.login(t, "tenant-c", "c-admin", "password123")
	if adminToken == "" || status != http.StatusOK {
		t.Fatalf("tenant C admin login = %d", status)
	}
	w = f.do(t, http.MethodPost, "/api/tenant/users", map[string]any{
		"username": "c-user", "password": "password123", "role": "user",
	}, adminToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("create tenant user = %d %s", w.Code, w.Body.String())
	}
	var createdUser struct {
		ID       int    `json:"id"`
		TenantID int64  `json:"tenant_id"`
		Username string `json:"username"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createdUser); err != nil {
		t.Fatalf("decode user: %v", err)
	}
	if createdUser.TenantID != created.ID {
		t.Fatalf("created user tenant = %d, want %d", createdUser.TenantID, created.ID)
	}

	w = f.do(t, http.MethodPut, "/api/tenant/users/"+strconv.Itoa(createdUser.ID), map[string]any{
		"password": "new-password-123",
	}, adminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("reset password = %d %s", w.Code, w.Body.String())
	}
	userToken, status := f.login(t, "tenant-c", "c-user", "new-password-123")
	if userToken == "" || status != http.StatusOK {
		t.Fatalf("login with reset password = %d", status)
	}

	w = f.do(t, http.MethodPatch, "/api/tenant/users/"+strconv.Itoa(createdUser.ID)+"/status", map[string]any{
		"status": "disabled",
	}, adminToken)
	if w.Code != http.StatusOK {
		t.Fatalf("disable user = %d %s", w.Code, w.Body.String())
	}
	if _, status := f.login(t, "tenant-c", "c-user", "new-password-123"); status != http.StatusUnauthorized {
		t.Fatalf("disabled user login = %d, want 401", status)
	}
	if got := f.do(t, http.MethodGet, "/api/bookshelf", nil, userToken); got.Code != http.StatusUnauthorized {
		t.Fatalf("disabled user token = %d, want 401", got.Code)
	}

	// Disabling the tenant blocks its admin token immediately.
	w = f.do(t, http.MethodPatch, "/api/admin/tenants/"+strconv.Itoa(int(created.ID))+"/status", map[string]any{
		"status": "disabled",
	}, platformToken)
	if w.Code != http.StatusOK {
		t.Fatalf("disable created tenant = %d", w.Code)
	}
	if got := f.do(t, http.MethodGet, "/api/tenant/users", nil, adminToken); got.Code != http.StatusForbidden {
		t.Fatalf("disabled tenant admin token = %d, want 403", got.Code)
	}
}

func TestMultiTenant_NodeAndAPIKeyAdmin(t *testing.T) {
	f := newMTFixture(t)
	platformToken, _ := f.login(t, "tenant-a", "admin-a", "password123")
	tenantAdminToken, _ := f.login(t, "tenant-a", "tenant-admin-a", "password123")
	if platformToken == "" || tenantAdminToken == "" {
		t.Fatal("logins failed")
	}

	if got := f.do(t, http.MethodGet, "/api/admin/nodes", nil, tenantAdminToken); got.Code != http.StatusForbidden {
		t.Fatalf("tenant admin list nodes = %d, want 403", got.Code)
	}

	w := f.do(t, http.MethodPost, "/api/admin/nodes", map[string]any{
		"name": "desktop-home", "tenant_id": f.tenantA.ID,
	}, platformToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("create node = %d %s", w.Code, w.Body.String())
	}
	var createdNode struct {
		ID       int    `json:"id"`
		TenantID int64  `json:"tenant_id"`
		Status   string `json:"status"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createdNode); err != nil || createdNode.Status != "active" {
		t.Fatalf("decode node: %v %s", err, w.Body.String())
	}
	if createdNode.TenantID != int64(f.tenantA.ID) {
		t.Fatalf("node tenant = %d, want %d", createdNode.TenantID, f.tenantA.ID)
	}

	w = f.do(t, http.MethodPost, "/api/admin/nodes/"+strconv.Itoa(createdNode.ID)+"/keys", map[string]any{
		"scopes": []string{"sync:read", "sync:write", "sync:events"},
	}, platformToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("create key = %d %s", w.Code, w.Body.String())
	}
	var createdKey struct {
		Key    string `json:"key"`
		APIKey struct {
			ID        int    `json:"id"`
			KeyPrefix string `json:"key_prefix"`
			Status    string `json:"status"`
		} `json:"api_key"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &createdKey); err != nil {
		t.Fatalf("decode key: %v", err)
	}
	if !strings.HasPrefix(createdKey.Key, "mrk_") || createdKey.APIKey.KeyPrefix == "" {
		t.Fatalf("unexpected key response: %+v", createdKey)
	}
	if !strings.HasPrefix(createdKey.Key, createdKey.APIKey.KeyPrefix) {
		t.Fatalf("key prefix %q does not prefix key %q", createdKey.APIKey.KeyPrefix, createdKey.Key)
	}

	// Listing keys must only expose the prefix, never the plaintext secret.
	w = f.do(t, http.MethodGet, "/api/admin/nodes/"+strconv.Itoa(createdNode.ID)+"/keys", nil, platformToken)
	if w.Code != http.StatusOK {
		t.Fatalf("list keys = %d", w.Code)
	}
	if strings.Contains(w.Body.String(), createdKey.Key) {
		t.Fatal("list keys leaked the full API key")
	}
	if !strings.Contains(w.Body.String(), createdKey.APIKey.KeyPrefix) {
		t.Fatal("list keys missing prefix")
	}

	// Rotation returns a new plaintext and keeps the old key active.
	w = f.do(t, http.MethodPost, "/api/admin/nodes/"+strconv.Itoa(createdNode.ID)+"/keys/"+strconv.Itoa(createdKey.APIKey.ID)+"/rotate", map[string]any{
		"revoke_old": false,
	}, platformToken)
	if w.Code != http.StatusCreated {
		t.Fatalf("rotate key = %d %s", w.Code, w.Body.String())
	}
	var rotated struct {
		Key string `json:"key"`
	}
	json.Unmarshal(w.Body.Bytes(), &rotated)
	if rotated.Key == "" || rotated.Key == createdKey.Key {
		t.Fatal("rotation did not return a fresh key")
	}

	// Revoking the old key is explicit.
	w = f.do(t, http.MethodDelete, "/api/admin/nodes/"+strconv.Itoa(createdNode.ID)+"/keys/"+strconv.Itoa(createdKey.APIKey.ID), nil, platformToken)
	if w.Code != http.StatusOK {
		t.Fatalf("revoke key = %d %s", w.Code, w.Body.String())
	}
	var revoked struct {
		Status string `json:"status"`
	}
	json.Unmarshal(w.Body.Bytes(), &revoked)
	if revoked.Status != "revoked" {
		t.Fatalf("revoked status = %q", revoked.Status)
	}

	// Deleting the node removes its key rows.
	w = f.do(t, http.MethodDelete, "/api/admin/nodes/"+strconv.Itoa(createdNode.ID), nil, platformToken)
	if w.Code != http.StatusOK {
		t.Fatalf("delete node = %d %s", w.Code, w.Body.String())
	}
	count, err := f.db.Client.NodeAPIKey.Query().Count(t.Context())
	if err != nil || count != 0 {
		t.Fatalf("keys after node delete = %d (err %v)", count, err)
	}
}
