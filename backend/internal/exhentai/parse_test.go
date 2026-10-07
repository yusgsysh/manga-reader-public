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
		// "g" as the first path segment: splits[i-1] must not be read (panic
		// regression: a relative href like this appeared in upstream HTML).
		{"relative g href", "g/3138775/30b0285f9b", "", "", ""},
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
