package config

import "testing"

// Values pasted through shell wrappers, devtools or a podman env-file can pick up
// quotes, whitespace or a trailing separator; sending those bytes makes ExHentai
// reject the session. Per-user cookies arrive as a browser-style header through
// ParseCookieString, so the normalisation has to happen there.
func TestParseCookieString_NormalizesPastedValues(t *testing.T) {
	got := ParseCookieString(" ipb_member_id= \"12345\" ; ipb_pass_hash=hash;\n; igneous='ig'; sk=sk,")
	want := CookieConfig{IpbMemberID: "12345", IpbPassHash: "hash", Igneous: "ig", SK: "sk"}
	if got != want {
		t.Fatalf("ParseCookieString() = %+v, want %+v", got, want)
	}
}

// Encode renders the session as a cookie header and ParseCookieString reads it
// back, which is how the settings API round-trips a submitted cookie.
func TestCookieConfig_RoundTrips(t *testing.T) {
	in := CookieConfig{IpbMemberID: "111", IpbPassHash: "hash", Igneous: "ig", SK: "sk"}
	if got := ParseCookieString(in.Encode()); got != in {
		t.Fatalf("round trip = %+v, want %+v", got, in)
	}
}

// IsValid requires the two long-lived credentials; igneous/sk are optional.
func TestCookieConfig_IsValid(t *testing.T) {
	if (CookieConfig{}).IsValid() {
		t.Fatal("empty cookie must be invalid")
	}
	if !(CookieConfig{IpbMemberID: "1", IpbPassHash: "h"}).IsValid() {
		t.Fatal("member id + pass hash must be valid")
	}
}

// MergedWith only overwrites fields the caller actually supplied; empty fields
// in the overlay leave the stored value untouched.
func TestCookieConfig_MergedWith(t *testing.T) {
	base := CookieConfig{IpbMemberID: "111", IpbPassHash: "hash", Igneous: "old", SK: "sk"}
	got := base.MergedWith(CookieConfig{Igneous: "new"})
	want := CookieConfig{IpbMemberID: "111", IpbPassHash: "hash", Igneous: "new", SK: "sk"}
	if got != want {
		t.Fatalf("MergedWith(igneous) = %+v, want %+v", got, want)
	}

	if got := base.MergedWith(CookieConfig{}); got != base {
		t.Fatalf("MergedWith(empty) = %+v, want unchanged %+v", got, base)
	}
}
