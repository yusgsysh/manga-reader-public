package node

import (
	"context"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ident"
)

const (
	ginKey     = "node.identity"
	contextKey = nodeContextKey("node.identity")
)

type nodeContextKey string

// Identity is the trusted peer behind an authenticated sync request. Node keys
// are instance-wide (UserID is nil); user sync keys carry the account they are
// allowed to replicate, so the host attributes every private row to that user.
type Identity struct {
	NodeID ident.UUID
	UserID ident.UUID
	Name   string
	Scopes []string
	KeyID  ident.UUID
}

// UserScoped reports whether the credential is bound to a single account.
func (i Identity) UserScoped() bool {
	return i.UserID != ident.Nil()
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

// HasScope reports whether the key includes a granted scope.
func (i Identity) HasScope(scope string) bool {
	for _, granted := range i.Scopes {
		if granted == scope {
			return true
		}
	}
	return false
}
