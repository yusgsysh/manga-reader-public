package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Bookshelf holds the schema definition for the Bookshelf entity.
type Bookshelf struct {
	ent.Schema
}

// Annotations of the Bookshelf.
func (Bookshelf) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "bookshelf"},
		compositeID("gallery_id", "token"),
	}
}

// Fields of the Bookshelf.
// Fields of the Bookshelf. Only the reference and timestamps are stored;
// metadata (title, thumbnail, pages, ...) is joined from gallery_cache so it
// always reflects the latest known upstream data.
func (Bookshelf) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("gallery_id"),
		field.String("token"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
