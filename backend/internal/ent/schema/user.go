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

// User is an account of the installation. Usernames are unique across the
// whole instance: a user is no longer scoped to a tenant.
//
// Passwords are stored as Argon2id PHC strings (never plaintext). TokenVersion
// is embedded in every JWT as the "ver" claim; logout increments it so all
// previously issued tokens are rejected by the middleware without keeping a
// list of revoked token IDs.
//
// A user may also enable TOTP two-factor authentication. Its secret is stored
// encrypted (Sensitive) and recovery codes are kept only as HMAC hashes.
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
		field.String("username").
			NotEmpty().
			Unique(),
		field.String("password_hash").
			NotEmpty().
			Sensitive(),
		// admin: manages users, nodes and instance-wide settings.
		// user: owns business data and personal settings only.
		field.String("role").
			Default("user"),
		field.String("status").
			Default("active"),
		field.Int("token_version").
			Default(0),
		// TOTP is an optional second factor for password login. The secret is
		// AES-256-GCM sealed with a key derived from JWT_SECRET and never
		// serialised; only the user's authenticator app holds the plaintext.
		field.String("totp_secret_ciphertext").
			Default("").
			Sensitive(),
		field.Bool("totp_enabled").
			Default(false),
		field.Time("totp_confirmed_at").
			Optional().
			Nillable(),
		// recovery_code_hashes holds one-way HMAC-SHA256 hashes of unused
		// one-time recovery codes. Plaintext codes are returned only once, at
		// enable/regenerate time, and are removed as they are used.
		field.JSON("recovery_code_hashes", []string{}).
			Optional().
			Sensitive(),
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
