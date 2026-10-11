package handler

import (
	"context"
	"fmt"
	"net/http"
	"sort"
	"strings"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/tagpreference"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/model"
)

// tagSyncUpdate is a remote row that exists locally but whose watch/hide/
// colour/weight no longer matches the local rule.
type tagSyncUpdate struct {
	Remote exhentai.UserTag
	Rule   model.TagPreference
}

// tagSyncPlan is the full-mirror diff between the local rules and the
// source My Tags page.
type tagSyncPlan struct {
	Adds      []model.TagPreference
	Updates   []tagSyncUpdate
	Deletes   []exhentai.UserTag // remote-only rows (candidates until confirmed)
	Unchanged int
}

// loadTagRules reads all stored tag preferences ordered by tag.
func (s *Server) loadTagRules(ctx context.Context) ([]model.TagPreference, error) {
	rows, err := s.DB.Client.TagPreference.Query().
		Order(ent.Asc(tagpreference.FieldTag)).
		All(ctx)
	if err != nil {
		return nil, err
	}
	tags := make([]model.TagPreference, 0, len(rows))
	for _, row := range rows {
		tags = append(tags, model.TagPreference{Tag: row.Tag, Action: row.Action, Weight: row.Weight})
	}
	return tags, nil
}

// ruleToUserTag maps a local rule to the source row state we want: like =
// Watched + non-negative weight, dislike = Hidden + non-negative weight,
// default colour (tagcolor "") so the source derives blue/red itself.
func ruleToUserTag(rule model.TagPreference) exhentai.UserTag {
	return exhentai.UserTag{
		Tag:    rule.Tag,
		Watch:  rule.Action == model.TagActionLike,
		Hide:   rule.Action == model.TagActionDislike,
		Color:  "",
		Weight: rule.Weight,
	}
}

// planTagSync diffs local rules against the remote snapshot. Match key is
// the lowercased tag; remote colour is compared case-insensitively after
// trimming.
func planTagSync(local []model.TagPreference, remote []exhentai.UserTag) tagSyncPlan {
	var plan tagSyncPlan

	remoteByKey := make(map[string]exhentai.UserTag, len(remote))
	for _, rt := range remote {
		remoteByKey[strings.ToLower(strings.TrimSpace(rt.Tag))] = rt
	}
	localKeys := make(map[string]bool, len(local))

	for _, rule := range local {
		key := strings.ToLower(strings.TrimSpace(rule.Tag))
		localKeys[key] = true
		want := ruleToUserTag(rule)
		rt, ok := remoteByKey[key]
		if !ok {
			plan.Adds = append(plan.Adds, rule)
			continue
		}
		if rt.Watch == want.Watch && rt.Hide == want.Hide && rt.Weight == want.Weight &&
			strings.EqualFold(strings.TrimSpace(rt.Color), want.Color) {
			plan.Unchanged++
			continue
		}
		plan.Updates = append(plan.Updates, tagSyncUpdate{Remote: rt, Rule: rule})
	}

	for _, rt := range remote {
		key := strings.ToLower(strings.TrimSpace(rt.Tag))
		if !localKeys[key] {
			plan.Deletes = append(plan.Deletes, rt)
		}
	}

	// Deterministic execution order (strongest mirror semantics, stable
	// logs): alphabetical by tag.
	sort.Slice(plan.Adds, func(i, j int) bool { return plan.Adds[i].Tag < plan.Adds[j].Tag })
	sort.Slice(plan.Updates, func(i, j int) bool { return plan.Updates[i].Rule.Tag < plan.Updates[j].Rule.Tag })
	sort.Slice(plan.Deletes, func(i, j int) bool { return plan.Deletes[i].Tag < plan.Deletes[j].Tag })

	return plan
}

