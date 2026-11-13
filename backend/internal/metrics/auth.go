package metrics

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware protects the Prometheus endpoint with credentials that are
// completely independent from user JWTs and node API keys.
//
// With no token configured the endpoint is loopback-only (the default safe
// deployment); a configured token is always required, including on loopback,
// so a wrong token can never fall back to "local access".
func AuthMiddleware(token string) gin.HandlerFunc {
	want := []byte(strings.TrimSpace(token))
	return func(c *gin.Context) {
		if len(want) == 0 {
			if isLoopbackRequest(c.Request.RemoteAddr) {
				c.Next()
				return
			}
			c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "metrics are restricted to loopback"})
			return
		}

		got := bearerToken(c.GetHeader("Authorization"))
		if got == "" {
			got = strings.TrimSpace(c.GetHeader("X-Metrics-Token"))
		}
		if got == "" || subtle.ConstantTimeCompare([]byte(got), want) != 1 {
			c.Header("WWW-Authenticate", `Bearer realm="metrics"`)
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid metrics token"})
			return
		}
		c.Next()
	}
}

func bearerToken(header string) string {
	scheme, value, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return ""
	}
	return strings.TrimSpace(value)
}

func isLoopbackRequest(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		host = remoteAddr
	}
	ip := net.ParseIP(strings.Trim(host, "[]"))
	return ip != nil && ip.IsLoopback()
}
