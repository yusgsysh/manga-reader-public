package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// TagSyncBaseline stores the source My Tags state as of the last successful
// sync: one row per tag that is believed to exist on the source. It is the
// "common ancestor" for three-way merging — a tag absent from the baseline
// but present locally was added locally (push), absent locally but present
// on the source means the source added it (pull), and when both sides
// changed since the baseline it is a conflict the user resolves explicitly
// (keep source or keep local).
type TagSyncBaseline struct {
	ent.Schema
}

// Annotations of the TagSyncBaseline.
func (TagSyncBaseline) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "tag_sync_baseline"},
	}
}

// Fields of the TagSyncBaseline.
func (TagSyncBaseline) Fields() []ent.Field {
	return []ent.Field{
		field.String("tag").Unique().NotEmpty(),
		field.String("action").NotEmpty(),
		field.Int("weight").Default(10),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
