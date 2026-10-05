package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// SyncChange is the sync outbox: one row per local mutation of a synced
// entity (reading_progress or bookshelf), appended by sync.Record.
//
// The auto-increment "id" doubles as the replication cursor: peers pull with
// "id > cursor", which keeps cursors monotonic on this instance's clock only
// (no cross-machine timestamp comparison when deciding what to send).
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
		index.Fields("entity", "gallery_id", "token"),
	}
}
