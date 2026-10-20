package app

import (
	"context"
	"sort"
	"strconv"
	"strings"

	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
)

// TagAction is how a tag rule marks a tag: liked (blue), disliked (red) or
// cleared (no rule).
type TagAction string

const (
	TagLike    TagAction = "like"
	TagDislike TagAction = "dislike"
)

// TagRule is one tag preference, mirroring ExHentai's My Tags: an action and a
// sort weight in [-999, 999]. This app only colours and orders tags with it;
// it never filters.
type TagRule struct {
	Tag    string
	Action TagAction
	Weight int
}

// defaultTagWeight is the weight the backend gives a rule that has none.
func defaultTagWeight(a TagAction) int {
	if a == TagDislike {
		return -10
	}
	return 10
}

// TagSettings is the tag-rule page state.
type TagSettings struct {
	rules   []TagRule
	loaded  bool
	loading bool
	err     string
	loadID  int
	busy    bool
	msg     string

	// input is the text field for adding a rule; tagQuery searches the
	// translation database.
	input    string
	tagQuery string
	action   TagAction
	weight   string
	inputErr string

	// sync options and the last result.
	syncConfirmDeletes string
	syncResolution     string
	syncing            bool
	syncErr            string
	syncResult         string
	// conflicts is the dialog for tags both sides changed; nil when closed.
	conflicts *conflictDialog
}

// conflictDialog resolves the sync's conflicts one tag at a time. Each item
// shows the local and source rule; the answer map keeps the decisions made so
// far, and submitting sends them all in one sync.
type conflictDialog struct {
	open     bool
	selected int
	items    []conflictItem
	choices  map[string]string // tag -> "local" or "remote"
	// submit asks the caller to run the sync with choices; the caller clears
	// it once it has.
	submit bool
}

// conflictItem is one tag both sides changed.
type conflictItem struct {
	tag    string
	local  *apiTagRule
	remote *apiTagRule
}

// tagRule returns the rule of a namespace:name tag, if any.
func (a *App) tagRule(tag string) *TagRule {
	if tag == "" {
		return nil
	}
	key := strings.ToLower(tag)
	for i := range a.tags.rules {
		if a.tags.rules[i].Tag == key {
			return &a.tags.rules[i]
		}
	}
	return nil
}

// ensureTagSettings loads the rules once.
func (a *App) ensureTagSettings() {
	s := &a.tags
	if s.loading || s.loaded {
		return
	}
	a.loadTagSettings()
}

func (a *App) loadTagSettings() {
	s := &a.tags
	if s.loading {
		return
	}
	s.loading = true
	s.err = ""
	s.loadID++
	lid := s.loadID
	go func() {
		rules, err := a.client.TagSettings(context.Background())
		a.update(func() {
			if s.loadID != lid {
				return
			}
			s.loading = false
			if err != nil {
				s.err = err.Error()
				return
			}
			s.rules = rules
			s.loaded = true
		})
	}()
}

// saveTagRules sends the whole list, as the API replaces all rules at once.
func (a *App) saveTagRules() {
	s := &a.tags
	if s.busy {
		return
	}
	s.busy = true
	s.msg = ""
	rules := append([]TagRule(nil), s.rules...)
	go func() {
		saved, err := a.client.SaveTagSettings(context.Background(), rules)
		a.update(func() {
			s.busy = false
			if err != nil {
				s.msg = err.Error()
				return
			}
			s.rules = saved
			s.msg = i18n.T("Saved")
		})
	}()
}

// tagInputKey normalises the add field the way the backend does.
func tagInputKey(raw string) string {
	return strings.ToLower(strings.TrimSpace(raw))
}

