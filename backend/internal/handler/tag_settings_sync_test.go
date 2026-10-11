package handler

import (
	"bytes"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"

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
	setUserTagCalls  []map[string]any
	addForms         []map[string]string
	massDeleteForms  [][]string
	nextAddID        int64
	failSetUserTag   bool
	failAddForm      bool
	failMassDelete   bool
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

func seedTagRule(t *testing.T, client *ent.Client, tag, action string, weight int) {
	t.Helper()
	if _, err := client.TagPreference.Create().SetTag(tag).SetAction(action).SetWeight(weight).Save(t.Context()); err != nil {
		t.Fatalf("seed %s: %v", tag, err)
	}
}

// ==================== planTagSync (pure diff) ====================

func TestPlanTagSync_AddUpdateDeleteUnchanged(t *testing.T) {
	local := []model.TagPreference{
		{Tag: "language:chinese", Action: model.TagActionLike, Weight: 90},      // matches remote
		{Tag: "male:gender change", Action: model.TagActionLike, Weight: 20},    // weight differs (+ colour to clear)
		{Tag: "artist:new person", Action: model.TagActionDislike, Weight: -5},  // local-only add
	}
	remote := []exhentai.UserTag{
		{TagID: 3437, Tag: "language:chinese", Watch: true, Weight: 90},
		{TagID: 5354, Tag: "male:gender change", Watch: true, Weight: 6, Color: "#AABBCC"},
		{TagID: 304301, Tag: "male:males only", Hide: true, Weight: -10}, // remote-only delete candidate
	}

	plan := planTagSync(local, remote)

	if len(plan.Adds) != 1 || plan.Adds[0].Tag != "artist:new person" {
		t.Errorf("Adds = %+v, want artist:new person", plan.Adds)
	}
	if len(plan.Updates) != 1 || plan.Updates[0].Rule.Tag != "male:gender change" {
		t.Errorf("Updates = %+v, want male:gender change", plan.Updates)
	}
	if plan.Updates[0].Remote.TagID != 5354 {
		t.Errorf("update remote tagid = %d, want 5354", plan.Updates[0].Remote.TagID)
	}
	if len(plan.Deletes) != 1 || plan.Deletes[0].Tag != "male:males only" {
		t.Errorf("Deletes = %+v, want male:males only", plan.Deletes)
	}
	if plan.Unchanged != 1 {
		t.Errorf("Unchanged = %d, want 1", plan.Unchanged)
	}
}

func TestPlanTagSync_WatchHideFlipCountsAsUpdate(t *testing.T) {
	local := []model.TagPreference{{Tag: "female:yuri", Action: model.TagActionDislike, Weight: -10}}
	remote := []exhentai.UserTag{{TagID: 7, Tag: "female:yuri", Watch: true, Weight: 10}}

	plan := planTagSync(local, remote)
	if len(plan.Updates) != 1 || len(plan.Adds) != 0 || len(plan.Deletes) != 0 || plan.Unchanged != 0 {
		t.Errorf("plan = %+v, want exactly one update", plan)
	}
}

func TestPlanTagSync_CaseInsensitiveMatch(t *testing.T) {
	local := []model.TagPreference{{Tag: "Language:Chinese", Action: model.TagActionLike, Weight: 90}}
	remote := []exhentai.UserTag{{TagID: 1, Tag: "language:chinese", Watch: true, Weight: 90}}

	plan := planTagSync(local, remote)
	if plan.Unchanged != 1 || len(plan.Adds) != 0 || len(plan.Updates) != 0 || len(plan.Deletes) != 0 {
		t.Errorf("plan = %+v, want unchanged (case-insensitive)", plan)
	}
}

// ==================== weight validation ====================

func TestHandleTagSettingsPut_WeightDefaultsAndValidation(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	r.PUT("/api/tag-settings", server.handleTagSettingsPut)

	// Omitted weight defaults from the action: like +10, dislike -10.
	req := httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(`{"tags":[` +
		`{"tag":"female:yuri","action":"like"},` +
		`{"tag":"tag:netorare","action":"dislike"}` +
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
	req = httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(`{"tags":[` +
		`{"tag":"female:yuri","action":"like","weight":0},` +
		`{"tag":"tag:netorare","action":"dislike","weight":-999}` +
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

	// Mixed signs and out-of-range weights are rejected.
	bad := []struct{ name, body string }{
		{"like negative", `{"tags":[{"tag":"female:yuri","action":"like","weight":-5}]}`},
		{"dislike positive", `{"tags":[{"tag":"tag:netorare","action":"dislike","weight":5}]}`},
		{"like over max", `{"tags":[{"tag":"female:yuri","action":"like","weight":1000}]}`},
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
		t.Errorf("status = %d, want 502 (nil client)", w.Code)
	}
}

func TestHandleTagSettingsSync_PendingDeletesWithoutConfirm(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// Two matching rules + one local-only add; remote also has male:males
	// only which is local-only on the remote side → pending delete.
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 90)  // unchanged
	seedTagRule(t, client, "artist:new person", model.TagActionLike, 10)  // add

	r := setupTestRouter()
	server := &Server{Client: newMockClient(upstream.URL), DB: &database.DB{Client: client}}
	r.POST("/api/tag-settings/sync", server.handleTagSettingsSync)

	req := httptest.NewRequest("POST", "/api/tag-settings/sync", strings.NewReader("{}"))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var result model.TagSyncResult
	if err := json.Unmarshal(w.Body.Bytes(), &result); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if result.Added != 1 {
		t.Errorf("added = %d, want 1", result.Added)
	}
	if result.Unchanged != 1 {
		t.Errorf("unchanged = %d, want 1", result.Unchanged)
	}
	if len(result.PendingDeletes) != 2 {
		// Remote-only: male:males only + male:gender change.
		t.Errorf("pending_deletes = %v, want the 2 remote-only tags", result.PendingDeletes)
	}
	if result.Deleted != 0 || len(upstream.massDeleteForms) != 0 {
		t.Errorf("deleted = %d / mass forms = %d, want 0 without confirm", result.Deleted, len(upstream.massDeleteForms))
	}
	if len(upstream.addForms) != 1 || upstream.addForms[0]["tagname_new"] != "artist:new person" {
		t.Errorf("add forms = %+v, want one add for artist:new person", upstream.addForms)
	}
	if len(upstream.setUserTagCalls) != 0 {
		t.Errorf("setusertag calls = %d, want 0 (nothing to update)", len(upstream.setUserTagCalls))
	}
}

func TestHandleTagSettingsSync_ConfirmDeletes(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// Only language:chinese locally; the two male:* remote rows are remote-only.
	seedTagRule(t, client, "language:chinese", model.TagActionLike, 90)

	r := setupTestRouter()
	server := &Server{Client: newMockClient(upstream.URL), DB: &database.DB{Client: client}}
	r.POST("/api/tag-settings/sync", server.handleTagSettingsSync)

	req := httptest.NewRequest("POST", "/api/tag-settings/sync", strings.NewReader(`{"confirm_deletes":true}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var result model.TagSyncResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Deleted != 2 {
		t.Errorf("deleted = %d, want 2", result.Deleted)
	}
	if len(result.PendingDeletes) != 0 {
		t.Errorf("pending_deletes = %v, want empty after confirm", result.PendingDeletes)
	}
	if len(upstream.massDeleteForms) != 1 {
		t.Fatalf("mass delete forms = %d, want 1", len(upstream.massDeleteForms))
	}
	ids := upstream.massDeleteForms[0]
	if len(ids) != 2 {
		t.Errorf("deleted ids = %v, want 2 ids", ids)
	}
	if result.Unchanged != 1 {
		t.Errorf("unchanged = %d, want 1", result.Unchanged)
	}
}

func TestHandleTagSettingsSync_UpdatesExecuted(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()

	client := newTestDB(t)
	// Dislike male:gender change (remote has it watched +6 + custom colour)
	// → update: hide=1, weight=-25, colour cleared.
	seedTagRule(t, client, "male:gender change", model.TagActionDislike, -25)

	r := setupTestRouter()
	server := &Server{Client: newMockClient(upstream.URL), DB: &database.DB{Client: client}}
	r.POST("/api/tag-settings/sync", server.handleTagSettingsSync)

	req := httptest.NewRequest("POST", "/api/tag-settings/sync", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var result model.TagSyncResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Updated != 1 {
		t.Fatalf("updated = %d, want 1; body = %s", result.Updated, w.Body.String())
	}
	if len(upstream.setUserTagCalls) != 1 {
		t.Fatalf("setusertag calls = %d, want 1", len(upstream.setUserTagCalls))
	}
	call := upstream.setUserTagCalls[0]
	if call["tagid"] != float64(5354) {
		t.Errorf("tagid = %v, want 5354", call["tagid"])
	}
	if call["tagwatch"] != float64(0) || call["taghide"] != float64(1) {
		t.Errorf("tagwatch/taghide = %v/%v, want 0/1", call["tagwatch"], call["taghide"])
	}
	if call["tagweight"] != "-25" {
		t.Errorf("tagweight = %v, want -25", call["tagweight"])
	}
	if call["tagcolor"] != "" {
		t.Errorf("tagcolor = %v, want empty (clear custom colour)", call["tagcolor"])
	}
}

func TestHandleTagSettingsSync_PartialFailure(t *testing.T) {
	upstream := newSyncMockServer(t)
	defer upstream.Close()
	upstream.failSetUserTag = true

	client := newTestDB(t)
	seedTagRule(t, client, "male:gender change", model.TagActionDislike, -25)

	r := setupTestRouter()
	server := &Server{Client: newMockClient(upstream.URL), DB: &database.DB{Client: client}}
	r.POST("/api/tag-settings/sync", server.handleTagSettingsSync)

	req := httptest.NewRequest("POST", "/api/tag-settings/sync", strings.NewReader(`{}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var result model.TagSyncResult
	json.Unmarshal(w.Body.Bytes(), &result)
	if result.Updated != 0 || len(result.Failed) != 1 {
		t.Errorf("updated/failed = %d/%d, want 0/1", result.Updated, len(result.Failed))
	}
	if len(result.Failed) == 1 && result.Failed[0].Tag != "male:gender change" {
		t.Errorf("failed tag = %q, want male:gender change", result.Failed[0].Tag)
	}
}
