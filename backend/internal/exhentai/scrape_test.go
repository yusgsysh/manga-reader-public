package exhentai

import (
	"strconv"
	"testing"
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