// addTagRule validates the add field and appends the rule locally, then saves.
func (a *App) addTagRule() {
	s := &a.tags
	key := tagInputKey(s.input)
	if key == "" {
		s.inputErr = i18n.T("Enter a tag, e.g. female:yuri")
		return
	}
	if !strings.Contains(key, ":") {
		// The API accepts bare tags too; a namespace keeps it explicit.
		key = "tag:" + key
	}
	weight := defaultTagWeight(s.action)
	if raw := strings.TrimSpace(s.weight); raw != "" {
		n, err := strconv.Atoi(raw)
		if err != nil || n < -999 || n > 999 {
			s.inputErr = i18n.T("Weight must be an integer from -999 to 999")
			return
		}
		weight = n
	}

	rule := TagRule{Tag: key, Action: s.action, Weight: weight}
	replaced := false
	for i := range s.rules {
		if s.rules[i].Tag == key {
			s.rules[i] = rule
			replaced = true
			break
		}
	}
	if !replaced {
		s.rules = append(s.rules, rule)
	}
	sort.Slice(s.rules, func(i, j int) bool { return s.rules[i].Tag < s.rules[j].Tag })

	s.input = ""
	s.weight = ""
	s.inputErr = ""
	a.saveTagRules()
}

// removeTagRule drops a rule and saves.
func (a *App) removeTagRule(tag string) {
	s := &a.tags
	kept := s.rules[:0]
	for _, r := range s.rules {
		if r.Tag != tag {
			kept = append(kept, r)
		}
	}
	s.rules = kept
	a.saveTagRules()
}

// toggleTagRule flips a tag between liked, disliked and neutral, which the
// gallery page's tag chips call.
func (a *App) toggleTagRule(tag string) {
	s := &a.tags
	cur := a.tagRule(tag)
	for _, r := range s.rules {
		if r.Tag == strings.ToLower(tag) {
			cur = &r
		}
	}
	next := TagLike
	if cur != nil {
		switch cur.Action {
		case TagLike:
			next = TagDislike
		default:
			next = ""
		}
	}

	kept := make([]TagRule, 0, len(s.rules)+1)
	for _, r := range s.rules {
		if r.Tag != strings.ToLower(tag) {
			kept = append(kept, r)
		}
	}
	if next != "" {
		kept = append(kept, TagRule{Tag: strings.ToLower(tag), Action: next, Weight: defaultTagWeight(next)})
	}
	sort.Slice(kept, func(i, j int) bool { return kept[i].Tag < kept[j].Tag })
	s.rules = kept
	a.saveTagRules()
}

// syncTagSettings runs the three-way merge with the source's My Tags. Tags
// both sides changed come back as conflicts; the dialog the page shows
// resolves them one by one, and then the sync runs again with those answers.
func (a *App) syncTagSettings() {
	s := &a.tags
	if s.syncing {
		return
	}
	confirmDeletes := strings.TrimSpace(s.syncConfirmDeletes) == i18n.T("Type 确认删除")
	resolution := ""
	switch s.syncResolution {
	case i18n.T("Keep source rule"):
		resolution = "remote"
	case i18n.T("Keep local rule"):
		resolution = "local"
	}
	a.runTagSync(confirmDeletes, resolution, nil)
}

// runTagSync performs one sync request. choices are per-tag answers that take
// precedence over resolution.
func (a *App) runTagSync(confirmDeletes bool, resolution string, choices []syncConflictChoice) {
	s := &a.tags
	s.syncing = true
	s.syncErr = ""
	s.syncResult = ""

	go func() {
		res, err := a.client.SyncTagSettings(context.Background(), confirmDeletes, resolution, choices)
		a.update(func() {
			s.syncing = false
			if err != nil {
				s.syncErr = err.Error()
				return
			}
			s.rules = tagRulesFromAPI(res.Tags)
			s.loaded = true
			s.syncResult = syncSummary(res)
			// A fresh conflict list replaces the old answers: they were for
			// the state before this sync.
			s.conflicts = newConflictDialog(res.Conflicts)
		})
	}()
}

// syncSummary is the one-line account of what a sync did.
func syncSummary(res apiTagSyncResult) string {
	line := i18n.Sprintf(
		i18n.T("Synced: %d pulled · %d added · %d updated · %d deleted · %d unchanged · %d pending deletions · %d conflicts · %d failed"),
		res.Pulled, res.Added, res.Updated, res.Deleted, res.Unchanged,
		len(res.PendingDeletes), len(res.Conflicts), len(res.Failed))
	if len(res.Failed) > 0 {
		// The first failure says why; the rest are listed by tag.
		line = i18n.Sprintf(i18n.T("%s; first failure: %s"), line, res.Failed[0].Error)
	}
	return line
}

