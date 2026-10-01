package exhentai

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/PuerkitoBio/goquery"
)

func TestParseStars(t *testing.T) {
	tests := []struct {
		name   string
		input  string
		expect float64
	}{
		{"5 stars", "background-position:0px -1px;opacity:1", 5.0},
		{"4.5 stars", "background-position:0px -21px;opacity:1", 4.5},
		{"4 stars", "background-position:-16px -1px;opacity:1", 4.0},
		{"3.5 stars", "background-position:-16px -21px;opacity:1", 3.5},
		{"3 stars", "background-position:-32px -1px;opacity:1", 3.0},
		{"2.5 stars", "background-position:-32px -21px;opacity:1", 2.5},
		{"2 stars", "background-position:-48px -1px;opacity:1", 2.0},
		{"1.5 stars", "background-position:-48px -21px;opacity:1", 1.5},
		{"1 star", "background-position:-64px -1px;opacity:1", 1.0},
		{"0.5 stars", "background-position:-64px -21px;opacity:1", 0.5},
		{"0 stars", "background-position:-80px -1px;opacity:1", 0.0},
		{"empty string", "", 0.0},
		{"garbage", "no-match-here", 0.0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := ParseStars(tt.input)
			if got != tt.expect {
				t.Errorf("ParseStars(%q) = %f, want %f", tt.input, got, tt.expect)
			}
		})
	}
}

func TestBuildCategoryFilter(t *testing.T) {
	tests := []struct {
		name   string
		input  []string
		expect string
	}{
		{"empty", nil, ""},
		{"doujinshi only", []string{"doujinshi"}, strconv.FormatUint(uint64(1023^2), 10)},
		{"manga only", []string{"manga"}, strconv.FormatUint(uint64(1023^4), 10)},
		{"doujinshi+manga", []string{"doujinshi", "manga"}, strconv.FormatUint(uint64(1023^6), 10)},
		{"all categories", []string{"doujinshi", "manga", "artistcg", "gamecg", "imageset", "cosplay", "asianporn", "nonh", "western", "misc"}, "0"},
		{"unknown category", []string{"unknown"}, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildCategoryFilter(tt.input)
			if got != tt.expect {
				t.Errorf("BuildCategoryFilter(%v) = %q, want %q", tt.input, got, tt.expect)
			}
		})
	}
}

func TestParseGalleryURL(t *testing.T) {
	tests := []struct {
		name         string
		input        string
		expectDomain string
		expectGId    string
		expectGToken string
	}{
		{"exhentai url", "https://exhentai.org/g/3138775/30b0285f9b/", "exhentai.org", "3138775", "30b0285f9b"},
		{"ehentai url", "https://e-hentai.org/g/3138775/30b0285f9b/", "e-hentai.org", "3138775", "30b0285f9b"},
		{"no trailing slash", "https://exhentai.org/g/3138775/30b0285f9b", "exhentai.org", "3138775", "30b0285f9b"},
		{"invalid url", "https://exhentai.org/not/a/gallery", "", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			domain, gId, gToken := ParseGalleryURL(tt.input)
			if domain != tt.expectDomain || gId != tt.expectGId || gToken != tt.expectGToken {
				t.Errorf("ParseGalleryURL(%q) = (%q, %q, %q), want (%q, %q, %q)",
					tt.input, domain, gId, gToken, tt.expectDomain, tt.expectGId, tt.expectGToken)
			}
		})
	}
}

