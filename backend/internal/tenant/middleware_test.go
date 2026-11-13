package tenant

import (
	"database/sql"
	"net/http"
	"net/http/httptest"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	"github.com/gin-gonic/gin"
	_ "modernc.org/sqlite"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/ent"
)

// principalKey mirrors the private Gin key used by auth.Middleware. Tests
// attach a Principal directly so the tenant boundary can be exercised without
// minting a JWT; the JWT path itself is covered by the auth package.
const principalKeyForTest = "auth.principal"

func newTenantTestDB(t *testing.T) (*ent.Client, *ent.Tenant, *ent.Tenant) {
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

	active, err := client.Tenant.Create().
		SetName(config.DefaultTenantName).SetSlug(config.DefaultTenantSlug).
		SetStatus(config.StatusActive).Save(t.Context())
	if err != nil {
		t.Fatalf("create active tenant: %v", err)
	}
	disabled, err := client.Tenant.Create().
		SetName("Disabled").SetSlug("disabled").SetStatus(config.StatusDisabled).Save(t.Context())
	if err != nil {
		t.Fatalf("create disabled tenant: %v", err)
	}
	return client, active, disabled
}

type tenantProbe struct {
	status int
	scope  Scope
	has    bool
}

func newTenantRouter(client *ent.Client, authEnabled bool, principal *auth.Principal) (*gin.Engine, *tenantProbe) {
	gin.SetMode(gin.TestMode)
	probe := &tenantProbe{}
	r := gin.New()
	r.Use(func(c *gin.Context) {
		if principal != nil {
			c.Set(principalKeyForTest, *principal)
		}
		c.Next()
	})
	r.Use(New(client, authEnabled).Middleware())
	r.GET("/api/protected", func(c *gin.Context) {
		scope, ok := From(c)
		probe.scope, probe.has = scope, ok
		probe.status = http.StatusOK
		c.Status(http.StatusOK)
	})
	// /api/auth/login is in auth.publicAPIs, so the tenant middleware must not
	// require a principal for it.
	r.POST("/api/auth/login", func(c *gin.Context) {
		probe.scope, probe.has = From(c)
		probe.status = http.StatusOK
		c.Status(http.StatusOK)
	})
	r.GET("/api/sync/config", func(c *gin.Context) {
		probe.scope, probe.has = From(c)
		probe.status = http.StatusOK
		c.Status(http.StatusOK)
	})
	r.GET("/not-api", func(c *gin.Context) {
		probe.scope, probe.has = From(c)
		probe.status = http.StatusOK
		c.Status(http.StatusOK)
	})
	return r, probe
}

func serve(r http.Handler, method, path string) (int, *httptest.ResponseRecorder) {
	req := httptest.NewRequest(method, path, nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	return w.Code, w
}

func TestMiddlewareRequiresAuthenticatedPrincipal(t *testing.T) {
	client, _, _ := newTenantTestDB(t)
	r, _ := newTenantRouter(client, true, nil)

	if code, _ := serve(r, http.MethodGet, "/api/protected"); code != http.StatusUnauthorized {
		t.Fatalf("protected route without principal = %d, want 401", code)
	}
	if code, _ := serve(r, http.MethodPost, "/api/auth/login"); code != http.StatusOK {
		t.Fatalf("public route without principal = %d, want 200", code)
	}
	if code, _ := serve(r, http.MethodGet, "/not-api"); code != http.StatusOK {
		t.Fatalf("non-API route without principal = %d, want 200", code)
	}
}

func TestMiddlewareRejectsUnavailableOrDisabledTenant(t *testing.T) {
	client, _, disabled := newTenantTestDB(t)

	unknown, _ := newTenantRouter(client, true, &auth.Principal{
		ID: 1, TenantID: 999, Username: "ghost", Role: config.RoleUser,
	})
	if code, _ := serve(unknown, http.MethodGet, "/api/protected"); code != http.StatusForbidden {
		t.Fatalf("missing tenant = %d, want 403", code)
	}

	blocked, probe := newTenantRouter(client, true, &auth.Principal{
		ID: 1, TenantID: int64(disabled.ID), Username: "user", Role: config.RoleUser,
	})
	if code, _ := serve(blocked, http.MethodGet, "/api/protected"); code != http.StatusForbidden {
		t.Fatalf("disabled tenant = %d, want 403", code)
	}
	if probe.has {
		t.Fatal("scope attached for a disabled tenant")
	}
}

func TestMiddlewareAttachesTrustedScope(t *testing.T) {
	client, active, _ := newTenantTestDB(t)
	r, probe := newTenantRouter(client, true, &auth.Principal{
		ID: 42, TenantID: int64(active.ID), Username: "alice", Role: config.RoleUser,
	})

	if code, _ := serve(r, http.MethodGet, "/api/protected"); code != http.StatusOK {
		t.Fatalf("active tenant = %d, want 200", code)
	}
	if !probe.has {
		t.Fatal("tenant scope was not attached")
	}
	if probe.scope.ID != int64(active.ID) || probe.scope.Slug != config.DefaultTenantSlug ||
		probe.scope.Name != config.DefaultTenantName || probe.scope.Status != config.StatusActive {
		t.Fatalf("scope = %+v, want tenant %+v", probe.scope, active)
	}
	if probe.scope.UserID != 42 || probe.scope.Role != config.RoleUser {
		t.Fatalf("scope identity = %d/%q, want 42/user", probe.scope.UserID, probe.scope.Role)
	}
}

func TestMiddlewarePlatformOnlyRoutes(t *testing.T) {
	client, active, _ := newTenantTestDB(t)
	tests := []struct {
		name string
		role string
		want int
	}{
		{"platform admin", config.RolePlatformAdmin, http.StatusOK},
		{"tenant admin", config.RoleTenantAdmin, http.StatusForbidden},
		{"normal user", config.RoleUser, http.StatusForbidden},
		{"no role", "", http.StatusForbidden},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r, _ := newTenantRouter(client, true, &auth.Principal{
				ID: 1, TenantID: int64(active.ID), Username: "u", Role: tt.role,
			})
			if code, _ := serve(r, http.MethodGet, "/api/sync/config"); code != tt.want {
				t.Fatalf("sync config as %q = %d, want %d", tt.role, code, tt.want)
			}
		})
	}
}

func TestMiddlewareLegacyAnonymousModeUsesDefaultTenant(t *testing.T) {
	client, _, _ := newTenantTestDB(t)
	r, probe := newTenantRouter(client, false, nil)

	if code, _ := serve(r, http.MethodGet, "/api/protected"); code != http.StatusOK {
		t.Fatalf("legacy protected route = %d, want 200", code)
	}
	if !probe.has || probe.scope.ID != config.DefaultTenantID {
		t.Fatalf("legacy scope = %+v (has=%v), want default tenant %d", probe.scope, probe.has, config.DefaultTenantID)
	}
	if probe.scope.UserID != 0 || probe.scope.Role != "" {
		t.Fatalf("legacy identity = %d/%q, want anonymous", probe.scope.UserID, probe.scope.Role)
	}
}
