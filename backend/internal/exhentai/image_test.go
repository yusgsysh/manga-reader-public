package exhentai

import (
	"testing"
)

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
