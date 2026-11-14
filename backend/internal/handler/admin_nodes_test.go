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
	if got, ok := parseKeyExpiry(nil); !ok || got != nil {
		t.Fatalf("omitted expiry = %v, %v; want nil, true", got, ok)
	}

	before := time.Now().UTC().AddDate(0, 0, 30)
	got, ok := parseKeyExpiry(intPtr(30))
	after := time.Now().UTC().AddDate(0, 0, 30)
	if !ok || got == nil {
		t.Fatal("30 days should be accepted")
	}
	if got.Before(before) || got.After(after) {
		t.Fatalf("30-day expiry = %v, want between %v and %v", got, before, after)
	}

	for _, days := range []int{0, -1, maxKeyExpiryDays + 1} {
		if _, ok := parseKeyExpiry(intPtr(days)); ok {
			t.Fatalf("%d days should be rejected", days)
		}
	}
}

func intPtr(v int) *int { return &v }
