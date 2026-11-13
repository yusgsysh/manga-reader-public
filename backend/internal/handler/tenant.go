package handler

import (
	"net/http"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
	"manga-reader/internal/tenant"
	"manga-reader/internal/web"
)

// requireTenant returns the trusted tenant ID for the request. Handlers must
// never read tenant_id from the client; this is the only accepted source.
func (s *Server) requireTenant(c *gin.Context) (int64, bool) {
	if scope, ok := tenant.From(c); ok && scope.ID > 0 {
		return scope.ID, true
	}
	if !s.authEnabled {
		// Legacy anonymous deployments and handler unit tests are explicitly
		// single-tenant; all pre-existing behavior maps to the default.
		return config.DefaultTenantID, true
	}
	web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "tenant context missing"})
	return 0, false
}

func (s *Server) requirePlatformAdmin(c *gin.Context) bool {
	scope, ok := tenant.From(c)
	if !ok || scope.Role != config.RolePlatformAdmin {
		web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "platform administrator required"})
		return false
	}
	return true
}

func (s *Server) requireTenantAdmin(c *gin.Context) bool {
	scope, ok := tenant.From(c)
	if !ok || scope.Role != config.RoleTenantAdmin {
		web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "tenant administrator required"})
		return false
	}
	return true
}
