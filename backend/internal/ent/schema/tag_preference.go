package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// TagPreference records a user preference ("like" / "dislike") for a single
// tag in `namespace:name` form, used to colour tags across the UI (blue/red)
// the same way ExHentai's My Tags highlights them. One row per tag.
type TagPreference struct {
	ent.Schema
}

// Annotations of the TagPreference.
func (TagPreference) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "tag_preference"},
	}
}

// Fields of the TagPreference.
func (TagPreference) Fields() []ent.Field {
	return []ent.Field{
		field.String("tag").Unique().NotEmpty(),
		field.String("action").NotEmpty(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
