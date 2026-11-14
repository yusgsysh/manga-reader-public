// Package ident defines the UUID type used for database identities (primary keys
// and ownership columns).
//
// It is a drop-in UUID that stores as 16 raw bytes (BLOB) in SQLite rather than
// the 36-character text form, so every user_id column and its composite indexes
// stay half as wide — the same 16-byte footprint as a Postgres native uuid.
//
// Auto-increment integer IDs are avoided because they collide across instances
// during data migration (dump/restore, merging deployments); a random UUID has
// no such collision, and New uses UUIDv7 so the leading timestamp keeps B-tree
// insertion locality close to a sequential counter.
package ident

import (
	"database/sql/driver"
	"fmt"

	"github.com/google/uuid"
)

// UUID is a 16-byte universally unique identifier. The zero value is the "nil"
// UUID, used as the unowned-row sentinel (the analogue of the old user_id = 0).
type UUID [16]byte

// New returns a time-ordered (UUIDv7) identifier.
func New() UUID {
	return UUID(uuid.Must(uuid.NewV7()))
}

// Nil is the zero UUID.
func Nil() UUID {
	return UUID{}
}

// Parse parses a UUID from its canonical (or raw-hex) textual form.
func Parse(s string) (UUID, error) {
	u, err := uuid.Parse(s)
	return UUID(u), err
}

// MustParse is Parse that panics on error (for tests and constants).
func MustParse(s string) UUID {
	u, err := uuid.Parse(s)
	if err != nil {
		panic(err)
	}
	return UUID(u)
}

// String returns the canonical 36-character form.
func (u UUID) String() string {
	return uuid.UUID(u).String()
}

// IsNil reports whether u is the zero UUID.
func (u UUID) IsNil() bool {
	return u == UUID{}
}

// Value implements driver.Valuer, storing the 16 raw bytes.
func (u UUID) Value() (driver.Value, error) {
	return u[:], nil
}

// Scan implements sql.Scanner, accepting 16 raw bytes (BLOB) or a textual UUID
// (string/[]byte of the canonical or raw-hex form).
func (u *UUID) Scan(src any) error {
	switch v := src.(type) {
	case nil:
		*u = UUID{}
	case []byte:
		if len(v) == 16 {
			copy(u[:], v)
			return nil
		}
		return u.scanText(string(v))
	case string:
		return u.scanText(v)
	default:
		return fmt.Errorf("id: cannot scan %T into UUID", src)
	}
	return nil
}

func (u *UUID) scanText(s string) error {
	if s == "" {
		*u = UUID{}
		return nil
	}
	parsed, err := uuid.Parse(s)
	if err != nil {
		return err
	}
	*u = UUID(parsed)
	return nil
}

// MarshalText implements encoding.TextMarshaler (and, transitively, JSON).
func (u UUID) MarshalText() ([]byte, error) {
	return []byte(u.String()), nil
}

// UnmarshalText implements encoding.TextUnmarshaler.
func (u *UUID) UnmarshalText(b []byte) error {
	return u.scanText(string(b))
}