// newConflictDialog opens the dialog for the conflicts a sync reported. It
// returns nil (closed) when there are none.
func newConflictDialog(conflicts []apiTagSyncConflict) *conflictDialog {
	if len(conflicts) == 0 {
		return nil
	}
	d := &conflictDialog{open: true, choices: map[string]string{}}
	for _, c := range conflicts {
		d.items = append(d.items, conflictItem{tag: c.Tag, local: c.Local, remote: c.Remote})
	}
	return d
}

// applyConflictChoice records the answer for the tag at index and moves to
// the next undecided one.
func (d *conflictDialog) applyConflictChoice(index int, resolution string) {
	if index < 0 || index >= len(d.items) {
		return
	}
	if d.choices == nil {
		d.choices = map[string]string{}
	}
	d.choices[d.items[index].tag] = resolution
	if next := d.nextUndecided(); next >= 0 {
		d.selected = next
		return
	}
	// The last answer: nothing left to ask.
	d.open = false
	d.submit = true
}

// nextUndecided is the first item after selected without an answer, then the
// first one anywhere, or -1 when all are answered.
func (d *conflictDialog) nextUndecided() int {
	for offset := 1; offset <= len(d.items); offset++ {
		i := (d.selected + offset) % len(d.items)
		if _, done := d.choices[d.items[i].tag]; !done {
			return i
		}
	}
	return -1
}

// remainingConflicts counts the answers still missing.
func (d *conflictDialog) remainingConflicts() int {
	n := 0
	for _, it := range d.items {
		if _, done := d.choices[it.tag]; !done {
			n++
		}
	}
	return n
}

// choiceList turns the answers into the request's list, in the order the
// conflicts were reported.
func (d *conflictDialog) choiceList() []syncConflictChoice {
	out := make([]syncConflictChoice, 0, len(d.items))
	for _, it := range d.items {
		if c, ok := d.choices[it.tag]; ok {
			out = append(out, syncConflictChoice{Tag: it.tag, Resolution: c})
		}
	}
	return out
}

// conflictSideLabel names one side of a conflict: the rule, or that the tag
// was deleted there.
func conflictSideLabel(side *apiTagRule) string {
	if side == nil {
		return i18n.T("Deleted")
	}
	label := i18n.T("Like")
	if side.Action == string(TagDislike) {
		label = i18n.T("Dislike")
	}
	return i18n.Sprintf("%s · weight %d", label, side.Weight)
}

// splitTag splits a "namespace:name" tag; a bare tag has an empty namespace.
func splitTag(tag string) (namespace, name string) {
	if i := strings.IndexByte(tag, ':'); i >= 0 {
		return tag[:i], tag[i+1:]
	}
	return "", tag
}

// tagChipText is the label of a tag chip, as the site writes it.
func tagChipText(namespace, name string) string {
	if namespace == "" {
		return name
	}
	return namespace + ":" + name
}

// tagActionColor is the chip colour of a rule: ExHentai's blue and red.
func tagActionColor(action TagAction) ui.Color {
	if action == TagDislike {
		return ui.Hex("#FF6666")
	}
	return ui.Hex("#3377FF")
}

// tagChip draws one tag, coloured and ordered by its rule, with its
// translation when the database knows one.
func (a *App) tagChip(c *ui.Context, namespace, name string) {
	tag := tagChipText(namespace, name)
	label := tag
	if tr := a.translations.Lookup(namespace, name); tr != "" && tr != name {
		label = tr + " / " + tag
	}
	rule := a.tagRule(tag)
	if rule == nil {
		ui.Badge(c, label)
		return
	}
	ui.Badge(c, label).Background(tagActionColor(rule.Action)).TextColor(ui.Hex("#f1f1f1"))
}

