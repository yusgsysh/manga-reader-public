package handler

import (
	"context"
	"errors"
	"net/http"

	"github.com/gin-gonic/gin"
	"manga-reader/internal/ident"

	"manga-reader/internal/auth"
	"manga-reader/internal/config"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/usercookie"
	"manga-reader/internal/web"
)

// requireUser returns the trusted user ID for the request. It is always taken
// from the verified JWT principal; query/header/body user identifiers are
// never accepted. Every private query, update and delete must include it.
func (s *Server) requireUser(c *gin.Context) (ident.UUID, bool) {
	if principal, ok := auth.PrincipalFrom(c); ok && principal.ID != ident.Nil() {
		return principal.ID, true
	}
	if !s.requireAuth {
		// Handler unit tests and standalone embedding may bypass the auth
		// middleware. The production constructor always sets requireAuth, so
		// a real request without a principal is rejected.
		return ident.Nil(), true
	}
	web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "authentication required"})
	return ident.Nil(), false
}

// requireAdmin guards instance-wide management endpoints.
func (s *Server) requireAdmin(c *gin.Context) bool {
	principal, ok := auth.PrincipalFrom(c)
	if !ok || principal.Role != config.RoleAdmin {
		web.AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "administrator required"})
		return false
	}
	return true
}

// upstreamClient returns the HTTP client for one user's ExHentai session. In
// production UserCookies builds a separate cookie jar per account; the plain
// Client fallback exists only for tests that construct Server directly.
func (s *Server) upstreamClient(ctx context.Context, userID ident.UUID) (*http.Client, error) {
	if s.UserCookies != nil {
		return s.UserCookies.Client(ctx, userID)
	}
	if s.Client != nil {
		return s.Client, nil
	}
	return nil, errors.New("upstream client is not configured")
}

// upstreamClientForRequest resolves the current user's client or writes the
// appropriate error response.
func (s *Server) upstreamClientForRequest(c *gin.Context) (*http.Client, ident.UUID, bool) {
	uid, ok := s.requireUser(c)
	if !ok {
		return nil, ident.Nil(), false
	}
	client, err := s.upstreamClient(c.Request.Context(), uid)
	if err != nil {
		if errors.Is(err, usercookie.ErrNotConfigured) {
			web.JSON(c, http.StatusPreconditionFailed, gin.H{"error": "exhentai cookie is not configured; add it in settings"})
			return nil, ident.Nil(), false
		}
		web.JSON(c, http.StatusBadGateway, gin.H{"error": "exhentai session unavailable"})
		return nil, ident.Nil(), false
	}
	return client, uid, true
}

// upstreamBase returns the base URL of the user's preferred upstream site.
// The cookie jar authenticates both hosts, so the preference only switches
// which one is browsed; the default stays exhentai.org.
func (s *Server) upstreamBase(ctx context.Context, userID ident.UUID) string {
	if s.UserCookies != nil {
		if site, err := s.UserCookies.Site(ctx, userID); err == nil {
			return exhentai.SiteBaseURL(site)
		}
	}
	return exhentai.ExhentaiURL
}
