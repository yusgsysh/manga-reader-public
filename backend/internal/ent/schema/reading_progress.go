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

// ReadingProgress holds the schema definition for the ReadingProgress entity.
type ReadingProgress struct {
	ent.Schema
}

// Annotations of the ReadingProgress.
func (ReadingProgress) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "reading_progress"},
	}
}

// Fields of the ReadingProgress.
//
// The primary key is a UUID (not an auto-increment counter) so rows survive
// cross-instance migration without id collisions. The (user_id, gallery_id,
// token) triple is a UNIQUE index rather than the primary key because ent
// 0.14.6 can only update composite keys of exactly two columns.
func (ReadingProgress) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", ident.UUID{}).
			Default(uuidV7).
			SchemaType(uuidSchemaType),
		field.UUID("user_id", ident.UUID{}).
			Default(uuidNil).
			SchemaType(uuidSchemaType),
		field.Int64("gallery_id"),
		field.String("token"),
		field.Int("current_page").Default(0).NonNegative(),
		field.Float("progress").Default(0).Min(0).Max(1),
		field.Bool("completed").Default(false),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the ReadingProgress.
func (ReadingProgress) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "gallery_id", "token").Unique(),
	}
}
