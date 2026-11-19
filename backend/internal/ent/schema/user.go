package schema

import (
	"time"

	"manga-reader/internal/ident"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// User is a local account of the installation. Identity is delegated to
// Kinde: UserID stores the verified JWT "sub" and is the only external
// identity key. Email is profile data, never an identity key.
//
// PasswordHash is retained for legacy rows only; interactive password login is
// no longer offered (Kinde owns login/register).
// TokenVersion is bumped on logout/disable so any derived browser session
// (the media cookie) is rejected immediately.
//
// Every private row (bookshelf, reading progress, prefill jobs, tag rules,
// sync outbox, personal settings and ExHentai cookie) carries this user's ID
// and is only ever queried with it in the predicate.
type User struct {
	ent.Schema
}

// Annotations of the User.
func (User) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "users"},
	}
}

// Fields of the User.
func (User) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", ident.UUID{}).
			Default(uuidV7).
			SchemaType(uuidSchemaType),
		// user_id is the verified Kinde subject ("sub"). Optional so legacy
		// rows can coexist; unique so concurrent first logins cannot create
		// two local accounts for one Kinde identity. The local primary key is
		// the UUID "id" above; user_id is an external identity, never a
		// client-supplied user id.
		field.String("user_id").
			Optional().
			Unique(),
		// username remains a unique local display handle for legacy rows and
		// the admin UI. New Kinde users get a deterministic handle derived
		// from their subject; email is stored separately and is not unique.
		field.String("username").
			NotEmpty().
			Unique(),
		field.String("password_hash").
			Optional().
			Sensitive(),
		field.String("email").
			Optional(),
		field.String("name").
			Optional(),
		field.String("avatar_url").
			Optional(),
		// admin: manages users, nodes and instance-wide settings.
		// user: owns business data and personal settings only.
		field.String("role").
			Default("user"),
		// active or disabled. disabled is enforced on every request, even
		// while the Kinde access token is still valid.
		field.String("status").
			Default("active"),
		field.Int("token_version").
			Default(0),
		field.Time("created_at").
			Default(time.Now).
			Immutable(),
		field.Time("updated_at").
			Default(time.Now).
			UpdateDefault(time.Now),
	}
}

// Indexes of the User.
func (User) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("role"),
		index.Fields("status"),
	}
}
