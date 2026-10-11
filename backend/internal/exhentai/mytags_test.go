package exhentai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	json "encoding/json/v2"

	"github.com/PuerkitoBio/goquery"
)

// myTagsFixture mirrors the real /mytags markup (ids, classes, attribute
// shapes) with dummy credentials, so parse tests run against the same
// structure the live page has.
const myTagsFixture = `<!DOCTYPE html><html><head></head><body>
<script type="text/javascript">
var apiuid = 12345;
var apikey = "deadbeefcafe12345678";
</script>
<form id="tagset_form" action="" method="post">
	<div id="tagset_outer">
		<div><input type="text" name="tagset_name" id="tagset_name" value="Tagset" /></div>
		<div><select onchange="change_tagset(this.value)"><option value="1" selected="selected">Tagset (3)</option></select></div>
		<div><input type="text" name="tagset_color" id="tagcolor" value="" placeholder="#default" size="7" maxlength="7" /></div>
	</div>
</form>
<form id="usertag_form" action="" method="post">
	<input type="hidden" id="usertag_action" name="usertag_action" value="" />
	<div id="usertag_0">
		<div><input type="text" id="tagname_0" name="tagname_new" /></div>
		<div><label class="lc"><input type="checkbox" id="tagwatch_0" name="tagwatch_new" /><span></span> Watched</label></div>
		<div><label class="lc"><input type="checkbox" id="taghide_0" name="taghide_new" /><span></span> Hidden</label></div>
		<div><input type="text" id="tagcolor_0" name="tagcolor_new" class="tagcolor" value="" /></div>
		<div><input type="text" id="tagweight_0" name="tagweight_new" value="10" size="3" maxlength="3" /></div>
	</div>
	<div id="usertag_3437">
		<div><a href="https://exhentai.org/tag/language:chinese"><div id="tagpreview_3437" class="gt" title="language:chinese">chinese</div></a></div>
		<div><label class="lc"><input type="checkbox" id="tagwatch_3437" checked="checked" /><span></span> Watched</label></div>
		<div><label class="lc"><input type="checkbox" id="taghide_3437" /><span></span> Hidden</label></div>
		<div><input type="text" id="tagcolor_3437" class="tagcolor" value="" /></div>
		<div><input type="text" id="tagweight_3437" value="90" size="3" maxlength="3" /></div>
	</div>
	<div id="usertag_304301">
		<div><a href="https://exhentai.org/tag/male:males+only"><div id="tagpreview_304301" class="gt" title="male:males only">m:males only</div></a></div>
		<div><label class="lc"><input type="checkbox" id="tagwatch_304301" /><span></span> Watched</label></div>
		<div><label class="lc"><input type="checkbox" id="taghide_304301" checked="checked" /><span></span> Hidden</label></div>
		<div><input type="text" id="tagcolor_304301" class="tagcolor" value="" /></div>
		<div><input type="text" id="tagweight_304301" value="-10" size="3" maxlength="3" /></div>
	</div>
	<div id="usertag_5354">
		<div><a href="https://exhentai.org/tag/male:gender+change"><div id="tagpreview_5354" class="gt" title="male:gender change">m:gender change</div></a></div>
		<div><label class="lc"><input type="checkbox" id="tagwatch_5354" checked="checked" /><span></span> Watched</label></div>
		<div><label class="lc"><input type="checkbox" id="taghide_5354" /><span></span> Hidden</label></div>
		<div><input type="text" id="tagcolor_5354" class="tagcolor" value="#AABBCC" /></div>
		<div><input type="text" id="tagweight_5354" value="6" size="3" maxlength="3" /></div>
	</div>
</form>
</body></html>`

// rewriteTransport sends every request to the httptest server, keeping the
// original path so /mytags and /api.php stay distinguishable.
type rewriteTransport struct {
	mockURL string
}

func (t *rewriteTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	u, err := url.Parse(t.mockURL)
	if err != nil {
		return nil, err
	}
	req.URL.Scheme = u.Scheme
	req.URL.Host = u.Host
	return http.DefaultTransport.RoundTrip(req)
}

func newRewriteClient(mockURL string) *http.Client {
	return &http.Client{Transport: &rewriteTransport{mockURL: mockURL}}
}

func fixtureDoc(t *testing.T, html string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	return doc
}

