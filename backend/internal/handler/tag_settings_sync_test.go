package handler

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	"manga-reader/internal/exhentai"
	"manga-reader/internal/model"
)

// syncMyTagsFixture is the /mytags page the sync tests serve: language:chinese
// watched/90, male:males only hidden/-10, male:gender change watched/#AABBCC/6.
const syncMyTagsFixture = `<!DOCTYPE html><html><head></head><body>
<script type="text/javascript">
var apiuid = 12345;
var apikey = "deadbeefcafe12345678";
</script>
<form id="tagset_form" action="" method="post">
	<div id="tagset_outer">
		<div><select onchange="change_tagset(this.value)"><option value="1" selected="selected">Tagset (3)</option></select></div>
		<div><input type="text" name="tagset_color" id="tagcolor" value="" /></div>
	</div>
</form>
<form id="usertag_form" action="" method="post">
	<input type="hidden" id="usertag_action" name="usertag_action" value="" />
	<div id="usertag_0">
		<div><input type="text" id="tagname_0" name="tagname_new" /></div>
		<div><input type="checkbox" id="tagwatch_0" name="tagwatch_new" /></div>
		<div><input type="checkbox" id="taghide_0" name="taghide_new" /></div>
		<div><input type="text" id="tagcolor_0" name="tagcolor_new" value="" /></div>
		<div><input type="text" id="tagweight_0" name="tagweight_new" value="10" /></div>
	</div>
	<div id="usertag_3437">
		<div><div id="tagpreview_3437" class="gt" title="language:chinese">chinese</div></div>
		<div><input type="checkbox" id="tagwatch_3437" checked="checked" /></div>
		<div><input type="checkbox" id="taghide_3437" /></div>
		<div><input type="text" id="tagcolor_3437" class="tagcolor" value="" /></div>
		<div><input type="text" id="tagweight_3437" value="90" /></div>
	</div>
	<div id="usertag_304301">
		<div><div id="tagpreview_304301" class="gt" title="male:males only">m:males only</div></div>
		<div><input type="checkbox" id="tagwatch_304301" /></div>
		<div><input type="checkbox" id="taghide_304301" checked="checked" /></div>
		<div><input type="text" id="tagcolor_304301" class="tagcolor" value="" /></div>
		<div><input type="text" id="tagweight_304301" value="-10" /></div>
	</div>
	<div id="usertag_5354">
		<div><div id="tagpreview_5354" class="gt" title="male:gender change">m:gender change</div></div>
		<div><input type="checkbox" id="tagwatch_5354" checked="checked" /></div>
		<div><input type="checkbox" id="taghide_5354" /></div>
		<div><input type="text" id="tagcolor_5354" class="tagcolor" value="#AABBCC" /></div>
		<div><input type="text" id="tagweight_5354" value="6" /></div>
	</div>
</form>
</body></html>`

// syncMyTagsFixtureWith appends one more row (used by add responses).
func syncMyTagsFixtureWith(tagID int64, tag, weight string) string {
	display := tag
	if parts := strings.SplitN(tag, ":", 2); len(parts) == 2 {
		display = parts[1]
	}
	row := fmt.Sprintf(`
	<div id="usertag_%d">
		<div><div id="tagpreview_%d" class="gt" title="%s">%s</div></div>
		<div><input type="checkbox" id="tagwatch_%d" checked="checked" /></div>
		<div><input type="checkbox" id="taghide_%d" /></div>
		<div><input type="text" id="tagcolor_%d" class="tagcolor" value="" /></div>
		<div><input type="text" id="tagweight_%d" value="%s" /></div>
	</div>`, tagID, tagID, tag, display, tagID, tagID, tagID, tagID, weight)
	return strings.Replace(syncMyTagsFixture, "</form>", row+"\n</form>", 1)
}

// newSyncMockServer serves the My Tags upstream: GET/POST /mytags and
// POST /api.php, recording every mutation.
type syncMockServer struct {
	*httptest.Server
	setUserTagCalls []map[string]any
	addForms        []map[string]string
	massDeleteForms [][]string
	nextAddID       int64
	failSetUserTag  bool
	failAddForm     bool
	failMassDelete  bool
}

