package exhentai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	json "encoding/json/v2"

	"manga-reader/internal/metrics"
)

// setUserTagAPIURL is the s.exhentai.org JSON endpoint used by the source
// My Tags page (ehg_mytags.c.js) to flip watch/hide/colour/weight on an
// existing tag row.
const setUserTagAPIURL = "https://s.exhentai.org/api.php"

// mutationSpacing is a small pause between consecutive My Tags mutations so a
// full mirror does not burst the upstream the way a scripted click-spammer
// would; the source JS saves serially per user click, this approximates it.
const mutationSpacing = 150 * time.Millisecond

var (
	apiuidScriptRe  = regexp.MustCompile(`var apiuid = (\d+);`)
	apikeyScriptRe  = regexp.MustCompile(`var apikey = "([a-fA-F0-9]+)";`)
	tagsetPathQuery = "?tagset="
)

// UserTag is one tag row on the source My Tags page. Tag is the full
// "namespace:name" form from the preview title attribute.
type UserTag struct {
	TagID  int64
	Tag    string
	Watch  bool
	Hide   bool
	Color  string // "" means the default colour for the tag's state
	Weight int
}

// MyTagsSnapshot is a parsed /mytags page: the account credentials, the
// active tagset, and every row currently configured on the source.
type MyTagsSnapshot struct {
	APIUID      int64
	APIKey      string
	TagsetID    int
	TagsetColor string
	Tags        []UserTag
}

// myTagsURL builds the /mytags URL for a tagset. The source keeps the active
// tagset in the query string; tagset 1 is the unqualified default page.
func myTagsURL(tagsetID int) string {
	if tagsetID > 1 {
		return ExhentaiURL + "/mytags" + tagsetPathQuery + strconv.Itoa(tagsetID)
	}
	return ExhentaiURL + "/mytags"
}

// FetchMyTags GETs the My Tags page (bypassing the dedup window so a sync
// always sees the current source state) and parses credentials + rows.
func FetchMyTags(ctx context.Context, client *http.Client) (*MyTagsSnapshot, error) {
	doc, err := httpGetDocDirect(ctx, client, myTagsURL(0))
	if err != nil {
		return nil, err
	}
	snapshot, err := parseMyTags(doc)
	if err != nil {
		metrics.ObserveClassification(metrics.Endpoint(myTagsURL(0)), metrics.OutcomeError, http.StatusOK)
		return nil, err
	}
	return snapshot, nil
}

// parseMyTags extracts apiuid/apikey, the active tagset, and every usertag
// row from a My Tags document.
func parseMyTags(doc *goquery.Document) (*MyTagsSnapshot, error) {
	snapshot := &MyTagsSnapshot{}

	doc.Find("script").EachWithBreak(func(_ int, s *goquery.Selection) bool {
		text := s.Text()
		if m := apiuidScriptRe.FindStringSubmatch(text); m != nil && snapshot.APIUID == 0 {
			if uid, err := strconv.ParseInt(m[1], 10, 64); err == nil {
				snapshot.APIUID = uid
			}
		}
		if m := apikeyScriptRe.FindStringSubmatch(text); m != nil && snapshot.APIKey == "" {
			snapshot.APIKey = m[1]
		}
		return snapshot.APIUID == 0 || snapshot.APIKey == ""
	})
	if snapshot.APIUID == 0 || snapshot.APIKey == "" {
		return nil, fmt.Errorf("%w: mytags page missing apiuid/apikey", ErrParsingFailed)
	}

	// Active tagset from the tagset switcher; default to 1 when absent.
	snapshot.TagsetID = 1
	doc.Find(`select[onchange^="change_tagset"] option`).EachWithBreak(func(_ int, o *goquery.Selection) bool {
		if _, selected := o.Attr("selected"); !selected {
			return true
		}
		if v, err := strconv.Atoi(o.AttrOr("value", "1")); err == nil && v > 0 {
			snapshot.TagsetID = v
		}
		return false
	})
	snapshot.TagsetColor = strings.TrimSpace(doc.Find(`input[name="tagset_color"]`).AttrOr("value", ""))

	// Tag rows: div#usertag_{id}; id 0 is the "add new tag" row, not a rule.
	doc.Find(`div[id^="usertag_"]`).Each(func(_ int, row *goquery.Selection) {
		idAttr, _ := row.Attr("id")
		idStr := strings.TrimPrefix(idAttr, "usertag_")
		id, err := strconv.ParseInt(idStr, 10, 64)
		if err != nil || id == 0 {
			return
		}
		preview := doc.Find(fmt.Sprintf("#tagpreview_%d", id))
		if preview.Length() == 0 {
			// Fall back to a search within the row in case the id drifted.
			preview = row.Find(".gt")
		}
		tagName := strings.TrimSpace(preview.AttrOr("title", ""))
		if tagName == "" {
			return
		}
		snapshot.Tags = append(snapshot.Tags, UserTag{
			TagID:  id,
			Tag:    tagName,
			Watch:  doc.Find(fmt.Sprintf("#tagwatch_%d", id)).Is(":checked"),
			Hide:   doc.Find(fmt.Sprintf("#taghide_%d", id)).Is(":checked"),
			Color:  strings.TrimSpace(doc.Find(fmt.Sprintf("#tagcolor_%d", id)).AttrOr("value", "")),
			Weight: parseTagWeight(doc.Find(fmt.Sprintf("#tagweight_%d", id)).AttrOr("value", "")),
		})
	})

	return snapshot, nil
}

