package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/crypto/argon2"
)

// Argon2id parameters. They follow OWASP's minimum recommendation for
// interactive logins (64 MiB, 3 iterations, 4 lanes). The parameters are
// embedded in the PHC string so hashes stay verifiable if these defaults are
// raised later.
const (
	argonMemory      = 64 * 1024 // KiB
	argonIterations  = 3
	argonParallelism = 4
	argonSaltLength  = 16
	argonKeyLength   = 32
)

// HashPassword returns a PHC-encoded Argon2id hash, e.g.
// $argon2id$v=19$m=65536,t=3,p=4$<salt>$<hash>.
func HashPassword(password string) (string, error) {
	salt := make([]byte, argonSaltLength)
	if _, err := rand.Read(salt); err != nil {
		return "", fmt.Errorf("generate password salt: %w", err)
	}
	key := argon2.IDKey([]byte(password), salt, argonIterations, argonMemory, argonParallelism, argonKeyLength)
	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		argonMemory,
		argonIterations,
		argonParallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// VerifyPassword reports whether password matches the PHC-encoded Argon2id
// hash. Malformed or unsupported hashes never match (fail closed).
func VerifyPassword(encoded, password string) bool {
	params, salt, want, ok := parseArgon2id(encoded)
	if !ok {
		return false
	}
	got := argon2.IDKey(
		[]byte(password),
		salt,
		params.iterations,
		params.memory,
		params.parallelism,
		uint32(len(want)),
	)
	return subtle.ConstantTimeCompare(got, want) == 1
}

type argon2Params struct {
	memory      uint32
	iterations  uint32
	parallelism uint8
}

func parseArgon2id(encoded string) (argon2Params, []byte, []byte, bool) {
	parts := strings.Split(encoded, "$")
	// ["", "argon2id", "v=19", "m=...,t=...,p=...", salt, hash]
	if len(parts) != 6 || parts[1] != "argon2id" {
		return argon2Params{}, nil, nil, false
	}
	version, err := strconv.Atoi(strings.TrimPrefix(parts[2], "v="))
	if err != nil || version != argon2.Version {
		return argon2Params{}, nil, nil, false
	}

	var params argon2Params
	for _, field := range strings.Split(parts[3], ",") {
		key, value, ok := strings.Cut(field, "=")
		if !ok {
			return argon2Params{}, nil, nil, false
		}
		n, err := strconv.ParseUint(value, 10, 32)
		if err != nil {
			return argon2Params{}, nil, nil, false
		}
		switch key {
		case "m":
			params.memory = uint32(n)
		case "t":
			params.iterations = uint32(n)
		case "p":
			if n == 0 || n > 255 {
				return argon2Params{}, nil, nil, false
			}
			params.parallelism = uint8(n)
		default:
			return argon2Params{}, nil, nil, false
		}
	}
	// Bound work derived from a stored hash so a corrupted row cannot make
	// verification allocate unbounded memory.
	if params.memory == 0 || params.memory > 1024*1024 {
		return argon2Params{}, nil, nil, false
	}
	if params.iterations == 0 || params.iterations > 32 {
		return argon2Params{}, nil, nil, false
	}
	if params.parallelism == 0 {
		return argon2Params{}, nil, nil, false
	}

	salt, err := base64.RawStdEncoding.DecodeString(parts[4])
	if err != nil || len(salt) < 8 {
		return argon2Params{}, nil, nil, false
	}
	hash, err := base64.RawStdEncoding.DecodeString(parts[5])
	if err != nil || len(hash) == 0 || len(hash) > 128 {
		return argon2Params{}, nil, nil, false
	}
	return params, salt, hash, true
}
