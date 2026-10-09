// Package basicauth guards the whole app (everything but /healthz) with HTTP
// Basic Auth. It replaced the reverse-proxy layer that used to sit in front of
// the API and the frontend — Gin now serves both from a single image.
//
// Credentials come from exactly one source, validated fail-closed by
// config.Load(): a single user in the environment, or an htpasswd file
// (apr1 / md5crypt / bcrypt).
package basicauth

import (
	"crypto/subtle"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
)

const (
	realm = "Manga Reader"

	// healthzPath stays reachable without credentials so container
	// healthchecks and external monitors keep working.
	healthzPath = "/healthz"

	// Rate limit mirrors the old proxy policy (50 req/s per client, burst
	// 200, over-limit answered with 429) so Basic Auth keeps its basic flood
	// and brute-force protection.
	limiterRate  = 50
	limiterBurst = 200

	// maxTrackedClients bounds the limiter table; beyond it idle entries are
	// swept instead of growing forever.
	maxTrackedClients = 8192
)

// Middleware returns the Basic Auth handler. A disabled config yields a
// pass-through so call sites do not need an `if` of their own.
func Middleware(cfg config.BasicAuthConfig) gin.HandlerFunc {
	if !cfg.Enabled {
		return func(c *gin.Context) { c.Next() }
	}

	creds := newVerifier(cfg)
	lim := newLimiter(limiterRate, limiterBurst)

	return func(c *gin.Context) {
		if c.Request.URL.Path == healthzPath {
			c.Next()
			return
		}

		if !lim.allow(c.ClientIP()) {
			c.Header("Retry-After", "1")
			c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}

		user, pass, ok := c.Request.BasicAuth()
		if !ok || !creds.verify(user, pass) {
			c.Header("WWW-Authenticate", fmt.Sprintf("Basic realm=%q, charset=%q", realm, "UTF-8"))
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

// verifier checks presented credentials against the configured source.
type verifier struct {
	username string
	password string
	// file is a world-readable htpasswd file, re-read per request so a
	// rotated secret takes effect without a restart.
	file string
}

func newVerifier(cfg config.BasicAuthConfig) *verifier {
	return &verifier{username: cfg.Username, password: cfg.Password, file: cfg.File}
}

func (v *verifier) verify(user, pass string) bool {
	if v.file == "" {
		userOK := subtle.ConstantTimeCompare([]byte(v.username), []byte(user)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(v.password), []byte(pass)) == 1
		return userOK && passOK
	}
	hash, ok := lookup(v.file, user)
	if !ok {
		return false
	}
	return verifyHash(hash, pass)
}

// lookup finds the htpasswd entry for user. Missing/unreadable files simply
// fail the request: auth must never fail open.
func lookup(path, user string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimRight(line, "\r")
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		name, hash, ok := strings.Cut(line, ":")
		if !ok || name != user {
			continue
		}
		return hash, true
	}
	return "", false
}

// limiter is a per-client token bucket (50 tokens/s, capacity 200).
type limiter struct {
	mu    sync.Mutex
	rate  float64
	burst float64
	// now is the clock; tests replace it to exercise refill without
	// sleeping on wall time.
	now     func() time.Time
	clients map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

func newLimiter(rate, burst float64) *limiter {
	return &limiter{
		rate:    rate,
		burst:   burst,
		now:     time.Now,
		clients: make(map[string]*bucket),
	}
}

func (l *limiter) allow(key string) bool {
	now := l.now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.clients) > maxTrackedClients {
		l.sweep(now)
	}

	b := l.clients[key]
	if b == nil {
		b = &bucket{tokens: l.burst, last: now}
		l.clients[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * l.rate
		if b.tokens > l.burst {
			b.tokens = l.burst
		}
	}
	b.last = now

	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

// sweep drops buckets that have been idle for at least a minute, which is far
// longer than the burst window, so an evicted client restarts full anyway.
func (l *limiter) sweep(now time.Time) {
	for key, b := range l.clients {
		if now.Sub(b.last) > time.Minute {
			delete(l.clients, key)
		}
	}
}
