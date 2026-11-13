// Package requestid adds a correlation id to every request so security and
// audit log lines can be tied back to the HTTP request without logging
// credentials or full headers.
package requestid

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
)

const (
	header = "X-Request-ID"
	key    = requestContextKey("request.id")
)

type requestContextKey string

// Middleware reuses a caller-supplied request id when it is short and safe,
// otherwise generates a new UUID.
func Middleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		id := strings.TrimSpace(c.GetHeader(header))
		if id == "" || len(id) > 128 || strings.ContainsAny(id, "\r\n") {
			id = uuid.NewString()
		}
		c.Set(string(key), id)
		c.Request = c.Request.WithContext(context.WithValue(c.Request.Context(), key, id))
		c.Header(header, id)
		c.Next()
	}
}

// From returns the request id for a Gin request.
func From(c *gin.Context) string {
	if c == nil {
		return ""
	}
	if value, ok := c.Get(string(key)); ok {
		if id, ok := value.(string); ok {
			return id
		}
	}
	return ""
}

// FromContext returns the request id from a plain context.
func FromContext(ctx context.Context) string {
	if id, ok := ctx.Value(key).(string); ok {
		return id
	}
	return ""
}
