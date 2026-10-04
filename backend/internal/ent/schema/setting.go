package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Setting is a single persisted configuration key/value. Rows only exist for
// keys the settings page has saved, which lets the rest of the configuration
// keep coming from the environment.
type Setting struct {
	ent.Schema
}

// Annotations of the Setting.
func (Setting) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "settings"},
	}
}

// Fields of the Setting.
func (Setting) Fields() []ent.Field {
	return []ent.Field{
		field.String("key").NotEmpty().Unique(),
		field.Text("value"),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
