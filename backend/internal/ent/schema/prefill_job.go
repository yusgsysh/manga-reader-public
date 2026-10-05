package schema

import (
	"time"

	"github.com/google/uuid"

	"entgo.io/ent"
	"entgo.io/ent/dialect"
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
		// Jobs are identified by a random UUID (stored as "job_id") instead of
		// an auto-increment counter: one gallery may hold several jobs, so the
		// (gallery_id, token) pair can only be a lookup key, never the identity.
		field.UUID("id", uuid.UUID{}).
			StorageKey("job_id").
			Default(uuid.New).
			// SQLite needs TEXT affinity: "uuid" would inherit NUMERIC affinity
			// and silently coerce some generated values into floats.
			SchemaType(map[string]string{dialect.SQLite: "text"}).
			Annotations(entsql.Annotation{
				// A database-level default lets an existing table that still has
				// an integer "id" column be rebuilt without violating NOT NULL.
				DefaultExprs: map[string]string{
					dialect.SQLite:   "lower(hex(randomblob(16)))",
					dialect.Postgres: "gen_random_uuid()",
				},
			}),
		field.Int64("gallery_id").Optional().Nillable(),
		field.String("token").Default(""),
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
		// Backs the dedupe lookup (gallery_id + token + status); not unique
		// because jobs without a gallery keep the empty defaults.
		index.Fields("gallery_id", "token"),
	}
}
