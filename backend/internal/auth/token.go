package auth

import (
	"errors"
	"fmt"
	"strconv"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"

	"manga-reader/internal/config"
)

// ErrInvalidToken is deliberately opaque: callers must not learn whether the
// signature, algorithm, issuer or expiry failed.
var ErrInvalidToken = errors.New("invalid token")

// Claims is the JWT payload. RegisteredClaims carries iss/sub/iat/exp/jti;
// username and ver make /me readable without a database lookup and let logout
// revoke every outstanding token by bumping users.token_version.
type Claims struct {
	Username     string `json:"username"`
	TenantID     int64  `json:"tenant_id"`
	Role         string `json:"role"`
	TokenVersion int    `json:"ver"`
	jwt.RegisteredClaims
}

type tokenManager struct {
	secret []byte
	issuer string
	ttl    time.Duration
	now    func() time.Time
}

func newTokenManager(secret, issuer string, ttl time.Duration) *tokenManager {
	return &tokenManager{
		secret: []byte(secret),
		issuer: issuer,
		ttl:    ttl,
		now:    time.Now,
	}
}

func (m *tokenManager) issue(userID int, tenantID int64, username, role string, tokenVersion int) (string, time.Time, error) {
	now := m.now()
	expires := now.Add(m.ttl)
	claims := Claims{
		Username:     username,
		TenantID:     tenantID,
		Role:         role,
		TokenVersion: tokenVersion,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    m.issuer,
			Subject:   strconv.Itoa(userID),
			IssuedAt:  jwt.NewNumericDate(now),
			NotBefore: jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(expires),
			ID:        uuid.NewString(),
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(m.secret)
	if err != nil {
		return "", time.Time{}, fmt.Errorf("sign token: %w", err)
	}
	return signed, expires, nil
}

func (m *tokenManager) parse(raw string) (*Claims, error) {
	if raw == "" {
		return nil, ErrInvalidToken
	}
	claims := new(Claims)
	token, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(token *jwt.Token) (any, error) {
			// Defend against algorithm confusion ("none", RS256 with the
			// HMAC secret as public key, ...) before returning the key.
			if token.Method != jwt.SigningMethodHS256 {
				return nil, ErrInvalidToken
			}
			return m.secret, nil
		},
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(m.issuer),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)
	if err != nil || !token.Valid {
		return nil, ErrInvalidToken
	}
	if claims.Subject == "" || claims.Username == "" || claims.TenantID <= 0 {
		return nil, ErrInvalidToken
	}
	switch claims.Role {
	case config.RolePlatformAdmin, config.RoleTenantAdmin, config.RoleUser:
	default:
		return nil, ErrInvalidToken
	}
	if claims.IssuedAt == nil || claims.ExpiresAt == nil {
		return nil, ErrInvalidToken
	}
	if !claims.ExpiresAt.After(claims.IssuedAt.Time) {
		return nil, ErrInvalidToken
	}
	if _, err := strconv.Atoi(claims.Subject); err != nil {
		return nil, ErrInvalidToken
	}
	if claims.TokenVersion < 0 {
		return nil, ErrInvalidToken
	}
	return claims, nil
}
