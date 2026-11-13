package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// AppSetting is a small key/value table for instance-wide configuration that
// cannot be expressed as an environment variable. Today its only entry is the
// generated JWT signing secret (key "auth.jwt_secret"), persisted on first boot
// when JWT_SECRET is unset so sessions survive restarts without the operator
// having to set anything manually. The value is marked sensitive so ent never
// serialises or logs it.
type AppSetting struct {
	ent.Schema
}

// Annotations of the AppSetting.
func (AppSetting) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "app_setting"},
	}
}

// Fields of the AppSetting.
func (AppSetting) Fields() []ent.Field {
	return []ent.Field{
		// The key is the identity: each setting is a single row, so an
		// auto-increment id would only duplicate the natural key. ent exposes
		// the primary key as "id" in Go and stores it in the "key" column.
		field.String("id").
			StorageKey("key").
			NotEmpty().
			Unique().
			Immutable(),
		field.String("value").Default("").Sensitive(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
