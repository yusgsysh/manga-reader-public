// Package synckey implements per-user sync keys: storage, verification and
// the Authorization: Bearer middleware used only by the peer sync routes.
//
// A sync key is bound to one account. The host attributes every private row
// from that peer to the account and only returns that account's changes, so
// two instances no longer need matching usernames or user UUIDs. The key is
// deliberately separate from the JWT middleware: a user token and a sync key
// are different credential classes and never authenticate each other's routes.
package synckey

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/usersynckey"
	"manga-reader/internal/ident"
	"manga-reader/internal/requestid"
)

// Sync scopes required by the peer routes. A user sync key always carries
// both; there is no instance-wide credential anymore.
const (
	ScopeSyncWrite = "sync:write"
	ScopeSyncEvent = "sync:events"
)

// KeyPrefix makes leaked keys recognisable in logs/scanners without revealing
// the secret.
const KeyPrefix = "mrk_"

// KeyMaterial is the one-time result of minting a sync key. Plaintext must
// never be persisted; only Hash and Prefix belong in the database.
type KeyMaterial struct {
	Plaintext string
	Hash      string
	Prefix    string
}

// GenerateKey creates at least 256 bits of randomness. The plaintext is
// returned exactly once, while the database stores only its SHA-256 hash and a
// short display prefix.
func GenerateKey() (KeyMaterial, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return KeyMaterial{}, err
	}
	plaintext := KeyPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return KeyMaterial{
		Plaintext: plaintext,
		Hash:      HashKey(plaintext),
		Prefix:    DisplayPrefix(plaintext),
	}, nil
}

// HashKey is the stable server-side representation of a key.
func HashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// DisplayPrefix returns a short non-secret identifier for the UI.
func DisplayPrefix(plaintext string) string {
	const n = 12
	if len(plaintext) <= n {
		return plaintext
	}
	return plaintext[:n]
}

// Identity is the trusted account behind an authenticated sync request. Every
// field comes from the key row and its user, never from request parameters.
type Identity struct {
	UserID ident.UUID
	Name   string
	Scopes []string
	KeyID  ident.UUID
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

// Service verifies user sync keys against the database.
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

// New builds a sync key authentication service.
func New(db *ent.Client) *Service {
	return &Service{db: db, clients: make(map[string]*bucket)}
}

// Middleware authenticates Authorization: Bearer <user sync key> and requires
// every listed scope. No fallback to any other credential type exists.
func (s *Service) Middleware(requiredScopes ...string) gin.HandlerFunc {
	return func(c *gin.Context) {
		raw, ok := keyBearer(c.GetHeader("Authorization"))
		if !ok {
			s.reject(c, "missing bearer key")
			return
		}
		prefix := DisplayPrefix(raw)
		hash := HashKey(raw)

		keys, err := s.db.UserSyncKey.Query().
			Where(
				usersynckey.KeyPrefixEQ(prefix),
				usersynckey.StatusEQ(config.StatusActive),
			).
			All(c.Request.Context())
		if err != nil {
			s.reject(c, "key lookup failed")
			return
		}
		var matched *ent.UserSyncKey
		for _, key := range keys {
			if subtle.ConstantTimeCompare([]byte(key.KeyHash), []byte(hash)) == 1 {
				matched = key
				break
			}
		}
		if matched == nil {
			s.reject(c, "key not found")
			return
		}
		now := time.Now().UTC()
		if matched.ExpiresAt != nil && !matched.ExpiresAt.After(now) {
			s.reject(c, "key expired")
			return
		}
		u, err := s.db.User.Get(c.Request.Context(), matched.UserID)
		if err != nil || u.Status != config.StatusActive {
			s.reject(c, "user disabled or missing")
			return
		}

		identity := Identity{
			UserID: matched.UserID,
			Name:   u.Username,
			// A sync key is a full sync credential for its account.
			Scopes: []string{ScopeSyncWrite, ScopeSyncEvent},
			KeyID:  matched.ID,
		}
		for _, scope := range requiredScopes {
			if !identity.HasScope(scope) {
				webAbort(c, http.StatusForbidden, "api key scope not allowed")
				return
			}
		}

		// Best-effort usage bookkeeping; auth already succeeded.
		_, _ = s.db.UserSyncKey.UpdateOneID(matched.ID).SetLastUsedAt(now).Save(c.Request.Context())

		WithGin(c, identity)
		c.Next()
	}
}

func (s *Service) reject(c *gin.Context, reason string) {
	if !s.allowAttempt(c.ClientIP()) {
		webAbort(c, http.StatusTooManyRequests, "too many invalid api keys")
		return
	}
	// Never include the presented credential. The request id links this line
	// to the access log without exposing a secret.
	slog.Warn("sync key rejected",
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

func keyBearer(header string) (string, bool) {
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

// Gin/context helpers keep the identity next to the request for handlers and
// tests.
const (
	ginKey = "synckey.identity"
)

// WithGin attaches the identity to the request.
func WithGin(c *gin.Context, identity Identity) {
	c.Set(ginKey, identity)
}

// From returns the authenticated sync identity.
func From(c *gin.Context) (Identity, bool) {
	value, ok := c.Get(ginKey)
	if !ok {
		return Identity{}, false
	}
	identity, ok := value.(Identity)
	return identity, ok
}