// sortedTags orders a gallery's tags likes first, then dislikes, then the
// rest, strongest weight first inside each group.
func (a *App) sortedTags(tags []apiTag) []apiTag {
	rank := func(t apiTag) int {
		if r := a.tagRule(t.Namespace + ":" + t.Name); r != nil {
			if r.Action == TagLike {
				return 0
			}
			return 1
		}
		return 2
	}
	weight := func(t apiTag) int {
		if r := a.tagRule(tagChipText(t.Namespace, t.Name)); r != nil {
			if r.Weight < 0 {
				return -r.Weight
			}
			return r.Weight
		}
		return 0
	}
	out := append([]apiTag(nil), tags...)
	sort.SliceStable(out, func(i, j int) bool {
		ri, rj := rank(out[i]), rank(out[j])
		if ri != rj {
			return ri < rj
		}
		return weight(out[i]) > weight(out[j])
	})
	return out
}

// tagSettingsPage edits the tag rules and syncs them with the source.
func (a *App) tagSettingsPage(c *ui.Context) {
	t := c.Theme()
	a.ensureTagSettings()
	a.ensureTranslations()
	s := &a.tags

	ui.Scroll(c).Fill().Children(func() {
		ui.Column(c).Padding(20, 24).Gap(14).MaxWidth(900).Children(func() {
			ui.Row(c).AlignItems(ui.End).Gap(12).Children(func() {
				ui.Column(c).Gap(2).Grow(1).Children(func() {
					ui.Text(c, i18n.T("Tag settings")).FontSize(26).Bold()
					ui.Text(c, i18n.T("Liked tags turn blue, disliked red; this only colours and orders, it never filters.")).
						FontSize(12).TextColor(t.TextMuted)
				})
				if ui.Button(c, i18n.T("Refresh")).Clicked() {
					a.loadTagSettings()
				}
			})

			a.tagAddRow(c, s)

			switch {
			case s.loading && !s.loaded:
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					ui.Text(c, i18n.T("Loading rules…")).FontSize(12).TextColor(t.TextMuted)
				})
			case s.err != "" && !s.loaded:
				a.errorBlock(c, s.err, func() { a.loadTagSettings() })
			case len(s.rules) == 0:
				ui.Text(c, i18n.T("No rules yet. Type a tag above and choose Like or Dislike.")).
					FontSize(12).TextColor(t.TextMuted)
			default:
				ui.Column(c).Gap(6).Children(func() {
					for _, r := range s.rules {
						a.tagRuleRow(c, r)
					}
				})
			}

			if s.busy {
				ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
					ui.Spinner(c)
					ui.Text(c, i18n.T("Saving…")).FontSize(12).TextColor(t.TextMuted)
				})
			} else if s.msg != "" {
				ui.Text(c, s.msg).FontSize(12).TextColor(t.TextMuted)
			}

			ui.Divider(c)
			a.tagSearchPanel(c, s)

			ui.Divider(c)
			ui.Text(c, i18n.T("Synced with the source")).FontSize(16).Bold()
			ui.Text(c, i18n.T("Three-way merge of local rules and the source's My Tags; tags both sides changed need a choice.")).
				FontSize(12).TextColor(t.TextMuted)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.TextInput(c, &s.syncConfirmDeletes).Width(180).
					Label(i18n.T("Confirm source deletions")).Placeholder(i18n.T("Type 确认删除"))
				ui.TextInput(c, &s.syncResolution).Width(160).
					Label(i18n.T("Conflict handling")).Placeholder(i18n.T("Keep local / keep source"))
				if ui.Button(c, i18n.T("Sync")).Disabled(s.syncing).Clicked() {
					a.syncTagSettings()
				}
				if s.syncing {
					ui.Spinner(c)
				}
			})
			if s.syncErr != "" {
				ui.Text(c, s.syncErr).FontSize(12).TextColor(t.Danger)
			}
			if s.syncResult != "" {
				ui.Text(c, s.syncResult).FontSize(12).TextColor(t.TextMuted)
			}
		})
	})

	a.tagConflictDialog(c, s)
}