func newSyncMockServer(t *testing.T) *syncMockServer {
	t.Helper()
	m := &syncMockServer{nextAddID: 900}
	m.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.URL.Path == "/api.php":
			defer r.Body.Close()
			var body map[string]any
			if err := json.UnmarshalRead(r.Body, &body); err != nil {
				t.Errorf("decode setusertag: %v", err)
			}
			m.setUserTagCalls = append(m.setUserTagCalls, body)
			w.Header().Set("Content-Type", "application/json")
			if m.failSetUserTag {
				fmt.Fprint(w, `{"error":"Something went wrong"}`)
				return
			}
			fmt.Fprintf(w, `{"tagid":%v}`, body["tagid"])
		case r.URL.Path == "/mytags" && r.Method == http.MethodPost:
			if err := r.ParseForm(); err != nil {
				t.Errorf("parse form: %v", err)
			}
			action := r.PostForm.Get("usertag_action")
			switch action {
			case "add":
				form := map[string]string{}
				for k, v := range r.PostForm {
					form[k] = v[0]
				}
				m.addForms = append(m.addForms, form)
				w.Header().Set("Content-Type", "text/html")
				if m.failAddForm {
					// Upstream ignored the add: respond without the new tag.
					fmt.Fprint(w, syncMyTagsFixture)
					return
				}
				m.nextAddID++
				fmt.Fprint(w, syncMyTagsFixtureWith(m.nextAddID, r.PostForm.Get("tagname_new"), r.PostForm.Get("tagweight_new")))
			case "mass":
				m.massDeleteForms = append(m.massDeleteForms, append([]string(nil), r.PostForm["modify_usertags[]"]...))
				w.Header().Set("Content-Type", "text/html")
				if m.failMassDelete {
					fmt.Fprint(w, syncMyTagsFixture)
					return
				}
				// Drop the deleted ids from the response page.
				html := syncMyTagsFixture
				for _, id := range r.PostForm["modify_usertags[]"] {
					html = strings.Replace(html, `id="usertag_`+id+`"`, `id="usertag_gone_`+id+`"`, 1)
					html = strings.Replace(html, `id="tagpreview_`+id+`"`, `id="tagpreview_gone_`+id+`"`, 1)
				}
				fmt.Fprint(w, html)
			default:
				t.Errorf("unexpected form action %q", action)
				w.WriteHeader(http.StatusBadRequest)
			}
		case r.URL.Path == "/mytags":
			w.Header().Set("Content-Type", "text/html")
			fmt.Fprint(w, syncMyTagsFixture)
		default:
			t.Errorf("unexpected upstream request %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	return m
}

// mutations records how many source mutations reached the mock upstream.
func (m *syncMockServer) mutations() int {
	return len(m.setUserTagCalls) + len(m.addForms) + len(m.massDeleteForms)
}

func seedTagRule(t *testing.T, client *ent.Client, tag, action string, weight int) {
	t.Helper()
	if _, err := client.TagPreference.Create().SetTag(tag).SetAction(action).SetWeight(weight).Save(t.Context()); err != nil {
		t.Fatalf("seed rule %s: %v", tag, err)
	}
}

func seedBaseline(t *testing.T, client *ent.Client, tag, action string, weight int) {
	t.Helper()
	if _, err := client.TagSyncBaseline.Create().SetTag(tag).SetAction(action).SetWeight(weight).Save(t.Context()); err != nil {
		t.Fatalf("seed baseline %s: %v", tag, err)
	}
}

// readRules loads the stored tag preference rows keyed by tag.
func readRules(t *testing.T, client *ent.Client) map[string]model.TagPreference {
	t.Helper()
	rows, err := client.TagPreference.Query().All(t.Context())
	if err != nil {
		t.Fatalf("query tag preferences: %v", err)
	}
	out := make(map[string]model.TagPreference, len(rows))
	for _, row := range rows {
		out[row.Tag] = model.TagPreference{Tag: row.Tag, Action: row.Action, Weight: row.Weight}
	}
	return out
}

// readBaseline loads the sync baseline rows keyed by tag.
func readBaseline(t *testing.T, client *ent.Client) map[string]model.TagPreference {
	t.Helper()
	rows, err := client.TagSyncBaseline.Query().All(t.Context())
	if err != nil {
		t.Fatalf("query tag sync baseline: %v", err)
	}
	out := make(map[string]model.TagPreference, len(rows))
	for _, row := range rows {
		out[row.Tag] = model.TagPreference{Tag: row.Tag, Action: row.Action, Weight: row.Weight}
	}
	return out
}

// setupSyncServer wires the sync handler against the mock upstream.
func setupSyncServer(t *testing.T, upstream *syncMockServer, db *ent.Client) *gin.Engine {
	t.Helper()
	r := setupTestRouter()
	server := &Server{Client: newMockClient(upstream.URL), DB: &database.DB{Client: db}}
	r.POST("/api/tag-settings/sync", server.handleTagSettingsSync)
	return r
}

// doSync POSTs a sync body and decodes a 200 response.
func doSync(t *testing.T, r *gin.Engine, body string) (int, model.TagSyncResult) {
	t.Helper()
	req := httptest.NewRequest("POST", "/api/tag-settings/sync", strings.NewReader(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	var result model.TagSyncResult
	if w.Code == http.StatusOK {
		if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
			t.Fatalf("decode sync result: %v (body %s)", err, w.Body.String())
		}
	}
	return w.Code, result
}

// ==================== mergeTagState (pure three-way merge) ====================

func TestMergeTagState_FirstRunImportsSourceAndPushesLocalOnly(t *testing.T) {
	local := []model.TagPreference{
		{Tag: "language:chinese", Action: model.TagActionLike, Weight: 90},     // matches remote
		{Tag: "artist:new person", Action: model.TagActionDislike, Weight: -5}, // local-only → push
	}
	remote := []exhentai.UserTag{
		{TagID: 3437, Tag: "language:chinese", Watch: true, Weight: 90},
		{TagID: 5354, Tag: "male:gender change", Watch: true, Weight: 6, Color: "#AABBCC"}, // remote-only → pull
		{TagID: 304301, Tag: "male:males only", Hide: true, Weight: -10},                   // remote-only → pull
	}

	plan := mergeTagState(local, nil, remote, "", nil)

	if len(plan.Adds) != 1 || plan.Adds[0].Tag != "artist:new person" {
		t.Errorf("Adds = %+v, want artist:new person", plan.Adds)
	}
	if len(plan.Pulls) != 2 {
		t.Fatalf("Pulls = %+v, want 2 imports", plan.Pulls)
	}
	if plan.Pulls[0].Key != "male:gender change" || plan.Pulls[0].Remote == nil ||
		plan.Pulls[0].Remote.Action != model.TagActionLike || plan.Pulls[0].Remote.Weight != 6 {
		t.Errorf("pull[0] = %+v, want import male:gender change like 6", plan.Pulls[0])
	}
	if plan.Pulls[1].Key != "male:males only" || plan.Pulls[1].Remote == nil ||
		plan.Pulls[1].Remote.Action != model.TagActionDislike || plan.Pulls[1].Remote.Weight != -10 {
		t.Errorf("pull[1] = %+v, want import male:males only dislike -10", plan.Pulls[1])
	}
	if plan.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", plan.Unchanged)
	}
	if len(plan.Conflicts) != 0 || len(plan.Updates) != 0 || len(plan.Deletes) != 0 {
		t.Errorf("conflicts/updates/deletes = %d/%d/%d, want 0", len(plan.Conflicts), len(plan.Updates), len(plan.Deletes))
	}
}

func TestMergeTagState_OnlyLocalChangedPushesUpdate(t *testing.T) {
	local := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionDislike, Weight: -25}}
	baseline := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionLike, Weight: 6}}
	remote := []exhentai.UserTag{{TagID: 5354, Tag: "male:gender change", Watch: true, Weight: 6, Color: "#AABBCC"}}

	plan := mergeTagState(local, baseline, remote, "", nil)

	if len(plan.Updates) != 1 || len(plan.Adds) != 0 || len(plan.Deletes) != 0 ||
		len(plan.Pulls) != 0 || len(plan.Conflicts) != 0 || plan.Unchanged != 0 {
		t.Fatalf("plan = %+v, want exactly one push update", plan)
	}
	if plan.Updates[0].Remote.TagID != 5354 {
		t.Errorf("update tagid = %d, want 5354", plan.Updates[0].Remote.TagID)
	}
	// Failed-push default: baseline stays at the pre-write source state.
	if got := plan.BaselineAfter["male:gender change"]; got == nil || got.Weight != 6 {
		t.Errorf("baseline default = %+v, want source weight 6", got)
	}
}

