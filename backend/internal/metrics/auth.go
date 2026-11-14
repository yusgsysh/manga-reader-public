package metrics

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware protects the Prometheus endpoint. Two credential classes are
// accepted, and neither is a user JWT:
//
//   - the operator-wide token from MANGA_READER_METRICS_TOKEN, or
//   - a node API key carrying metrics:read, verified by nodeAuth — the same
//     middleware as /api/sync/*, so keys are issued, rotated and revoked from
//     the settings page.
//
// With no token configured and no Authorization header the endpoint stays
// loopback-only (the default safe deployment). A configured token is always
// required, including on loopback, so a wrong token can never fall back to
// "local access"; an explicit Authorization credential is always verified
// rather than silently ignored.
func AuthMiddleware(token string, nodeAuth gin.HandlerFunc) gin.HandlerFunc {
	want := []byte(strings.TrimSpace(token))
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		got := bearerToken(header)
		if got == "" {
			got = strings.TrimSpace(c.GetHeader("X-Metrics-Token"))
		}

		if len(want) == 0 {
			// No operator token: an explicit Authorization credential must be a
			// node key; otherwise keep the loopback-only fallback.
			if header == "" {
				if isLoopbackRequest(c.Request.RemoteAddr) {
					c.Next()
					return
				}
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "metrics are restricted to loopback"})
				return
			}
			useNodeAuth(c, nodeAuth)
			return
		}

		if got != "" && subtle.ConstantTimeCompare([]byte(got), want) == 1 {
			c.Next()
			return
		}
		if header != "" {
			useNodeAuth(c, nodeAuth)
			return
		}
		unauthorizedMetrics(c)
	}
}

func useNodeAuth(c *gin.Context, nodeAuth gin.HandlerFunc) {
	if nodeAuth == nil {
		unauthorizedMetrics(c)
		return
	}
	nodeAuth(c)
}

func unauthorizedMetrics(c *gin.Context) {
	c.Header("WWW-Authenticate", `Bearer realm="metrics"`)
	c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "invalid metrics token"})
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
