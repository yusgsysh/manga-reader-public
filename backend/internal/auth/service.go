// Package auth verifies Kinde OIDC access tokens for every business API,
// synchronizes the verified identity into the local users table, and exposes
// /api/auth/me and /api/auth/logout. Kinde owns login, registration and token
// issuance; this package never trusts client-supplied user ids.
package auth

import (
	"errors"
	"log/slog"
	"net/http"
	"strings"
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
	// CookieName is an HttpOnly bridge for browser-native requests that cannot
	// set an Authorization header: <img> proxies, EventSource and download
	// links. It carries the same short-lived Kinde access token the SPA uses,
	// is scoped to /api, and is only accepted on those native GET routes.
	CookieName = "manga_reader_access"

	// principalKey is private so handlers cannot be tricked into trusting a
	// client-supplied value through c.Set.
	principalKey = "auth.principal"
)

// Principal is the authenticated identity attached by Middleware. It is always
// the local user row matched by the verified Kinde subject; IDs from request
// bodies, queries or paths are never used in its place.
type Principal struct {
	ID        ident.UUID
	UserID    string
	Username  string
	Email     string
	Name      string
	AvatarURL string
	Role      string
	Status    string
}

// Service owns token verification, local user synchronization and the
// /api/auth/* endpoints.
type Service struct {
	db            *ent.Client
	cfg           config.AuthConfig
	verifier      *Verifier
	adminEmails   map[string]struct{}
	adminSubjects map[string]struct{}
}

// New builds the auth service around an already initialized Kinde verifier.
func New(db *ent.Client, cfg config.AuthConfig, verifier *Verifier, adminEmails, adminSubjects []string) *Service {
	admins := make(map[string]struct{}, len(adminEmails))
	for _, email := range adminEmails {
		if email = strings.ToLower(strings.TrimSpace(email)); email != "" {
			admins[email] = struct{}{}
		}
	}
	subjects := make(map[string]struct{}, len(adminSubjects))
	for _, subject := range adminSubjects {
		if subject = strings.TrimSpace(subject); subject != "" {
			subjects[subject] = struct{}{}
		}
	}
	return &Service{db: db, cfg: cfg, verifier: verifier, adminEmails: admins, adminSubjects: subjects}
}

// publicAPIs are the only /api/* routes reachable without a Kinde access
// token. Sync peer endpoints authenticate with a user sync key in their own
// middleware and never accept a user token.
var publicAPIs = map[string]struct{}{
	"/api/sync/push":   {},
	"/api/sync/events": {},
}

// IsPublicAPI reports whether the matched route template is public.
func IsPublicAPI(route string) bool {
	_, ok := publicAPIs[route]
	return ok
}

// Middleware verifies the Kinde access token for every non-public /api/*
// route and injects the local Principal into context. Missing, malformed,
// invalid or expired tokens all produce the same 401 body so verification
// internals are never leaked. A verified but locally disabled account gets 403.
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

		raw, fromCookie, err := s.tokenFromRequest(c, route)
		if err != nil {
			s.abortUnauthorized(c)
			return
		}
		claims, err := s.verifier.Verify(c.Request.Context(), raw)
		if err != nil {
			s.abortUnauthorized(c)
			return
		}
		principal, err := s.principalFromClaims(c.Request.Context(), claims)
		if err != nil {
			if errors.Is(err, ErrUserDisabled) {
				web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "forbidden"})
				return
			}
			s.abortUnauthorized(c)
			return
		}

		// Keep the browser-native bridge fresh from every regular API call so
		// the SPA never has to manage a second token copy in JavaScript.
		if !fromCookie {
			s.setCookie(c, raw, claims.ExpiresAt)
		}
		c.Set(principalKey, principal)
		c.Next()
	}
}

func (s *Service) abortUnauthorized(c *gin.Context) {
	logAuthFailure(c)
	s.clearCookie(c)
	web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
}

