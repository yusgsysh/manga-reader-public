package authtest

import (
	"encoding/base64"
	"encoding/binary"
)

func base64URL(b []byte) string {
	return base64.RawURLEncoding.EncodeToString(b)
}

func bigEndianInt(v int) []byte {
	buf := make([]byte, 4)
	binary.BigEndian.PutUint32(buf, uint32(v))
	for len(buf) > 1 && buf[0] == 0 {
		buf = buf[1:]
	}
	return buf
}