func TestParseMyTags(t *testing.T) {
	snapshot, err := parseMyTags(fixtureDoc(t, myTagsFixture))
	if err != nil {
		t.Fatalf("parseMyTags: %v", err)
	}

	if snapshot.APIUID != 12345 {
		t.Errorf("APIUID = %d, want 12345", snapshot.APIUID)
	}
	if snapshot.APIKey != "deadbeefcafe12345678" {
		t.Errorf("APIKey = %q, want deadbeefcafe12345678", snapshot.APIKey)
	}
	if snapshot.TagsetID != 1 {
		t.Errorf("TagsetID = %d, want 1", snapshot.TagsetID)
	}
	if snapshot.TagsetColor != "" {
		t.Errorf("TagsetColor = %q, want empty", snapshot.TagsetColor)
	}

	if len(snapshot.Tags) != 3 {
		t.Fatalf("tags len = %d, want 3 (usertag_0 is the add row and must be skipped)", len(snapshot.Tags))
	}

	byID := map[int64]UserTag{}
	for _, tag := range snapshot.Tags {
		byID[tag.TagID] = tag
	}

	chinese := byID[3437]
	if chinese.Tag != "language:chinese" || !chinese.Watch || chinese.Hide || chinese.Color != "" || chinese.Weight != 90 {
		t.Errorf("language:chinese = %+v, want watched/90/default colour", chinese)
	}
	males := byID[304301]
	if males.Tag != "male:males only" || males.Watch || !males.Hide || males.Weight != -10 {
		t.Errorf("male:males only = %+v, want hidden/-10", males)
	}
	colored := byID[5354]
	if colored.Tag != "male:gender change" || !colored.Watch || colored.Color != "#AABBCC" || colored.Weight != 6 {
		t.Errorf("male:gender change = %+v, want watched/#AABBCC/6", colored)
	}
}

func TestParseMyTags_MissingCredentials(t *testing.T) {
	_, err := parseMyTags(fixtureDoc(t, `<html><body><p>login</p></body></html>`))
	if err == nil {
		t.Fatal("expected error for page without apiuid/apikey")
	}
}

func TestFetchMyTags(t *testing.T) {
	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.RequestURI()
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, myTagsFixture)
	}))
	defer server.Close()

	snapshot, err := FetchMyTags(context.Background(), newRewriteClient(server.URL))
	if err != nil {
		t.Fatalf("FetchMyTags: %v", err)
	}
	if gotPath != "/mytags" {
		t.Errorf("request path = %q, want /mytags", gotPath)
	}
	if len(snapshot.Tags) != 3 {
		t.Errorf("tags len = %d, want 3", len(snapshot.Tags))
	}
}

func TestSetUserTag(t *testing.T) {
	var gotBody map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api.php" {
			t.Errorf("path = %q, want /api.php", r.URL.Path)
		}
		defer r.Body.Close()
		if err := json.UnmarshalRead(r.Body, &gotBody); err != nil {
			t.Errorf("decode request: %v", err)
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"tagid":3437}`)
	}))
	defer server.Close()

	snap := &MyTagsSnapshot{APIUID: 99, APIKey: "cafebabe0123456789"}
	err := SetUserTag(context.Background(), newRewriteClient(server.URL), snap, UserTag{
		TagID:  3437,
		Tag:    "language:chinese",
		Watch:  true,
		Hide:   false,
		Weight: 50,
	})
	if err != nil {
		t.Fatalf("SetUserTag: %v", err)
	}

	if gotBody["method"] != "setusertag" {
		t.Errorf("method = %v, want setusertag", gotBody["method"])
	}
	if gotBody["apiuid"] != float64(99) {
		t.Errorf("apiuid = %v, want 99", gotBody["apiuid"])
	}
	if gotBody["apikey"] != "cafebabe0123456789" {
		t.Errorf("apikey = %v", gotBody["apikey"])
	}
	if gotBody["tagid"] != float64(3437) {
		t.Errorf("tagid = %v, want 3437", gotBody["tagid"])
	}
	if gotBody["tagwatch"] != float64(1) || gotBody["taghide"] != float64(0) {
		t.Errorf("tagwatch/taghide = %v/%v, want 1/0", gotBody["tagwatch"], gotBody["taghide"])
	}
	if gotBody["tagcolor"] != "" {
		t.Errorf("tagcolor = %v, want empty", gotBody["tagcolor"])
	}
	if gotBody["tagweight"] != "50" {
		t.Errorf("tagweight = %v, want string 50", gotBody["tagweight"])
	}
}

func TestSetUserTag_APIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"error":"Something went wrong"}`)
	}))
	defer server.Close()

	err := SetUserTag(context.Background(), newRewriteClient(server.URL), &MyTagsSnapshot{}, UserTag{TagID: 1})
	if err == nil || !strings.Contains(err.Error(), "Something went wrong") {
		t.Fatalf("err = %v, want api error", err)
	}
}

