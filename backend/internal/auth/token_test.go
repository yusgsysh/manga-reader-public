package auth

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"manga-reader/internal/ident"

	"manga-reader/internal/config"
)

// testID returns a deterministic UUID whose last byte is n.
func testID(n byte) ident.UUID {
	var id ident.UUID
	id[15] = n
	return id
}

func TestTokenManagerRoundTrip(t *testing.T) {
	m := newTokenManager(strings.Repeat("s", 32), "manga-reader-test", time.Hour)
	fixed := time.Now().UTC()
	m.now = func() time.Time { return fixed }

	raw, expires, err := m.issue(testID(7), "alice", config.RoleAdmin, 3)
	if err != nil {
		t.Fatalf("issue: %v", err)
	}
	if !expires.Equal(fixed.Add(time.Hour)) {
		t.Fatalf("expires = %v, want %v", expires, fixed.Add(time.Hour))
	}

	claims, err := m.parse(raw)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if claims.Subject != testID(7).String() || claims.UserID != testID(7) || claims.Username != "alice" ||
		claims.Role != config.RoleAdmin || claims.TokenVersion != 3 {
		t.Fatalf("claims = %+v", claims)
	}
	if claims.Issuer != "manga-reader-test" || claims.ID == "" {
		t.Fatalf("registered claims = %+v", claims.RegisteredClaims)
	}
}

// TestTokenManagerRejectsInvalidClaims exercises every claim check that turns
// a syntactically valid signature into a rejected credential. These are the
// boundaries that keep a forged or downgraded token out of the API.
func TestTokenManagerRejectsInvalidClaims(t *testing.T) {
	const secret = "0123456789abcdef0123456789abcdef"
	m := newTokenManager(secret, "manga-reader-test", time.Hour)
	now := time.Now().UTC()
	m.now = func() time.Time { return now }

	base := func() Claims {
		return Claims{
			UserID:       testID(1),
			Username:     "alice",
			Role:         config.RoleUser,
			TokenVersion: 0,
			RegisteredClaims: jwt.RegisteredClaims{
				Issuer:    "manga-reader-test",
				Subject:   testID(1).String(),
				IssuedAt:  jwt.NewNumericDate(now.Add(-time.Minute)),
				NotBefore: jwt.NewNumericDate(now.Add(-time.Minute)),
				ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
				ID:        "jti",
			},
		}
	}
	sign := func(claims Claims) string {
		t.Helper()
		raw, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
		if err != nil {
			t.Fatalf("sign: %v", err)
		}
		return raw
	}

	tests := []struct {
		name string
		raw  string
	}{
		{"empty", ""},
		{"garbage", "not-a-jwt"},
		{
			name: "expired",
			raw: sign(func() Claims {
				c := base()
				c.IssuedAt = jwt.NewNumericDate(now.Add(-2 * time.Hour))
				c.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Hour))
				return c
			}()),
		},
		{
			name: "expiry not after issue",
			raw: sign(func() Claims {
				c := base()
				c.ExpiresAt = jwt.NewNumericDate(c.IssuedAt.Time)
				return c
			}()),
		},
		{
			name: "missing expiry",
			raw: sign(func() Claims {
				c := base()
				c.ExpiresAt = nil
				return c
			}()),
		},
		{
			name: "wrong issuer",
			raw: sign(func() Claims {
				c := base()
				c.Issuer = "someone-else"
				return c
			}()),
		},
		{
			name: "missing issuer",
			raw: sign(func() Claims {
				c := base()
				c.Issuer = ""
				return c
			}()),
		},
		{
			name: "missing subject",
			raw: sign(func() Claims {
				c := base()
				c.Subject = ""
				return c
			}()),
		},
		{
			name: "invalid subject",
			raw: sign(func() Claims {
				c := base()
				c.Subject = "admin"
				return c
			}()),
		},
		{
			name: "missing username",
			raw: sign(func() Claims {
				c := base()
				c.Username = ""
				return c
			}()),
		},
		{
			name: "zero user",
			raw: sign(func() Claims {
				c := base()
				c.UserID = ident.Nil()
				return c
			}()),
		},
		{
			name: "subject does not match user",
			raw: sign(func() Claims {
				c := base()
				c.Subject = testID(2).String()
				return c
			}()),
		},
		{
			name: "unknown role",
			raw: sign(func() Claims {
				c := base()
				c.Role = "superuser"
				return c
			}()),
		},
		{
			name: "missing role",
			raw: sign(func() Claims {
				c := base()
				c.Role = ""
				return c
			}()),
		},
		{
			name: "negative token version",
			raw: sign(func() Claims {
				c := base()
				c.TokenVersion = -1
				return c
			}()),
		},
		{
			name: "wrong signing algorithm",
			raw: func() string {
				raw, err := jwt.NewWithClaims(jwt.SigningMethodHS384, base()).SignedString([]byte(secret))
				if err != nil {
					t.Fatalf("sign hs384: %v", err)
				}
				return raw
			}(),
		},
		{
			name: "alg none",
			raw: func() string {
				raw, err := jwt.NewWithClaims(jwt.SigningMethodNone, base()).
					SignedString(jwt.UnsafeAllowNoneSignatureType)
				if err != nil {
					t.Fatalf("sign none: %v", err)
				}
				return raw
			}(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := m.parse(tt.raw); !errors.Is(err, ErrInvalidToken) {
				t.Fatalf("parse error = %v, want ErrInvalidToken", err)
			}
		})
	}

	// The same manager must still accept a correctly signed token, so the
	// rejection table is not passing because everything fails.
	if _, err := m.parse(sign(base())); err != nil {
		t.Fatalf("valid token rejected: %v", err)
	}
}
