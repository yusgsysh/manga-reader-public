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

// Passkey is one WebAuthn credential (a passkey) owned by a user. The
// credential ID uniquely identifies it across the instance and is the row
// looked up during discoverable login, before the user is known.
//
// The full webauthn.Credential record is kept in credential_json because the
// library needs the public key, flags, attestation metadata and signature
// counter exactly as issued. The scalar columns duplicate the parts the
// settings page displays so listing does not have to decode every record.
// Nothing here is secret: a passkey's private key never leaves the
// authenticator.
type Passkey struct {
	ent.Schema
}

// Annotations of the Passkey.
func (Passkey) Annotations() []schema.Annotation {
	return []schema.Annotation{
		entsql.Annotation{Table: "passkey"},
	}
}

// Fields of the Passkey.
func (Passkey) Fields() []ent.Field {
	return []ent.Field{
		field.UUID("id", ident.UUID{}).
			Default(uuidV7).
			SchemaType(uuidSchemaType),
		field.UUID("user_id", ident.UUID{}).
			SchemaType(uuidSchemaType),
		// credential_id is the raw authenticator credential ID (not base64).
		// It is globally unique, so discoverable login can find the owner
		// before it knows which account is authenticating.
		field.Bytes("credential_id").
			NotEmpty().
			Unique(),
		field.Bytes("public_key").
			NotEmpty().
			Sensitive(),
		field.String("attestation_type").Default(""),
		field.String("attestation_format").Default(""),
		// transports is the authenticator's getTransports() hint list.
		field.JSON("transports", []string{}).Optional(),
		field.Bytes("aaguid").Optional(),
		field.Int64("sign_count").Default(0),
		field.Bool("clone_warning").Default(false),
		field.Bool("backup_eligible").Default(false),
		field.Bool("backup_state").Default(false),
		field.Bool("user_present").Default(false),
		field.Bool("user_verified").Default(false),
		// name is a user-editable label ("MacBook", "YubiKey").
		field.String("name").Default(""),
		// credential_json is the canonical serialized webauthn.Credential.
		field.Bytes("credential_json").
			NotEmpty().
			Sensitive(),
		field.Time("created_at").Default(time.Now).Immutable(),
		field.Time("last_used_at").Optional().Nillable(),
	}
}

// Indexes of the Passkey.
func (Passkey) Indexes() []ent.Index {
	return []ent.Index{
		index.Fields("user_id"),
	}
}
