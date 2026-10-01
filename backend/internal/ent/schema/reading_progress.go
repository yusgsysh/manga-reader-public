package schema

import (
	"time"

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
func (ReadingProgress) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("gallery_id"),
		field.String("token"),
		field.Int("current_page").Default(0).NonNegative(),
		field.Float("progress").Default(0).Min(0).Max(1),
		field.Bool("completed").Default(false),
		field.Time("started_at").Optional().Nillable(),
		field.Time("updated_at").Optional().Nillable().UpdateDefault(time.Now),
	}
}

// Indexes of the ReadingProgress.
func (ReadingProgress) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("gallery_id", "token").Unique(),
	}
}
