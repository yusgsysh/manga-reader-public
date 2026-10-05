// Command entc runs ent code generation for manga-reader.
//
// It exists because ent 0.14.6 only supports composite primary keys for edge
// schemas (join tables): the field.ID annotation is documented as "valid only
// for edge schemas" and Type.HasCompositeID() returns true only when the type
// is an edge schema. For regular node schemas ent always materialises the
// implicit auto-increment "id" column as PRIMARY KEY.
//
// This program closes that gap using ent's own supported extension points
// (entc.Extension hooks + template overrides) instead of hand-editing the
// generated files:
//
//   - Hooks read the field.ID annotation declared by a node schema and promote
//     those fields to the node's composite identifier (Type.EdgeSchema.ID),
//     dropping the implicit "id". Every built-in template already renders
//     composite identifiers (that is how edge schemas are generated), so the
//     entity/query/create/update/mutation code paths are the stock ones.
//   - The "schema" template override (see schema.tmpl) emits the matching
//     PRIMARY KEY (col, col) clause for migrate/schema.go. It is a verbatim
//     copy of ent's migrate/schema.tmpl with only the PrimaryKey block
//     extended, because ent's own addCompositePK helper only resolves
//     foreign-key (edge) columns.
package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"log"

	"entgo.io/ent/entc"
	"entgo.io/ent/entc/gen"
	"entgo.io/ent/schema/field"
)

//go:embed schema.tmpl
var schemaTmpl string

func main() {
	if err := entc.Generate("./schema", &gen.Config{}, entc.Extensions(extension{})); err != nil {
		log.Fatal(err)
	}
}

type extension struct {
	entc.DefaultExtension
}

// Hooks promotes the fields declared with field.ID to a composite identifier.
func (extension) Hooks() []gen.Hook {
	return []gen.Hook{func(next gen.Generator) gen.Generator {
		return gen.GenerateFunc(func(g *gen.Graph) error {
			if err := applyCompositeIDs(g); err != nil {
				return err
			}
			return next.Generate(g)
		})
	}}
}

// Templates overrides ent's migrate/schema.go generator so that composite
// identifiers are emitted as a real PRIMARY KEY (col, ...) clause.
func (extension) Templates() []*gen.Template {
	t, err := gen.NewTemplate("schema").Parse(schemaTmpl)
	if err != nil {
		panic(fmt.Errorf("parse schema.tmpl: %w", err))
	}
	return []*gen.Template{t}
}

func applyCompositeIDs(g *gen.Graph) error {
	for _, n := range g.Nodes {
		if n.IsEdgeSchema() {
			// Edge schemas are handled by ent itself.
			continue
		}
		ant, ok := fieldAnnotation(n.Annotations)
		if !ok || len(ant.ID) == 0 {
			continue
		}
		if len(ant.ID) < 2 {
			return fmt.Errorf("schema %s: composite primary key %q needs at least 2 fields", n.Name, ant.ID)
		}
		ids := make([]*gen.Field, 0, len(ant.ID))
		for _, name := range ant.ID {
			f, ok := findField(n, name)
			if !ok {
				return fmt.Errorf("schema %s: primary key field %q is not declared in Fields()", n.Name, name)
			}
			ids = append(ids, f)
		}
		n.ID = nil
		// HasCompositeID() is "IsEdgeSchema() && len(EdgeSchema.ID) > 1".
		// Ent only ever sets EdgeSchema.From/To for edge.Through relations, so
		// stamp a marker edge here; no template reads it for node schemas.
		n.EdgeSchema.From = &gen.Edge{Name: "composite_id", Owner: n}
		n.EdgeSchema.ID = ids
	}
	return nil
}

// fieldAnnotation decodes the field.ID annotation of a schema.
func fieldAnnotation(annotations map[string]any) (*field.Annotation, bool) {
	raw, ok := annotations["Fields"]
	if !ok || raw == nil {
		return nil, false
	}
	buf, err := json.Marshal(raw)
	if err != nil {
		return nil, false
	}
	ant := new(field.Annotation)
	if err := json.Unmarshal(buf, ant); err != nil {
		return nil, false
	}
	return ant, true
}

func findField(n *gen.Type, name string) (*gen.Field, bool) {
	for _, f := range n.Fields {
		if f.Name == name {
			return f, true
		}
	}
	return nil, false
}