func TestMergeTagState_OnlyRemoteChangedPullsUpdate(t *testing.T) {
	local := []model.TagPreference{{Tag: "language:chinese", Action: model.TagActionLike, Weight: 10}}
	baseline := []model.TagPreference{{Tag: "language:chinese", Action: model.TagActionLike, Weight: 10}}
	remote := []exhentai.UserTag{{TagID: 3437, Tag: "language:chinese", Watch: true, Weight: 90}}

	plan := mergeTagState(local, baseline, remote, "", nil)

	if len(plan.Pulls) != 1 || plan.Pulls[0].Remote == nil ||
		plan.Pulls[0].Remote.Weight != 90 || plan.Pulls[0].Existing == nil {
		t.Fatalf("Pulls = %+v, want one update pull to weight 90", plan.Pulls)
	}
	if len(plan.Updates) != 0 || len(plan.Conflicts) != 0 {
		t.Errorf("updates/conflicts = %d/%d, want 0", len(plan.Updates), len(plan.Conflicts))
	}
}

func TestMergeTagState_BothChangedIsConflict(t *testing.T) {
	local := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionDislike, Weight: -25}}
	baseline := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionLike, Weight: 90}}
	remote := []exhentai.UserTag{{TagID: 5354, Tag: "male:gender change", Watch: true, Weight: 6}}

	plan := mergeTagState(local, baseline, remote, "", nil)

	if len(plan.Conflicts) != 1 {
		t.Fatalf("Conflicts = %+v, want 1", plan.Conflicts)
	}
	c := plan.Conflicts[0]
	if c.Tag != "male:gender change" {
		t.Errorf("conflict tag = %q", c.Tag)
	}
	if c.Local == nil || c.Local.Action != model.TagActionDislike || c.Local.Weight != -25 {
		t.Errorf("conflict local = %+v, want dislike -25", c.Local)
	}
	if c.Remote == nil || c.Remote.Action != model.TagActionLike || c.Remote.Weight != 6 {
		t.Errorf("conflict remote = %+v, want like 6", c.Remote)
	}
	if len(plan.Updates) != 0 || len(plan.Pulls) != 0 || len(plan.Adds) != 0 || len(plan.Deletes) != 0 {
		t.Errorf("unresolved conflict must not schedule ops: %+v", plan)
	}
	if _, touched := plan.BaselineAfter["male:gender change"]; touched {
		t.Error("unresolved conflict must leave the baseline untouched")
	}
}

func TestMergeTagState_ResolutionRemotePulls(t *testing.T) {
	local := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionDislike, Weight: -25}}
	baseline := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionLike, Weight: 90}}
	remote := []exhentai.UserTag{{TagID: 5354, Tag: "male:gender change", Watch: true, Weight: 6}}

	plan := mergeTagState(local, baseline, remote, "remote", nil)

	if len(plan.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none after resolution", plan.Conflicts)
	}
	if len(plan.Pulls) != 1 || plan.Pulls[0].Remote == nil || plan.Pulls[0].Remote.Weight != 6 {
		t.Fatalf("Pulls = %+v, want the source value", plan.Pulls)
	}
	if len(plan.Updates) != 0 {
		t.Errorf("Updates = %+v, want none (source wins)", plan.Updates)
	}
}

func TestMergeTagState_ResolutionLocalPushes(t *testing.T) {
	local := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionDislike, Weight: -25}}
	baseline := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionLike, Weight: 90}}
	remote := []exhentai.UserTag{{TagID: 5354, Tag: "male:gender change", Watch: true, Weight: 6}}

	plan := mergeTagState(local, baseline, remote, "local", nil)

	if len(plan.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none after resolution", plan.Conflicts)
	}
	if len(plan.Updates) != 1 || plan.Updates[0].Rule.Weight != -25 {
		t.Fatalf("Updates = %+v, want the local value", plan.Updates)
	}
	if len(plan.Pulls) != 0 {
		t.Errorf("Pulls = %+v, want none (local wins)", plan.Pulls)
	}
}

// TestMergeTagState_PerTagChoices checks the dialog's per-tag answers: two
// conflicts get opposite resolutions in one merge, each applied on its own
// side, and a tag with no answer stays a conflict.
func TestMergeTagState_PerTagChoices(t *testing.T) {
	local := []model.TagPreference{
		{Tag: "male:gender change", Action: model.TagActionDislike, Weight: -25},
		{Tag: "language:chinese", Action: model.TagActionLike, Weight: 10},
		{Tag: "female:yuri", Action: model.TagActionDislike, Weight: -5},
	}
	baseline := []model.TagPreference{
		{Tag: "male:gender change", Action: model.TagActionLike, Weight: 90},
		{Tag: "language:chinese", Action: model.TagActionLike, Weight: 90},
		{Tag: "female:yuri", Action: model.TagActionLike, Weight: 50},
	}
	remote := []exhentai.UserTag{
		{TagID: 5354, Tag: "male:gender change", Watch: true, Weight: 6},
		{TagID: 3437, Tag: "language:chinese", Watch: true, Weight: 20},
		{TagID: 304301, Tag: "female:yuri", Hide: true, Weight: -7},
	}

	plan := mergeTagState(local, baseline, remote, "", map[string]string{
		"male:gender change": "remote",
		"language:chinese":   "local",
	})

	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Tag != "female:yuri" {
		t.Fatalf("Conflicts = %+v, want only female:yuri unresolved", plan.Conflicts)
	}
	if len(plan.Pulls) != 1 || plan.Pulls[0].Key != "male:gender change" ||
		plan.Pulls[0].Remote == nil || plan.Pulls[0].Remote.Weight != 6 {
		t.Errorf("Pulls = %+v, want gender change pulled to 6", plan.Pulls)
	}
	if len(plan.Updates) != 1 || plan.Updates[0].Rule.Tag != "language:chinese" ||
		plan.Updates[0].Rule.Weight != 10 {
		t.Errorf("Updates = %+v, want chinese pushed at 10", plan.Updates)
	}
	if got := plan.BaselineAfter["language:chinese"]; got == nil || got.Weight != 20 {
		t.Errorf("baseline for chinese = %+v, want the pre-write source value 20", got)
	}
	if _, touched := plan.BaselineAfter["female:yuri"]; touched {
		t.Error("unresolved conflict must leave its baseline row alone")
	}
}

