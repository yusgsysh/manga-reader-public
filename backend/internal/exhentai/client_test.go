package exhentai

import (
	"net/http"
	"net/url"
	"testing"
)

func cookieNames(t *testing.T, jar http.CookieJar) []string {
	t.Helper()
	u, err := url.Parse(ExhentaiBase)
	if err != nil {
		t.Fatalf("parse base: %v", err)
	}
	var names []string
	for _, c := range jar.Cookies(u) {
		names = append(names, c.Name)
	}
	return names
}

func hasCookie(jar http.CookieJar, name, value string) bool {
	u, _ := url.Parse(ExhentaiBase)
	for _, c := range jar.Cookies(u) {
		if c.Name == name && c.Value == value {
			return true
		}
	}
	return false
}

func TestNewHotJarAcceptsAnIncompleteConfiguration(t *testing.T) {
	// The server must boot before cookies exist, so the settings page can fix
	// them; an empty jar is simply an unauthenticated client.
	jar := NewHotJar(CookieConfig{})
	if names := cookieNames(t, jar); len(names) != 0 {
		t.Errorf("cookies = %v, want none", names)
	}
}

func TestNewHotJarPreloadsConfiguredCookies(t *testing.T) {
	jar := NewHotJar(CookieConfig{
		IpbMemberID: "member",
		IpbPassHash: "hash",
		Igneous:     "igneous",
		SK:          "sk",
	})
	for _, want := range []struct{ name, value string }{
		{"ipb_member_id", "member"},
		{"ipb_pass_hash", "hash"},
		{"igneous", "igneous"},
		{"sk", "sk"},
	} {
		if !hasCookie(jar, want.name, want.value) {
			t.Errorf("cookie %s = missing, want %s", want.name, want.value)
		}
	}
}

func TestHotJarReplaceDiscardsThePreviousCredentials(t *testing.T) {
	jar := NewHotJar(CookieConfig{IpbMemberID: "old", IpbPassHash: "old-hash"})

	u, _ := url.Parse(ExhentaiBase)
	jar.SetCookies(u, []*http.Cookie{{Name: "upstream_session", Value: "received"}})

	if err := jar.Replace(CookieConfig{IpbMemberID: "new", IpbPassHash: "new-hash"}); err != nil {
		t.Fatalf("Replace: %v", err)
	}

	if hasCookie(jar, "ipb_member_id", "old") {
		t.Error("old member id survived the swap")
	}
	if !hasCookie(jar, "ipb_member_id", "new") {
		t.Error("new member id missing")
	}
	// Cookies learned from the site belong to the old identity too.
	if hasCookie(jar, "upstream_session", "received") {
		t.Error("upstream cookie survived the credential change")
	}
}

func TestNewHTTPClientUsesTheReturnedJar(t *testing.T) {
	client, jar := NewHTTPClient(CookieConfig{IpbMemberID: "m", IpbPassHash: "h"})
	if client.Jar != jar {
		t.Fatal("client.Jar is not the returned HotJar; settings changes would not reach it")
	}
	if !hasCookie(client.Jar, "ipb_member_id", "m") {
		t.Error("client jar does not carry the configured cookie")
	}
	if client.Transport == nil {
		t.Error("Transport is nil")
	}
}
