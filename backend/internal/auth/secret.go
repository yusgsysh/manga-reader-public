package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"

	"manga-reader/internal/ent"
	"manga-reader/internal/ent/appsetting"
)

// jwtSecretKey is the app_setting key under which the generated JWT secret is
// persisted when JWT_SECRET is not set in the environment.
const jwtSecretKey = "auth.jwt_secret"

// ResolveSecret returns the JWT secret to use for this process.
//
// An explicit JWT_SECRET always wins (callers validate its length before
// reaching here). Otherwise the secret is read from the app_setting table; on
// the very first boot a fresh 32-byte secret is generated and persisted so it
// survives restarts. The returned generated flag is true only when this call
// minted and stored a new secret.
func ResolveSecret(ctx context.Context, db *ent.Client, envSecret string) (string, bool, error) {
	if envSecret != "" {
		return envSecret, false, nil
	}
	if db == nil {
		return "", false, errors.New("auth database is nil")
	}

	row, err := db.AppSetting.Query().
		Where(appsetting.ID(jwtSecretKey)).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return "", false, fmt.Errorf("read JWT secret from database: %w", err)
	}
	if row != nil && row.Value != "" {
		return row.Value, false, nil
	}

	secret, err := generateSecret()
	if err != nil {
		return "", false, fmt.Errorf("generate JWT secret: %w", err)
	}
	if row == nil {
		if _, err := db.AppSetting.Create().
			SetID(jwtSecretKey).
			SetValue(secret).
			Save(ctx); err != nil {
			return "", false, fmt.Errorf("persist JWT secret: %w", err)
		}
		return secret, true, nil
	}
	if err := db.AppSetting.Update().
		Where(appsetting.ID(jwtSecretKey)).
		SetValue(secret).
		Exec(ctx); err != nil {
		return "", false, fmt.Errorf("persist JWT secret: %w", err)
	}
	return secret, true, nil
}

// generateSecret returns 32 cryptographically random bytes, hex-encoded.
func generateSecret() (string, error) {
	buf := make([]byte, 32)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}
