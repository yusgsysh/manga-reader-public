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
	"manga-reader/internal/ent/tagsyncbaseline"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/model"
)

// syncKey normalizes a tag for matching local rules, baseline rows and
// source rows against each other.
func syncKey(tag string) string {
	return strings.ToLower(strings.TrimSpace(tag))
}

// tagSyncUpdate is a source row that exists locally but whose watch/hide/
// weight no longer matches the local rule.
type tagSyncUpdate struct {
	Remote exhentai.UserTag
	Rule   model.TagPreference
}

// tagSyncDelete is a source row whose local rule was deleted. Consented
// means the user approved this deletion in the conflict dialog, so it runs
// without confirm_deletes.
type tagSyncDelete struct {
	Remote    exhentai.UserTag
	Consented bool
}

// tagPullOp applies source state to the local list: Existing is the exact
// local rule to replace (nil for imports), Remote nil means the source
// deleted the tag and the local rule must go.
type tagPullOp struct {
	Key      string
	Existing *model.TagPreference
	Remote   *model.TagPreference
}

// tagSyncPlan is the three-way merge between local rules, the sync baseline
// (source state at the last sync) and the fetched source snapshot.
type tagSyncPlan struct {
	Adds      []model.TagPreference
	Updates   []tagSyncUpdate
	Deletes   []tagSyncDelete
	Pulls     []tagPullOp
	Conflicts []model.TagSyncConflict
	// BaselineAfter maps sync key to the baseline row to keep after this
	// sync (nil deletes the row). Keys absent from the map keep their
	// existing row — that is how unresolved conflicts stay untouched. The
	// defaults reflect the source state before our writes; the handler
	// overrides them as pushes succeed so failed pushes retry next sync.
	BaselineAfter map[string]*model.TagPreference
	Unchanged     int
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

// loadTagBaseline reads the last-synced source snapshot used as the merge
// common ancestor.
func (s *Server) loadTagBaseline(ctx context.Context) ([]model.TagPreference, error) {
	rows, err := s.DB.Client.TagSyncBaseline.Query().
		Order(ent.Asc(tagsyncbaseline.FieldTag)).
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
// Watched + positive weight, dislike = Hidden + negative weight (verified
// against the live API: hidden rows accept negative weights), default colour
// (tagcolor "") so the source derives blue/red itself.
func ruleToUserTag(rule model.TagPreference) exhentai.UserTag {
	return exhentai.UserTag{
		Tag:    rule.Tag,
		Watch:  rule.Action == model.TagActionLike,
		Hide:   rule.Action == model.TagActionDislike,
		Color:  "",
		Weight: rule.Weight,
	}
}

// normalizeRemoteTag maps a source row into the local rule shape: Hide →
// dislike, Watch → like, neither → sign of the weight; the weight is clamped
// to the local bounds and to its action's sign. The source colour is not
// part of the local model and is ignored.
func normalizeRemoteTag(rt exhentai.UserTag) model.TagPreference {
	action := model.TagActionLike
	switch {
	case rt.Hide:
		action = model.TagActionDislike
	case rt.Watch:
		action = model.TagActionLike
	default:
		if rt.Weight < 0 {
			action = model.TagActionDislike
		}
	}
	weight := rt.Weight
	if weight > maxTagWeight {
		weight = maxTagWeight
	}
	if weight < minTagWeight {
		weight = minTagWeight
	}
	if action == model.TagActionDislike && weight > 0 {
		weight = -weight
	}
	if action == model.TagActionLike && weight < 0 {
		weight = -weight
	}
	return model.TagPreference{Tag: syncKey(rt.Tag), Action: action, Weight: weight}
}

// ruleChanged reports whether one side differs from the baseline; absence is
// a distinct value, so appearing/disappearing counts as a change.
func ruleChanged(v model.TagPreference, hasV bool, b model.TagPreference, hasB bool) bool {
	if hasV != hasB {
		return true
	}
	if !hasV {
		return false
	}
	return v.Action != b.Action || v.Weight != b.Weight
}

// canonicalRule returns the rule stored under its sync key.
func canonicalRule(key string, rule model.TagPreference) model.TagPreference {
	return model.TagPreference{Tag: key, Action: rule.Action, Weight: rule.Weight}
}

// conflictSide snapshots one side of a conflict (nil = deleted there).
func conflictSide(rule model.TagPreference, has bool) *model.TagPreference {
	if !has {
		return nil
	}
	side := canonicalRule(syncKey(rule.Tag), rule)
	return &side
}

type mergeRemoteRow struct {
	Row  exhentai.UserTag
	Rule model.TagPreference
}

// addPull records a source → local operation and its baseline outcome.
func (p *tagSyncPlan) addPull(key string, local model.TagPreference, hasLocal bool, remote mergeRemoteRow, hasRemote bool) {
	if !hasRemote {
		// The source deleted the tag: drop the local rule, retire the baseline row.
		if hasLocal {
			// Keep the exact stored tag so the delete matches regardless of case.
			existing := local
			p.Pulls = append(p.Pulls, tagPullOp{Key: key, Existing: &existing})
		}
		p.BaselineAfter[key] = nil
		return
	}
	pulled := canonicalRule(key, remote.Rule)
	var existing *model.TagPreference
	if hasLocal {
		rule := local
		existing = &rule
	}
	p.Pulls = append(p.Pulls, tagPullOp{Key: key, Existing: existing, Remote: &pulled})
	p.BaselineAfter[key] = &pulled
}

// addPush records a local → source operation. The default baseline outcome
// is the source state before our write, so a failed push retries next sync.
func (p *tagSyncPlan) addPush(key string, local model.TagPreference, hasLocal bool, remote mergeRemoteRow, hasRemote bool, consented bool) {
	if hasRemote {
		source := canonicalRule(key, remote.Rule)
		p.BaselineAfter[key] = &source
	} else {
		p.BaselineAfter[key] = nil
	}
	switch {
	case !hasLocal:
		// The local rule was deleted: delete the source row (confirm-gated
		// unless the conflict dialog already consented to it).
		p.Deletes = append(p.Deletes, tagSyncDelete{Remote: remote.Row, Consented: consented})
	case !hasRemote:
		p.Adds = append(p.Adds, canonicalRule(key, local))
	default:
		p.Updates = append(p.Updates, tagSyncUpdate{Remote: remote.Row, Rule: canonicalRule(key, local)})
	}
}

// mergeTagState three-way merges local rules, the sync baseline and the
// fetched source snapshot. Match key is the lowercased tag. Per tag:
//
//   - both sides agree → unchanged (baseline converges)
//   - only local changed → push (adds/updates; deletions gated by confirm)
//   - only source changed → pull (local rules updated/removed)
//   - both changed → conflict: unresolved without a resolution, otherwise
//     "remote" pulls the source value, "local" pushes the local value
//
// With an empty baseline (first run) everything counts as changed on the
// side that exists: source-only rows are imported, local-only rows are
// pushed, and overlapping rows that differ are conflicts.
func mergeTagState(local, baseline []model.TagPreference, remote []exhentai.UserTag, resolution string) tagSyncPlan {
	plan := tagSyncPlan{BaselineAfter: make(map[string]*model.TagPreference)}

	localByKey := make(map[string]model.TagPreference, len(local))
	for _, rule := range local {
		localByKey[syncKey(rule.Tag)] = rule
	}
	baseByKey := make(map[string]model.TagPreference, len(baseline))
	for _, rule := range baseline {
		baseByKey[syncKey(rule.Tag)] = rule
	}
	remoteByKey := make(map[string]mergeRemoteRow, len(remote))
	for _, rt := range remote {
		remoteByKey[syncKey(rt.Tag)] = mergeRemoteRow{Row: rt, Rule: normalizeRemoteTag(rt)}
	}

	keySet := make(map[string]bool, len(localByKey)+len(baseByKey)+len(remoteByKey))
	for key := range localByKey {
		keySet[key] = true
	}
	for key := range baseByKey {
		keySet[key] = true
	}
	for key := range remoteByKey {
		keySet[key] = true
	}
	keys := make([]string, 0, len(keySet))
	for key := range keySet {
		keys = append(keys, key)
	}
	// Deterministic execution order (strongest mirror semantics, stable logs).
	sort.Strings(keys)

	for _, key := range keys {
		localRule, hasLocal := localByKey[key]
		baseRule, hasBase := baseByKey[key]
		remoteRow, hasRemote := remoteByKey[key]

		// Deleted on both sides: retire a stale baseline row, count nothing.
		if !hasLocal && !hasRemote {
			if hasBase {
				plan.BaselineAfter[key] = nil
			}
			continue
		}
		// Both present and equal: nothing to do, converge the baseline.
		if hasLocal && hasRemote &&
			localRule.Action == remoteRow.Rule.Action && localRule.Weight == remoteRow.Rule.Weight {
			plan.Unchanged++
			plan.BaselineAfter[key] = &model.TagPreference{
				Tag: key, Action: localRule.Action, Weight: localRule.Weight,
			}
			continue
		}

		localChanged := ruleChanged(localRule, hasLocal, baseRule, hasBase)
		remoteChanged := ruleChanged(remoteRow.Rule, hasRemote, baseRule, hasBase)

		switch {
		case localChanged && remoteChanged:
			if resolution == "" {
				// Unresolved: report and leave local, source and baseline alone.
				plan.Conflicts = append(plan.Conflicts, model.TagSyncConflict{
					Tag:    key,
					Local:  conflictSide(localRule, hasLocal),
					Remote: conflictSide(remoteRow.Rule, hasRemote),
				})
				continue
			}
			if resolution == "remote" {
				plan.addPull(key, localRule, hasLocal, remoteRow, hasRemote)
			} else {
				plan.addPush(key, localRule, hasLocal, remoteRow, hasRemote, true)
			}
		case remoteChanged:
			plan.addPull(key, localRule, hasLocal, remoteRow, hasRemote)
		case localChanged:
			plan.addPush(key, localRule, hasLocal, remoteRow, hasRemote, false)
		default:
			// Unreachable unless the baseline is corrupt: values differ but
			// neither side moved. Asking the user is the safe fallback.
			plan.Conflicts = append(plan.Conflicts, model.TagSyncConflict{
				Tag:    key,
				Local:  conflictSide(localRule, hasLocal),
				Remote: conflictSide(remoteRow.Rule, hasRemote),
			})
		}
	}

	return plan
}

// handleTagSettingsSync three-way merges the local rules with the source
// My Tags page: source changes are pulled into the local list, local changes
// are pushed to the source, and tags both sides changed are reported as
// conflicts until the client passes conflict_resolution ("remote"/"local").
// Source deletions of locally-deleted rows still require confirm_deletes
// (or an explicit conflict resolution).
func (s *Server) handleTagSettingsSync(c *gin.Context) {
	var body struct {
		ConfirmDeletes     bool   `json:"confirm_deletes"`
		ConflictResolution string `json:"conflict_resolution"`
	}
	// An empty body means confirm_deletes=false; only reject malformed JSON.
	if err := c.ShouldBindJSON(&body); err != nil && !strings.Contains(err.Error(), "EOF") {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid JSON body"})
		return
	}
	switch body.ConflictResolution {
	case "", "remote", "local":
	default:
		c.JSON(http.StatusBadRequest, gin.H{"error": fmt.Sprintf("invalid conflict_resolution %q", body.ConflictResolution)})
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
	baseline, err := s.loadTagBaseline(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("load tag sync baseline failed: %v", err)})
		return
	}

	snapshot, err := exhentai.FetchMyTags(ctx, s.Client)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": fmt.Sprintf("fetch mytags failed: %v", err)})
		return
	}

	plan := mergeTagState(local, baseline, snapshot.Tags, body.ConflictResolution)

	result := model.TagSyncResult{
		Unchanged:      plan.Unchanged,
		Pulled:         len(plan.Pulls),
		PendingDeletes: make([]string, 0, len(plan.Deletes)),
		Conflicts:      plan.Conflicts,
		Failed:         make([]model.TagSyncFailure, 0),
		Tags:           make([]model.TagPreference, 0),
	}
	if result.Conflicts == nil {
		result.Conflicts = make([]model.TagSyncConflict, 0)
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
		plan.BaselineAfter[syncKey(rule.Tag)] = &model.TagPreference{
			Tag: syncKey(rule.Tag), Action: rule.Action, Weight: rule.Weight,
		}
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
		plan.BaselineAfter[syncKey(up.Rule.Tag)] = &model.TagPreference{
			Tag: syncKey(up.Rule.Tag), Action: up.Rule.Action, Weight: up.Rule.Weight,
		}
		if err := exhentai.PauseMutations(ctx); err != nil {
			failRemaining(&result, nil, remainingUpdateTags(plan.Updates[i+1:]), err)
			break
		}
	}

	// Deletions: consented (conflict-resolution) run immediately, plain
	// local deletions wait for confirm_deletes and surface as pending.
	if len(plan.Deletes) > 0 {
		toDelete := make([]exhentai.UserTag, 0, len(plan.Deletes))
		for _, d := range plan.Deletes {
			if d.Consented || body.ConfirmDeletes {
				toDelete = append(toDelete, d.Remote)
			} else {
				result.PendingDeletes = append(result.PendingDeletes, d.Remote.Tag)
			}
		}
		if len(toDelete) > 0 {
			ids := make([]int64, len(toDelete))
			for i, d := range toDelete {
				ids[i] = d.TagID
			}
			if err := exhentai.DeleteUserTagsForm(ctx, s.Client, snapshot, ids); err != nil {
				for _, d := range toDelete {
					result.Failed = append(result.Failed, model.TagSyncFailure{Tag: d.Tag, Error: err.Error()})
				}
			} else {
				result.Deleted = len(toDelete)
				for _, d := range toDelete {
					plan.BaselineAfter[syncKey(d.Tag)] = nil
				}
			}
		}
	}

	// Apply pulls and the refreshed baseline in one transaction; a failure
	// here leaves everything as-is (the next sync redoes it).
	if err := s.applyTagSyncState(ctx, plan.Pulls, baseline, plan.BaselineAfter); err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("apply tag sync failed: %v", err)})
		return
	}

	tags, err := s.loadTagRules(ctx)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": fmt.Sprintf("reload tag settings failed: %v", err)})
		return
	}
	result.Tags = tags

	c.JSON(http.StatusOK, result)
}

