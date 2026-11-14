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
// Fields of the Bookshelf. Only the reference and timestamps are stored;
// metadata (title, thumbnail, pages, ...) is joined from gallery_cache so it
// always reflects the latest known upstream data.
//
// The primary key is a UUID (not an auto-increment counter) so rows survive
// cross-instance migration without id collisions. The (user_id, gallery_id,
// token) triple is a UNIQUE index rather than the primary key because ent
// 0.14.6 can only update composite keys of exactly two columns — a three-column
// key would break the upsert used by the reading-progress endpoint.
func (Bookshelf) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", ident.UUID{}).
			Default(uuidV7).
			SchemaType(uuidSchemaType),
		field.UUID("user_id", ident.UUID{}).
			Default(uuidNil).
			SchemaType(uuidSchemaType),
		field.Int64("gallery_id"),
		field.String("token"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the Bookshelf.
func (Bookshelf) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "gallery_id", "token").Unique(),
	}
}