func TestMergeTagState_PerTagChoiceBeatsGlobalResolution(t *testing.T) {
	local := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionDislike, Weight: -25}}
	baseline := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionLike, Weight: 90}}
	remote := []exhentai.UserTag{{TagID: 5354, Tag: "male:gender change", Watch: true, Weight: 6}}

	plan := mergeTagState(local, baseline, remote, "remote", map[string]string{
		"male:gender change": "local",
	})

	if len(plan.Updates) != 1 || len(plan.Pulls) != 0 {
		t.Fatalf("plan = %+v, want the tag's own local choice to win over the global remote", plan)
	}
}

func TestMergeTagState_PerTagChoiceIgnoresOtherTags(t *testing.T) {
	local := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionDislike, Weight: -25}}
	baseline := []model.TagPreference{{Tag: "male:gender change", Action: model.TagActionLike, Weight: 90}}
	remote := []exhentai.UserTag{{TagID: 5354, Tag: "male:gender change", Watch: true, Weight: 6}}

	// A choice for a tag that is not in conflict does nothing to it.
	plan := mergeTagState(local, baseline, remote, "", map[string]string{
		"language:chinese": "local",
	})

	if len(plan.Conflicts) != 1 || len(plan.Updates) != 0 || len(plan.Pulls) != 0 {
		t.Fatalf("plan = %+v, want the conflict still unresolved", plan)
	}
}

func TestMergeTagState_LocalDeleteStaysPending(t *testing.T) {
	// Local rule deleted, source unchanged → source deletion candidate.
	baseline := []model.TagPreference{{Tag: "male:males only", Action: model.TagActionDislike, Weight: -10}}
	remote := []exhentai.UserTag{{TagID: 304301, Tag: "male:males only", Hide: true, Weight: -10}}

	plan := mergeTagState(nil, baseline, remote, "", nil)

	if len(plan.Deletes) != 1 {
		t.Fatalf("Deletes = %+v, want 1", plan.Deletes)
	}
	if plan.Deletes[0].Consented {
		t.Error("plain local deletion must not be pre-consented")
	}
	if got := plan.BaselineAfter["male:males only"]; got == nil || got.Weight != -10 {
		t.Errorf("baseline default = %+v, want source row retained", got)
	}
}

func TestMergeTagState_SourceDeletePullsLocalRule(t *testing.T) {
	local := []model.TagPreference{{Tag: "female:yuri", Action: model.TagActionLike, Weight: 10}}
	baseline := []model.TagPreference{{Tag: "female:yuri", Action: model.TagActionLike, Weight: 10}}

	plan := mergeTagState(local, baseline, nil, "", nil)

	if len(plan.Pulls) != 1 || plan.Pulls[0].Remote != nil || plan.Pulls[0].Existing == nil {
		t.Fatalf("Pulls = %+v, want one local removal", plan.Pulls)
	}
	if plan.Pulls[0].Existing.Tag != "female:yuri" {
		t.Errorf("removal targets %q", plan.Pulls[0].Existing.Tag)
	}
	if got, ok := plan.BaselineAfter["female:yuri"]; !ok || got != nil {
		t.Errorf("baseline outcome = %v/%v, want key present with nil (row dropped)", got, ok)
	}
	if len(plan.Deletes) != 0 {
		t.Errorf("Deletes = %+v, want none (source-side deletion is a pull)", plan.Deletes)
	}
}

func TestMergeTagState_BothDeletedRetiresBaseline(t *testing.T) {
	baseline := []model.TagPreference{{Tag: "female:yuri", Action: model.TagActionLike, Weight: 10}}

	plan := mergeTagState(nil, baseline, nil, "", nil)

	if len(plan.Pulls) != 0 || len(plan.Deletes) != 0 || len(plan.Conflicts) != 0 || plan.Unchanged != 0 {
		t.Errorf("plan = %+v, want a pure baseline cleanup", plan)
	}
	if got, ok := plan.BaselineAfter["female:yuri"]; !ok || got != nil {
		t.Errorf("baseline outcome = %v/%v, want key present with nil", got, ok)
	}
}

func TestMergeTagState_ConflictResolutionLocalConsentsDelete(t *testing.T) {
	// Local deleted the tag while the source changed it: keeping local means
	// deleting the changed source row, which the dialog already consented to.
	baseline := []model.TagPreference{{Tag: "male:males only", Action: model.TagActionLike, Weight: 5}}
	remote := []exhentai.UserTag{{TagID: 304301, Tag: "male:males only", Hide: true, Weight: -10}}

	plan := mergeTagState(nil, baseline, remote, "", nil)
	if len(plan.Conflicts) != 1 || plan.Conflicts[0].Local != nil {
		t.Fatalf("Conflicts = %+v, want one with nil local side", plan.Conflicts)
	}

	plan = mergeTagState(nil, baseline, remote, "local", nil)
	if len(plan.Deletes) != 1 || !plan.Deletes[0].Consented {
		t.Fatalf("Deletes = %+v, want one consented delete", plan.Deletes)
	}
	if len(plan.Conflicts) != 0 {
		t.Errorf("Conflicts = %+v, want none", plan.Conflicts)
	}
}

func TestMergeTagState_CaseInsensitiveMatch(t *testing.T) {
	local := []model.TagPreference{{Tag: "Language:Chinese", Action: model.TagActionLike, Weight: 90}}
	remote := []exhentai.UserTag{{TagID: 1, Tag: "language:chinese", Watch: true, Weight: 90}}

	plan := mergeTagState(local, nil, remote, "", nil)
	if plan.Unchanged != 1 || len(plan.Adds) != 0 || len(plan.Updates) != 0 || len(plan.Pulls) != 0 {
		t.Errorf("plan = %+v, want unchanged (case-insensitive)", plan)
	}
}

