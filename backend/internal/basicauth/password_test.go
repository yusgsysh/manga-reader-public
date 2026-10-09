package basicauth

import (
	"strings"
	"testing"

	"golang.org/x/crypto/bcrypt"
)

// Known-answer vectors. The $1$ / $apr1$ entries were produced with
//
//	openssl passwd -1  -salt <salt> <password>
//	openssl passwd -apr1 -salt <salt> <password>
//
// and the mickey5/alexandrew ones double as a cross-check against an
// independent apr1 implementation's published test data.
func TestMD5CryptKnownAnswers(t *testing.T) {
	cases := []struct {
		password string
		salt     string
		prefix   string
		want     string
	}{
		{"p@ss word", "abc123", "$1$", "$1$abc123$kORtjaAASMbhKo9Nqtn2X1"},
		{"p@ss word", "abc123", "$apr1$", "$apr1$abc123$C1W9wEtCUokt5AGXtRc8C."},
		{"hello", "s4lt", "$1$", "$1$s4lt$AzrhOMAak4o8PLtemaUSL0"},
		{"secret", "", "$apr1$", "$apr1$$g/UEjvgvl9nWOQl6rNKvZ."},
		{"x", "ab", "$1$", "$1$ab$e2KlfqG5YBMTjSz7XF.Eu1"},
		// Longer than 16 chars: exercises the alternate-digest chunking.
		{"a-long-password-beyond-sixteen-chars", "SALT8", "$1$", "$1$SALT8$5rUx20QwZbQxUimN0zCLQ0"},
		{"a-long-password-beyond-sixteen-chars", "SALT8", "$apr1$", "$apr1$SALT8$QqvVkongnCCEji0CrNnGN."},
		{"mickey5", "D89ubl/e", "$1$", "$1$D89ubl/e$dJ8XW4DfrJHTrnwCdx3Ji1"},
		{"alexandrew", "D89ubl/e", "$1$", "$1$D89ubl/e$xuQ74IxhM3J10sv0QHVgA/"},
	}

	for _, tc := range cases {
		got := tc.prefix + tc.salt + "$" + md5Crypt(tc.password, tc.salt, tc.prefix)
		if got != tc.want {
			t.Errorf("md5Crypt(%q, %q, %q) = %q, want %q", tc.password, tc.salt, tc.prefix, got, tc.want)
		}
	}
}

// crypt() only ever uses the first 8 salt characters, so a longer salt must
// hash (and compare) as its truncated form.
func TestMD5CryptTruncatesSaltToEightChars(t *testing.T) {
	short := md5Crypt("pw", "12345678", "$1$")
	long := md5Crypt("pw", "12345678extra", "$1$")
	if short != long {
		t.Fatalf("salt longer than 8 chars changed the digest: %q vs %q", short, long)
	}
}

func TestVerifyHash(t *testing.T) {
	vectors := []struct {
		hash string
		pass string
	}{
		{"$1$abc123$kORtjaAASMbhKo9Nqtn2X1", "p@ss word"},
		{"$apr1$abc123$C1W9wEtCUokt5AGXtRc8C.", "p@ss word"},
		{"$apr1$D89ubl/e$ly82FLUqn.6pAmf5K9.LF.", "mickey5"},
	}
	for _, v := range vectors {
		if !verifyHash(v.hash, v.pass) {
			t.Errorf("verifyHash(%q) = false for the correct password", v.hash)
		}
		if verifyHash(v.hash, v.pass+"x") {
			t.Errorf("verifyHash(%q) = true for a wrong password", v.hash)
		}
	}
}

func TestVerifyHashBcrypt(t *testing.T) {
	hash, err := bcrypt.GenerateFromPassword([]byte("s3cret-pass"), bcrypt.MinCost)
	if err != nil {
		t.Fatalf("bcrypt.GenerateFromPassword: %v", err)
	}
	if !verifyHash(string(hash), "s3cret-pass") {
		t.Error("verifyHash = false for the correct bcrypt password")
	}
	if verifyHash(string(hash), "wrong") {
		t.Error("verifyHash = true for a wrong bcrypt password")
	}

	// htpasswd -B writes $2y$, which must verify too.
	twoY := "$2y$" + strings.TrimPrefix(string(hash), "$2a$")
	if !verifyHash(twoY, "s3cret-pass") {
		t.Error("verifyHash = false for a $2y$ bcrypt hash")
	}
}

func TestVerifyHashRejectsUnknownOrMalformedSchemes(t *testing.T) {
	cases := []struct {
		name string
		hash string
	}{
		{"sha512-crypt unsupported", "$6$salt$0000000000000000000000000000000000000000000000000000000000000000"},
		{"sha256-crypt unsupported", "$5$salt$00000000000000000000000000000000"},
		{"empty", ""},
		{"apr1 without a hash part", "$apr1$onlysalt"},
		{"md5 without a hash part", "$1$onlysalt$"},
		{"not a hash at all", "hunter2"},
	}
	for _, tc := range cases {
		if verifyHash(tc.hash, "anything") {
			t.Errorf("%s: verifyHash = true, want false", tc.name)
		}
	}
}
