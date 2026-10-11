package handler

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/tagpreference"
	"manga-reader/internal/model"
)

// maxTagSettings bounds a single PUT payload so a bad client cannot bloat
// the table; far above any realistic My Tags list.
const maxTagSettings = 10000

// handleTagSettingsGet returns all stored tag preferences, ordered by tag.
func (s *Server) handleTagSettingsGet(c *gin.Context) {
	ctx := c.Request.Context()

	rows, err := s.DB.Client.TagPreference.Query().
		Order(ent.Asc(tagpreference.FieldTag)).
		All(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list tag settings failed: %v", err)})
		return
	}

	tags := make([]model.TagPreference, 0, len(rows))
	for _, row := range rows {
		tags = append(tags, model.TagPreference{Tag: row.Tag, Action: row.Action})
	}
	c.JSON(http.StatusOK, model.TagSettingsResponse{Tags: tags})
}

// handleTagSettingsPut replaces the full preference list. Rules are trimmed,
// lowercased and deduplicated (last occurrence wins) before persisting, so
// the stored set always matches what the UI would render.
func (s *Server) handleTagSettingsPut(c *gin.Context) {
	var body model.TagSettingsResponse
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if len(body.Tags) > maxTagSettings {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("too many tags (max %d)", maxTagSettings)})
		return
	}

	// Deduplicate by tag, keeping the last action for each.
	normalized := make([]model.TagPreference, 0, len(body.Tags))
	indexByTag := make(map[string]int, len(body.Tags))
	for _, t := range body.Tags {
		tag := strings.ToLower(strings.TrimSpace(t.Tag))
		action := strings.TrimSpace(t.Action)
		if tag == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "tag must not be empty"})
			return
		}
		if action != model.TagActionLike && action != model.TagActionDislike {
			c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid action %q for tag %q", t.Action, t.Tag)})
			return
		}
		if i, ok := indexByTag[tag]; ok {
			normalized[i].Action = action
			continue
		}
		indexByTag[tag] = len(normalized)
		normalized = append(normalized, model.TagPreference{Tag: tag, Action: action})
	}

	// Persist and respond in tag order so PUT mirrors GET.
	sort.Slice(normalized, func(i, j int) bool {
		return normalized[i].Tag < normalized[j].Tag
	})

	ctx := c.Request.Context()

	tx, err := s.DB.Client.Tx(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("start transaction failed: %v", err)})
		return
	}

	if _, err := tx.TagPreference.Delete().Exec(ctx); err != nil {
		tx.Rollback()
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("clear tag settings failed: %v", err)})
		return
	}

	if len(normalized) > 0 {
		builders := make([]*ent.TagPreferenceCreate, 0, len(normalized))
		for _, t := range normalized {
			builders = append(builders, tx.TagPreference.Create().SetTag(t.Tag).SetAction(t.Action))
		}
		if _, err := tx.TagPreference.CreateBulk(builders...).Save(ctx); err != nil {
			tx.Rollback()
			c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("save tag settings failed: %v", err)})
			return
		}
	}

	if err := tx.Commit(); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("commit tag settings failed: %v", err)})
		return
	}

	c.JSON(http.StatusOK, model.TagSettingsResponse{Tags: normalized})
}
