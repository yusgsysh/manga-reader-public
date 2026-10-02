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

// listingHTML renders a minimal listing page whose "Next >" cursor points at
// nextHref (omit nextHref to simulate the last page).
func listingHTML(nextHref string) string {
	next := ""
	if nextHref != "" {
		next = `<a href="` + nextHref + `">Next &gt;</a>`
	}
	return `<html><head><title>x</title></head><body><div class="ido">` + next + `</div></body></html>`
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
		results, _, err := ScrapeGalleryList(t.Context(), srv.Client(), srv.URL+"/", page, nil, nil, nil)
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

	if _, _, err := ScrapeGalleryList(t.Context(), srv.Client(), srv.URL+"/", 2, nil, nil, nil); err != nil {
		t.Fatalf("page 2: %v", err)
	}
	if got := requests.Load(); got != 3 {
		t.Errorf("after random page 2: requests = %d, want 3 (walk 0..2)", got)
	}
	if _, _, err := ScrapeGalleryList(t.Context(), srv.Client(), srv.URL+"/", 3, nil, nil, nil); err != nil {
		t.Fatalf("page 3: %v", err)
	}
	if got := requests.Load(); got != 4 {
		t.Errorf("after page 3: requests = %d, want 4 (reuse cached cursor)", got)
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

func TestScrapeGalleryList_ForwardsNavParams(t *testing.T) {
	var srv *httptest.Server
	var gotJump string
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotJump = r.URL.Query().Get("jump")
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, listingHTML(""))
	}))
	t.Cleanup(srv.Close)

	results, _, err := ScrapeGalleryList(t.Context(), srv.Client(), srv.URL+"/", 0, nil, nil, &ListingNavOptions{Jump: "3d"})
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
