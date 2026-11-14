package schema

import (
	"time"

	"manga-reader/internal/ident"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SyncChange is the sync outbox: one row per local mutation of a synced
// entity (reading_progress or bookshelf), appended by sync.Record.
//
// The auto-increment "id" is deliberately retained here: it doubles as the
// replication cursor (peers pull with "id > cursor"), which needs a strictly
// monotonic, append-only sequence local to this instance. This is the one
// place an auto-increment column earns its keep — replacing it with a
// timestamp+UUID cursor would be slower and error-prone across clock changes.
type SyncChange struct {
	ent.Schema
}

// Annotations of the SyncChange.
func (SyncChange) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sync_change"},
	}
}

// Fields of the SyncChange.
func (SyncChange) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("user_id", ident.UUID{}).
			Default(uuidNil).
			SchemaType(uuidSchemaType),
		field.String("entity"),
		field.Int64("gallery_id"),
		field.String("token"),
		// "upsert" carries the current row state at push time; "delete" is a
		// tombstone whose changed_at is the LWW timestamp for the removal.
		field.String("op").Default("upsert"),
		field.Time("changed_at").Default(time.Now).Immutable(),
	}
}

// Indexes of the SyncChange.
func (SyncChange) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "entity", "gallery_id", "token"),
	}
}
