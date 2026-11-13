// Package auth implements the single-administrator JWT login flow:
// Argon2id password storage, HS256 access tokens, the Gin middleware that
// guards non-public API routes, and the /api/auth/* handlers.
package auth

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/tenant"
	"manga-reader/internal/ent/user"
	"manga-reader/internal/web"
)

const (
	// CookieName is the HttpOnly cookie that carries the same JWT as the
	// Authorization header. It restores the session after a refresh and
	// authenticates direct download links (Prefill ZIP) without JavaScript.
	CookieName = "manga_reader_auth"

	// principalKey is private so handlers cannot be tricked into trusting a
	// client-supplied value through c.Set.
	principalKey = "auth.principal"

	// Login rate limit (per client IP) protects the Argon2id verifier from
	// being used as a memory/CPU amplification oracle: 30 attempts may burst,
	// then tokens refill at one per second.
	loginRate  = 1.0
	loginBurst = 30.0
)

// Principal is the authenticated identity attached by Middleware. User IDs
// from request bodies or queries are never used in its place.
type Principal struct {
	ID           int
	TenantID     int64
	Username     string
	Role         string
	TokenVersion int
}

// Service owns JWT verification and the login/logout/me endpoints.
type Service struct {
	db     *ent.Client
	cfg    config.AuthConfig
	tokens *tokenManager

	loginMu      sync.Mutex
	loginClients map[string]*loginBucket
}

type loginBucket struct {
	tokens float64
	last   time.Time
}

// New builds the auth service. When cfg.Enabled is false the service still
// serves /api/auth/status so the SPA can detect the legacy anonymous mode.
func New(db *ent.Client, cfg config.AuthConfig) *Service {
	s := &Service{
		db:           db,
		cfg:          cfg,
		loginClients: make(map[string]*loginBucket),
	}
	if cfg.Enabled {
		s.tokens = newTokenManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTExpiresIn)
	}
	return s
}

// Enabled reports whether JWT authentication is active.
func (s *Service) Enabled() bool {
	return s != nil && s.cfg.Enabled && s.tokens != nil
}

// EnsureAdmin seeds the first administrator when the users table is empty.
// Existing rows are never modified, so changing ADMIN_PASSWORD in the
// environment cannot silently reset an operator's password.
func (s *Service) EnsureAdmin(ctx context.Context) (bool, error) {
	if !s.Enabled() {
		return false, nil
	}
	if s.db == nil {
		return false, errors.New("auth database is nil")
	}

	defaultTenant, err := s.db.Tenant.Query().
		Where(tenant.SlugEQ(config.DefaultTenantSlug)).
		Only(ctx)
	if err != nil {
		return false, fmt.Errorf("default tenant is missing: %w", err)
	}

	count, err := s.db.User.Query().
		Where(user.TenantIDEQ(int64(defaultTenant.ID))).
		Count(ctx)
	if err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	if s.cfg.AdminUsername == "" {
		return false, errors.New("no administrator exists and ADMIN_USERNAME is empty")
	}
	if s.cfg.AdminPassword == "" {
		return false, errors.New("no administrator exists; set ADMIN_USERNAME and ADMIN_PASSWORD for the first startup")
	}
	if len(s.cfg.AdminPassword) < 8 {
		return false, errors.New("ADMIN_PASSWORD must be at least 8 characters")
	}

	hash, err := HashPassword(s.cfg.AdminPassword)
	if err != nil {
		return false, err
	}
	_, err = s.db.User.Create().
		SetTenantID(int64(defaultTenant.ID)).
		SetUsername(s.cfg.AdminUsername).
		SetPasswordHash(hash).
		SetRole(config.RolePlatformAdmin).
		SetStatus(config.StatusActive).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("create administrator: %w", err)
	}
	return true, nil
}

// publicAPIs are the only routes reachable without a JWT when auth is
// enabled. The default is deny for every other /api/* route, so newly added
// user-data endpoints are protected automatically.
//
// Gallery/search/image endpoints stay public because <img> and EventSource
// cannot attach an Authorization header, and they expose upstream content
// rather than local user data. Sync peer endpoints keep their own
// X-Sync-Token check.
var publicAPIs = map[string]struct{}{
	"/api/auth/login":  {},
	"/api/auth/status": {},

	"/api/search":    {},
	"/api/galleries": {},
	"/api/watched":   {},
	"/api/popular":   {},

	"/api/gallery/:id/:token":                         {},
	"/api/gallery/:id/:token/details":                 {},
	"/api/gallery/:id/:token/pages":                   {},
	"/api/gallery/:id/:token/torrents":                {},
	"/api/gallery/:id/:token/torrents/:gtid/info":     {},
	"/api/gallery/:id/:token/torrents/:gtid/download": {},

	"/api/gallery-cache/:id/:token":         {},
	"/api/gallery-cache/:id/:token/details": {},
	"/api/gallery-cache/:id/:token/pages":   {},

	"/api/image/page":           {},
	"/api/image/thumbnail":      {},
	"/api/image/page-thumbnail": {},

	"/api/image-cache/page":           {},
	"/api/image-cache/thumbnail":      {},
	"/api/image-cache/page-thumbnail": {},

	// Peer sync is authenticated by X-Sync-Token in the sync service.
	"/api/sync/push":   {},
	"/api/sync/events": {},
}