func TestBuildNlFallbackURL(t *testing.T) {
	tests := []struct {
		name     string
		pageURL  string
		onclick  string
		expected string
	}{
		{"with nl code", "https://exhentai.org/s/abc123/3138775-1", `return nl('SZF-483294')`, "https://exhentai.org/s/abc123/3138775-1?nl=SZF-483294"},
		{"with existing query params", "https://exhentai.org/s/abc123/3138775-1?param=value", `return nl('XYZ-999')`, "https://exhentai.org/s/abc123/3138775-1?param=value&nl=XYZ-999"},
		{"empty onclick", "https://exhentai.org/s/abc123/3138775-1", "", ""},
		{"non-matching onclick", "https://exhentai.org/s/abc123/3138775-1", "return somethingElse('test')", ""},
		{"invalid URL", "://invalid", `return nl('ABC-123')`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildNlFallbackURL(tt.pageURL, tt.onclick)
			if got != tt.expected {
				t.Errorf("BuildNlFallbackURL(%q, %q) = %q, want %q", tt.pageURL, tt.onclick, got, tt.expected)
			}
		})
	}
}

func BenchmarkParseStars(b *testing.B) {
	for b.Loop() {
		ParseStars("background-position:-32px -1px;opacity:1")
	}
}

func BenchmarkBuildCategoryFilter(b *testing.B) {
	cats := []string{"doujinshi", "manga", "artistcg"}
	for b.Loop() {
		BuildCategoryFilter(cats)
	}
}

//go:fix inline
func intPtr(v int) *int { return new(v) }

func TestBuildSearchQuery(t *testing.T) {
	tests := []struct {
		name     string
		keyword  string
		cats     []string
		opts     *SearchOptions
		expected map[string]string
	}{
		{
			name:    "basic keyword only",
			keyword: "yuri",
			opts:    nil,
			expected: map[string]string{
				"f_search": "yuri",
			},
		},
		{
			name:    "keyword with categories",
			keyword: "test",
			cats:    []string{"doujinshi"},
			opts:    nil,
			expected: map[string]string{
				"f_search": "test",
				"f_cats":   strconv.FormatUint(uint64(1023^2), 10),
			},
		},
		{
			name:    "page range",
			keyword: "test",
			opts: &SearchOptions{
				MinPages: new(10),
				MaxPages: new(200),
			},
			expected: map[string]string{
				"f_search":  "test",
				"advsearch": "1",
				"f_spf":     "10",
				"f_spt":     "200",
			},
		},
		{
			name:    "min rating",
			keyword: "test",
			opts: &SearchOptions{
				MinRating: new(4),
			},
			expected: map[string]string{
				"f_search":  "test",
				"advsearch": "1",
				"f_sr":      "on",
				"f_srdd":    "4",
			},
		},
		{
			name:    "has torrent",
			keyword: "test",
			opts: &SearchOptions{
				HasTorrent: true,
			},
			expected: map[string]string{
				"f_search":  "test",
				"advsearch": "1",
				"f_sto":     "on",
			},
		},
		{
			name:    "include expunged",
			keyword: "test",
			opts: &SearchOptions{
				IncludeExpunged: true,
			},
			expected: map[string]string{
				"f_search":  "test",
				"advsearch": "1",
				"f_sh":      "on",
			},
		},
		{
			name:    "search targets",
			keyword: "test",
			opts: &SearchOptions{
				SearchName:        true,
				SearchTags:        true,
				SearchDescription: true,
			},
			expected: map[string]string{
				"f_search":  "test",
				"advsearch": "1",
				"f_sname":   "on",
				"f_stags":   "on",
				"f_sdesc":   "on",
			},
		},
		{
			name:    "low power and downvoted tags",
			keyword: "test",
			opts: &SearchOptions{
				IncludeLowPowerTags:  true,
				IncludeDownvotedTags: true,
			},
			expected: map[string]string{
				"f_search":  "test",
				"advsearch": "1",
				"f_sdt1":    "on",
				"f_sdt2":    "on",
			},
		},
		{
			name:    "disable filters",
			keyword: "test",
			opts: &SearchOptions{
				DisableLanguageFilter: true,
				DisableUploaderFilter: true,
				DisableTagFilter:      true,
			},
			expected: map[string]string{
				"f_search":  "test",
				"advsearch": "1",
				"f_sfl":     "on",
				"f_sfu":     "on",
				"f_sft":     "on",
			},
		},
		{
			name:    "combined advanced search",
			keyword: "o:3d$",
			opts: &SearchOptions{
				MinPages:   new(10),
				MaxPages:   new(200),
				MinRating:  new(4),
				HasTorrent: true,
			},
			expected: map[string]string{
				"f_search":  "o:3d$",
				"advsearch": "1",
				"f_spf":     "10",
				"f_spt":     "200",
				"f_sr":      "on",
				"f_srdd":    "4",
				"f_sto":     "on",
			},
		},
		{
			name:    "empty opts no advanced",
			keyword: "test",
			opts:    &SearchOptions{},
			expected: map[string]string{
				"f_search": "test",
			},
		},
		{
			name:    "only min_pages",
			keyword: "test",
			opts: &SearchOptions{
				MinPages: new(5),
			},
			expected: map[string]string{
				"f_search":  "test",
				"advsearch": "1",
				"f_spf":     "5",
			},
		},
		{
			name:    "only max_pages",
			keyword: "test",
			opts: &SearchOptions{
				MaxPages: new(100),
			},
			expected: map[string]string{
				"f_search":  "test",
				"advsearch": "1",
				"f_spt":     "100",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := BuildSearchQuery(tt.keyword, tt.cats, tt.opts)

			for key, wantVal := range tt.expected {
				gotVal := got.Get(key)
				if gotVal != wantVal {
					t.Errorf("BuildSearchQuery()[%q] = %q, want %q", key, gotVal, wantVal)
				}
			}

			if len(got) != len(tt.expected) {
				t.Errorf("BuildSearchQuery() returned %d params, expected %d", len(got), len(tt.expected))
				for k := range got {
					if _, ok := tt.expected[k]; !ok {
						t.Errorf("  unexpected param: %q=%q", k, got.Get(k))
					}
				}
			}
		})
	}
}

