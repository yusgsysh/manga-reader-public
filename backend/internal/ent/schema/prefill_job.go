package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"

	"manga-reader/internal/ident"
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
		field.UUID("user_id", ident.UUID{}).
			Default(uuidNil).
			SchemaType(uuidSchemaType),
		// Jobs are identified by a random UUID (stored as "job_id") instead of
		// an auto-increment counter: one gallery may hold several jobs, so the
		// (gallery_id, token) pair can only be a lookup key, never the identity.
		field.UUID("id", ident.UUID{}).
			StorageKey("job_id").
			Default(uuidV7).
			SchemaType(uuidSchemaType),
		field.Int64("gallery_id").Optional().Nillable(),
		field.String("token").Default(""),
		field.String("title").Default(""),
		field.JSON("urls", []string{}),
		field.String("status").Default("queued"),
		field.Int("total").Default(0),
		field.Int("failed_count").Default(0),
		field.JSON("errors", []model.PrefillItemError{}).Optional(),
		// Archive metadata is written only after a ZIP has been fully built and
		// uploaded to the archive object store. Until then these stay NULL, so a
		// job with no archive is indistinguishable from one whose upload failed
		// and can be rebuilt on demand.
		field.String("archive_key").Optional().Nillable(),
		field.Int64("archive_size").Optional().Nillable(),
		field.String("archive_etag").Optional().Nillable(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
		field.Time("finished_at").Optional().Nillable(),
	}
}

// Indexes of the PrefillJob.
func (PrefillJob) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id", "status", "created_at"),
		// Backs the dedupe lookup (user + gallery_id + token + status); not
		// unique because jobs without a gallery keep the empty defaults.
		index.Fields("user_id", "gallery_id", "token"),
	}
}