// handleTagSettingsSync mirrors the local preference list onto the source
// My Tags page. Adds and updates always run; remote-only rows are only
// deleted when the caller passes confirm_deletes=true, otherwise they come
// back as pending_deletes for the client to confirm.
func (s *Server) handleTagSettingsSync(c *gin.Context) {
	var body struct {
		ConfirmDeletes bool `json:"confirm_deletes"`
	}
	// An empty body means confirm_deletes=false; only reject malformed JSON.
	if err := c.ShouldBindJSON(&body); err != nil && !strings.Contains(err.Error(), "EOF") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}

	if s.Client == nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "upstream client not configured"})
		return
	}

	// Serialize syncs so concurrent saves cannot interleave mutations on
	// the same source tagset.
	s.tagSyncMu.Lock()
	defer s.tagSyncMu.Unlock()

	ctx := c.Request.Context()

	local, err := s.loadTagRules(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("load tag settings failed: %v", err)})
		return
	}

	snapshot, err := exhentai.FetchMyTags(ctx, s.Client)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch mytags failed: %v", err)})
		return
	}

	plan := planTagSync(local, snapshot.Tags)
	result := model.TagSyncResult{
		Unchanged:      plan.Unchanged,
		PendingDeletes: make([]string, 0, len(plan.Deletes)),
		Failed:         make([]model.TagSyncFailure, 0),
	}

	// Execute adds first (new source rows), then updates, pausing between
	// mutations so a large first-time mirror stays polite to the upstream.
	for i, rule := range plan.Adds {
		if ctx.Err() != nil {
			failRemaining(&result, plan.Adds[i:], nil, ctx.Err())
			break
		}
		if err := exhentai.AddUserTagForm(ctx, s.Client, snapshot, ruleToUserTag(rule)); err != nil {
			result.Failed = append(result.Failed, model.TagSyncFailure{Tag: rule.Tag, Error: err.Error()})
			continue
		}
		result.Added++
		if err := exhentai.PauseMutations(ctx); err != nil {
			failRemaining(&result, plan.Adds[i+1:], nil, err)
			break
		}
	}

	for i, up := range plan.Updates {
		if ctx.Err() != nil {
			failRemaining(&result, nil, remainingUpdateTags(plan.Updates[i:]), ctx.Err())
			break
		}
		want := ruleToUserTag(up.Rule)
		want.TagID = up.Remote.TagID
		if err := exhentai.SetUserTag(ctx, s.Client, snapshot, want); err != nil {
			result.Failed = append(result.Failed, model.TagSyncFailure{Tag: up.Rule.Tag, Error: err.Error()})
			continue
		}
		result.Updated++
		if err := exhentai.PauseMutations(ctx); err != nil {
			failRemaining(&result, nil, remainingUpdateTags(plan.Updates[i+1:]), err)
			break
		}
	}

	if len(plan.Deletes) > 0 {
		if body.ConfirmDeletes {
			ids := make([]int64, len(plan.Deletes))
			for i, d := range plan.Deletes {
				ids[i] = d.TagID
			}
			if err := exhentai.DeleteUserTagsForm(ctx, s.Client, snapshot, ids); err != nil {
				for _, d := range plan.Deletes {
					result.Failed = append(result.Failed, model.TagSyncFailure{Tag: d.Tag, Error: err.Error()})
				}
			} else {
				result.Deleted = len(plan.Deletes)
			}
		} else {
			for _, d := range plan.Deletes {
				result.PendingDeletes = append(result.PendingDeletes, d.Tag)
			}
		}
	}

	c.JSON(http.StatusOK, result)
}

// failRemaining records tags the sync did not get to as failed with the
// abort reason, so the client always sees the full picture.
func failRemaining(r *model.TagSyncResult, adds []model.TagPreference, updateTags []string, err error) {
	reason := "sync aborted: " + err.Error()
	for _, rule := range adds {
		r.Failed = append(r.Failed, model.TagSyncFailure{Tag: rule.Tag, Error: reason})
	}
	for _, tag := range updateTags {
		r.Failed = append(r.Failed, model.TagSyncFailure{Tag: tag, Error: reason})
	}
}

func remainingUpdateTags(updates []tagSyncUpdate) []string {
	tags := make([]string, 0, len(updates))
	for _, up := range updates {
		tags = append(tags, up.Rule.Tag)
	}
	return tags
}
