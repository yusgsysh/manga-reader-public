// Package node implements node API key storage, verification and the
// Authorization: Bearer <node key> middleware used only by peer sync routes.
//
// It is deliberately separate from the JWT middleware: a user token and a
// node key are different credential classes and must never authenticate each
// other's routes.
package node

import (
	"crypto/subtle"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/nodeapikey"
	"manga-reader/internal/ent/usersynckey"
	"manga-reader/internal/requestid"
)

// Node API key scopes. A key must carry every scope required by the route.
// Sync scopes are granted by default; metrics access is opt-in so a key minted
// for sync cannot read the Prometheus surface without an explicit checkbox.
const (
	ScopeSyncRead    = "sync:read"
	ScopeSyncWrite   = "sync:write"
	ScopeSyncEvent   = "sync:events"
	ScopeMetricsRead = "metrics:read"
)

// DefaultScopes are granted to a newly minted key.
var DefaultScopes = []string{ScopeSyncRead, ScopeSyncWrite, ScopeSyncEvent}

// AllScopes is the accepted scope allow-list for management input.
var AllScopes = []string{ScopeSyncRead, ScopeSyncWrite, ScopeSyncEvent, ScopeMetricsRead}

// Service verifies node keys against the database.
type Service struct {
	db *ent.Client

	mu      sync.Mutex
	clients map[string]*bucket
}

type bucket struct {
	tokens float64
	last   time.Time
}

const (
	invalidRate  = 1.0
	invalidBurst = 20.0
)

// New builds a node authentication service.
func New(db *ent.Client) *Service {
	return &Service{db: db, clients: make(map[string]*bucket)}
}

// Middleware authenticates Authorization: Bearer <node API key> and requires
// every listed scope. No fallback to any other credential type exists.
func (s *Service) Middleware(requiredScopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		s.authenticate(c, requiredScopes, false)
	}
}

// AnyMiddleware accepts either a node API key (instance-wide) or a user sync
// key (one account). Sync routes use it so both peer models keep working; the
// resolved Identity tells the handler whether to scope the exchange to a user.
func (s *Service) AnyMiddleware(requiredScopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		s.authenticate(c, requiredScopes, true)
	}
}

func (s *Service) authenticate(c *gin.Context, requiredScopes []string, allowUserKeys bool) {
	raw, ok := nodeBearer(c.GetHeader("Authorization"))
	if !ok {
		s.reject(c, "missing bearer key")
		return
	}
	prefix := DisplayPrefix(raw)
	hash := HashKey(raw)
	now := time.Now().UTC()

	keys, err := s.db.NodeAPIKey.Query().
		Where(
			nodeapikey.KeyPrefixEQ(prefix),
			nodeapikey.StatusEQ(config.StatusActive),
		).
		All(c.Request.Context())
	if err != nil {
		s.reject(c, "key lookup failed")
		return
	}

	var matched *ent.NodeAPIKey
	for _, key := range keys {
		if subtle.ConstantTimeCompare([]byte(key.KeyHash), []byte(hash)) == 1 {
			matched = key
			break
		}
	}
	if matched != nil {
		s.finishNodeKey(c, matched, requiredScopes, now)
		return
	}
	if !allowUserKeys {
		s.reject(c, "key not found")
		return
	}

	userKeys, err := s.db.UserSyncKey.Query().
		Where(
			usersynckey.KeyPrefixEQ(prefix),
			usersynckey.StatusEQ(config.StatusActive),
		).
		All(c.Request.Context())
	if err != nil {
		s.reject(c, "key lookup failed")
		return
	}
	var matchedUser *ent.UserSyncKey
	for _, key := range userKeys {
		if subtle.ConstantTimeCompare([]byte(key.KeyHash), []byte(hash)) == 1 {
			matchedUser = key
			break
		}
	}
	if matchedUser == nil {
		s.reject(c, "key not found")
		return
	}
	s.finishUserKey(c, matchedUser, requiredScopes, now)
}

