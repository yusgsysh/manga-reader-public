package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Node is a trusted sync peer bound to exactly one tenant. Nodes are managed
// by platform administrators; a node's API keys inherit its tenant scope.
type Node struct {
	ent.Schema
}

// Annotations of the Node.
func (Node) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "node"},
	}
}

// Fields of the Node.
func (Node) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").NotEmpty(),
		field.Int64("tenant_id"),
		field.String("status").Default("active"),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the Node.
func (Node) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("tenant_id", "name").Unique(),
		index.Fields("status"),
	}
}
