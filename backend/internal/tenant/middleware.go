package tenant

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/web"
)

// Service validates the tenant referenced by the verified JWT on every
// tenant-owned API request.
type Service struct {
	db          *ent.Client
	authEnabled bool
}

// New builds the tenant middleware. When JWT auth is disabled the legacy
// anonymous deployment keeps working against the default tenant, but no
// administrative role is granted.
func New(db *ent.Client, authEnabled bool) *Service {
	return &Service{db: db, authEnabled: authEnabled}
}

// Middleware must run after auth.Middleware. It turns the JWT tenant claim
// into the trusted Scope used by data access, and rejects missing, deleted or
// disabled tenants.
func (s *Service) Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		route := c.FullPath()
		if route == "" || !strings.HasPrefix(route, "/api/") {
			c.Next()
			return
		}

		if s.authEnabled {
			if auth.IsPublicAPI(route) {
				c.Next()
				return
			}
			principal, ok := auth.PrincipalFrom(c)
			if !ok {
				web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
				return
			}
			s.scopeFor(c, principal.TenantID, principal.ID, principal.Role)
			return
		}

		// Legacy anonymous mode: all business data lives in the default
		// tenant. Admin role checks still fail because Role is empty.
		s.scopeFor(c, config.DefaultTenantID, 0, "")
	}
}

// platformOnlyAPIs are global administrative surfaces: a tenant admin or
// ordinary user must not change settings that affect every tenant.
var platformOnlyAPIs = map[string]struct{}{
	"/api/sync/config":       {},
	"/api/sync/status":       {},
	"/api/dev/upstream-down": {},
}

func (s *Service) scopeFor(c *gin.Context, tenantID int64, userID int, role string) {
	t, err := s.db.Tenant.Get(c.Request.Context(), int(tenantID))
	if err != nil {
		// Do not reveal whether the tenant was deleted vs never existed.
		web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "tenant unavailable"})
		return
	}
	if t.Status != config.StatusActive {
		web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "tenant disabled"})
		return
	}
	if s.authEnabled {
		if _, global := platformOnlyAPIs[c.FullPath()]; global && role != config.RolePlatformAdmin {
			web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "platform administrator required"})
			return
		}
	}
	WithGin(c, Scope{
		ID:     int64(t.ID),
		Slug:   t.Slug,
		Name:   t.Name,
		Status: t.Status,
		UserID: userID,
		Role:   role,
	})
	c.Next()
}
