package metrics

import (
	"crypto/subtle"
	"net"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

// AuthMiddleware protects the Prometheus endpoint with the operator-wide token
// from MANGA_READER_METRICS_TOKEN. With no token configured the endpoint stays
// loopback-only (the default safe deployment). A configured token is always
// required, including on loopback, so a wrong token can never fall back to
// "local access"; an explicit Authorization credential is always verified
// rather than silently ignored. User JWTs and sync keys are not metrics
// credentials.
func AuthMiddleware(token string) gin.HandlerFunc {
	want := []byte(strings.TrimSpace(token))
	return func(c *gin.Context) {
		header := strings.TrimSpace(c.GetHeader("Authorization"))
		got := bearerToken(header)
		if got == "" {
			got = strings.TrimSpace(c.GetHeader("X-Metrics-Token"))
		}

		if len(want) == 0 {
			if header == "" {
				if isLoopbackRequest(c.Request.RemoteAddr) {
					c.Next()
					return
				}
				c.AbortWithStatusJSON(http.StatusForbidden, gin.H{"error": "metrics are restricted to loopback"})
				return
			}
			unauthorizedMetrics(c)
			return
		}

		if got != "" && subtle.ConstantTimeCompare([]byte(got), want) == 1 {
			c.Next()
			return
		}
		unauthorizedMetrics(c)
	}
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
