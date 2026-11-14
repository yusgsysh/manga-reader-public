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

// UserSyncKey authenticates a peer that syncs exactly one account's private
// rows. Unlike a node API key (instance-wide), the key itself carries the
// owner: the host attributes incoming rows to that user and only returns that
// user's changes, so the two sides no longer need matching usernames or user
// UUIDs. Only the SHA-256 hash is stored.
type UserSyncKey struct {
	ent.Schema
}

// Annotations of the UserSyncKey.
func (UserSyncKey) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_sync_key"},
	}
}

// Fields of the UserSyncKey.
func (UserSyncKey) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", ident.UUID{}).
			Default(uuidV7).
			SchemaType(uuidSchemaType),
		field.UUID("user_id", ident.UUID{}).
			SchemaType(uuidSchemaType),
		field.String("key_hash").NotEmpty().Unique().Sensitive(),
		// key_prefix is safe to display and narrows verification candidates
		// before the constant-time hash comparison.
		field.String("key_prefix").NotEmpty(),
		field.String("status").Default("active"),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("last_used_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("revoked_at").Optional().Nillable(),
	}
}

// Indexes of the UserSyncKey.
func (UserSyncKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "status"),
		index.Fields("key_prefix"),
	}
}
