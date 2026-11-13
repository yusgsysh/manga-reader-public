package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// Tenant is an isolated usage space. Tenants share the same database and
// tables; every tenant-owned row carries tenant_id and must be accessed with
// that tenant as part of the predicate.
type Tenant struct {
	ent.Schema
}

// Annotations of the Tenant.
func (Tenant) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "tenant"},
	}
}

// Fields of the Tenant.
func (Tenant) Fields() []ent.Field {
	return []ent.Field{
		field.String("name").
			NotEmpty(),
		// slug is the stable tenant identifier used at login and in URLs.
		field.String("slug").
			NotEmpty().
			Unique(),
		field.String("status").
			Default("active"),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}
