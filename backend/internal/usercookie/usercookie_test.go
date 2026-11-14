package usercookie

import (
	"database/sql"
	"strings"
	"testing"

	entsql "entgo.io/ent/dialect/sql"
	"manga-reader/internal/ident"
	_ "modernc.org/sqlite"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/usersetting"
)

// testID returns a deterministic UUID whose last byte is n.
func testID(n byte) ident.UUID {
	var id ident.UUID
	id[15] = n
	return id
}

func newTestClient(t *testing.T) *ent.Client {
	t.Helper()
	conn, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	conn.SetMaxOpenConns(1)
	if _, err := conn.Exec("PRAGMA foreign_keys=ON"); err != nil {
		t.Fatalf("foreign keys: %v", err)
	}
	client := ent.NewClient(ent.Driver(entsql.OpenDB("sqlite3", conn)))
	if err := client.Schema.Create(t.Context()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	t.Cleanup(func() { client.Close() })
	return client
}

// Site returns the stored preference, defaulting to exhentai for users
// without a row or an unset site.
func TestSitePreference(t *testing.T) {
	client := newTestClient(t)
	svc := New(client, strings.Repeat("s", 32))
	ctx := t.Context()

	site, err := svc.Site(ctx, testID(7))
	if err != nil {
		t.Fatalf("site without row: %v", err)
	}
	if site != SiteExHentai {
		t.Fatalf("default site = %q, want %q", site, SiteExHentai)
	}

	cfg := config.CookieConfig{IpbMemberID: "1", IpbPassHash: strings.Repeat("a", 32)}
	if _, err := svc.SaveCookie(ctx, testID(7), cfg, SiteEHentai); err != nil {
		t.Fatalf("save cookie: %v", err)
	}
	site, err = svc.Site(ctx, testID(7))
	if err != nil {
		t.Fatalf("site after save: %v", err)
	}
	if site != SiteEHentai {
		t.Fatalf("stored site = %q, want %q", site, SiteEHentai)
	}
	if _, err := svc.SaveSite(ctx, testID(7), SiteExHentai); err != nil {
		t.Fatalf("save site: %v", err)
	}
	if site, _ = svc.Site(ctx, testID(7)); site != SiteExHentai {
		t.Fatalf("site after SaveSite = %q, want %q", site, SiteExHentai)
	}
}

func TestCookieIsEncryptedAndUserScoped(t *testing.T) {
	client := newTestClient(t)
	svc := New(client, strings.Repeat("s", 32))

	cfgA := config.CookieConfig{IpbMemberID: "111", IpbPassHash: strings.Repeat("a", 32), Igneous: "ign-a"}
	if _, err := svc.SaveCookie(t.Context(), testID(1), cfgA, "exhentai"); err != nil {
		t.Fatalf("save A: %v", err)
	}
	cfgB := config.CookieConfig{IpbMemberID: "222", IpbPassHash: strings.Repeat("b", 32), SK: "sk-b"}
	if _, err := svc.SaveCookie(t.Context(), testID(2), cfgB, "e-hentai"); err != nil {
		t.Fatalf("save B: %v", err)
	}

	rowA, err := client.UserSetting.Query().Where(usersetting.IDEQ(testID(1))).Only(t.Context())
	if err != nil {
		t.Fatalf("row A: %v", err)
	}
	if strings.Contains(rowA.CookieCiphertext, "ipb_pass_hash") || strings.Contains(rowA.CookieCiphertext, cfgA.IpbPassHash) {
		t.Fatalf("cookie stored in plaintext: %q", rowA.CookieCiphertext)
	}
	clientA, err := svc.Client(t.Context(), testID(1))
	if err != nil {
		t.Fatalf("client A: %v", err)
	}
	clientB, err := svc.Client(t.Context(), testID(2))
	if err != nil {
		t.Fatalf("client B: %v", err)
	}
	if clientA == clientB {
		t.Fatal("users share one client")
	}
	if clientA.Jar == nil || clientB.Jar == nil {
		t.Fatal("per-user clients must carry a cookie jar")
	}

	summaryA, err := svc.Summary(t.Context(), testID(1))
	if err != nil {
		t.Fatalf("summary A: %v", err)
	}
	if !summaryA.Configured || !summaryA.HasIgneous || summaryA.HasSK {
		t.Fatalf("summary A = %+v", summaryA)
	}

	// Clearing A leaves B configured.
	if err := svc.ClearCookie(t.Context(), testID(1)); err != nil {
		t.Fatalf("clear A: %v", err)
	}
	summaryB, err := svc.Summary(t.Context(), testID(2))
	if err != nil {
		t.Fatalf("summary B: %v", err)
	}
	if !summaryB.Configured || summaryB.Site != SiteEHentai {
		t.Fatalf("summary B damaged by A clear: %+v", summaryB)
	}
	if _, err := svc.Client(t.Context(), testID(1)); err != ErrNotConfigured {
		t.Fatalf("A client after clear = %v, want ErrNotConfigured", err)
	}
}

func TestPreferencesAreUserScoped(t *testing.T) {
	client := newTestClient(t)
	svc := New(client, strings.Repeat("s", 32))
	if err := svc.SavePreferences(t.Context(), testID(1), map[string]any{"reader": "rtl"}); err != nil {
		t.Fatalf("save A: %v", err)
	}
	a, err := svc.Preferences(t.Context(), testID(1))
	if err != nil || a["reader"] != "rtl" {
		t.Fatalf("prefs A = %+v err %v", a, err)
	}
	b, err := svc.Preferences(t.Context(), testID(2))
	if err != nil || len(b) != 0 {
		t.Fatalf("prefs B = %+v err %v, want empty", b, err)
	}
}
