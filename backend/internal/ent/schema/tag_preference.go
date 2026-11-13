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
		compositeID("tenant_id", "id"),
	}
}

// Fields of the TagPreference.
func (TagPreference) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("tenant_id").Default(1),
		// The tag itself is the identity within a tenant: one row per
		// `namespace:name`; the primary key is (tenant_id, tag).
		field.String("id").
			StorageKey("tag").
			NotEmpty().
			Immutable(),
		field.String("action").NotEmpty(),
		// Weight mirrors ExHentai My Tags tag weight: like rules are >= 0,
		// dislike rules are <= 0. Used only for ordering (strongest first),
		// never for filtering inside this app.
		field.Int("weight").Default(10),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
