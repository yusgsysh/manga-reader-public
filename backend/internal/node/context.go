package node

import (
	"context"

	"github.com/gin-gonic/gin"
)

const (
	ginKey     = "node.identity"
	contextKey = nodeContextKey("node.identity")
)

type nodeContextKey string

// Identity is the trusted node behind an authenticated sync request. Nothing
// in it comes from request parameters; NodeID/TenantID/Scopes are resolved from
// the API key row and its node.
type Identity struct {
	NodeID   int
	Name     string
	TenantID int64
	Scopes   []string
	KeyID    int
}

// WithGin attaches the identity to the request.
func WithGin(c *gin.Context, identity Identity) {
	c.Set(ginKey, identity)
	c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), contextKey, identity))
}

// From returns the authenticated node identity.
func From(c *gin.Context) (Identity, bool) {
	value, ok := c.Get(ginKey)
	if !ok {
		return Identity{}, false
	}
	identity, ok := value.(Identity)
	return identity, ok
}

// FromContext is available to background code that receives the request
// context rather than the Gin context.
func FromContext(ctx context.Context) (Identity, bool) {
	identity, ok := ctx.Value(contextKey).(Identity)
	return identity, ok
}

// HasScope reports whether the key includes a granted scope.
func (i Identity) HasScope(scope string) bool {
	for _, granted := range i.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}