// IsPublicAPI reports whether the matched route template is public.
func IsPublicAPI(route string) bool {
	_, ok := publicAPIs[route]
	return ok
}

// Middleware verifies Authorization: Bearer <jwt> (or the HttpOnly cookie)
// for every non-public /api/* route and injects a Principal into context.
func (s *Service) Middleware() gin.HandlerFunc {
	if !s.Enabled() {
		return func(c *gin.Context) { c.Next() }
	}
	return func(c *gin.Context) {
		route := c.FullPath()
		if route == "" {
			// Unknown paths fall through to Gin's 404; they are not an auth
			// bypass because no handler will run.
			c.Next()
			return
		}
		if !strings.HasPrefix(route, "/api/") || IsPublicAPI(route) {
			c.Next()
			return
		}

		principal, err := s.authenticate(c)
		if err != nil {
			s.clearCookie(c)
			web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
			return
		}
		c.Set(principalKey, principal)
		c.Next()
	}
}

func (s *Service) authenticate(c *gin.Context) (Principal, error) {
	raw, err := tokenFromRequest(c)
	if err != nil {
		return Principal{}, err
	}
	claims, err := s.tokens.parse(raw)
	if err != nil {
		return Principal{}, err
	}
	id, err := strconv.Atoi(claims.Subject)
	if err != nil || id <= 0 {
		return Principal{}, ErrInvalidToken
	}

	u, err := s.db.User.Get(c.Request.Context(), id)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	// Logout increments token_version, which invalidates every token that was
	// issued before it (not just the copy in this browser).
	if u.Status != config.StatusActive || u.TenantID != claims.TenantID || u.Role != claims.Role {
		return Principal{}, ErrInvalidToken
	}
	if u.TokenVersion != claims.TokenVersion {
		return Principal{}, ErrInvalidToken
	}
	return Principal{
		ID:           u.ID,
		TenantID:     u.TenantID,
		Username:     u.Username,
		Role:         u.Role,
		TokenVersion: u.TokenVersion,
	}, nil
}

// tokenFromRequest prefers the explicit Authorization header (API clients,
// desktop sync) and falls back to the HttpOnly cookie (browser).
func tokenFromRequest(c *gin.Context) (string, error) {
	if header := strings.TrimSpace(c.GetHeader("Authorization")); header != "" {
		scheme, value, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(value) == "" {
			return "", ErrInvalidToken
		}
		return strings.TrimSpace(value), nil
	}
	if cookie, err := c.Cookie(CookieName); err == nil && cookie != "" {
		return cookie, nil
	}
	return "", ErrInvalidToken
}

// PrincipalFrom returns the identity attached by Middleware, if any.
func PrincipalFrom(c *gin.Context) (Principal, bool) {
	value, ok := c.Get(principalKey)
	if !ok {
		return Principal{}, false
	}
	principal, ok := value.(Principal)
	return principal, ok
}

// RegisterRoutes mounts the public auth endpoints plus the protected
// logout/me routes. The middleware is applied globally by main, so logout/me
// are protected by the same default-deny rule as the rest of the API.
func (s *Service) RegisterRoutes(r gin.IRouter) {
	r.POST("/api/auth/login", noStore, s.handleLogin)
	r.GET("/api/auth/status", noStore, s.handleStatus)
	r.POST("/api/auth/logout", noStore, s.handleLogout)
	r.GET("/api/auth/me", noStore, s.handleMe)
}

func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Next()
}

func (s *Service) handleStatus(c *gin.Context) {
	initialized := false
	if s.db != nil {
		count, err := s.db.User.Query().Count(c.Request.Context())
		if err != nil {
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "failed to read auth status"})
			return
		}
		initialized = count > 0
	}
	web.JSON(c, http.StatusOK, gin.H{
		"enabled":     s.Enabled(),
		"initialized": initialized,
	})
}