// ==================== normalizeRemoteTag ====================

func TestNormalizeRemoteTag(t *testing.T) {
	cases := []struct {
		name   string
		row    exhentai.UserTag
		action string
		weight int
	}{
		{"watched", exhentai.UserTag{Tag: "a:b", Watch: true, Weight: 90}, model.TagActionLike, 90},
		{"hidden", exhentai.UserTag{Tag: "a:b", Hide: true, Weight: -10}, model.TagActionDislike, -10},
		{"hidden positive weight keeps sign", exhentai.UserTag{Tag: "a:b", Hide: true, Weight: 500}, model.TagActionDislike, 500},
		{"watched negative weight keeps sign", exhentai.UserTag{Tag: "a:b", Watch: true, Weight: -3}, model.TagActionLike, -3},
		{"neutral negative weight is dislike", exhentai.UserTag{Tag: "a:b", Weight: -10}, model.TagActionDislike, -10},
		{"neutral non-negative weight is like", exhentai.UserTag{Tag: "a:b", Weight: 0}, model.TagActionLike, 0},
		{"weight clamped to bounds", exhentai.UserTag{Tag: "a:b", Watch: true, Weight: 1500}, model.TagActionLike, 999},
		{"weight clamped below lower bound", exhentai.UserTag{Tag: "a:b", Watch: true, Weight: -1500}, model.TagActionLike, -999},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalizeRemoteTag(tc.row)
			if got.Action != tc.action || got.Weight != tc.weight {
				t.Errorf("normalize = %s %+d, want %s %+d", got.Action, got.Weight, tc.action, tc.weight)
			}
			if got.Tag != syncKey(tc.row.Tag) {
				t.Errorf("tag = %q, want normalized key", got.Tag)
			}
		})
	}
}

// ==================== weight validation ====================

