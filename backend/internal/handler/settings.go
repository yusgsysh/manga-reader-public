package handler

import (
	"context"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/settings"
)

// settingsTimeout bounds a settings save. Saving can reach out to the object
// store to prove the new endpoint works, so it needs a longer window than a
// normal request — but it must still fail rather than hang on a dead endpoint.
const settingsTimeout = 30 * time.Second

// handleSettingsGet returns the active configuration with secrets masked.
func (s *Server) handleSettingsGet(c *gin.Context) {
	c.JSON(http.StatusOK, s.settings.Get())
}

// handleSettingsPut validates, applies and persists a settings payload.
//
// The context deliberately does not come from the request: a save that has
// started must finish even if the browser navigates away, otherwise the
// database and the running process would disagree about what is configured.
func (s *Server) handleSettingsPut(c *gin.Context) {
	var in settings.Update
	if err := c.ShouldBindJSON(&in); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid settings payload: " + err.Error()})
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), settingsTimeout)
	defer cancel()

	snapshot, err := s.settings.Update(ctx, in)
	if err != nil {
		if settings.IsInvalid(err) {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, snapshot)
}
