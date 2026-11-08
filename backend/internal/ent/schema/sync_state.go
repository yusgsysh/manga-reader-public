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
		// The key is the identity: each setting is a single row, so an
		// auto-increment id would only duplicate the natural key. ent exposes
		// the primary key as "id" in Go and stores it in the "key" column.
		field.String("id").
			StorageKey("key").
			NotEmpty().
			Unique().
			Immutable(),
		field.String("value").Default(""),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