func TestHandleTagSettingsPut_WeightDefaultsAndValidation(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	r.PUT("/api/tag-settings", server.handleTagSettingsPut)

	// Omitted weight defaults from the action: like +10, dislike -10.
	req := httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(`{"tags":[`+
		`{"tag":"female:yuri","action":"like"},`+
		`{"tag":"tag:netorare","action":"dislike"}`+
		`]}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	var resp model.TagSettingsResponse
	json.Unmarshal(w.Body.Bytes(), &resp)
	byTag := map[string]model.TagPreference{}
	for _, tag := range resp.Tags {
		byTag[tag.Tag] = tag
	}
	if byTag["female:yuri"].Weight != 10 {
		t.Errorf("like default weight = %d, want 10", byTag["female:yuri"].Weight)
	}
	if byTag["tag:netorare"].Weight != -10 {
		t.Errorf("dislike default weight = %d, want -10", byTag["tag:netorare"].Weight)
	}

	// Explicit weights are kept, including 0.
	req = httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(`{"tags":[`+
		`{"tag":"female:yuri","action":"like","weight":0},`+
		`{"tag":"tag:netorare","action":"dislike","weight":-999}`+
		`]}`))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("explicit weights status = %d, body = %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	byTag = map[string]model.TagPreference{}
	for _, tag := range resp.Tags {
		byTag[tag.Tag] = tag
	}
	if byTag["female:yuri"].Weight != 0 {
		t.Errorf("explicit 0 weight = %d, want 0", byTag["female:yuri"].Weight)
	}
	if byTag["tag:netorare"].Weight != -999 {
		t.Errorf("explicit -999 weight = %d, want -999", byTag["tag:netorare"].Weight)
	}

	// Mixed signs are accepted: the weight sign is independent of the action.
	req = httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(`{"tags":[`+
		`{"tag":"female:yuri","action":"like","weight":-50},`+
		`{"tag":"tag:netorare","action":"dislike","weight":20}`+
		`]}`))
	w = httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("mixed signs status = %d, body = %s", w.Code, w.Body.String())
	}
	json.Unmarshal(w.Body.Bytes(), &resp)
	byTag = map[string]model.TagPreference{}
	for _, tag := range resp.Tags {
		byTag[tag.Tag] = tag
	}
	if byTag["female:yuri"].Weight != -50 {
		t.Errorf("like weight = %d, want -50", byTag["female:yuri"].Weight)
	}
	if byTag["tag:netorare"].Weight != 20 {
		t.Errorf("dislike weight = %d, want 20", byTag["tag:netorare"].Weight)
	}

	// Out-of-range weights are rejected regardless of the action.
	bad := []struct{ name, body string }{
		{"like over max", `{"tags":[{"tag":"female:yuri","action":"like","weight":1000}]}`},
		{"like under min", `{"tags":[{"tag":"female:yuri","action":"like","weight":-1000}]}`},
		{"dislike over max", `{"tags":[{"tag":"tag:netorare","action":"dislike","weight":1000}]}`},
		{"dislike under min", `{"tags":[{"tag":"tag:netorare","action":"dislike","weight":-1000}]}`},
	}
	for _, tc := range bad {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(tc.body))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want 400; body = %s", w.Code, w.Body.String())
			}
		})
	}
}

// ==================== sync endpoint ====================

func TestHandleTagSettingsSync_UpstreamDown(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{Client: errorClient(), DB: &database.DB{Client: client}}
	r.POST("/api/tag-settings/sync", server.handleTagSettingsSync)

	req := httptest.NewRequest("POST", "/api/tag-settings/sync", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502; body = %s", w.Code, w.Body.String())
	}
}

func TestHandleTagSettingsSync_NoClient(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	r.POST("/api/tag-settings/sync", server.handleTagSettingsSync)

	req := httptest.NewRequest("POST", "/api/tag-settings/sync", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadGateway {
		t.Errorf("status = %d, want 502 (nil client); body = %s", w.Code, w.Body.String())
	}
}

func TestHandleTagSettingsSync_InvalidConflictResolution(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	r := setupSyncServer(t, upstream, client)

	code, _ := doSync(t, r, `{"conflict_resolution":"sideways"}`)
	if code != http.StatusBadRequest {
		t.Errorf("status = %d, want 400", code)
	}
	if upstream.mutations() != 0 {
		t.Errorf("upstream mutations = %d, want 0", upstream.mutations())
	}
}

func TestHandleTagSettingsSync_FirstRunImportsSource(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	r := setupSyncServer(t, upstream, client)

	code, result := doSync(t, r, `{}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if result.Pulled != 3 || result.Added != 0 || result.Updated != 0 || result.Deleted != 0 {
		t.Errorf("pulled/added/updated/deleted = %d/%d/%d/%d, want 3/0/0/0",
			result.Pulled, result.Added, result.Updated, result.Deleted)
	}
	if len(result.PendingDeletes) != 0 {
		t.Errorf("pending_deletes = %v, want none (first run imports instead)", result.PendingDeletes)
	}
	if len(result.Conflicts) != 0 || len(result.Failed) != 0 {
		t.Errorf("conflicts/failed = %d/%d, want 0/0", len(result.Conflicts), len(result.Failed))
	}
	if upstream.mutations() != 0 {
		t.Errorf("upstream mutations = %d, want 0 (pull-only)", upstream.mutations())
	}

	rules := readRules(t, client)
	if len(rules) != 3 {
		t.Fatalf("local rules = %d, want 3", len(rules))
	}
	if r := rules["language:chinese"]; r.Action != model.TagActionLike || r.Weight != 90 {
		t.Errorf("chinese = %+v, want like 90", r)
	}
	if r := rules["male:gender change"]; r.Action != model.TagActionLike || r.Weight != 6 {
		t.Errorf("gender change = %+v, want like 6 (colour ignored)", r)
	}
	if r := rules["male:males only"]; r.Action != model.TagActionDislike || r.Weight != -10 {
		t.Errorf("males only = %+v, want dislike -10", r)
	}
	if len(result.Tags) != 3 {
		t.Errorf("response tags = %d, want 3", len(result.Tags))
	}

	baseline := readBaseline(t, client)
	if len(baseline) != 3 {
		t.Errorf("baseline rows = %d, want 3", len(baseline))
	}
}

func TestHandleTagSettingsSync_LocalChangePushesUpdate(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// Baseline mirrors the source; local flipped gender change to dislike.
	seedBaseline(t, client, "language:chinese", model.TagActionLike, 90)
	seedBaseline(t, client, "male:males only", model.TagActionDislike, -10)
	seedBaseline(t, client, "male:gender change", model.TagActionLike, 6)
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 90)
	seedTagRule(t, client, "male:males only", model.TagActionDislike, -10)
	seedTagRule(t, client, "male:gender change", model.TagActionDislike, -25)

	r := setupSyncServer(t, upstream, client)
	code, result := doSync(t, r, `{}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if result.Updated != 1 || result.Unchanged != 2 || result.Pulled != 0 {
		t.Errorf("updated/unchanged/pulled = %d/%d/%d, want 1/2/0",
			result.Updated, result.Unchanged, result.Pulled)
	}
	if len(result.Conflicts) != 0 || len(result.Failed) != 0 {
		t.Errorf("conflicts/failed = %v/%v, want empty", result.Conflicts, result.Failed)
	}
	if len(upstream.setUserTagCalls) != 1 {
		t.Fatalf("setusertag calls = %d, want 1", len(upstream.setUserTagCalls))
	}
	call := upstream.setUserTagCalls[0]
	if call["tagid"] != float64(5354) || call["tagwatch"] != float64(0) || call["taghide"] != float64(1) {
		t.Errorf("call = %+v, want tagid 5354 watch 0 hide 1", call)
	}
	if call["tagweight"] != "-25" || call["tagcolor"] != "" {
		t.Errorf("weight/color = %v/%v, want -25/empty", call["tagweight"], call["tagcolor"])
	}

	// Successful push converges the baseline to the pushed value.
	baseline := readBaseline(t, client)
	if b := baseline["male:gender change"]; b.Action != model.TagActionDislike || b.Weight != -25 {
		t.Errorf("baseline gender change = %+v, want dislike -25", b)
	}
}

func TestHandleTagSettingsSync_ConflictReportedWithoutMutation(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// Baseline (like 90) differs from both local (-25) and source (6).
	seedBaseline(t, client, "language:chinese", model.TagActionLike, 90)
	seedBaseline(t, client, "male:males only", model.TagActionDislike, -10)
	seedBaseline(t, client, "male:gender change", model.TagActionLike, 90)
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 90)
	seedTagRule(t, client, "male:males only", model.TagActionDislike, -10)
	seedTagRule(t, client, "male:gender change", model.TagActionDislike, -25)

	r := setupSyncServer(t, upstream, client)
	code, result := doSync(t, r, `{}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("conflicts = %+v, want 1", result.Conflicts)
	}
	c := result.Conflicts[0]
	if c.Tag != "male:gender change" {
		t.Errorf("conflict tag = %q", c.Tag)
	}
	if c.Local == nil || c.Local.Weight != -25 {
		t.Errorf("conflict local = %+v, want dislike -25", c.Local)
	}
	if c.Remote == nil || c.Remote.Action != model.TagActionLike || c.Remote.Weight != 6 {
		t.Errorf("conflict remote = %+v, want like 6", c.Remote)
	}
	// Non-conflicting tags still sync: the two matching rows stay unchanged.
	if result.Updated != 0 || result.Pulled != 0 || result.Unchanged != 2 {
		t.Errorf("updated/pulled/unchanged = %d/%d/%d, want 0/0/2",
			result.Updated, result.Pulled, result.Unchanged)
	}
	if upstream.mutations() != 0 {
		t.Errorf("upstream mutations = %d, want 0 while unresolved", upstream.mutations())
	}
	// Neither side moved: local rule and baseline stay put.
	rules := readRules(t, client)
	if rules["male:gender change"].Weight != -25 {
		t.Errorf("local weight = %d, want -25 (untouched)", rules["male:gender change"].Weight)
	}
	if b := readBaseline(t, client)["male:gender change"]; b.Weight != 90 {
		t.Errorf("baseline weight = %d, want 90 (untouched)", b.Weight)
	}
}

