// Package auth implements the multi-user JWT login flow: Argon2id password
// storage, HS256 access tokens, the Gin middleware that guards every
// user-private API route, and the /api/auth/* handlers.
package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ident"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/user"
	"manga-reader/internal/requestid"
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
	ID           ident.UUID
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

	// setupMu serializes first-boot admin registration so two concurrent
	// requests cannot both observe an empty users table and create admins.
	setupMu sync.Mutex
}

type loginBucket struct {
	tokens float64
	last   time.Time
}

// New builds the auth service. Authentication is always on: the SPA login page
// and the default-deny middleware are the only entry points to user data.
func New(db *ent.Client, cfg config.AuthConfig) *Service {
	return &Service{
		db:           db,
		cfg:          cfg,
		tokens:       newTokenManager(cfg.JWTSecret, cfg.JWTIssuer, cfg.JWTExpiresIn),
		loginClients: make(map[string]*loginBucket),
	}
}

// EnsureAdmin seeds the first administrator when the users table is empty.
// Existing rows are never modified, so changing ADMIN_PASSWORD in the
// environment cannot silently reset an operator's password. When no admin
// credentials are configured the instance is left uninitialized instead: the
// operator then creates the first administrator through the web setup page
// (POST /api/auth/register).
func (s *Service) EnsureAdmin(ctx context.Context) (bool, error) {
	if s.db == nil {
		return false, errors.New("auth database is nil")
	}

	count, err := s.db.User.Query().Count(ctx)
	if err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	if count > 0 {
		return false, nil
	}

	// No env-configured password: skip seeding so the web setup page can run.
	if s.cfg.AdminPassword == "" {
		return false, nil
	}
	if s.cfg.AdminUsername == "" {
		return false, errors.New("ADMIN_PASSWORD is set but ADMIN_USERNAME is empty")
	}
	if len(s.cfg.AdminPassword) < 8 {
		return false, errors.New("ADMIN_PASSWORD must be at least 8 characters")
	}

	hash, err := HashPassword(s.cfg.AdminPassword)
	if err != nil {
		return false, err
	}
	_, err = s.db.User.Create().
		SetUsername(s.cfg.AdminUsername).
		SetPasswordHash(hash).
		SetRole(config.RoleAdmin).
		SetStatus(config.StatusActive).
		Save(ctx)
	if err != nil {
		return false, fmt.Errorf("create administrator: %w", err)
	}
	return true, nil
}

// IsInitialized reports whether at least one account exists, i.e. whether the
// first administrator has already been created (via env vars or the web setup
// page).
func (s *Service) IsInitialized(ctx context.Context) (bool, error) {
	if s.db == nil {
		return false, errors.New("auth database is nil")
	}
	count, err := s.db.User.Query().Count(ctx)
	if err != nil {
		return false, fmt.Errorf("count users: %w", err)
	}
	return count > 0, nil
}

// publicAPIs are the only routes reachable without a JWT. Every route that
// touches user data, including gallery/search/image proxies (which need the
// caller's ExHentai cookie and are reachable from <img>/EventSource through the
// HttpOnly session cookie), requires an authenticated user. Sync peer endpoints
// keep their own Node API Key middleware and never accept a user JWT.
var publicAPIs = map[string]struct{}{
	"/api/auth/login":    {},
	"/api/auth/register": {},
	"/api/auth/status":   {},

	// Peer sync is authenticated by Node API Key middleware in the sync service.
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
	id, err := ident.Parse(claims.Subject)
	if err != nil || id == ident.Nil() {
		return Principal{}, ErrInvalidToken
	}

	u, err := s.db.User.Get(c.Request.Context(), id)
	if err != nil {
		return Principal{}, ErrInvalidToken
	}
	// Logout increments token_version, which invalidates every token that was
	// issued before it (not just the copy in this browser).
	if u.Status != config.StatusActive || u.ID != claims.UserID || u.Role != claims.Role {
		return Principal{}, ErrInvalidToken
	}
	if u.TokenVersion != claims.TokenVersion {
		return Principal{}, ErrInvalidToken
	}
	return Principal{
		ID:           u.ID,
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
	r.POST("/api/auth/register", noStore, s.handleRegister)
	r.GET("/api/auth/status", noStore, s.handleStatus)
	r.POST("/api/auth/logout", noStore, s.handleLogout)
	r.GET("/api/auth/me", noStore, s.handleMe)
}

func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Next()
}

func (s *Service) handleStatus(c *gin.Context) {
	initialized, err := s.IsInitialized(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "failed to read auth status"})
		return
	}
	web.JSON(c, http.StatusOK, gin.H{
		"initialized": initialized,
	})
}

// handleRegister creates the first administrator through the web setup page. It
// only runs while the users table is empty; once any account exists it returns
// 409 so the endpoint cannot be used to mint additional administrators.
func (s *Service) handleRegister(c *gin.Context) {
	if !s.allowLogin(c.ClientIP()) {
		c.Header("Retry-After", "60")
		web.JSON(c, http.StatusTooManyRequests, gin.H{"error": "too many requests"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	if body.Username == "" || len(body.Password) < 8 {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "username and a password of at least 8 characters are required"})
		return
	}

	// Hash before taking the lock: Argon2id is deliberately slow and must not
	// extend the critical section.
	hash, err := HashPassword(body.Password)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "password hashing failed"})
		return
	}

	ctx := c.Request.Context()
	s.setupMu.Lock()
	defer s.setupMu.Unlock()

	initialized, err := s.IsInitialized(ctx)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "failed to read auth status"})
		return
	}
	if initialized {
		web.JSON(c, http.StatusConflict, gin.H{"error": "already initialized"})
		return
	}

	u, err := s.db.User.Create().
		SetUsername(body.Username).
		SetPasswordHash(hash).
		SetRole(config.RoleAdmin).
		SetStatus(config.StatusActive).
		Save(ctx)
	if err != nil {
		if ent.IsConstraintError(err) {
			web.JSON(c, http.StatusConflict, gin.H{"error": "username already exists"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "create administrator failed"})
		return
	}

	token, expires, err := s.tokens.issue(u.ID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "registration failed"})
		return
	}
	s.setCookie(c, token, expires)
	slog.Info("administrator registered",
		"request_id", requestid.From(c),
		"username", u.Username,
	)
	web.JSON(c, http.StatusOK, gin.H{
		"access_token": token,
		"token_type":   "Bearer",
		"expires_in":   s.cfg.ExpiresInSeconds(),
	})
}

func (s *Service) handleLogin(c *gin.Context) {
	if !s.allowLogin(c.ClientIP()) {
		c.Header("Retry-After", "60")
		web.JSON(c, http.StatusTooManyRequests, gin.H{"error": "too many login attempts"})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 16<<10)
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	body.Username = strings.TrimSpace(body.Username)
	loginResult := "failure"
	defer func() {
		slog.Info("user login",
			"request_id", requestid.From(c),
			"username", body.Username,
			"result", loginResult,
		)
	}()
	if body.Username == "" || body.Password == "" {
		// Same message as a wrong password: do not reveal which field failed.
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid username or password"})
		return
	}

	ctx := c.Request.Context()
	u, err := s.db.User.Query().
		Where(
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

	token, expires, err := s.tokens.issue(u.ID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}
	s.setCookie(c, token, expires)
	loginResult = "success"
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
		Where(user.ID(principal.ID)).
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
	web.JSON(c, http.StatusOK, gin.H{
		"id":       principal.ID,
		"user_id":  principal.ID,
		"username": principal.Username,
		"role":     principal.Role,
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
