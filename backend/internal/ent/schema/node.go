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

// Node is a trusted sync peer of this installation. Nodes are managed by
// administrators. Synchronised rows carry their owner user_id, so a peer
// replicates every user's data without losing per-user isolation.
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
		field.UUID("id", ident.UUID{}).
			Default(uuidV7).
			SchemaType(uuidSchemaType),
		field.String("name").NotEmpty(),
		field.String("status").Default("active"),
		field.Time("last_seen_at").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the Node.
func (Node) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("name").Unique(),
		index.Fields("status"),
	}
}