func TestHandleTagSettingsSync_ResolutionRemotePullsSource(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// chinese/males only are fully synced; only gender change is in conflict.
	seedBaseline(t, client, "language:chinese", model.TagActionLike, 90)
	seedBaseline(t, client, "male:males only", model.TagActionDislike, -10)
	seedBaseline(t, client, "male:gender change", model.TagActionLike, 90)
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 90)
	seedTagRule(t, client, "male:males only", model.TagActionDislike, -10)
	seedTagRule(t, client, "male:gender change", model.TagActionDislike, -25)

	r := setupSyncServer(t, upstream, client)
	code, result := doSync(t, r, `{"conflict_resolution":"remote"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("conflicts = %+v, want none", result.Conflicts)
	}
	if result.Pulled != 1 {
		t.Errorf("pulled = %d, want 1", result.Pulled)
	}
	if upstream.mutations() != 0 {
		t.Errorf("upstream mutations = %d, want 0 (source wins, nothing to push)", upstream.mutations())
	}

	rules := readRules(t, client)
	if rules["male:gender change"].Action != model.TagActionLike || rules["male:gender change"].Weight != 6 {
		t.Errorf("local rule = %+v, want like 6 (pulled)", rules["male:gender change"])
	}
	if b := readBaseline(t, client)["male:gender change"]; b.Weight != 6 {
		t.Errorf("baseline = %+v, want converged to 6", b)
	}
}

func TestHandleTagSettingsSync_ResolutionLocalPushesLocal(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	seedBaseline(t, client, "male:gender change", model.TagActionLike, 90)
	seedTagRule(t, client, "male:gender change", model.TagActionDislike, -25)

	r := setupSyncServer(t, upstream, client)
	code, result := doSync(t, r, `{"conflict_resolution":"local"}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(result.Conflicts) != 0 || result.Updated != 1 {
		t.Errorf("conflicts/updated = %v/%d, want none/1", result.Conflicts, result.Updated)
	}
	if len(upstream.setUserTagCalls) != 1 || upstream.setUserTagCalls[0]["tagweight"] != "-25" {
		t.Fatalf("setusertag calls = %+v, want one push of -25", upstream.setUserTagCalls)
	}

	rules := readRules(t, client)
	if rules["male:gender change"].Weight != -25 {
		t.Errorf("local rule = %+v, want unchanged -25", rules["male:gender change"])
	}
	if b := readBaseline(t, client)["male:gender change"]; b.Weight != -25 {
		t.Errorf("baseline = %+v, want converged to -25", b)
	}
}

// TestHandleTagSettingsSync_PerTagConflicts covers the dialog's per-tag
// answers over HTTP: one conflict resolved toward the source, one toward
// the local rules, in a single request.
func TestHandleTagSettingsSync_PerTagConflicts(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// Both are conflicts: baseline differs from local and source on each.
	seedBaseline(t, client, "male:gender change", model.TagActionLike, 90)
	seedBaseline(t, client, "language:chinese", model.TagActionLike, 90)
	seedTagRule(t, client, "male:gender change", model.TagActionDislike, -25)
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 10)
	// The fixture's third row stays equal on both sides.
	seedBaseline(t, client, "male:males only", model.TagActionDislike, -10)
	seedTagRule(t, client, "male:males only", model.TagActionDislike, -10)

	r := setupSyncServer(t, upstream, client)
	code, result := doSync(t, r, `{"conflicts":[
		{"tag":"male:gender change","resolution":"remote"},
		{"tag":"language:chinese","resolution":"local"}
	]}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(result.Conflicts) != 0 {
		t.Errorf("conflicts = %+v, want none resolved", result.Conflicts)
	}
	if result.Pulled != 1 || result.Updated != 1 {
		t.Errorf("pulled/updated = %d/%d, want 1/1", result.Pulled, result.Updated)
	}
	rules := readRules(t, client)
	if rules["male:gender change"].Weight != 6 {
		t.Errorf("gender change = %+v, want pulled to the source's 6", rules["male:gender change"])
	}
	if rules["language:chinese"].Weight != 10 {
		t.Errorf("chinese = %+v, want the local 10 kept", rules["language:chinese"])
	}
	if len(upstream.setUserTagCalls) != 1 || upstream.setUserTagCalls[0]["tagweight"] != "10" {
		t.Fatalf("setusertag calls = %+v, want only chinese pushed at 10", upstream.setUserTagCalls)
	}
}

func TestHandleTagSettingsSync_InvalidPerTagChoice(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()
	r := setupSyncServer(t, upstream, newTestDB(t))

	for _, body := range []string{
		`{"conflicts":[{"tag":"male:gender change","resolution":"sideways"}]}`,
		`{"conflicts":[{"tag":"","resolution":"local"}]}`,
	} {
		if code, _ := doSync(t, r, body); code != http.StatusBadRequest {
			t.Errorf("body %s: status = %d, want 400", body, code)
		}
	}
	if upstream.mutations() != 0 {
		t.Errorf("upstream mutations = %d, want 0 for rejected bodies", upstream.mutations())
	}
}

func TestHandleTagSettingsSync_PendingDeletesWithoutConfirm(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// Baseline knows both male:* rows (previously synced); local deleted them.
	seedBaseline(t, client, "language:chinese", model.TagActionLike, 90)
	seedBaseline(t, client, "male:males only", model.TagActionDislike, -10)
	seedBaseline(t, client, "male:gender change", model.TagActionLike, 6)
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 90)

	r := setupSyncServer(t, upstream, client)
	code, result := doSync(t, r, `{}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if len(result.PendingDeletes) != 2 {
		t.Fatalf("pending_deletes = %v, want the 2 locally-deleted tags", result.PendingDeletes)
	}
	if result.Deleted != 0 || len(upstream.massDeleteForms) != 0 {
		t.Errorf("deleted = %d / mass forms = %d, want 0 without confirm", result.Deleted, len(upstream.massDeleteForms))
	}
	if result.Unchanged != 1 || result.Pulled != 0 || result.Added != 0 {
		t.Errorf("unchanged/pulled/added = %d/%d/%d, want 1/0/0", result.Unchanged, result.Pulled, result.Added)
	}
	// Pending deletions keep their baseline row so they stay pending.
	if len(readBaseline(t, client)) != 3 {
		t.Errorf("baseline rows = %d, want 3 (pending rows retained)", len(readBaseline(t, client)))
	}
}

