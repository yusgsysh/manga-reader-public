// Package authtest provides a local OIDC discovery + JWKS server and token
// signer for tests. It never talks to Kinde and is not used by the server.
package authtest

import (
	"crypto/rand"
	"crypto/rsa"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

// Issuer is an in-process OIDC provider with one RSA signing key.
type Issuer struct {
	URL      string
	KeyID    string
	Key      *rsa.PrivateKey
	audience string
	server   *httptest.Server
}

// NewIssuer starts a discovery + JWKS server. The audience is embedded in the
// discovery document's issuer-independent metadata only for convenience.
func NewIssuer(t *testing.T, audience string) *Issuer {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatalf("generate RSA key: %v", err)
	}
	iss := &Issuer{KeyID: "test-key-1", Key: key, audience: audience}

	mux := http.NewServeMux()
	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                iss.URL,
			"authorization_endpoint":                iss.URL + "/oauth2/auth",
			"token_endpoint":                        iss.URL + "/oauth2/token",
			"jwks_uri":                              iss.URL + "/.well-known/jwks.json",
			"id_token_signing_alg_values_supported": []string{"RS256"},
			"response_types_supported":              []string{"code"},
			"subject_types_supported":               []string{"public"},
		})
	})
	mux.HandleFunc("/.well-known/jwks.json", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"keys": []map[string]any{{
				"kty": "RSA",
				"alg": "RS256",
				"use": "sig",
				"kid": iss.KeyID,
				"n":   base64URL(key.N.Bytes()),
				"e":   base64URL(bigEndianInt(key.E)),
			}},
		})
	})
	iss.server = httptest.NewServer(mux)
	iss.URL = iss.server.URL
	t.Cleanup(iss.server.Close)
	return iss
}

// Claims configures one signed token. Zero values get sensible defaults.
type Claims struct {
	Subject       string
	Issuer        string
	Audience      string
	KeyID         string
	Key           *rsa.PrivateKey
	ExpiresAt     time.Time
	NotBefore     time.Time
	IssuedAt      time.Time
	Email         string
	EmailVerified bool
	Name          string
	Picture       string
	Roles         []string
	Permissions   []string
	RolesClaim    bool
	// Method overrides the signing method (used for negative tests).
	Method jwt.SigningMethod
	// NoExpiry omits exp entirely.
	NoExpiry bool
}

// Sign returns a signed JWT for the configured issuer.
func (i *Issuer) Sign(t *testing.T, c Claims) string {
	t.Helper()
	if c.Subject == "" {
		c.Subject = "kp_test_subject"
	}
	if c.Issuer == "" {
		c.Issuer = i.URL
	}
	if c.Audience == "" {
		c.Audience = i.audience
	}
	if c.KeyID == "" {
		c.KeyID = i.KeyID
	}
	if c.Key == nil {
		c.Key = i.Key
	}
	if c.IssuedAt.IsZero() {
		c.IssuedAt = time.Now()
	}
	if c.ExpiresAt.IsZero() && !c.NoExpiry {
		c.ExpiresAt = time.Now().Add(time.Hour)
	}
	method := c.Method
	if method == nil {
		method = jwt.SigningMethodRS256
	}

	claims := jwt.MapClaims{
		"sub": c.Subject,
		"iss": c.Issuer,
		"aud": c.Audience,
		"iat": c.IssuedAt.Unix(),
	}
	if !c.NoExpiry {
		claims["exp"] = c.ExpiresAt.Unix()
	}
	if !c.NotBefore.IsZero() {
		claims["nbf"] = c.NotBefore.Unix()
	}
	if c.Email != "" {
		claims["email"] = c.Email
		claims["email_verified"] = c.EmailVerified
	}
	if c.Name != "" {
		claims["name"] = c.Name
	}
	if c.Picture != "" {
		claims["picture"] = c.Picture
	}
	if c.RolesClaim || len(c.Roles) > 0 {
		claims["roles"] = c.Roles
	}
	if len(c.Permissions) > 0 {
		claims["permissions"] = c.Permissions
	}

	token := jwt.NewWithClaims(method, claims)
	token.Header["kid"] = c.KeyID
	var signed string
	var err error
	if method == jwt.SigningMethodNone {
		signed, err = token.SignedString(jwt.UnsafeAllowNoneSignatureType)
	} else {
		signed, err = token.SignedString(c.Key)
	}
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return signed
}