// tagConflictDialog shows the tags both sides changed and takes one answer
// for each: 保留本地 pushes the local rule over the source's, 保留源站 takes
// the source's. When every conflict has an answer the dialog hands the list
// to the page, which runs the sync again with them.
func (a *App) tagConflictDialog(c *ui.Context, s *TagSettings) {
	d := s.conflicts
	if d == nil {
		return
	}
	t := c.Theme()
	if d.selected < 0 || d.selected >= len(d.items) {
		d.selected = 0
	}
	// A frame after the last answer (or after Apply) closes the dialog
	// before building it, so the closed dialog is never drawn.
	if d.submit || d.remainingConflicts() == 0 {
		d.open = false
	}
	it := d.items[d.selected]
	ui.Modal(c, &d.open, func() {
		ui.Column(c).Width(560).Gap(12).Children(func() {
			ui.Textf(c, i18n.T("Sync conflict %d / %d"), d.selected+1, len(d.items)).FontSize(18).Bold()
			ui.Text(c, i18n.T("Both sides changed this tag. Choose which side to keep.")).
				FontSize(12).TextColor(t.TextMuted)
			ui.Text(c, it.tag).FontSize(15).Bold()
			ui.Column(c).Gap(4).Children(func() {
				ui.Textf(c, i18n.T("Local: %s"), conflictSideLabel(it.local)).FontSize(12)
				ui.Textf(c, i18n.T("Source: %s"), conflictSideLabel(it.remote)).FontSize(12)
			})
			if chosen, done := d.choices[it.tag]; done {
				label := i18n.T("Keep source rule")
				if chosen == "local" {
					label = i18n.T("Keep local rule")
				}
				ui.Badge(c, i18n.T("Chosen: ")+label)
			}
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.Button(c, "保留本地").Label(i18n.T("Keep local rule")).Clicked() {
					d.applyConflictChoice(d.selected, "local")
				}
				if ui.Button(c, "保留源站").Label(i18n.T("Keep source rule")).Clicked() {
					d.applyConflictChoice(d.selected, "remote")
				}
				if ui.Button(c, i18n.T("Skip")).Label("跳过此冲突").Clicked() {
					d.selected = (d.selected + 1) % len(d.items)
				}
				if ui.Button(c, "稍后").Label(i18n.T("Close the conflict dialog")).Clicked() {
					d.open = false
				}
			})
			ui.Textf(c, i18n.T("%d conflicts still unanswered"), d.remainingConflicts()).
				FontSize(11).TextColor(t.TextMuted)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				if ui.PrimaryButton(c, i18n.T("Apply choices")).Disabled(len(d.choices) == 0 || s.syncing).Clicked() {
					d.open = false
					d.submit = true
				}
				if s.syncing {
					ui.Spinner(c)
				}
			})
		})
	})
	if !d.open {
		// Closed from the backdrop, Escape or 稍后. Every answer in means
		// the dialog is done, so the answers are applied; a close without
		// answers drops it and the conflicts come back with the next sync.
		switch {
		case d.remainingConflicts() == 0 && len(d.items) > 0:
			d.submit = true
		case len(d.choices) == 0:
			s.conflicts = nil
		}
	}
	if d.submit {
		s.conflicts = nil
		a.runTagSync(strings.TrimSpace(s.syncConfirmDeletes) == i18n.T("Type 确认删除"), "", d.choiceList())
	}
}

func (a *App) tagAddRow(c *ui.Context, s *TagSettings) {
	if s.action != TagLike && s.action != TagDislike {
		s.action = TagLike
	}
	ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
		if ui.TextInput(c, &s.input).Width(240).Label(i18n.T("Add tag")).
			Placeholder("female:yuri").Submitted() {
			a.addTagRule()
		}
		action := i18n.T("Like")
		if s.action == TagDislike {
			action = i18n.T("Dislike")
		}
		if ui.Button(c, action).Label(i18n.T("Toggle like/dislike")).Clicked() {
			if s.action == TagLike {
				s.action = TagDislike
			} else {
				s.action = TagLike
			}
		}
		ui.TextInput(c, &s.weight).Width(100).Label(i18n.T("Weight")).
			Placeholder(strconv.Itoa(defaultTagWeight(s.action)))
		if ui.PrimaryButton(c, i18n.T("Add")).Clicked() {
			a.addTagRule()
		}
	})
	if s.inputErr != "" {
		ui.Text(c, s.inputErr).FontSize(11).TextColor(c.Theme().Danger)
	}
}

