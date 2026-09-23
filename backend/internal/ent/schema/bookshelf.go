package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// Bookshelf holds the schema definition for the Bookshelf entity.
type Bookshelf struct {
	ent.Schema
}

// Annotations of the Bookshelf.
func (Bookshelf) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "bookshelf"},
	}
}

// Fields of the Bookshelf.
func (Bookshelf) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("gallery_id"),
		field.String("token"),
		field.String("title").Default(""),
		field.String("title_jpn").Default(""),
		field.String("category").Default(""),
		field.String("thumbnail").Default(""),
		field.Int("page_count").Default(0),
		field.Time("added_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the Bookshelf.
func (Bookshelf) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("gallery_id", "token").Unique(),
	}
}
