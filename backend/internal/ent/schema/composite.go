package schema

import (
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// compositeID declares the given fields as the table's composite primary key,
// e.g. compositeID("gallery_id", "token").
//
// ent 0.14.6 only honours field.ID for edge schemas (join tables): on a regular
// node schema it is silently ignored and ent keeps generating the implicit
// auto-increment "id" primary key. internal/ent/entc therefore applies this
// annotation to node schemas while running code generation, which drops "id"
// and emits PRIMARY KEY (gallery_id, token) in migrate/schema.go.
//
// StructTag is set to a non-nil empty map on purpose: ent's "model" template
// dereferences $.Annotations.Fields.StructTag.<field>, and a nil map
// (field.ID's default) makes code generation fail.
func compositeID(fields ...string) schema.Annotation {
	return field.Annotation{
		ID:        fields,
		StructTag: map[string]string{},
	}
}
