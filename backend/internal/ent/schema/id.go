package schema

import (
	"entgo.io/ent/dialect"

	"manga-reader/internal/ident"
)

// uuidV7 returns a time-ordered (UUIDv7) identity for primary keys and
// ownership columns.
//
// Auto-increment integer IDs are avoided in this schema because they collide
// across instances during data migration (dump/restore, merging deployments).
// A random UUID has no such collision, and UUIDv7's leading timestamp keeps
// insertion locality in the B-tree close to a sequential counter, unlike a
// fully random UUIDv4.
func uuidV7() ident.UUID {
	return ident.New()
}

// uuidNil returns the zero UUID, the "unowned" sentinel for rows that have no
// owner yet (matching the previous user_id = 0 default).
func uuidNil() ident.UUID {
	return ident.Nil()
}

// uuidSchemaType stores UUIDs as 16 raw bytes: BLOB in SQLite (half the width
// of the textual form) and bytea in PostgreSQL. Both are 16 bytes on disk, the
// same footprint as a Postgres native uuid. The stock uuid.UUID would bind via
// Value() as a 36-character string, so the custom ident.UUID type (which binds
// 16 bytes) is required to keep the columns narrow.
var uuidSchemaType = map[string]string{
	dialect.SQLite:   "blob",
	dialect.Postgres: "bytea",
}