func (s *Service) handleLogin(c *gin.Context) {
	if !s.Enabled() {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "authentication is disabled"})
		return
	}
	if !s.allowLogin(c.ClientIP()) {
		c.Header("Retry-After", "60")
		web.JSON(c, http.StatusTooManyRequests, gin.H{"error": "too many login attempts"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var body struct {
		Tenant   string `json:"tenant"`
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	body.Tenant = strings.TrimSpace(body.Tenant)
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || body.Password == "" {
		// Same message as a wrong password: do not reveal which field failed.
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}
	if body.Tenant == "" {
		// Backwards-compatible default for older clients; the tenant is still
		// resolved server-side, never trusted as an isolation boundary.
		body.Tenant = config.DefaultTenantSlug
	}

	ctx := c.Request.Context()
	t, err := s.db.Tenant.Query().
		Where(tenant.SlugEQ(body.Tenant), tenant.StatusEQ(config.StatusActive)).
		Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "login failed"})
			return
		}
		// Burn the password hash for unknown/disabled tenants too.
		_, _ = HashPassword(body.Password)
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	u, err := s.db.User.Query().
		Where(
			user.TenantIDEQ(int64(t.ID)),
			user.UsernameEQ(body.Username),
			user.StatusEQ(config.StatusActive),
		).
		Only(ctx)
	if err != nil {
		if !ent.IsNotFound(err) {
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "login failed"})
			return
		}
		// Hash the supplied password anyway so a missing username and a wrong
		// password cost roughly the same Argon2id work.
		_, _ = HashPassword(body.Password)
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}
	if !VerifyPassword(u.PasswordHash, body.Password) {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	token, expires, err := s.tokens.issue(u.ID, u.TenantID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}
	s.setCookie(c, token, expires)
	web.JSON(c, http.StatusOK, gin.H{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   s.cfg.ExpiresInSeconds(),
	})
}

func (s *Service) handleLogout(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	// Bump token_version so this and every other outstanding token for the
	// account fail validation immediately. This is server-side revocation,
	// not just a client-side cookie deletion.
	if err := s.db.User.Update().
		Where(user.ID(principal.ID), user.TenantIDEQ(principal.TenantID)).
		AddTokenVersion(1).
		Exec(c.Request.Context()); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "logout failed"})
		return
	}
	s.clearCookie(c)
	web.JSON(c, http.StatusOK, gin.H{"success": true})
}

func (s *Service) handleMe(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	t, err := s.db.Tenant.Get(c.Request.Context(), int(principal.TenantID))
	if err != nil {
		web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "tenant unavailable"})
		return
	}
	tenantJSON := gin.H{
		"id":     int64(t.ID),
		"name":   t.Name,
		"slug":   t.Slug,
		"status": t.Status,
	}
	userJSON := gin.H{
		"id":        principal.ID,
		"tenant_id": principal.TenantID,
		"username":  principal.Username,
		"role":      principal.Role,
	}
	web.JSON(c, http.StatusOK, gin.H{
		"id":        principal.ID,
		"tenant_id": principal.TenantID,
		"username":  principal.Username,
		"role":      principal.Role,
		"tenant":    tenantJSON,
		"user":      userJSON,
	})
}

func (s *Service) setCookie(c *gin.Context, token string, expires time.Time) {
	secure := s.cfg.CookieSecure
	if s.cfg.CookieSecureAuto {
		secure = c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		// Lax keeps the cookie on top-level navigations/refresh while
		// blocking cross-site subrequests; csrfGuard independently rejects
		// cross-origin state-changing requests.
		SameSite: http.SameSiteLaxMode,
		Expires:  expires,
		MaxAge:   s.cfg.ExpiresInSeconds(),
	})
}

func (s *Service) clearCookie(c *gin.Context) {
	secure := s.cfg.CookieSecure
	if s.cfg.CookieSecureAuto {
		secure = c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
	})
}

func (s *Service) allowLogin(key string) bool {
	now := time.Now()
	s.loginMu.Lock()
	defer s.loginMu.Unlock()

	if len(s.loginClients) > 4096 {
		for k, bucket := range s.loginClients {
			if now.Sub(bucket.last) > time.Minute {
				delete(s.loginClients, k)
			}
		}
	}
	bucket := s.loginClients[key]
	if bucket == nil {
		bucket = &loginBucket{tokens: loginBurst, last: now}
		s.loginClients[key] = bucket
	}
	if elapsed := now.Sub(bucket.last).Seconds(); elapsed > 0 {
		bucket.tokens += elapsed * loginRate
		if bucket.tokens > loginBurst {
			bucket.tokens = loginBurst
		}
	}
	bucket.last = now
	if bucket.tokens < 1 {
		return false
	}
	bucket.tokens--
	return true
}
