package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
)

// ErrInvalidToken is deliberately opaque: callers must not learn whether the
// signature, algorithm, issuer, audience or expiry failed.
var ErrInvalidToken = errors.New("invalid token")

// Verifier validates Kinde access tokens against the tenant's JWKS. go-oidc
// fetches the discovery document once, caches the signing keys, refreshes them
// when an unknown key id appears, and enforces issuer, audience, expiry and
// not-before. Signature algorithms are restricted to RS256 (the only algorithm
// Kinde publishes for access/id tokens).
type Verifier struct {
	issuer   string
	audience string
	verifier *oidc.IDTokenVerifier
}

// NewVerifier builds a verifier for the Kinde tenant. issuer is the tenant
// domain (https://<tenant>.kinde.com) and audience is the API resource
// (https://api.manga.example.com), never the React application client id.
func NewVerifier(ctx context.Context, issuer, audience string) (*Verifier, error) {
	issuer = strings.TrimRight(strings.TrimSpace(issuer), "/")
	audience = strings.TrimSpace(audience)
	if issuer == "" {
		return nil, fmt.Errorf("KINDE_ISSUER is required")
	}
	if audience == "" {
		return nil, fmt.Errorf("KINDE_API_AUDIENCE is required")
	}

	provider, err := oidc.NewProvider(ctx, issuer)
	if err != nil {
		return nil, fmt.Errorf("discover Kinde OIDC provider: %w", err)
	}
	if provider.Endpoint().AuthURL == "" {
		return nil, fmt.Errorf("kinde discovery document is incomplete")
	}

	return &Verifier{
		issuer:   issuer,
		audience: audience,
		verifier: provider.Verifier(&oidc.Config{
			ClientID:             audience,
			SupportedSigningAlgs: []string{oidc.RS256},
		}),
	}, nil
}

// Issuer returns the tenant issuer the verifier was built for.
func (v *Verifier) Issuer() string { return v.issuer }

// Audience returns the API audience tokens must be minted for.
func (v *Verifier) Audience() string { return v.audience }

// Verify parses and validates a raw Kinde access token. The returned claims
// have already passed signature, algorithm, issuer, audience, expiry and
// not-before checks. Errors are intentionally collapsed to ErrInvalidToken.
func (v *Verifier) Verify(ctx context.Context, raw string) (*Claims, error) {
	if v == nil || v.verifier == nil || strings.TrimSpace(raw) == "" {
		return nil, ErrInvalidToken
	}
	token, err := v.verifier.Verify(ctx, raw)
	if err != nil {
		return nil, ErrInvalidToken
	}
	if token.Subject == "" {
		return nil, ErrInvalidToken
	}

	var payload tokenClaims
	if err := token.Claims(&payload); err != nil {
		return nil, ErrInvalidToken
	}
	if payload.Email != "" && !payload.EmailVerified {
		// Keep the address for display, but only a verified address may ever
		// participate in the administrator allowlist.
		payload.EmailVerified = false
	}

	name := strings.TrimSpace(payload.Name)
	if name == "" {
		name = strings.TrimSpace(strings.TrimSpace(payload.GivenName) + " " + strings.TrimSpace(payload.FamilyName))
	}

	return &Claims{
		Subject:       token.Subject,
		Email:         strings.TrimSpace(payload.Email),
		EmailVerified: payload.EmailVerified,
		Name:          name,
		AvatarURL:     strings.TrimSpace(payload.Picture),
		Roles:         mergeClaimList(payload.Roles, payload.HasuraRoles),
		Permissions:   mergeClaimList(payload.Permissions, payload.HasuraPermissions),
		RolesClaim:    hasClaim(payload.Roles) || hasClaim(payload.HasuraRoles),
		ExpiresAt:     token.Expiry,
		IssuedAt:      token.IssuedAt,
	}, nil
}

// roleClaim is one role object as Kinde may serialize it in a customized
// access token ({"key":"admin","name":"Admin"}).
type roleClaim struct {
	Key  string `json:"key"`
	Name string `json:"name"`
}

// claimList accepts the shapes used by Kinde/Hasura token customization:
// ["admin"], [{"key":"admin"}], or a single string.
func claimList(raw json.RawMessage) []string {
	if len(raw) == 0 || string(raw) == "null" {
		return nil
	}
	var stringsList []string
	if err := json.Unmarshal(raw, &stringsList); err == nil {
		return cleanList(stringsList)
	}
	var objects []roleClaim
	if err := json.Unmarshal(raw, &objects); err == nil {
		out := make([]string, 0, len(objects))
		for _, object := range objects {
			if object.Key != "" {
				out = append(out, object.Key)
			} else if object.Name != "" {
				out = append(out, object.Name)
			}
		}
		return cleanList(out)
	}
	var single string
	if err := json.Unmarshal(raw, &single); err == nil {
		return cleanList([]string{single})
	}
	return nil
}

func hasClaim(raw json.RawMessage) bool {
	return len(raw) > 0 && string(raw) != "null"
}

func mergeClaimList(groups ...json.RawMessage) []string {
	seen := make(map[string]struct{})
	var out []string
	for _, raw := range groups {
		for _, value := range claimList(raw) {
			key := strings.ToLower(value)
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			out = append(out, value)
		}
	}
	return out
}

func cleanList(values []string) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		if value = strings.TrimSpace(value); value != "" {
			out = append(out, value)
		}
	}
	return out
}

// tokenClaims is the subset of Kinde access-token claims we trust after
// verification. Any of the profile fields may be absent; only "sub" is
// guaranteed by OIDC.
type tokenClaims struct {
	Email             string          `json:"email"`
	EmailVerified     bool            `json:"email_verified"`
	Name              string          `json:"name"`
	GivenName         string          `json:"given_name"`
	FamilyName        string          `json:"family_name"`
	Picture           string          `json:"picture"`
	Roles             json.RawMessage `json:"roles"`
	HasuraRoles       json.RawMessage `json:"x-hasura-roles"`
	Permissions       json.RawMessage `json:"permissions"`
	HasuraPermissions json.RawMessage `json:"x-hasura-permissions"`
}

// Claims is the verified identity extracted from a Kinde access token.
type Claims struct {
	Subject       string
	Email         string
	EmailVerified bool
	Name          string
	AvatarURL     string
	// Roles and Permissions come from the token customization configured in
	// Kinde. They are empty when the application has not added those claims.
	Roles       []string
	Permissions []string
	// RolesClaim is true when the access token carried roles (even an empty
	// array). It distinguishes "RBAC not configured" from "configured but no
	// roles", which must be denied.
	RolesClaim bool
	ExpiresAt  time.Time
	IssuedAt   time.Time
}