// tokenFromRequest accepts Authorization: Bearer on every API route. The
// media cookie is only accepted on browser-native GET routes, so a cookie can
// never be used to call a state-changing or admin endpoint.
func (s *Service) tokenFromRequest(c *gin.Context, route string) (string, bool, error) {
	if header := strings.TrimSpace(c.GetHeader("Authorization")); header != "" {
		scheme, value, ok := strings.Cut(header, " ")
		if !ok || !strings.EqualFold(scheme, "Bearer") || strings.TrimSpace(value) == "" {
			return "", false, ErrInvalidToken
		}
		return strings.TrimSpace(value), false, nil
	}
	if isNativeAuthRoute(c, route) {
		if cookie, err := c.Cookie(CookieName); err == nil && cookie != "" {
			return cookie, true, nil
		}
	}
	return "", false, ErrInvalidToken
}

func isNativeAuthRoute(c *gin.Context, route string) bool {
	switch c.Request.Method {
	case http.MethodGet, http.MethodHead:
	default:
		return false
	}
	switch {
	case strings.HasPrefix(route, "/api/image/"),
		strings.HasPrefix(route, "/api/image-cache/"),
		route == "/api/events/changes",
		strings.HasSuffix(route, "/zip"),
		strings.HasSuffix(route, "/torrents/:gtid/download"):
		return true
	default:
		return false
	}
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

// RegisterRoutes mounts the authenticated identity endpoints. Login/register
// are intentionally absent: those happen in Kinde and never touch this API.
func (s *Service) RegisterRoutes(r gin.IRouter) {
	r.GET("/api/auth/me", noStore, s.handleMe)
	r.POST("/api/auth/logout", noStore, s.handleLogout)
}

func noStore(c *gin.Context) {
	c.Header("Cache-Control", "no-store")
	c.Next()
}

func userResponse(u *ent.User) gin.H {
	avatar := any(nil)
	if u.AvatarURL != "" {
		avatar = u.AvatarURL
	}
	return gin.H{
		"id":         u.ID,
		"user_id":    u.UserID,
		"username":   u.Username,
		"email":      u.Email,
		"name":       u.Name,
		"avatar_url": avatar,
		"role":       u.Role,
		"status":     u.Status,
	}
}

// handleMe returns the local account for the verified request token. It takes
// no user id from the client; the principal comes from Middleware.
func (s *Service) handleMe(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	u, err := s.db.User.Get(c.Request.Context(), principal.ID)
	if err != nil {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	web.JSON(c, http.StatusOK, userResponse(u))
}

func (s *Service) handleLogout(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	// Bump token_version for the local revocation model and clear the media
	// bridge cookie. Kinde remains the owner of the access token and its own
	// session logout; the React SDK performs that redirect.
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

func (s *Service) cookieSecure(c *gin.Context) bool {
	if s.cfg.CookieSecureAuto {
		return c.Request.TLS != nil || strings.EqualFold(c.GetHeader("X-Forwarded-Proto"), "https")
	}
	return s.cfg.CookieSecure
}

func (s *Service) setCookie(c *gin.Context, token string, expires time.Time) {
	if token == "" || expires.IsZero() {
		return
	}
	if existing, err := c.Cookie(CookieName); err == nil && existing == token {
		return
	}
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CookieName,
		Value:    token,
		Path:     "/api",
		HttpOnly: true,
		Secure:   s.cookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		Expires:  expires.UTC(),
		MaxAge:   int(time.Until(expires).Seconds()),
	})
}

func (s *Service) clearCookie(c *gin.Context) {
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     CookieName,
		Value:    "",
		Path:     "/api",
		HttpOnly: true,
		Secure:   s.cookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
	})
}

// logAuthFailure records only the request id and route. Tokens, authorization
// headers and verification errors are deliberately absent from logs.
func logAuthFailure(c *gin.Context) {
	slog.Info("authentication rejected", "request_id", requestid.From(c), "route", c.FullPath())
}