// parseTagWeight converts the My Tags weight input to an int; empty or
// malformed input means 0 (no ordering contribution on the source either).
func parseTagWeight(raw string) int {
	w, err := strconv.Atoi(strings.TrimSpace(raw))
	if err != nil {
		return 0
	}
	return w
}

// PauseMutations spaces consecutive My Tags writes so a full mirror does not
// hammer the upstream back-to-back.
func PauseMutations(ctx context.Context) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-time.After(mutationSpacing):
		return nil
	}
}

// AddUserTagForm POSTs the My Tags "add" form for a tag that does not exist
// on the source yet (the source only knows the tag by name here, no tagid).
// It verifies the new row landed by re-parsing the response document.
func AddUserTagForm(ctx context.Context, client *http.Client, snap *MyTagsSnapshot, tag UserTag) error {
	form := url.Values{}
	form.Set("usertag_action", "add")
	form.Set("tagname_new", tag.Tag)
	if tag.Watch {
		form.Set("tagwatch_new", "on")
	}
	if tag.Hide {
		form.Set("taghide_new", "on")
	}
	form.Set("tagcolor_new", tag.Color)
	form.Set("tagweight_new", strconv.Itoa(tag.Weight))

	doc, err := postFormDoc(ctx, client, myTagsURL(snap.TagsetID), form)
	if err != nil {
		return err
	}
	// The response is the updated My Tags page; the new tag must be present
	// for the add to count as applied.
	normalized := strings.ToLower(tag.Tag)
	for _, row := range mustSnapshotTags(doc) {
		if strings.ToLower(row.Tag) == normalized {
			return nil
		}
	}
	return fmt.Errorf("%w: added tag %q not present in response", ErrAPIError, tag.Tag)
}

// mustSnapshotTags re-parses tag rows from a response document for add
// verification; parsing failures surface as "not present".
func mustSnapshotTags(doc *goquery.Document) []UserTag {
	snapshot, err := parseMyTags(doc)
	if err != nil {
		return nil
	}
	return snapshot.Tags
}

// SetUserTag flips watch/hide/colour/weight on an existing source row via the
// JSON API the source My Tags page itself uses (ehg_mytags.c.js).
func SetUserTag(ctx context.Context, client *http.Client, snap *MyTagsSnapshot, tag UserTag) error {
	type request struct {
		Method    string `json:"method"`
		APIUID    int64  `json:"apiuid"`
		APIKey    string `json:"apikey"`
		TagID     int64  `json:"tagid"`
		TagWatch  int    `json:"tagwatch"`
		TagHide   int    `json:"taghide"`
		TagColor  string `json:"tagcolor"`
		TagWeight string `json:"tagweight"`
	}
	type response struct {
		TagID  *int64 `json:"tagid"`
		Error  string `json:"error"`
		Login  bool   `json:"login"`
	}

	reqBody := request{
		Method:    "setusertag",
		APIUID:    snap.APIUID,
		APIKey:    snap.APIKey,
		TagID:     tag.TagID,
		TagWatch:  boolInt(tag.Watch),
		TagHide:   boolInt(tag.Hide),
		TagColor:  tag.Color,
		TagWeight: strconv.Itoa(tag.Weight),
	}
	b, err := json.Marshal(reqBody)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, "POST", setUserTagAPIURL, bytes.NewReader(b))
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	httpReq.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	httpReq.Header.Set("Referer", ExhentaiURL+"/")

	resp, err := client.Do(httpReq)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrRequestFailed, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		metrics.ObserveClassification(metrics.Endpoint(setUserTagAPIURL), metrics.OutcomeHTTPError, resp.StatusCode)
		return &httpStatusError{code: resp.StatusCode}
	}

	var result response
	if err := json.UnmarshalRead(io.LimitReader(resp.Body, maxDocBytes), &result); err != nil {
		return fmt.Errorf("%w: %v", ErrParsingFailed, err)
	}
	if result.Login {
		return fmt.Errorf("%w: setusertag requires login (check cookies)", ErrAPIError)
	}
	if result.Error != "" {
		return fmt.Errorf("%w: %s", ErrAPIError, result.Error)
	}
	if result.TagID == nil || *result.TagID != tag.TagID {
		return fmt.Errorf("%w: setusertag returned unexpected tagid for %d", ErrAPIError, tag.TagID)
	}
	return nil
}

// DeleteUserTagsForm POSTs the mass-delete form for the given source tag ids.
// The source itself batches every selected id into one request.
func DeleteUserTagsForm(ctx context.Context, client *http.Client, snap *MyTagsSnapshot, ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	form := url.Values{}
	form.Set("usertag_action", "mass")
	form.Set("usertag_target", "0")
	for _, id := range ids {
		form.Add("modify_usertags[]", strconv.FormatInt(id, 10))
	}

	doc, err := postFormDoc(ctx, client, myTagsURL(snap.TagsetID), form)
	if err != nil {
		return err
	}
	// Verify each id is gone from the response page.
	present := make(map[int64]bool, len(ids))
	for _, row := range mustSnapshotTags(doc) {
		present[row.TagID] = true
	}
	for _, id := range ids {
		if present[id] {
			return fmt.Errorf("%w: tag %d still present after mass delete", ErrAPIError, id)
		}
	}
	return nil
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}