// listingHTML renders a minimal listing page whose "Next >" cursor points at
// nextHref (omit nextHref to simulate the last page).
func listingHTML(nextHref string) string {
	next := ""
	if nextHref != "" {
		next = `<a href="` + nextHref + `">Next &gt;</a>`
	}
	return `<html><head><title>x</title></head><body><div class="ido">` + next + `</div></body></html>`
}

// searchHTML renders a search results page (with the result-count banner when
// withBanner) and a "Next >" cursor at nextHref.
func searchHTML(withBanner bool, nextHref string) string {
	banner := ""
	if withBanner {
		banner = `<div class="searchtext"><p>Found 100 results</p></div>`
	}
	next := ""
	if nextHref != "" {
		next = `<a href="` + nextHref + `">Next &gt;</a>`
	}
	return `<html><head><title>x</title></head><body>
<div class="ido"><div></div><div>` + banner + `
<table class="itg gltm"><tbody><tr>
<td class="gl3m glname"><a href="https://exhentai.org/g/123/abc"><div class="glink">Title</div></a></td>
<td class="gl1m"><div class="cs">Doujinshi</div></td>
<td class="gl2m"><div class="ir" style="background-position:-16px -1px"></div></td>
<td class="gl5m"><div><a>uploader</a></div></td>
</tr></tbody></table>
</div></div>` + next + `</body></html>`
}

func cursorServer(t *testing.T, render func(withBanner bool, nextHref string) string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var requests atomic.Int32
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "text/html")
		n := 1
		if v := r.URL.Query().Get("next"); v != "" {
			n, _ = strconv.Atoi(v)
		}
		fmt.Fprint(w, render(r.URL.Query().Get("next") == "", fmt.Sprintf("%s/?next=%d", srv.URL, n+1)))
	}))
	t.Cleanup(srv.Close)
	return srv, &requests
}

