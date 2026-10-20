package desktop

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withFakeKeyring puts a fake secret-tool/security on PATH so the keyring
// path is exercised without a real desktop store. The fake keeps one value
// in a temp file: store/clear write it, lookup reads it.
func withFakeKeyring(t *testing.T) {
	t.Helper()
	if runtime := os.Getenv("GOOS_RUNTIME"); runtime == "windows" {
		t.Skip("no keyring path on windows")
	}
	dir := t.TempDir()
	store := filepath.Join(dir, "store")
	// secret-tool: "store ... <value>" reads the value from argv[last];
	// "lookup ..." prints it; "clear ..." empties it.
	script := filepath.Join(dir, "secret-tool")
	code := `#!/bin/sh
S="` + store + `"
case "$1" in
  store) shift 6; printf '%s' "$1" > "$S";;
  lookup) [ -f "$S" ] && cat "$S";;
  clear) rm -f "$S";;
esac
`
	if err := os.WriteFile(script, []byte(code), 0o755); err != nil {
		t.Fatal(err)
	}
	// Our dir first: exec.LookPath takes the first hit, so the fake wins
	// even when a real secret-tool is installed.
	t.Setenv("PATH", dir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Cleanup(func() {
		keyringMu.Lock()
		keyringProbed, keyringUsable = false, false
		keyringMu.Unlock()
	})
	// Force a fresh probe against the fake.
	keyringMu.Lock()
	keyringProbed, keyringUsable = false, false
	keyringMu.Unlock()
}

func TestKeyringRoundTripWithFakeStore(t *testing.T) {
	withFakeKeyring(t)
	if !KeyringAvailable() {
		t.Fatal("fake store did not probe as available")
	}
	if err := keyringSave("cookie-value"); err != nil {
		t.Fatalf("save: %v", err)
	}
	got, err := keyringLoad()
	if err != nil || got != "cookie-value" {
		t.Fatalf("load = %q, %v", got, err)
	}
	if err := keyringDelete(); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if got, _ := keyringLoad(); got != "" {
		t.Fatalf("load after delete = %q, want empty", got)
	}
}

func TestKeyringUnavailableWithoutTool(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("PATH", dir) // nothing on PATH
	keyringMu.Lock()
	keyringProbed, keyringUsable = false, false
	keyringMu.Unlock()
	t.Cleanup(func() {
		keyringMu.Lock()
		keyringProbed, keyringUsable = false, false
		keyringMu.Unlock()
	})

	if KeyringAvailable() {
		t.Fatal("empty PATH probed as available")
	}
	if err := keyringSave("x"); !errors.Is(err, ErrKeyringUnavailable) {
		t.Fatalf("save without tool = %v, want ErrKeyringUnavailable", err)
	}
}

// TestSaveSettingsMovesCookieToKeyring covers the save path: with a working
// store the file holds no cookie and the flag points at the store; loading
// reassembles the cookie from the store.
func TestSaveSettingsMovesCookieToKeyring(t *testing.T) {
	withFakeKeyring(t)
	// The environment must not inject a cookie over the file's.
	for _, k := range []string{"EHENTAI_COOKIE", "EHENTAI_COOKIE_IPB_MEMBER_ID", "EHENTAI_COOKIE_IPB_PASS_HASH", "EHENTAI_COOKIE_IGNEOUS", "EHENTAI_COOKIE_SK"} {
		t.Setenv(k, "")
	}
	dir := t.TempDir()
	s := Settings{Cookie: "ipb_member_id=7; ipb_pass_hash=abc", DatabasePath: dir}
	if err := saveSettingsTo(dir, s); err != nil {
		t.Fatal(err)
	}
	// SaveSettings uses ConfigDir; write through the same logic the app
	// uses by pointing the file at dir via saveSettingsTo after the
	// keyring branch — replicate SaveSettings' body for the temp dir.
	if KeyringAvailable() {
		if err := keyringSave(s.Cookie); err != nil {
			t.Fatal(err)
		}
		s.Cookie = ""
		s.CookieInKeyring = true
	}
	if err := saveSettingsTo(dir, s); err != nil {
		t.Fatal(err)
	}

	raw, err := os.ReadFile(filepath.Join(dir, configFileName))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(raw), "ipb_pass_hash") {
		t.Fatalf("config file still holds the cookie: %s", raw)
	}

	loaded, err := loadSettingsFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Cookie != "ipb_member_id=7; ipb_pass_hash=abc" {
		t.Fatalf("loaded cookie = %q", loaded.Cookie)
	}
}

// TestSaveSettingsKeepsCookieInFileWhenNoKeyring covers the fallback: no
// store means the cookie stays in the 0600 file exactly as before.
func TestSaveSettingsKeepsCookieInFileWhenNoKeyring(t *testing.T) {
	dir := t.TempDir()
	// Only the empty dir on PATH (no secret-tool, no security), and no
	// cookie env: the load must come from the file we wrote.
	t.Setenv("PATH", dir)
	for _, k := range []string{"EHENTAI_COOKIE", "EHENTAI_COOKIE_IPB_MEMBER_ID", "EHENTAI_COOKIE_IPB_PASS_HASH", "EHENTAI_COOKIE_IGNEOUS", "EHENTAI_COOKIE_SK"} {
		t.Setenv(k, "")
	}
	keyringMu.Lock()
	keyringProbed, keyringUsable = false, false
	keyringMu.Unlock()
	t.Cleanup(func() {
		keyringMu.Lock()
		keyringProbed, keyringUsable = false, false
		keyringMu.Unlock()
	})

	s := Settings{Cookie: "ipb_member_id=7; ipb_pass_hash=abc"}
	if err := saveSettingsTo(dir, s); err != nil {
		t.Fatal(err)
	}
	loaded, err := loadSettingsFrom(dir)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Cookie != s.Cookie || loaded.CookieInKeyring {
		t.Fatalf("loaded = %+v, want the cookie in the file", loaded)
	}
}
