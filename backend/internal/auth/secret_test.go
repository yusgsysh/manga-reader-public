package auth

import (
	"strings"
	"testing"

	"manga-reader/internal/ent/appsetting"
)

func TestResolveSecret_EnvWins(t *testing.T) {
	client := newTestClient(t)
	const env = "0123456789abcdef0123456789abcdef"

	secret, generated, err := ResolveSecret(t.Context(), client, env)
	if err != nil {
		t.Fatalf("ResolveSecret: %v", err)
	}
	if secret != env {
		t.Fatalf("secret = %q, want the environment value", secret)
	}
	if generated {
		t.Fatal("generated = true, want false when JWT_SECRET is set")
	}
	// The environment value must not be written to the database.
	n, err := client.AppSetting.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count app settings: %v", err)
	}
	if n != 0 {
		t.Fatalf("app_setting rows = %d, want 0 when env secret is set", n)
	}
}

func TestResolveSecret_GeneratesOnceAndPersists(t *testing.T) {
	client := newTestClient(t)

	first, generated, err := ResolveSecret(t.Context(), client, "")
	if err != nil {
		t.Fatalf("ResolveSecret (first): %v", err)
	}
	if !generated {
		t.Fatal("generated = false on first boot, want true")
	}
	if len(first) < 32 {
		t.Fatalf("generated secret too short: %d bytes", len(first))
	}

	// The secret is persisted under the expected key.
	row, err := client.AppSetting.Get(t.Context(), jwtSecretKey)
	if err != nil {
		t.Fatalf("read persisted secret: %v", err)
	}
	if row.Value != first {
		t.Fatalf("stored value = %q, want %q", row.Value, first)
	}

	// A subsequent boot (fresh call, same DB) loads the same secret without
	// regenerating it, so sessions survive restarts.
	second, generated, err := ResolveSecret(t.Context(), client, "")
	if err != nil {
		t.Fatalf("ResolveSecret (second): %v", err)
	}
	if generated {
		t.Fatal("generated = true on second boot, want false")
	}
	if second != first {
		t.Fatalf("second secret = %q, want %q (must not change across restarts)", second, first)
	}
	if _, err := client.AppSetting.Query().Where(appsetting.ID(jwtSecretKey)).Only(t.Context()); err != nil {
		t.Fatalf("secret row missing after reload: %v", err)
	}
}

func TestResolveSecret_RequiresDatabase(t *testing.T) {
	_, _, err := ResolveSecret(t.Context(), nil, "")
	if err == nil {
		t.Fatal("expected an error when JWT_SECRET is unset and no database is available")
	}
	if !strings.Contains(err.Error(), "database") {
		t.Fatalf("error = %q, want a database-related message", err)
	}
}
