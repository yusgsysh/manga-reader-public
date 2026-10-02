package exhentai

import (
	"context"
	"net/http"
	"net/http/httptest"
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

// A non-OK page response must surface as a status-classified error so callers
// treat it as permanent and self-heal via a gallery refresh, rather than the
// generic parse failure.
func TestScrapePageImageURL_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	}))
	defer srv.Close()

	_, _, err := ScrapePageImageURL(context.Background(), srv.Client(), srv.URL+"/s/abc/123-1")
	if err == nil {
		t.Fatal("expected error for 404 page response")
	}
	if got := HTTPStatusCode(err); got != http.StatusNotFound {
		t.Errorf("HTTPStatusCode = %d, want %d", got, http.StatusNotFound)
	}
	if !IsPermanentUpstreamError(err) {
		t.Error("404 page response should be a permanent upstream error")
	}
}

func TestScrapePageImageURL_OKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte(`<html><body><div id="imgd"><img id="img" src="https://example.com/a.webp"/></div></body></html>`))
	}))
	defer srv.Close()

	imgURL, fallbackURL, err := ScrapePageImageURL(context.Background(), srv.Client(), srv.URL+"/s/abc/123-1")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if imgURL != "https://example.com/a.webp" {
		t.Errorf("imgURL = %q, want %q", imgURL, "https://example.com/a.webp")
	}
	if fallbackURL != "" {
		t.Errorf("fallbackURL = %q, want empty", fallbackURL)
	}
}
