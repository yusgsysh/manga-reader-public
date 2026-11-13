package schema

import (
	"time"

	"entgo.io/ent"
	"entgo.io/ent/dialect/entsql"
	"entgo.io/ent/schema"
	"entgo.io/ent/schema/field"
	"entgo.io/ent/schema/index"
)

// User is an account inside one tenant. Usernames are unique per tenant, so
// two tenants may both have an "admin" account.
//
// Passwords are stored as Argon2id PHC strings (never plaintext). TokenVersion
// is embedded in every JWT as the "ver" claim; logout increments it so all
// previously issued tokens are rejected by the middleware without keeping a
// list of revoked token IDs.
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
		field.Int64("tenant_id").Default(1),
		field.String("username").
			NotEmpty(),
		field.String("password_hash").
			NotEmpty().
			Sensitive(),
		// platform_admin: global tenant management.
		// tenant_admin: users and business data inside one tenant.
		// user: business data only.
		field.String("role").
			Default("user"),
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
		index.Fields("tenant_id", "username").Unique(),
		index.Fields("tenant_id", "role"),
	}
}
