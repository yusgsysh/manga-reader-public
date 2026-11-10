package config

import "testing"

// Individual variables must win over a combined EHENTAI_COOKIE string: with the
// old override behaviour a stale partial string silently dropped a fresh
// igneous/sk from the individual setup and ExHentai answered with sad panda.
func TestLoadCookieConfig_IndividualVarsWinOverCombined(t *testing.T) {
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "111")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb")
	t.Setenv("EHENTAI_COOKIE_IGNEOUS", "freshigneous")
	t.Setenv("EHENTAI_COOKIE_SK", "freshsk")
	t.Setenv("EHENTAI_COOKIE", "ipb_member_id=999; ipb_pass_hash=aaaa; igneous=staleigneous; sk=oldsk")

	got := loadCookieConfig()
	if got.IpbMemberID != "111" || got.IpbPassHash != "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb" ||
		got.Igneous != "freshigneous" || got.SK != "freshsk" {
		t.Fatalf("loadCookieConfig() = %+v, want the individual variables", got)
	}
}

// A combined string still works on its own and fills the fields the individual
// variables leave empty, so either documented form is sufficient.
func TestLoadCookieConfig_CombinedFillsMissingFields(t *testing.T) {
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", "111")
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "")
	t.Setenv("EHENTAI_COOKIE_IGNEOUS", "")
	t.Setenv("EHENTAI_COOKIE_SK", "")
	t.Setenv("EHENTAI_COOKIE", "ipb_member_id=999; ipb_pass_hash=hash; igneous=ig; sk=sk")

	got := loadCookieConfig()
	if got.IpbMemberID != "111" {
		t.Errorf("IpbMemberID = %q, want the individual variable", got.IpbMemberID)
	}
	if got.IpbPassHash != "hash" || got.Igneous != "ig" || got.SK != "sk" {
		t.Errorf("combined string did not fill the gaps: %+v", got)
	}
}

// Values pasted through shell wrappers, devtools or a podman env-file can pick up
// quotes, whitespace or a trailing separator; sending those bytes makes ExHentai
// reject the session.
func TestLoadCookieConfig_NormalizesPastedValues(t *testing.T) {
	t.Setenv("EHENTAI_COOKIE_IPB_MEMBER_ID", ` "12345" `)
	t.Setenv("EHENTAI_COOKIE_IPB_PASS_HASH", "hash;\n")
	t.Setenv("EHENTAI_COOKIE_IGNEOUS", `'ig'`)
	t.Setenv("EHENTAI_COOKIE_SK", "sk,")
	t.Setenv("EHENTAI_COOKIE", "")

	got := loadCookieConfig()
	want := CookieConfig{IpbMemberID: "12345", IpbPassHash: "hash", Igneous: "ig", SK: "sk"}
	if got != want {
		t.Fatalf("loadCookieConfig() = %+v, want %+v", got, want)
	}
}