func TestScrapeGalleryList_SequentialPagesUseCachedCursor(t *testing.T) {
	srv, requests := cursorServer(t, func(_ bool, nextHref string) string {
		return listingHTML(nextHref)
	})

	for page := range 4 {
		results, _, err := ScrapeGalleryList(t.Context(), srv.Client(), srv.URL+"/", page, nil, nil)
		if err != nil {
			t.Fatalf("page %d: %v", page, err)
		}
		if len(results) != 0 {
			t.Fatalf("page %d: expected empty results, got %d", page, len(results))
		}
	}
	if got := requests.Load(); got != 4 {
		t.Errorf("upstream requests = %d, want 4 (one per sequential page)", got)
	}
}

func TestScrapeGalleryList_RandomAccessWalksThenCaches(t *testing.T) {
	srv, requests := cursorServer(t, func(_ bool, nextHref string) string {
		return listingHTML(nextHref)
	})

	if _, _, err := ScrapeGalleryList(t.Context(), srv.Client(), srv.URL+"/", 2, nil, nil); err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("after random page 2: requests = %d, want 3 (walk 0..2)", got)
	}
	if _, _, err := ScrapeGalleryList(t.Context(), srv.Client(), srv.URL+"/", 3, nil, nil); err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if got := requests.Load(); got != 4 {
		t.Errorf("after page 3: requests = %d, want 4 (reuse cached cursor)", got)
	}
}

func TestScrapeSearch_UsesCachedCursorAcrossPages(t *testing.T) {
	srv, requests := cursorServer(t, func(_ bool, nextHref string) string {
		return searchHTML(true, nextHref)
	})

	total, results, _, err := ScrapeSearch(t.Context(), srv.Client(), srv.URL+"/", "test", nil, 1, nil, nil)
	if err != nil {
		t.Fatalf("page 1: %v", err)
	}
	if total != 100 || len(results) != 1 {
		t.Fatalf("page 1: total=%d results=%d, want 100/1", total, len(results))
	}
	if got := requests.Load(); got != 2 {
		t.Errorf("after page 1: requests = %d, want 2 (walk 0..1)", got)
	}

	total, results, _, err = ScrapeSearch(t.Context(), srv.Client(), srv.URL+"/", "test", nil, 2, nil, nil)
	if err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if total != 100 || len(results) != 1 {
		t.Fatalf("page 2: total=%d results=%d, want 100/1", total, len(results))
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("after page 2: requests = %d, want 3 (reuse cached cursor)", got)
	}
}

func TestScrapeSearch_FallsBackToPageZeroForTotal(t *testing.T) {
	srv, requests := cursorServer(t, func(withBanner bool, nextHref string) string {
		return searchHTML(withBanner, nextHref)
	})

	total, results, _, err := ScrapeSearch(t.Context(), srv.Client(), srv.URL+"/", "test", nil, 1, nil, nil)
	if err != nil {
		t.Fatalf("ScrapeSearch error: %v", err)
	}
	if total != 100 || len(results) != 1 {
		t.Fatalf("total=%d results=%d, want 100/1", total, len(results))
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("upstream requests = %d, want 3 (walk 0..1 + page 0 fallback)", got)
	}
}

