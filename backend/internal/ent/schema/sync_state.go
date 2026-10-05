package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// SyncState is a small key/value table for sync client configuration
// (server URL, token, enabled) and replication state (cursors, last sync
// outcome). Values are plain strings; booleans/timestamps are encoded.
type SyncState struct {
	ent.Schema
}

// Annotations of the SyncState.
func (SyncState) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "sync_state"},
	}
}

// Fields of the SyncState.
func (SyncState) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").Unique(),
		field.String("value").Default(""),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
