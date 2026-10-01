package exhentai

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"
)

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
