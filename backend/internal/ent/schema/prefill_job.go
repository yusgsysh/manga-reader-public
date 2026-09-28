package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"manga-reader/internal/model"
)

// PrefillJob holds the schema definition for the PrefillJob entity.
//
// Only durable task metadata is stored here. Progress counters (done/cached/
// fetched) are intentionally kept in memory because the underlying MinIO
// image cache expires, which would make persisted progress misleading.
type PrefillJob struct {
	ent.Schema
}

// Annotations of the PrefillJob.
func (PrefillJob) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "prefill_job"},
	}
}

// Fields of the PrefillJob.
func (PrefillJob) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("gallery_id").Optional().Nillable(),
		field.String("gallery_token").Default(""),
		field.String("title").Default(""),
		field.JSON("urls", []string{}),
		field.String("status").Default("queued"),
		field.Int("total").Default(0),
		field.Int("failed_count").Default(0),
		field.JSON("errors", []model.PrefillItemError{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("finished_at").Optional().Nillable(),
	}
}

// Indexes of the PrefillJob.
func (PrefillJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("status", "created_at"),
	}
}