func (s *Service) finishNodeKey(c *gin.Context, matched *ent.NodeAPIKey, requiredScopes []string, now time.Time) {
	if matched.ExpiresAt != nil && !matched.ExpiresAt.After(now) {
		s.reject(c, "key expired")
		return
	}

	n, err := s.db.Node.Get(c.Request.Context(), matched.NodeID)
	if err != nil || n.Status != config.StatusActive {
		s.reject(c, "node disabled or missing")
		return
	}
	identity := Identity{
		NodeID: n.ID,
		Name:   n.Name,
		Scopes: append([]string(nil), matched.Scopes...),
		KeyID:  matched.ID,
	}
	for _, scope := range requiredScopes {
		if !identity.HasScope(scope) {
			webAbort(c, http.StatusForbidden, "api key scope not allowed")
			return
		}
	}

	// Best-effort usage bookkeeping; auth already succeeded.
	_, _ = s.db.NodeAPIKey.UpdateOneID(matched.ID).SetLastUsedAt(now).Save(c.Request.Context())
	_, _ = s.db.Node.UpdateOneID(n.ID).SetLastSeenAt(now).Save(c.Request.Context())

	WithGin(c, identity)
	c.Next()
}

func (s *Service) finishUserKey(c *gin.Context, matched *ent.UserSyncKey, requiredScopes []string, now time.Time) {
	if matched.ExpiresAt != nil && !matched.ExpiresAt.After(now) {
		s.reject(c, "key expired")
		return
	}
	u, err := s.db.User.Get(c.Request.Context(), matched.UserID)
	if err != nil || u.Status != config.StatusActive {
		s.reject(c, "user disabled or missing")
		return
	}
	// A user key is always a full sync credential for its account; node-only
	// scopes such as metrics:read are never granted.
	identity := Identity{
		UserID: matched.UserID,
		Name:   u.Username,
		Scopes: append([]string(nil), DefaultScopes...),
		KeyID:  matched.ID,
	}
	for _, scope := range requiredScopes {
		if !identity.HasScope(scope) {
			webAbort(c, http.StatusForbidden, "api key scope not allowed")
			return
		}
	}

	_, _ = s.db.UserSyncKey.UpdateOneID(matched.ID).SetLastUsedAt(now).Save(c.Request.Context())

	WithGin(c, identity)
	c.Next()
}

func (s *Service) reject(c *gin.Context, reason string) {
	if !s.allowAttempt(c.ClientIP()) {
		webAbort(c, http.StatusTooManyRequests, "too many invalid api keys")
		return
	}
	// Never include the presented credential. The request id links this line
	// to the access log without exposing a secret.
	slog.Warn("node api key rejected",
		"request_id", requestid.From(c),
		"reason", reason,
		"remote", c.ClientIP(),
	)
	webAbort(c, http.StatusUnauthorized, "unauthorized")
}

func (s *Service) allowAttempt(key string) bool {
	now := time.Now()
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.clients) > 4096 {
		for k, b := range s.clients {
			if now.Sub(b.last) > time.Minute {
				delete(s.clients, k)
			}
		}
	}
	b := s.clients[key]
	if b == nil {
		b = &bucket{tokens: invalidBurst, last: now}
		s.clients[key] = b
	}
	if elapsed := now.Sub(b.last).Seconds(); elapsed > 0 {
		b.tokens += elapsed * invalidRate
		if b.tokens > invalidBurst {
			b.tokens = invalidBurst
		}
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func nodeBearer(header string) (string, bool) {
	scheme, value, ok := strings.Cut(strings.TrimSpace(header), " ")
	if !ok || !strings.EqualFold(scheme, "Bearer") {
		return "", false
	}
	value = strings.TrimSpace(value)
	return value, value != ""
}

func webAbort(c *gin.Context, status int, message string) {
	c.AbortWithStatusJSON(status, gin.H{"error": message})
}
