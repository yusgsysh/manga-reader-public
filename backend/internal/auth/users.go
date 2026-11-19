package auth

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/user"
)

// ErrUserDisabled signals a valid Kinde identity whose local account is
// disabled. It is distinct from ErrInvalidToken so the middleware can answer
// 403 instead of 401.
var ErrUserDisabled = errors.New("user disabled")

// principalFromClaims maps a verified Kinde identity onto the local user row.
// The first successful login creates the row atomically (relying on the unique
// index on user_id, not a read-then-insert race), later logins refresh
// only profile fields that Kinde owns.
func (s *Service) principalFromClaims(ctx context.Context, claims *Claims) (Principal, error) {
	u, err := s.upsertUser(ctx, claims)
	if err != nil {
		return Principal{}, err
	}
	if u.Status != config.StatusActive {
		return Principal{}, ErrUserDisabled
	}
	return Principal{
		ID:        u.ID,
		UserID:    u.UserID,
		Username:  u.Username,
		Email:     u.Email,
		Name:      u.Name,
		AvatarURL: u.AvatarURL,
		Role:      u.Role,
		Status:    u.Status,
	}, nil
}

func (s *Service) upsertUser(ctx context.Context, claims *Claims) (*ent.User, error) {
	u, err := s.db.User.Query().
		Where(user.UserID(claims.Subject)).
		Only(ctx)
	if err != nil && !ent.IsNotFound(err) {
		return nil, fmt.Errorf("read user by Kinde subject: %w", err)
	}
	if err == nil {
		return s.refreshProfile(ctx, u, claims)
	}

	create := s.db.User.Create().
		SetUserID(claims.Subject).
		SetUsername(usernameForSubject(claims.Subject)).
		SetStatus(config.StatusActive).
		SetRole(s.initialRole(claims))
	applyProfile(create, claims)

	created, err := create.Save(ctx)
	if err == nil {
		return created, nil
	}
	if !ent.IsConstraintError(err) {
		return nil, fmt.Errorf("create local user: %w", err)
	}

	// A concurrent first login won the unique-index race. Re-read the row it
	// created instead of returning an error (or trying to insert again).
	u, err = s.db.User.Query().
		Where(user.UserID(claims.Subject)).
		Only(ctx)
	if err != nil {
		return nil, fmt.Errorf("read concurrently created user: %w", err)
	}
	return s.refreshProfile(ctx, u, claims)
}

func (s *Service) refreshProfile(ctx context.Context, u *ent.User, claims *Claims) (*ent.User, error) {
	update := s.db.User.UpdateOneID(u.ID)
	changed := false

	if claims.Email != "" && claims.Email != u.Email {
		update.SetEmail(claims.Email)
		changed = true
	}
	if claims.Name != "" && claims.Name != u.Name {
		update.SetName(claims.Name)
		changed = true
	}
	if claims.AvatarURL != "" && claims.AvatarURL != u.AvatarURL {
		update.SetAvatarURL(claims.AvatarURL)
		changed = true
	}
	if !changed {
		return u, nil
	}
	updated, err := update.Save(ctx)
	if err != nil {
		// Never fail an otherwise valid login because a profile refresh lost a
		// race with an admin edit; the identity and status checks already ran.
		return u, nil
	}
	return updated, nil
}

func applyProfile(create *ent.UserCreate, claims *Claims) {
	if claims.Email != "" {
		create.SetEmail(claims.Email)
	}
	if claims.Name != "" {
		create.SetName(claims.Name)
	}
	if claims.AvatarURL != "" {
		create.SetAvatarURL(claims.AvatarURL)
	}
}

func (s *Service) initialRole(claims *Claims) string {
	if s.isAdminEmail(claims) {
		return config.RoleAdmin
	}
	return config.RoleUser
}

func (s *Service) isAdminEmail(claims *Claims) bool {
	if _, ok := s.adminSubjects[claims.Subject]; ok {
		return true
	}
	if claims.Email == "" || !claims.EmailVerified || len(s.adminEmails) == 0 {
		return false
	}
	_, ok := s.adminEmails[strings.ToLower(claims.Email)]
	return ok
}

// usernameForSubject derives a stable, unique local handle. The subject is the
// only guaranteed identifier in a Kinde access token; email is profile data and
// may be absent or change, so it must not be used for uniqueness.
func usernameForSubject(subject string) string {
	sum := sha256.Sum256([]byte(subject))
	return "kinde_" + hex.EncodeToString(sum[:8])
}