func (a *App) tagRuleRow(c *ui.Context, r TagRule) {
	t := c.Theme()
	ui.Row(c).Gap(10).AlignItems(ui.Center).Padding(8).Radius(8).
		Border(1, t.Border).Children(func() {
		label := i18n.T("Like")
		if r.Action == TagDislike {
			label = i18n.T("Dislike")
		}
		ui.Badge(c, label).Background(tagActionColor(r.Action)).
			TextColor(ui.Hex("#f1f1f1"))
		ui.Text(c, r.Tag).Grow(1).SingleLine()
		ui.Textf(c, i18n.T("Weight %d"), r.Weight).FontSize(11).TextColor(t.TextMuted)
		if ui.Button(c, i18n.T("Delete")).Label(i18n.T("Delete tag rule")).Clicked() {
			a.removeTagRule(r.Tag)
		}
	})
}

// syncTagRulesLocked copies the current rules into the lists and details that
// draw tag chips when they changed, so a new rule recolours what is on screen
// without refetching anything.
func (a *App) syncTagRulesLocked() {
	rules := a.tags.rules
	if len(rules) == len(a.announcedRules) {
		same := true
		for i := range rules {
			if rules[i] != a.announcedRules[i] {
				same = false
				break
			}
		}
		if same {
			return
		}
	}
	a.announcedRules = append([]TagRule(nil), rules...)
	apply := func(g *Gallery) {
		if len(g.Tags) == 0 {
			return
		}
		g.Tags = append([]apiTag(nil), g.Tags...)
	}
	for _, s := range a.lists {
		for i := range s.items {
			apply(&s.items[i])
		}
	}
	for _, d := range a.details {
		apply(&d.g)
	}
}

// isLoading reports whether a load or update is running.
func (db *TagTranslationDB) isLoading() bool {
	db.mu.RLock()
	defer db.mu.RUnlock()
	return db.loading
}

// tagSuggestionRow is one translation hit in the tag page; clicking it fills
// the add field with namespace:name.
func (a *App) tagSuggestionRow(c *ui.Context, s *TagSettings, sug TagSuggestion) {
	t := c.Theme()
	tag := sug.Namespace + ":" + sug.Tag
	row := ui.Column(c).Gap(2).Padding(6).Radius(6).Focusable().Label(i18n.Tf("Use %s", tag))
	row.Children(func() {
		ui.Text(c, tag+" — "+sug.Translation).FontSize(12)
		if sug.Description != "" {
			ui.Text(c, sug.Description).FontSize(10).TextColor(t.TextMuted).SingleLine()
		}
	})
	if row.Clicked() {
		s.input = tag
		s.inputErr = ""
		s.tagQuery = ""
	}
}

// tagSearchPanel shows the translation database's state, a query box and its
// matches, and the update, import and export buttons.
func (a *App) tagSearchPanel(c *ui.Context, s *TagSettings) {
	t := c.Theme()
	db := &a.translations
	ui.Text(c, i18n.T("Tag translations")).FontSize(16).Bold()
	ui.Text(c, db.Status()).FontSize(12).TextColor(t.TextMuted)
	ui.Row(c).Gap(8).AlignItems(ui.End).Children(func() {
		ui.TextInput(c, &s.tagQuery).Width(320).Label(i18n.T("Search tag translations")).
			Placeholder(i18n.T("Type Chinese or English, e.g. 百合 / yuri"))
		if ui.Button(c, i18n.T("Update library")).Disabled(db.isLoading()).Clicked() {
			db.Update()
		}
		if ui.Button(c, i18n.T("Import custom translations")).Disabled(a.transBusy).Clicked() {
			a.importCustomTranslations()
		}
		if ui.Button(c, i18n.T("Export custom translations")).Disabled(a.transBusy).Clicked() {
			a.exportCustomTranslations()
		}
		if a.transBusy {
			ui.Spinner(c)
		}
	})
	if a.transMsg != "" {
		ui.Text(c, a.transMsg).FontSize(11).TextColor(t.TextMuted)
	}
	if db.CustomPath() != "" {
		ui.Text(c, i18n.Tf("Custom translations are saved to %s; imports merge into it.", db.CustomPath())).
			FontSize(11).TextColor(t.TextMuted)
	}
	for _, sug := range db.Search(s.tagQuery, 8) {
		a.tagSuggestionRow(c, s, sug)
	}
}