func TestHandleTagSettingsSync_ConfirmDeletesRemovesSourceAndBaseline(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	seedBaseline(t, client, "language:chinese", model.TagActionLike, 90)
	seedBaseline(t, client, "male:males only", model.TagActionDislike, -10)
	seedBaseline(t, client, "male:gender change", model.TagActionLike, 6)
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 90)

	r := setupSyncServer(t, upstream, client)
	code, result := doSync(t, r, `{"confirm_deletes":true}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if result.Deleted != 2 {
		t.Errorf("deleted = %d, want 2", result.Deleted)
	}
	if len(result.PendingDeletes) != 0 {
		t.Errorf("pending_deletes = %v, want empty after confirm", result.PendingDeletes)
	}
	if len(upstream.massDeleteForms) != 1 || len(upstream.massDeleteForms[0]) != 2 {
		t.Fatalf("mass delete forms = %v, want one form with 2 ids", upstream.massDeleteForms)
	}
	baseline := readBaseline(t, client)
	if len(baseline) != 1 {
		t.Errorf("baseline rows = %d, want 1 (deleted rows retired)", len(baseline))
	}
	if _, ok := baseline["language:chinese"]; !ok {
		t.Errorf("baseline = %v, want language:chinese retained", baseline)
	}
}

func TestHandleTagSettingsSync_ResolutionLocalDeleteNeedsNoConfirm(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// Local deleted males only; the source changed it (baseline like 5).
	seedBaseline(t, client, "male:males only", model.TagActionLike, 5)
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 90)

	r := setupSyncServer(t, upstream, client)

	// Without a resolution it is a conflict, nothing is deleted.
	code, result := doSync(t, r, `{}`)
	if code != http.StatusOK || len(result.Conflicts) != 1 {
		t.Fatalf("status/conflicts = %d/%+v, want 200 with one conflict", code, result.Conflicts)
	}
	if len(upstream.massDeleteForms) != 0 {
		t.Fatalf("mass delete forms = %d, want 0 while unresolved", len(upstream.massDeleteForms))
	}

	// Choosing local consents to the source deletion.
	code, result = doSync(t, r, `{"conflict_resolution":"local"}`)
	if code != http.StatusOK || len(result.Conflicts) != 0 {
		t.Fatalf("status/conflicts = %d/%+v, want 200 resolved", code, result.Conflicts)
	}
	if result.Deleted != 1 {
		t.Errorf("deleted = %d, want 1 (consented by the dialog)", result.Deleted)
	}
	if len(upstream.massDeleteForms) != 1 {
		t.Errorf("mass delete forms = %d, want 1", len(upstream.massDeleteForms))
	}
	if _, ok := readBaseline(t, client)["male:males only"]; ok {
		t.Errorf("baseline = %v, want male:males only retired", readBaseline(t, client))
	}
}

func TestHandleTagSettingsSync_SourceDeletePullsLocalRule(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// female:yuri exists locally and in the baseline but no longer on the source.
	seedBaseline(t, client, "language:chinese", model.TagActionLike, 90)
	seedBaseline(t, client, "female:yuri", model.TagActionLike, 10)
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 90)
	seedTagRule(t, client, "female:yuri", model.TagActionLike, 10)

	r := setupSyncServer(t, upstream, client)
	code, result := doSync(t, r, `{}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	// yuri removed locally + the two source-only rows imported.
	if result.Pulled != 3 {
		t.Errorf("pulled = %d, want 3 (1 removal + 2 imports)", result.Pulled)
	}
	if upstream.mutations() != 0 {
		t.Errorf("upstream mutations = %d, want 0 (pull never writes the source)", upstream.mutations())
	}
	rules := readRules(t, client)
	if _, ok := rules["female:yuri"]; ok {
		t.Errorf("local rules = %v, want female:yuri removed", rules)
	}
	if len(rules) != 3 {
		t.Errorf("local rules = %d, want 3", len(rules))
	}
	if _, ok := readBaseline(t, client)["female:yuri"]; ok {
		t.Error("baseline still has female:yuri, want retired")
	}
}

func TestHandleTagSettingsSync_SecondSyncIsNoOp(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// Local and baseline already mirror the source snapshot.
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 90)
	seedTagRule(t, client, "male:males only", model.TagActionDislike, -10)
	seedTagRule(t, client, "male:gender change", model.TagActionLike, 6)
	seedBaseline(t, client, "language:chinese", model.TagActionLike, 90)
	seedBaseline(t, client, "male:males only", model.TagActionDislike, -10)
	seedBaseline(t, client, "male:gender change", model.TagActionLike, 6)

	r := setupSyncServer(t, upstream, client)
	code, result := doSync(t, r, `{}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d", code)
	}
	if result.Added != 0 || result.Updated != 0 || result.Deleted != 0 || result.Pulled != 0 {
		t.Errorf("added/updated/deleted/pulled = %d/%d/%d/%d, want all 0",
			result.Added, result.Updated, result.Deleted, result.Pulled)
	}
	if result.Unchanged != 3 {
		t.Errorf("unchanged = %d, want 3", result.Unchanged)
	}
	if upstream.mutations() != 0 {
		t.Errorf("upstream mutations = %d, want 0", upstream.mutations())
	}
	if len(result.Tags) != 3 {
		t.Errorf("response tags = %d, want 3", len(result.Tags))
	}
}

func TestHandleTagSettingsSync_PartialFailure(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()
	upstream.failSetUserTag = true

	client := newTestDB(t)
	seedBaseline(t, client, "male:gender change", model.TagActionLike, 6)
	seedTagRule(t, client, "male:gender change", model.TagActionDislike, -25)

	r := setupSyncServer(t, upstream, client)
	code, result := doSync(t, r, `{}`)
	if code != http.StatusOK {
		t.Fatalf("status = %d, want 200", code)
	}
	if result.Updated != 0 || len(result.Failed) != 1 {
		t.Errorf("updated/failed = %d/%d, want 0/1", result.Updated, len(result.Failed))
	}
	if len(result.Failed) == 1 && result.Failed[0].Tag != "male:gender change" {
		t.Errorf("failed tag = %q, want male:gender change", result.Failed[0].Tag)
	}
	// Failed push keeps the old baseline so the next sync retries it.
	if b := readBaseline(t, client)["male:gender change"]; b.Weight != 6 {
		t.Errorf("baseline = %+v, want untouched source weight 6", b)
	}
}
