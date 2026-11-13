package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// NodeAPIKey authenticates a node on the peer sync endpoints. Only the
// SHA-256 hash of the secret is stored; the plaintext is returned once when
// the key is created or rotated.
type NodeAPIKey struct {
	ent.Schema
}

// Annotations of the NodeAPIKey.
func (NodeAPIKey) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "node_api_key"},
	}
}

// Fields of the NodeAPIKey.
func (NodeAPIKey) Fields() []ent.Field {
	return []ent.Field{
		field.Int("node_id"),
		field.String("key_hash").NotEmpty().Unique().Sensitive(),
		// key_prefix is safe to display in the UI and lets verification
		// select a small candidate set before constant-time comparison.
		field.String("key_prefix").NotEmpty(),
		field.JSON("scopes", []string{}).Optional(),
		field.String("status").Default("active"),
		field.Time("expires_at").Optional().Nillable(),
		field.Time("last_used_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("revoked_at").Optional().Nillable(),
	}
}

// Indexes of the NodeAPIKey.
func (NodeAPIKey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("node_id", "status"),
		index.Fields("key_prefix"),
	}
}