func TestExtractNextURL(t *testing.T) {
	tests := []struct {
		name string
		html string
		want string
	}{
		{
			"unext id preferred over label",
			`<html><body><a id="unext" href="/?next=2">Next &gt;</a><a href="/?other=1">Next &gt;</a></body></html>`,
			"/?next=2",
		},
		{
			"rel=next",
			`<html><body><a rel="next" href="/?next=5">anything</a></body></html>`,
			"/?next=5",
		},
		{
			"label fallback",
			`<html><body><a href="/?next=7">Next &gt;</a></body></html>`,
			"/?next=7",
		},
		{
			"none",
			`<html><body><a href="/?x=1">Prev</a></body></html>`,
			"",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := goquery.NewDocumentFromReader(strings.NewReader(tt.html))
			if err != nil {
				t.Fatal(err)
			}
			if got := extractNextURL(doc); got != tt.want {
				t.Errorf("extractNextURL() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestListingNavOptions_Apply(t *testing.T) {
	if (*ListingNavOptions)(nil).Active() {
		t.Error("nil options should not be active")
	}
	if (&ListingNavOptions{}).Active() {
		t.Error("empty options should not be active")
	}

	q := url.Values{}
	nav := &ListingNavOptions{Seek: "2020-01", Jump: "1y"}
	if !nav.Active() {
		t.Error("options with seek/jump should be active")
	}
	nav.Apply(q)

	if got := q.Get("seek"); got != "2020-01" {
		t.Errorf("seek = %q, want %q", got, "2020-01")
	}
	if got := q.Get("jump"); got != "1y" {
		t.Errorf("jump = %q, want %q", got, "1y")
	}
}

func TestParseListingNav(t *testing.T) {
	const navHTML = `<html><head></head><body><script type="text/javascript">
var prevurl="https://exhentai.org/?f_search=x&prev=1814200";
var nexturl="https://exhentai.org/?f_search=x&next=1813761";
var maxdate="2026-10-01";
var mindate="2007-03-20";
var rangeurl="https://exhentai.org/?f_search=x";
var rangemin=74;
var rangemax=80;
var rangespan=2;
</script></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(navHTML))
	if err != nil {
		t.Fatal(err)
	}
	nav := parseListingNav(doc)

	if nav.Prev != "1814200" {
		t.Errorf("Prev = %q, want %q", nav.Prev, "1814200")
	}
	if nav.Next != "1813761" {
		t.Errorf("Next = %q, want %q", nav.Next, "1813761")
	}
	if nav.MinDate != "2007-03-20" || nav.MaxDate != "2026-10-01" {
		t.Errorf("dates = %q..%q, want 2007-03-20..2026-10-01", nav.MinDate, nav.MaxDate)
	}
	if nav.RangeMin != 74 || nav.RangeMax != 80 || nav.RangeSpan != 2 {
		t.Errorf("range = %d..%d span %d, want 74..80 span 2", nav.RangeMin, nav.RangeMax, nav.RangeSpan)
	}
}

func TestParseListingNav_Empty(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(listingHTML("")))
	if err != nil {
		t.Fatal(err)
	}
	if nav := parseListingNav(doc); nav != (ListingNav{}) {
		t.Errorf("parseListingNav() = %+v, want zero value", nav)
	}
}

func TestScrapeSearch_ForwardsNavParams(t *testing.T) {
	var srv *httptest.Server
	var gotSeek, gotJump string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotSeek = r.URL.Query().Get("seek")
		gotJump = r.URL.Query().Get("jump")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, searchHTML(true, ""))
	}))
	t.Cleanup(srv.Close)

	_, _, _, err := ScrapeSearch(t.Context(), srv.Client(), srv.URL+"/", "test", nil, 0, nil, &ListingNavOptions{Seek: "2020", Jump: "1y"})
	if err != nil {
		t.Fatalf("ScrapeSearch: %v", err)
	}
	if gotSeek != "2020" || gotJump != "1y" {
		t.Errorf("upstream seek=%q jump=%q, want 2020/1y", gotSeek, gotJump)
	}
}

func TestScrapeGalleryList_ForwardsNavParams(t *testing.T) {
	var srv *httptest.Server
	var gotJump string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotJump = r.URL.Query().Get("jump")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, listingHTML(""))
	}))
	t.Cleanup(srv.Close)

	results, _, err := ScrapeGalleryList(t.Context(), srv.Client(), srv.URL+"/", 0, nil, &ListingNavOptions{Jump: "3d"})
	if err != nil {
		t.Fatalf("ScrapeGalleryList: %v", err)
	}
	if gotJump != "3d" {
		t.Errorf("upstream jump = %q, want %q", gotJump, "3d")
	}
	if len(results) != 0 {
		t.Errorf("results = %d, want 0", len(results))
	}
}
