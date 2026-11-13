package handler

import (
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/usercookie"
	"manga-reader/internal/web"
)

// cookieBody accepts either a browser-style cookie header in "cookie" or the
// four individual fields. Secrets are write-only: no response ever echoes them.
type cookieBody struct {
	Cookie      string `json:"cookie"`
	IpbMemberID string `json:"ipb_member_id"`
	IpbPassHash string `json:"ipb_pass_hash"`
	Igneous     string `json:"igneous"`
	SK          string `json:"sk"`
	Site        string `json:"site"`
}

func (b cookieBody) config() config.CookieConfig {
	cfg := config.ParseCookieString(b.Cookie)
	if strings.TrimSpace(b.IpbMemberID) != "" {
		cfg.IpbMemberID = strings.TrimSpace(b.IpbMemberID)
	}
	if strings.TrimSpace(b.IpbPassHash) != "" {
		cfg.IpbPassHash = strings.TrimSpace(b.IpbPassHash)
	}
	if strings.TrimSpace(b.Igneous) != "" {
		cfg.Igneous = strings.TrimSpace(b.Igneous)
	}
	if strings.TrimSpace(b.SK) != "" {
		cfg.SK = strings.TrimSpace(b.SK)
	}
	// Re-parse the encoded form so structured fields get the same trimming
	// and quote/separator cleanup as a pasted cookie header.
	return config.ParseCookieString(cfg.Encode())
}

func (s *Server) requireCookieService(c *gin.Context) *usercookie.Service {
	if s.UserCookies == nil {
		web.JSON(c, http.StatusServiceUnavailable, gin.H{"error": "user settings are not configured"})
		return nil
	}
	return s.UserCookies
}

func (s *Server) handleCookieGet(c *gin.Context) {
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	svc := s.requireCookieService(c)
	if svc == nil {
		return
	}
	summary, err := svc.Summary(c.Request.Context(), uid)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read cookie settings failed"})
		return
	}
	web.JSON(c, http.StatusOK, summary)
}

func (s *Server) handleCookiePut(c *gin.Context) {
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	svc := s.requireCookieService(c)
	if svc == nil {
		return
	}
	limitRequestBody(c, 32<<10)
	var body cookieBody
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	cfg := body.config()
	if !cfg.IsValid() {
		// A site-only update is allowed once a cookie exists; it must not
		// require re-pasting the secret.
		current, err := svc.Summary(c.Request.Context(), uid)
		if err != nil {
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read cookie settings failed"})
			return
		}
		if current.Configured && strings.TrimSpace(body.Site) != "" {
			if _, err := svc.SaveSite(c.Request.Context(), uid, body.Site); err != nil {
				web.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
				return
			}
			updated, err := svc.Summary(c.Request.Context(), uid)
			if err != nil {
				web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read cookie settings failed"})
				return
			}
			web.JSON(c, http.StatusOK, updated)
			return
		}
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "ipb_member_id and ipb_pass_hash are required"})
		return
	}
	summary, err := svc.SaveCookie(c.Request.Context(), uid, cfg, body.Site)
	if err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	web.JSON(c, http.StatusOK, summary)
}

func (s *Server) handleCookieDelete(c *gin.Context) {
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	svc := s.requireCookieService(c)
	if svc == nil {
		return
	}
	if err := svc.ClearCookie(c.Request.Context(), uid); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "clear cookie failed"})
		return
	}
	web.JSON(c, http.StatusOK, usercookie.Summary{Status: usercookie.StatusUnset, Site: usercookie.SiteExHentai})
}

func (s *Server) handleCookieVerify(c *gin.Context) {
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	svc := s.requireCookieService(c)
	if svc == nil {
		return
	}
	client, err := svc.Client(c.Request.Context(), uid)
	if err != nil {
		if errors.Is(err, usercookie.ErrNotConfigured) {
			web.JSON(c, http.StatusPreconditionFailed, gin.H{"error": "exhentai cookie is not configured"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "load exhentai session failed"})
		return
	}
	status := usercookie.StatusValid
	if err := exhentai.VerifySession(c.Request.Context(), client); err != nil {
		status = usercookie.StatusInvalid
	}
	if err := svc.SetStatus(c.Request.Context(), uid, status); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "record cookie status failed"})
		return
	}
	summary, err := svc.Summary(c.Request.Context(), uid)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read cookie settings failed"})
		return
	}
	web.JSON(c, http.StatusOK, summary)
}

// handleSettingsGet returns the user's personal preferences plus a non-secret
// cookie summary. Nothing here is shared with another account.
func (s *Server) handleSettingsGet(c *gin.Context) {
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	svc := s.requireCookieService(c)
	if svc == nil {
		return
	}
	prefs, err := svc.Preferences(c.Request.Context(), uid)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read settings failed"})
		return
	}
	summary, err := svc.Summary(c.Request.Context(), uid)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read settings failed"})
		return
	}
	web.JSON(c, http.StatusOK, gin.H{
		"preferences": prefs,
		"site":        summary.Site,
		"cookie":      summary,
	})
}

// handleSettingsPut replaces the preference document. The cookie has its own
// write endpoint, so it cannot be set accidentally through this body.
func (s *Server) handleSettingsPut(c *gin.Context) {
	uid, ok := s.requireUser(c)
	if !ok {
		return
	}
	svc := s.requireCookieService(c)
	if svc == nil {
		return
	}
	limitRequestBody(c, 256<<10)
	var body struct {
		Preferences map[string]any `json:"preferences"`
		Site        string         `json:"site"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	if body.Preferences == nil {
		body.Preferences = map[string]any{}
	}
	if strings.TrimSpace(body.Site) != "" {
		if _, err := svc.SaveSite(c.Request.Context(), uid, body.Site); err != nil {
			web.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
	}
	if err := svc.SavePreferences(c.Request.Context(), uid, body.Preferences); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("save settings failed: %v", err)})
		return
	}
	prefs, err := svc.Preferences(c.Request.Context(), uid)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read settings failed"})
		return
	}
	summary, _ := svc.Summary(c.Request.Context(), uid)
	web.JSON(c, http.StatusOK, gin.H{
		"preferences": prefs,
		"site":        summary.Site,
		"cookie":      summary,
	})
}
