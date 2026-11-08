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
	"manga-reader/internal/web"
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

	// authCacheTTL short-circuits repeated successful Basic Auth checks so
	// bcrypt/md5-crypt is not paid on every image and SSE request. A rotated
	// credential takes effect within this window.
	authCacheTTL = 30 * time.Second
	// maxAuthCacheEntries bounds the success cache; it is cleared wholesale
	// when full, which only costs a few extra hash verifications.
	maxAuthCacheEntries = 1024
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
			web.AbortWithStatusJSON(c, http.StatusTooManyRequests, gin.H{"error": "too many requests"})
			return
		}

		user, pass, ok := c.Request.BasicAuth()
		if !ok || !creds.verify(user, pass) {
			c.Header("WWW-Authenticate", fmt.Sprintf("Basic realm=%q, charset=%q", realm, "UTF-8"))
			web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Next()
	}
}

// verifier checks presented credentials against the configured source.
type verifier struct {
	username string
	password string
	// file is only used as a fallback when hashes was not preloaded (tests
	// construct the config directly).
	file string
	// hashes is the htpasswd content parsed once at startup.
	hashes map[string]string

	mu    sync.Mutex
	cache map[string]time.Time
}

func newVerifier(cfg config.BasicAuthConfig) *verifier {
	return &verifier{
		username: cfg.Username,
		password: cfg.Password,
		file:     cfg.File,
		hashes:   cfg.Hashes,
		cache:    make(map[string]time.Time),
	}
}

func (v *verifier) verify(user, pass string) bool {
	key := user + "\x00" + pass
	if v.cacheHit(key) {
		return true
	}
	ok := v.verifyUncached(user, pass)
	if ok {
		v.remember(key)
	}
	return ok
}

func (v *verifier) verifyUncached(user, pass string) bool {
	if v.file == "" && len(v.hashes) == 0 {
		userOK := subtle.ConstantTimeCompare([]byte(v.username), []byte(user)) == 1
		passOK := subtle.ConstantTimeCompare([]byte(v.password), []byte(pass)) == 1
		return userOK && passOK
	}
	hash, ok := v.hashes[user]
	if !ok && v.file != "" {
		hash, ok = lookup(v.file, user)
	}
	if !ok {
		return false
	}
	return verifyHash(hash, pass)
}

func (v *verifier) cacheHit(key string) bool {
	v.mu.Lock()
	defer v.mu.Unlock()
	expiry, ok := v.cache[key]
	if !ok {
		return false
	}
	if time.Now().After(expiry) {
		delete(v.cache, key)
		return false
	}
	return true
}

func (v *verifier) remember(key string) {
	v.mu.Lock()
	defer v.mu.Unlock()
	if len(v.cache) >= maxAuthCacheEntries {
		clear(v.cache)
	}
	v.cache[key] = time.Now().Add(authCacheTTL)
}

// lookup finds the htpasswd entry for user. Missing/unreadable files simply
// fail the request: auth must never fail open.
func lookup(path, user string) (string, bool) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	for line := range strings.SplitSeq(string(data), "\n") {
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
	mu      sync.Mutex
	rate    float64
	burst   float64
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
		clients: make(map[string]*bucket),
	}
}

func (l *limiter) allow(key string) bool {
	now := time.Now()

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.clients) > maxTrackedClients {
		l.sweep(now)
		// A fast flood can create more non-idle buckets than the cap within
		// the sweep window; evict arbitrary entries so the map stays bounded.
		for key := range l.clients {
			if len(l.clients) <= maxTrackedClients {
				break
			}
			delete(l.clients, key)
		}
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
