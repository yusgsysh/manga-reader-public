package basicauth

import (
	"crypto/md5"
	"crypto/subtle"
	"strings"

	"golang.org/x/crypto/bcrypt"
)

// htpasswd hash prefixes understood by verifyHash.
const (
	prefixApr1   = "$apr1$" // Apache htpasswd -m (apr1)
	prefixMD5    = "$1$"    // md5crypt (openssl passwd -1)
	prefixBCrypt = "$2"     // $2a$ / $2b$ / $2y$ (htpasswd -B)
)

// verifyHash compares a plaintext password with a single htpasswd hash.
// Unsupported schemes ($5$, $6$, plaintext) never match — an operator who
// points MANGA_READER_BASIC_AUTH_FILE at an unsupported file gets locked out
// instead of a silently weaker check.
func verifyHash(encoded, password string) bool {
	switch {
	case strings.HasPrefix(encoded, prefixApr1), strings.HasPrefix(encoded, prefixMD5):
		prefix, salt, sum, ok := splitMD5Hash(encoded)
		if !ok {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(md5Crypt(password, salt, prefix)), []byte(sum)) == 1
	case strings.HasPrefix(encoded, prefixBCrypt):
		return bcrypt.CompareHashAndPassword([]byte(encoded), []byte(password)) == nil
	default:
		return false
	}
}

// splitMD5Hash parses "$apr1$salt$sum" / "$1$salt$sum".
func splitMD5Hash(encoded string) (prefix, salt, sum string, ok bool) {
	for _, p := range []string{prefixApr1, prefixMD5} {
		if !strings.HasPrefix(encoded, p) {
			continue
		}
		rest := strings.TrimPrefix(encoded, p)
		salt, sum, ok = strings.Cut(rest, "$")
		if !ok || sum == "" {
			return "", "", "", false
		}
		return p, salt, sum, true
	}
	return "", "", "", false
}

// md5Crypt implements the md5-crypt / apr1 digest and returns the 22-char
// crypt() checksum for the given password, salt and magic prefix. Salt is
// truncated to the 8 characters crypt() actually uses.
//
// Known-answer tests pin it to `openssl passwd -1` / `-apr1` output, which is
// the same digest htpasswd writes into MANGA_READER_BASIC_AUTH_FILE files.
func md5Crypt(password, salt, prefix string) string {
	if len(salt) > 8 {
		salt = salt[:8]
	}

	// Digest of password+salt+password; its bytes are spliced into the
	// initial message below.
	alt := md5.Sum([]byte(password + salt + password))

	msg := make([]byte, 0, len(password)+len(prefix)+len(salt)+16+1)
	msg = append(msg, password...)
	msg = append(msg, prefix...)
	msg = append(msg, salt...)
	for i := len(password); i > 0; i -= 16 {
		n := i
		if n > 16 {
			n = 16
		}
		msg = append(msg, alt[:n]...)
	}
	for i := len(password); i > 0; i >>= 1 {
		if i&1 == 1 {
			msg = append(msg, 0)
		} else {
			msg = append(msg, password[0])
		}
	}
	sum := md5.Sum(msg)

	for i := 0; i < 1000; i++ {
		block := make([]byte, 0, len(password)*2+len(salt)+16)
		if i&1 == 1 {
			block = append(block, password...)
		} else {
			block = append(block, sum[:]...)
		}
		if i%3 != 0 {
			block = append(block, salt...)
		}
		if i%7 != 0 {
			block = append(block, password...)
		}
		if i&1 == 1 {
			block = append(block, sum[:]...)
		} else {
			block = append(block, password...)
		}
		sum = md5.Sum(block)
	}

	return encodeMD5(sum)
}

const itoa64 = "./0123456789ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz"

// encodeMD5 folds a 16-byte digest into crypt()'s 22-character base64
// (6-bit groups, custom alphabet) with the index permutation md5-crypt uses.
func encodeMD5(sum [md5.Size]byte) string {
	var out [22]byte
	i := 0
	fill := func(bytes ...byte) {
		v := uint(bytes[0])<<16 | uint(bytes[1])<<8 | uint(bytes[2])
		for j := 0; j < 4 && i < len(out); j++ {
			out[i] = itoa64[v&0x3f]
			v >>= 6
			i++
		}
	}
	fill(sum[0], sum[6], sum[12])
	fill(sum[1], sum[7], sum[13])
	fill(sum[2], sum[8], sum[14])
	fill(sum[3], sum[9], sum[15])
	fill(sum[4], sum[10], sum[5])
	fill(0, 0, sum[11])
	return string(out[:])
}
