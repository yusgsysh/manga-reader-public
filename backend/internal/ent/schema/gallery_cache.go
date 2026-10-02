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

// GalleryCache stores the latest known metadata and page list for a gallery so
// that bookshelf / reading history stay usable when the upstream is unreachable.
type GalleryCache struct {
	ent.Schema
}

// Annotations of the GalleryCache.
func (GalleryCache) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "gallery_cache"},
	}
}

// Fields of the GalleryCache.
func (GalleryCache) Fields() []ent.Field {
	return []ent.Field{
		field.Int64("gallery_id"),
		field.String("token"),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.String("title").Default(""),
		field.String("title_jpn").Default(""),
		field.String("category").Default(""),
		field.String("thumbnail").Default(""),
		field.Int("page_count").Default(0),
		field.Float("rating").Default(0),
		field.Int("rating_count").Default(0),
		field.String("uploader").Default(""),
		field.String("posted").Default(""),
		field.Time("posted_at").Optional().Nillable(),
		field.String("language").Default(""),
		field.Bool("translated").Default(false),
		field.String("file_size").Default(""),
		field.Int("favorited").Default(0),
		field.Bool("expunged").Default(false),
		field.JSON("tags", []model.Tag{}).Optional(),
		field.JSON("page_urls", []string{}).Optional(),
		field.JSON("thumbnails", []model.GalleryPageThumb{}).Optional(),
		field.Time("meta_fetched_at").Optional().Nillable(),
		field.Time("details_fetched_at").Optional().Nillable(),
		field.Time("pages_fetched_at").Optional().Nillable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}

// Indexes of the GalleryCache.
func (GalleryCache) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("gallery_id", "token").Unique(),
	}
}
