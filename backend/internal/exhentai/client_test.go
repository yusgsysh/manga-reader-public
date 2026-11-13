package exhentai

import (
	"net/url"
	"testing"
)

// CreateHTTPClient must scope the session cookies to every *.exhentai.org
// host. Host-only cookies would never reach s.exhentai.org, and the
// setusertag API calls would go out unauthenticated.
func TestCreateHTTPClient_CookiesReachAPISubdomain(t *testing.T) {
	client, err := CreateHTTPClient(&CookieConfig{
		IpbMemberID: "123",
		IpbPassHash: "abc",
		Igneous:     "ig",
		SK:          "sk",
	})
	if err != nil {
		t.Fatalf("CreateHTTPClient: %v", err)
	}

	want := map[string]bool{
		"ipb_member_id": true,
		"ipb_pass_hash": true,
		"igneous":       true,
		"sk":            true,
	}
	for _, raw := range []string{
		"https://exhentai.org/mytags",
		"https://s.exhentai.org/api.php",
		// One user may browse either site, so the jar carries the session on
		// e-hentai.org too (same account, same cookie values).
		"https://e-hentai.org/g/1/abc/",
	} {
		u, _ := url.Parse(raw)
		got := map[string]bool{}
		for _, c := range client.Jar.Cookies(u) {
			got[c.Name] = true
		}
		for name := range want {
			if !got[name] {
				t.Errorf("cookie %q missing for %s (got %v)", name, raw, got)
			}
		}
	}
}

// SiteBaseURL maps the per-user site preference to its browsing host; an
// unknown or empty preference falls back to exhentai.
func TestSiteBaseURL(t *testing.T) {
	cases := map[string]string{
		"":           ExhentaiURL,
		"exhentai":   ExhentaiURL,
		"e-hentai":   EHentaiURL,
		"something+": ExhentaiURL,
	}
	for site, want := range cases {
		if got := SiteBaseURL(site); got != want {
			t.Errorf("SiteBaseURL(%q) = %s, want %s", site, got, want)
		}
	}
}

// GalleryURLAt keeps id/token semantics on either host.
func TestGalleryURLAt(t *testing.T) {
	got := GalleryURLAt(EHentaiURL, "42", "tok")
	if got != "https://e-hentai.org/g/42/tok/" {
		t.Fatalf("GalleryURLAt = %s", got)
	}
}
