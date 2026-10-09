package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// resetBasicAuth makes sure an enabled configuration from the developer's
// shell never leaks into a test that is about something else.
func resetBasicAuth(t *testing.T) {
	t.Helper()
	for _, key := range []string{
		"MANGA_READER_BASIC_AUTH_ENABLED",
		"MANGA_READER_BASIC_AUTH_USERNAME",
		"MANGA_READER_BASIC_AUTH_PASSWORD",
		"MANGA_READER_BASIC_AUTH_PASSWORD_FILE",
		"MANGA_READER_BASIC_AUTH_FILE",
	} {
		t.Setenv(key, "")
	}
}

func TestBasicAuthDisabledByDefault(t *testing.T) {
	setRequiredCookies(t)
	resetBasicAuth(t)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BasicAuth.Enabled {
		t.Fatal("BasicAuth.Enabled = true, want false by default")
	}
}

func TestBasicAuthSwitchValues(t *testing.T) {
	for _, value := range []string{"false", "0", "no", "", "FALSE", "No"} {
		t.Run("off/"+value, func(t *testing.T) {
			setRequiredCookies(t)
			resetBasicAuth(t)
			t.Setenv("MANGA_READER_BASIC_AUTH_ENABLED", value)

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if cfg.BasicAuth.Enabled {
				t.Fatal("enabled, want disabled")
			}
		})
	}

	for _, value := range []string{"true", "1", "yes", "TRUE", "Yes"} {
		t.Run("on/"+value, func(t *testing.T) {
			setRequiredCookies(t)
			resetBasicAuth(t)
			t.Setenv("MANGA_READER_BASIC_AUTH_ENABLED", value)
			t.Setenv("MANGA_READER_BASIC_AUTH_USERNAME", "admin")
			t.Setenv("MANGA_READER_BASIC_AUTH_PASSWORD", "hunter2")

			cfg, err := Load()
			if err != nil {
				t.Fatalf("Load: %v", err)
			}
			if !cfg.BasicAuth.Enabled {
				t.Fatal("disabled, want enabled")
			}
			if cfg.BasicAuth.Username != "admin" || cfg.BasicAuth.Password != "hunter2" {
				t.Fatalf("credentials = %q/%q", cfg.BasicAuth.Username, cfg.BasicAuth.Password)
			}
		})
	}
}

// Fail closed: an unparsable switch value or a missing credential source must
// stop the server from starting rather than serve the app anonymously.
func TestBasicAuthFailClosed(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
	}{
		{"invalid switch value", map[string]string{
			"MANGA_READER_BASIC_AUTH_ENABLED": "maybe",
		}},
		{"enabled without credentials", map[string]string{
			"MANGA_READER_BASIC_AUTH_ENABLED": "true",
		}},
		{"enabled without username", map[string]string{
			"MANGA_READER_BASIC_AUTH_ENABLED":  "true",
			"MANGA_READER_BASIC_AUTH_PASSWORD": "hunter2",
		}},
		{"enabled without password", map[string]string{
			"MANGA_READER_BASIC_AUTH_ENABLED":       "true",
			"MANGA_READER_BASIC_AUTH_USERNAME":      "admin",
			"MANGA_READER_BASIC_AUTH_PASSWORD":      "",
			"MANGA_READER_BASIC_AUTH_PASSWORD_FILE": "",
		}},
		{"username contains a colon", map[string]string{
			"MANGA_READER_BASIC_AUTH_ENABLED":  "true",
			"MANGA_READER_BASIC_AUTH_USERNAME": "ad:min",
			"MANGA_READER_BASIC_AUTH_PASSWORD": "hunter2",
		}},
		{"htpasswd file missing", map[string]string{
			"MANGA_READER_BASIC_AUTH_ENABLED": "true",
			"MANGA_READER_BASIC_AUTH_FILE":    filepath.Join(t.TempDir(), "nope.htpasswd"),
		}},
		{"htpasswd file empty", map[string]string{
			"MANGA_READER_BASIC_AUTH_ENABLED": "true",
			"MANGA_READER_BASIC_AUTH_FILE":    writeBasicAuthFile(t, "", 0o644),
		}},
		{"htpasswd file not world-readable", map[string]string{
			"MANGA_READER_BASIC_AUTH_ENABLED": "true",
			"MANGA_READER_BASIC_AUTH_FILE":    writeBasicAuthFile(t, "admin:$apr1$x$y\n", 0o600),
		}},
		{"password file missing", map[string]string{
			"MANGA_READER_BASIC_AUTH_ENABLED":       "true",
			"MANGA_READER_BASIC_AUTH_USERNAME":      "admin",
			"MANGA_READER_BASIC_AUTH_PASSWORD_FILE": filepath.Join(t.TempDir(), "nope"),
		}},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			setRequiredCookies(t)
			resetBasicAuth(t)
			for key, value := range tc.env {
				t.Setenv(key, value)
			}

			_, err := Load()
			if err == nil {
				t.Fatal("Load succeeded, want a fail-closed error")
			}
			// Mis-pasted secrets and raw switch values must never surface.
			if v := tc.env["MANGA_READER_BASIC_AUTH_ENABLED"]; v == "maybe" && strings.Contains(err.Error(), v) {
				t.Fatalf("error leaks the raw switch value: %v", err)
			}
			if v := tc.env["MANGA_READER_BASIC_AUTH_PASSWORD"]; v != "" && strings.Contains(err.Error(), v) {
				t.Fatalf("error leaks the password: %v", err)
			}
			if v := tc.env["MANGA_READER_BASIC_AUTH_USERNAME"]; v == "ad:min" && strings.Contains(err.Error(), v) {
				t.Fatalf("error leaks the username: %v", err)
			}
		})
	}
}

func TestBasicAuthPasswordFileFirstLine(t *testing.T) {
	setRequiredCookies(t)
	resetBasicAuth(t)
	t.Setenv("MANGA_READER_BASIC_AUTH_ENABLED", "true")
	t.Setenv("MANGA_READER_BASIC_AUTH_USERNAME", "admin")
	t.Setenv("MANGA_READER_BASIC_AUTH_PASSWORD_FILE", writeBasicAuthFile(t, "line1-pw-from-file\r\nsecond line ignored\n", 0o600))

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BasicAuth.Password != "line1-pw-from-file" {
		t.Fatalf("password = %q, want the first line with CR stripped", cfg.BasicAuth.Password)
	}
}

func TestBasicAuthHtpasswdFileSource(t *testing.T) {
	setRequiredCookies(t)
	resetBasicAuth(t)
	path := writeBasicAuthFile(t, "admin:$apr1$saltsalt$r/QcFGT5pNL28bNkeDMHR.\n", 0o644)
	t.Setenv("MANGA_READER_BASIC_AUTH_ENABLED", "true")
	t.Setenv("MANGA_READER_BASIC_AUTH_FILE", path)

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if cfg.BasicAuth.File != path {
		t.Fatalf("File = %q, want %q", cfg.BasicAuth.File, path)
	}
	if cfg.BasicAuth.Username != "" || cfg.BasicAuth.Password != "" {
		t.Fatal("environment credentials must stay empty for the htpasswd source")
	}
}

func writeBasicAuthFile(t *testing.T, content string, perm os.FileMode) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "auth.htpasswd")
	if err := os.WriteFile(path, []byte(content), perm); err != nil {
		t.Fatalf("write file: %v", err)
	}
	// WriteFile applies the process umask; force the mode under test.
	if err := os.Chmod(path, perm); err != nil {
		t.Fatalf("chmod: %v", err)
	}
	return path
}
