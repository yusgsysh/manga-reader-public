package handler

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

// TestPageImageRejectsNonAllowlistedURL covers the SSRF/same-origin XSS gap:
// /api/image/page must apply the same URL allowlist as the cached-page
// endpoint instead of fetching whatever the caller passes.
func TestPageImageRejectsNonAllowlistedURL(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	for _, target := range []string{
		"http://169.254.169.254/latest/meta-data/",
		"https://example.com/s/abc/123-1",
		"http://exhentai.org/s/abc/123-1",
		"https://evil.exhentai.org/s/abc/123-1",
	} {
		req := httptest.NewRequest("GET", "/api/image/page?url="+url.QueryEscape(target), nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusBadRequest {
			t.Errorf("%s: status = %d, want 400 (body %s)", target, w.Code, w.Body.String())
		}
	}
}

// TestSearchRejectsOversizedPage: a cold request for page N walks N upstream
// fetches, so absurd page numbers must be rejected up front.
func TestSearchRejectsOversizedPage(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/search?q=yuri&page=100000", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Errorf("page=100000: status = %d, want 400 (body %s)", w.Code, w.Body.String())
	}
}