func TestSetUserTag_LoginRequired(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"login":true}`)
	}))
	defer server.Close()

	err := SetUserTag(context.Background(), newRewriteClient(server.URL), &MyTagsSnapshot{}, UserTag{TagID: 1})
	if err == nil || !strings.Contains(err.Error(), "requires login") {
		t.Fatalf("err = %v, want login error", err)
	}
}

// myTagsFixtureWith appends one more usertag row to the fixture, mimicking
// the updated page the source returns after an add.
func myTagsFixtureWith(tagID int64, tag string) string {
	display := tag
	if parts := strings.SplitN(tag, ":", 2); len(parts) == 2 {
		display = parts[1]
	}
	row := fmt.Sprintf(`
	<div id="usertag_%d">
		<div><a href="https://exhentai.org/tag/%s"><div id="tagpreview_%d" class="gt" title="%s">%s</div></a></div>
		<div><label class="lc"><input type="checkbox" id="tagwatch_%d" checked="checked" /><span></span> Watched</label></div>
		<div><label class="lc"><input type="checkbox" id="taghide_%d" /><span></span> Hidden</label></div>
		<div><input type="text" id="tagcolor_%d" class="tagcolor" value="" /></div>
		<div><input type="text" id="tagweight_%d" value="10" size="3" maxlength="3" /></div>
	</div>`, tagID, strings.ReplaceAll(tag, " ", "+"), tagID, tag, display, tagID, tagID, tagID, tagID)
	return strings.Replace(myTagsFixture, "</form>", row+"\n</form>", 1)
}

func TestAddUserTagForm(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotForm = r.PostForm
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, myTagsFixtureWith(777, "artist:test person"))
	}))
	defer server.Close()

	snap := &MyTagsSnapshot{TagsetID: 1}
	err := AddUserTagForm(context.Background(), newRewriteClient(server.URL), snap, UserTag{
		Tag:    "artist:test person",
		Watch:  true,
		Weight: 10,
	})
	if err != nil {
		t.Fatalf("AddUserTagForm: %v", err)
	}

	if gotForm.Get("usertag_action") != "add" {
		t.Errorf("usertag_action = %q, want add", gotForm.Get("usertag_action"))
	}
	if gotForm.Get("tagname_new") != "artist:test person" {
		t.Errorf("tagname_new = %q", gotForm.Get("tagname_new"))
	}
	if gotForm.Get("tagwatch_new") != "on" {
		t.Errorf("tagwatch_new = %q, want on", gotForm.Get("tagwatch_new"))
	}
	if gotForm.Get("tagweight_new") != "10" {
		t.Errorf("tagweight_new = %q, want 10", gotForm.Get("tagweight_new"))
	}
}

func TestAddUserTagForm_NotPresentInResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Response does not contain the added tag — e.g. upstream rejected it.
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, myTagsFixture)
	}))
	defer server.Close()

	err := AddUserTagForm(context.Background(), newRewriteClient(server.URL), &MyTagsSnapshot{}, UserTag{Tag: "artist:ghost"})
	if err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("err = %v, want not-present error", err)
	}
}

func TestDeleteUserTagsForm(t *testing.T) {
	var gotForm url.Values
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := r.ParseForm(); err != nil {
			t.Errorf("parse form: %v", err)
		}
		gotForm = r.PostForm
		// Respond with the two deleted rows removed.
		html := myTagsFixture
		html = strings.Replace(html, `<div id="usertag_3437">`, `<div id="usertag_removed_3437" style="display:none">`, 1)
		html = strings.Replace(html, `id="tagpreview_3437"`, `id="tagpreview_removed_3437"`, 1)
		html = strings.Replace(html, `<div id="usertag_304301">`, `<div id="usertag_removed_304301" style="display:none">`, 1)
		html = strings.Replace(html, `id="tagpreview_304301"`, `id="tagpreview_removed_304301"`, 1)
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, html)
	}))
	defer server.Close()

	err := DeleteUserTagsForm(context.Background(), newRewriteClient(server.URL), &MyTagsSnapshot{}, []int64{3437, 304301})
	if err != nil {
		t.Fatalf("DeleteUserTagsForm: %v", err)
	}

	if gotForm.Get("usertag_action") != "mass" {
		t.Errorf("usertag_action = %q, want mass", gotForm.Get("usertag_action"))
	}
	if gotForm.Get("usertag_target") != "0" {
		t.Errorf("usertag_target = %q, want 0", gotForm.Get("usertag_target"))
	}
	ids := gotForm["modify_usertags[]"]
	if len(ids) != 2 || ids[0] != "3437" || ids[1] != "304301" {
		t.Errorf("modify_usertags[] = %v, want [3437 304301]", ids)
	}
}

func TestDeleteUserTagsForm_StillPresent(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, myTagsFixture)
	}))
	defer server.Close()

	err := DeleteUserTagsForm(context.Background(), newRewriteClient(server.URL), &MyTagsSnapshot{}, []int64{3437})
	if err == nil || !strings.Contains(err.Error(), "still present") {
		t.Fatalf("err = %v, want still-present error", err)
	}
}

func TestDeleteUserTagsForm_Empty(t *testing.T) {
	if err := DeleteUserTagsForm(context.Background(), nil, &MyTagsSnapshot{}, nil); err != nil {
		t.Errorf("empty ids should be a no-op, got %v", err)
	}
}
