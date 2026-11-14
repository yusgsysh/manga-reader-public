package schema

import (
	"time"

	"manga-reader/internal/ident"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
)

// UserSetting is the private configuration of one user. Exactly one row exists
// per user (user_id is unique). The ExHentai session cookie is stored encrypted
// and marked sensitive so it is never serialised or logged; the generic
// preferences document holds search defaults, filters, display and reader
// preferences, preload policy and the preferred ExHentai site.
type UserSetting struct {
	ent.Schema
}

// Annotations of the UserSetting.
func (UserSetting) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "user_setting"},
	}
}

// Fields of the UserSetting.
func (UserSetting) Fields() []ent.Field {
	return []ent.Field{
		// The owning user is the identity: exactly one settings row per user,
		// so the user id doubles as the primary key (stored in "user_id").
		// ent exposes it as "id" in Go, mirroring app_setting/sync_state.
		field.UUID("id", ident.UUID{}).
			StorageKey("user_id").
			Immutable().
			SchemaType(uuidSchemaType),
		// cookie_ciphertext is AES-256-GCM sealed; the plaintext cookie never
		// touches the database or an API response.
		field.String("cookie_ciphertext").
			Default("").
			Sensitive(),
		// unset | unverified | valid | invalid
		field.String("cookie_status").Default("unset"),
		field.Time("cookie_verified_at").Optional().Nillable(),
		// exhentai | e-hentai (the account's preferred upstream host).
		field.String("site").Default("exhentai"),
		// Arbitrary personal preferences (search defaults, tag filters, display,
		// reader, preload, image policy). Structured server-side, never shared
		// between users.
		field.JSON("preferences", map[string]any{}).Optional(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("updated_at").Default(time.Now).UpdateDefault(time.Now),
	}
}
