package handler

import (
	"testing"
	"time"

	nodeauth "manga-reader/internal/node"
)

func TestValidateScopesAcceptsMetrics(t *testing.T) {
	scopes, ok := validateScopes([]string{nodeauth.ScopeMetricsRead})
	if !ok || len(scopes) != 1 || scopes[0] != nodeauth.ScopeMetricsRead {
		t.Fatalf("metrics scope = %v, %v; want [metrics:read], true", scopes, ok)
	}

	defaults, ok := validateScopes(nil)
	if !ok {
		t.Fatal("empty scopes should use defaults")
	}
	for _, scope := range defaults {
		if scope == nodeauth.ScopeMetricsRead {
			t.Fatal("metrics:read must stay opt-in for new keys")
		}
	}

	if _, ok := validateScopes([]string{"metrics:write"}); ok {
		t.Fatal("unknown scope should be rejected")
	}
}

func TestParseKeyExpiryDays(t *testing.T) {
	if got, ok := parseKeyExpiry(nil, nil); !ok || got != nil {
		t.Fatalf("omitted expiry = %v, %v; want nil, true", got, ok)
	}

	before := time.Now().UTC().AddDate(0, 0, 30)
	got, ok := parseKeyExpiry(nil, intPtr(30))
	after := time.Now().UTC().AddDate(0, 0, 30)
	if !ok || got == nil {
		t.Fatal("30 days should be accepted")
	}
	if got.Before(before) || got.After(after) {
		t.Fatalf("30-day expiry = %v, want between %v and %v", got, before, after)
	}

	for _, days := range []int{0, -1, maxKeyExpiryDays + 1} {
		if _, ok := parseKeyExpiry(nil, intPtr(days)); ok {
			t.Fatalf("%d days should be rejected", days)
		}
	}

	// expires_at remains supported for API clients and custom-date UIs when no
	// day count is given.
	future := time.Now().UTC().Add(time.Hour).Format(time.RFC3339)
	if got, ok := parseKeyExpiry(&future, nil); !ok || got == nil {
		t.Fatal("future expires_at should be accepted")
	}
	past := time.Now().UTC().Add(-time.Hour).Format(time.RFC3339)
	if _, ok := parseKeyExpiry(&past, nil); ok {
		t.Fatal("past expires_at should be rejected")
	}
}

func intPtr(v int) *int { return &v }