// applyTagSyncState writes source → local pulls and the post-sync baseline
// in one transaction. The baseline starts from the rows loaded before the
// merge (so unresolved conflicts keep their row) and is overlaid with the
// plan's outcomes; keys mapped to nil drop their row.
func (s *Server) applyTagSyncState(
	ctx context.Context,
	pulls []tagPullOp,
	baseline []model.TagPreference,
	after map[string]*model.TagPreference,
) error {
	final := make(map[string]model.TagPreference, len(baseline)+len(after))
	for _, rule := range baseline {
		final[syncKey(rule.Tag)] = rule
	}
	for key, rule := range after {
		if rule == nil {
			delete(final, key)
			continue
		}
		final[key] = *rule
	}

	tx, err := s.DB.Client.Tx(ctx)
	if err != nil {
		return fmt.Errorf("start transaction: %w", err)
	}

	for _, op := range pulls {
		if op.Existing != nil {
			if _, err := tx.TagPreference.Delete().
				Where(tagpreference.TagEQ(op.Existing.Tag)).
				Exec(ctx); err != nil {
				tx.Rollback()
				return fmt.Errorf("remove pulled tag %q: %w", op.Key, err)
			}
		}
		if op.Remote != nil {
			if _, err := tx.TagPreference.Create().
				SetTag(op.Remote.Tag).
				SetAction(op.Remote.Action).
				SetWeight(op.Remote.Weight).
				Save(ctx); err != nil {
				tx.Rollback()
				return fmt.Errorf("import pulled tag %q: %w", op.Key, err)
			}
		}
	}

	if _, err := tx.TagSyncBaseline.Delete().Exec(ctx); err != nil {
		tx.Rollback()
		return fmt.Errorf("clear tag sync baseline: %w", err)
	}
	if len(final) > 0 {
		finalKeys := make([]string, 0, len(final))
		for key := range final {
			finalKeys = append(finalKeys, key)
		}
		sort.Strings(finalKeys)
		builders := make([]*ent.TagSyncBaselineCreate, 0, len(finalKeys))
		for _, key := range finalKeys {
			rule := final[key]
			builders = append(builders, tx.TagSyncBaseline.Create().
				SetTag(key).
				SetAction(rule.Action).
				SetWeight(rule.Weight))
		}
		if _, err := tx.TagSyncBaseline.CreateBulk(builders...).Save(ctx); err != nil {
			tx.Rollback()
			return fmt.Errorf("save tag sync baseline: %w", err)
		}
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit tag sync state: %w", err)
	}
	return nil
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
