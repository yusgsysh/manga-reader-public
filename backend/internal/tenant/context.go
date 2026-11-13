// Package tenant provides the authenticated tenant scope and the middleware
// that validates it on every tenant-owned API request.
//
// Handlers must never read tenant_id from a query string, header or body.
// They take it from Scope (set from the verified JWT) and include it in every
// query/create/update/delete predicate.
package tenant

import (
	"context"

	"github.com/gin-gonic/gin"
)

const (
	ginKey     = "tenant.scope"
	contextKey = tenantContextKey("tenant.scope")
)

type tenantContextKey string

// Scope is the trusted identity of the current request: the JWT user mapped to
// a live, enabled tenant. It is attached only after auth.Middleware has
// verified the token.
type Scope struct {
	ID     int64
	Slug   string
	Name   string
	Status string
	UserID int
	Role   string
}

// WithGin stores the scope in Gin's request context.
func WithGin(c *gin.Context, scope Scope) {
	c.Set(ginKey, scope)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), contextKey, scope))
}

// From returns the tenant scope attached by Middleware.
func From(c *gin.Context) (Scope, bool) {
	value, ok := c.Get(ginKey)
	if !ok {
		return Scope{}, false
	}
	scope, ok := value.(Scope)
	return scope, ok
}
