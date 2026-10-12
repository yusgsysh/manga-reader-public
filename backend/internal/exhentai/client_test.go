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
