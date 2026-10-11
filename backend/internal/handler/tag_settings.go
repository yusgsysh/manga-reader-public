package handler

import (
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/ent"
	"manga-reader/internal/model"
)

// maxTagSettings bounds a single PUT payload so a bad client cannot bloat
// the table; far above any realistic My Tags list.
const maxTagSettings = 10000

// Weight bounds mirror the source My Tags input (maxlength="3" digits plus
// sign); anything beyond ±999 is rejected rather than clamped silently.
const (
	minTagWeight = -999
	maxTagWeight = 999
)

// tagSettingsPutEntry is the wire shape of one rule in a PUT body. Weight is
// a pointer so an omitted weight can be defaulted from the action, while an
// explicit 0 is preserved.
type tagSettingsPutEntry struct {
	Tag    string `json:"tag"`
	Action string `json:"action"`
	Weight *int   `json:"weight"`
}

// normalizeTagRule validates one rule and returns the canonical form (tag
// trimmed/lowercased, weight defaulted and sign-checked against the action).
func normalizeTagRule(entry tagSettingsPutEntry) (model.TagPreference, error) {
	tag := strings.ToLower(strings.TrimSpace(entry.Tag))
	action := strings.TrimSpace(entry.Action)
	if tag == "" {
		return model.TagPreference{}, fmt.Errorf("tag must not be empty")
	}
	if action != model.TagActionLike && action != model.TagActionDislike {
		return model.TagPreference{}, fmt.Errorf("invalid action %q for tag %q", entry.Action, entry.Tag)
	}

	weight := defaultTagWeight(action)
	if entry.Weight != nil {
		weight = *entry.Weight
	}
	if weight < minTagWeight || weight > maxTagWeight {
		return model.TagPreference{}, fmt.Errorf("weight %d out of range [%d, %d] for tag %q", weight, minTagWeight, maxTagWeight, tag)
	}
	// The weight sign mirrors the action, exactly like the source (blue
	// positive / red negative); reject mixed signs so the mirror stays
	// consistent rather than silently flipping user input.
	if action == model.TagActionLike && weight < 0 {
		return model.TagPreference{}, fmt.Errorf("weight %d must be >= 0 for liked tag %q", weight, tag)
	}
	if action == model.TagActionDislike && weight > 0 {
		return model.TagPreference{}, fmt.Errorf("weight %d must be <= 0 for disliked tag %q", weight, tag)
	}

	return model.TagPreference{Tag: tag, Action: action, Weight: weight}, nil
}

// defaultTagWeight mirrors the source My Tags default (10), signed by action.
func defaultTagWeight(action string) int {
	if action == model.TagActionDislike {
		return -10
	}
	return 10
}

// handleTagSettingsGet returns all stored tag preferences, ordered by tag.
func (s *Server) handleTagSettingsGet(c *gin.Context) {
	tags, err := s.loadTagRules(c.Request.Context())
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("list tag settings failed: %v", err)})
		return
	}
	c.JSON(http.StatusOK, model.TagSettingsResponse{Tags: tags})
}

// handleTagSettingsPut replaces the full preference list. Rules are trimmed,
// lowercased, weight-defaulted and deduplicated (last occurrence wins) before
// persisting, so the stored set always matches what the UI would render.
func (s *Server) handleTagSettingsPut(c *gin.Context) {
	var body struct {
		Tags []tagSettingsPutEntry `json:"tags"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	if len(body.Tags) > maxTagSettings {
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("too many tags (max %d)", maxTagSettings)})
		return
	}

	// Deduplicate by tag, keeping the last rule for each.
	normalized := make([]model.TagPreference, 0, len(body.Tags))
	indexByTag := make(map[string]int, len(body.Tags))
	for _, entry := range body.Tags {
		rule, err := normalizeTagRule(entry)
		if err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		if i, ok := indexByTag[rule.Tag]; ok {
			normalized[i] = rule
			continue
		}
		indexByTag[rule.Tag] = len(normalized)
		normalized = append(normalized, rule)
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
			builders = append(builders, tx.TagPreference.Create().SetTag(t.Tag).SetAction(t.Action).SetWeight(t.Weight))
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
