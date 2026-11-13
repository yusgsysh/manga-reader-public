package node

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
)

// APIKeyPrefix makes leaked keys recognisable in logs/scanners without
// revealing the secret.
const APIKeyPrefix = "mrk_"

// KeyMaterial is the one-time result of minting a node API key. Plaintext must
// never be persisted; only Hash and Prefix belong in the database.
type KeyMaterial struct {
	Plaintext string
	Hash      string
	Prefix    string
}

// GenerateKey creates at least 256 bits of randomness. The plaintext is
// returned exactly once to the administrator, while the database stores only
// its SHA-256 hash and a short display prefix.
func GenerateKey() (KeyMaterial, error) {
	raw := make([]byte, 32)
	if _, err := rand.Read(raw); err != nil {
		return KeyMaterial{}, fmt.Errorf("generate node api key: %w", err)
	}
	plaintext := APIKeyPrefix + base64.RawURLEncoding.EncodeToString(raw)
	return KeyMaterial{
		Plaintext: plaintext,
		Hash:      HashKey(plaintext),
		Prefix:    DisplayPrefix(plaintext),
	}, nil
}

// HashKey is the stable server-side representation of a node key.
func HashKey(plaintext string) string {
	sum := sha256.Sum256([]byte(plaintext))
	return hex.EncodeToString(sum[:])
}

// DisplayPrefix returns a short non-secret identifier for the UI.
func DisplayPrefix(plaintext string) string {
	const n = 12
	if len(plaintext) <= n {
		return plaintext
	}
	return plaintext[:n]
}
