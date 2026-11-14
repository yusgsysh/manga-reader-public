package metrics

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestMetricsAuthMiddleware(t *testing.T) {
	gin.SetMode(gin.TestMode)
	newRouter := func(token string, nodeAuth gin.HandlerFunc) *gin.Engine {
		r := gin.New()
		r.GET("/metrics", AuthMiddleware(token, nodeAuth), func(c *gin.Context) {
			c.String(http.StatusOK, "metrics")
		})
		return r
	}
	do := func(r *gin.Engine, remote, auth string) *httptest.ResponseRecorder {
		req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
		req.RemoteAddr = remote
		if auth != "" {
			req.Header.Set("Authorization", auth)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}
	const nodeKey = "mrk_node_key"
	nodeOK := func(c *gin.Context) {
		if c.GetHeader("Authorization") != "Bearer "+nodeKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
	nodeDeny := func(c *gin.Context) {
		c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
	}

	loopback := newRouter("", nodeOK)
	if w := do(loopback, "127.0.0.1:1234", ""); w.Code != http.StatusOK {
		t.Fatalf("loopback without token = %d, want 200", w.Code)
	}
	if w := do(loopback, "192.0.2.10:1234", ""); w.Code != http.StatusForbidden {
		t.Fatalf("public without token = %d, want 403", w.Code)
	}
	if w := do(loopback, "127.0.0.1:1234", "Bearer mrk_node_key"); w.Code != http.StatusOK {
		t.Fatalf("loopback with node key = %d, want 200", w.Code)
	}

	const token = "metrics-secret-token-0123456789abcdef"
	guarded := newRouter(token, nodeOK)
	if w := do(guarded, "127.0.0.1:1234", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("loopback without configured token = %d, want 401", w.Code)
	}
	if w := do(guarded, "192.0.2.10:1234", "Bearer wrong"); w.Code != http.StatusUnauthorized {
		t.Fatalf("wrong token = %d, want 401", w.Code)
	}
	if w := do(guarded, "192.0.2.10:1234", "Basic not-base64"); w.Code != http.StatusUnauthorized {
		t.Fatalf("Basic token = %d, want 401", w.Code)
	}
	if w := do(guarded, "192.0.2.10:1234", "Bearer "+token); w.Code != http.StatusOK {
		t.Fatalf("valid token = %d, want 200", w.Code)
	}
	if w := do(guarded, "192.0.2.10:1234", "Bearer mrk_node_key"); w.Code != http.StatusOK {
		t.Fatalf("node key fallback = %d, want 200", w.Code)
	}
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	req.RemoteAddr = "192.0.2.10:1234"
	req.Header.Set("X-Metrics-Token", token)
	w := httptest.NewRecorder()
	guarded.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("X-Metrics-Token = %d, want 200", w.Code)
	}

	rejected := newRouter(token, nodeDeny)
	if w := do(rejected, "192.0.2.10:1234", "Bearer mrk_bad_key"); w.Code != http.StatusUnauthorized {
		t.Fatalf("node key denied = %d, want 401", w.Code)
	}

	// Without a node-auth middleware a non-matching bearer must not slip through.
	noFallback := newRouter(token, nil)
	if w := do(noFallback, "192.0.2.10:1234", "Bearer mrk_node_key"); w.Code != http.StatusUnauthorized {
		t.Fatalf("missing node auth = %d, want 401", w.Code)
	}
}
