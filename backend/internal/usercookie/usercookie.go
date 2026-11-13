// Package usercookie stores each user's ExHentai session encrypted at rest and
// builds a dedicated HTTP client (with its own cookie jar) for that user.
//
// Every outbound ExHentai request must use the client returned by Client: the
// jar carries the caller's account, and the exhentai fetch cache keys on the
// client pointer, so one user's session can never answer another user's
// request.
package usercookie

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/usersetting"
	"manga-reader/internal/exhentai"
)

// ErrNotConfigured means the user has not supplied ExHentai credentials yet.
var ErrNotConfigured = errors.New("exhentai cookie is not configured")

// Cookie status values stored in user_setting.cookie_status.
const (
	StatusUnset      = "unset"
	StatusUnverified = "unverified"
	StatusValid      = "valid"
	StatusInvalid    = "invalid"
)

// Site values for the user's preferred upstream host.
const (
	SiteExHentai = exhentai.SiteExHentai
	SiteEHentai  = exhentai.SiteEHentai
)

// Summary is the non-secret state of a user's cookie configuration. It is safe
// to return to the browser; neither the cookie nor its hash is included.
type Summary struct {
	Configured bool       `json:"configured"`
	Status     string     `json:"status"`
	Site       string     `json:"site"`
	VerifiedAt *time.Time `json:"verified_at,omitempty"`
	UpdatedAt  *time.Time `json:"updated_at,omitempty"`
	// HasIgneous / HasSK let the UI distinguish a complete session from a
	// credentials-only one without revealing any value.
	HasIgneous bool `json:"has_igneous"`
	HasSK      bool `json:"has_sk"`
}

// Service owns the encrypted cookie rows and the per-user HTTP client cache.
type Service struct {
	db  *ent.Client
	key []byte

	mu      sync.Mutex
	clients map[int64]cachedClient
}

type cachedClient struct {
	client   *http.Client
	sealed   string
	site     string
	loadedAt time.Time
}

// New derives a dedicated AES-256 key from the application's JWT secret. The
// label makes it a distinct key from the token signing key.
func New(db *ent.Client, jwtSecret string) *Service {
	sum := sha256.Sum256([]byte("manga-reader/user-cookie/v1\x00" + jwtSecret))
	return &Service{
		db:      db,
		key:     sum[:],
		clients: make(map[int64]cachedClient),
	}
}

// Client returns the user's dedicated ExHentai HTTP client. Credentials are
// loaded from the encrypted row and never shared with another user.
func (s *Service) Client(ctx context.Context, userID int64) (*http.Client, error) {
	row, err := s.setting(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil || row.CookieCiphertext == "" {
		return nil, ErrNotConfigured
	}

	s.mu.Lock()
	cached, ok := s.clients[userID]
	s.mu.Unlock()
	if ok && cached.sealed == row.CookieCiphertext && cached.site == row.Site {
		return cached.client, nil
	}

	plain, err := s.open(row.CookieCiphertext)
	if err != nil {
		return nil, fmt.Errorf("decrypt exhentai cookie: %w", err)
	}
	cfg := config.ParseCookieString(plain)
	if !cfg.IsValid() {
		return nil, ErrNotConfigured
	}
	client, err := exhentai.CreateHTTPClient(&cfg)
	if err != nil {
		return nil, err
	}

	s.mu.Lock()
	s.clients[userID] = cachedClient{
		client:   client,
		sealed:   row.CookieCiphertext,
		site:     row.Site,
		loadedAt: time.Now(),
	}
	s.mu.Unlock()
	return client, nil
}

// Invalidate drops a cached client, e.g. after the cookie was changed.
func (s *Service) Invalidate(userID int64) {
	s.mu.Lock()
	delete(s.clients, userID)
	s.mu.Unlock()
}

// Site returns the user's preferred upstream site ("exhentai" or "e-hentai").
// Users without a stored preference default to exhentai.
func (s *Service) Site(ctx context.Context, userID int64) (string, error) {
	row, err := s.setting(ctx, userID)
	if err != nil {
		return "", err
	}
	if row == nil || row.Site == "" {
		return SiteExHentai, nil
	}
	return row.Site, nil
}

// Summary reads the non-secret cookie state.
func (s *Service) Summary(ctx context.Context, userID int64) (Summary, error) {
	row, err := s.setting(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	if row == nil {
		return Summary{Status: StatusUnset, Site: SiteExHentai}, nil
	}
	summary := Summary{
		Configured: row.CookieCiphertext != "",
		Status:     row.CookieStatus,
		Site:       row.Site,
		VerifiedAt: row.CookieVerifiedAt,
		UpdatedAt:  &row.UpdatedAt,
	}
	if row.CookieCiphertext != "" {
		if plain, err := s.open(row.CookieCiphertext); err == nil {
			cfg := config.ParseCookieString(plain)
			summary.HasIgneous = cfg.Igneous != ""
			summary.HasSK = cfg.SK != ""
		}
	}
	return summary, nil
}

// SaveCookie validates, encrypts and stores the cookie for one user. Only the
// given user's row changes; other users keep their sessions.
func (s *Service) SaveCookie(ctx context.Context, userID int64, cookie config.CookieConfig, site string) (Summary, error) {
	if !cookie.IsValid() {
		return Summary{}, errors.New("ipb_member_id and ipb_pass_hash are required")
	}
	site, err := NormalizeSite(site)
	if err != nil {
		return Summary{}, err
	}
	sealed, err := s.seal(cookie.Encode())
	if err != nil {
		return Summary{}, err
	}
	row, err := s.setting(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	now := time.Now().UTC()
	if row == nil {
		_, err = s.db.UserSetting.Create().
			SetUserID(int(userID)).
			SetCookieCiphertext(sealed).
			SetCookieStatus(StatusUnverified).
			SetSite(site).
			SetUpdatedAt(now).
			Save(ctx)
	} else {
		_, err = row.Update().
			SetCookieCiphertext(sealed).
			SetCookieStatus(StatusUnverified).
			ClearCookieVerifiedAt().
			SetSite(site).
			SetUpdatedAt(now).
			Save(ctx)
	}
	if err != nil {
		return Summary{}, err
	}
	s.Invalidate(userID)
	return s.Summary(ctx, userID)
}

// ClearCookie removes the user's credentials and cached client.
func (s *Service) ClearCookie(ctx context.Context, userID int64) error {
	if err := s.db.UserSetting.Update().
		Where(usersetting.UserIDEQ(int(userID))).
		SetCookieCiphertext("").
		SetCookieStatus(StatusUnset).
		ClearCookieVerifiedAt().
		Exec(ctx); err != nil {
		return err
	}
	s.Invalidate(userID)
	return nil
}

// SetStatus records the outcome of a verification attempt.
func (s *Service) SetStatus(ctx context.Context, userID int64, status string) error {
	if err := s.db.UserSetting.Update().
		Where(usersetting.UserIDEQ(int(userID))).
		SetCookieStatus(status).
		SetCookieVerifiedAt(time.Now().UTC()).
		Exec(ctx); err != nil {
		return err
	}
	return nil
}

// Preferences returns the stored personal preference document.
func (s *Service) Preferences(ctx context.Context, userID int64) (map[string]any, error) {
	row, err := s.setting(ctx, userID)
	if err != nil {
		return nil, err
	}
	if row == nil || row.Preferences == nil {
		return map[string]any{}, nil
	}
	return row.Preferences, nil
}

// SavePreferences replaces the preference document for one user.
func (s *Service) SavePreferences(ctx context.Context, userID int64, prefs map[string]any) error {
	row, err := s.setting(ctx, userID)
	if err != nil {
		return err
	}
	if prefs == nil {
		prefs = map[string]any{}
	}
	if row == nil {
		_, err = s.db.UserSetting.Create().
			SetUserID(int(userID)).
			SetPreferences(prefs).
			Save(ctx)
		return err
	}
	_, err = row.Update().SetPreferences(prefs).Save(ctx)
	return err
}

// NormalizeSite validates the site preference.
func NormalizeSite(site string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(site)) {
	case "", SiteExHentai:
		return SiteExHentai, nil
	case SiteEHentai, "ehentai", "e_hentai":
		return SiteEHentai, nil
	default:
		return "", fmt.Errorf("site must be %q or %q", SiteExHentai, SiteEHentai)
	}
}

func (s *Service) setting(ctx context.Context, userID int64) (*ent.UserSetting, error) {
	row, err := s.db.UserSetting.Query().
		Where(usersetting.UserIDEQ(int(userID))).
		Only(ctx)
	if err != nil {
		if ent.IsNotFound(err) {
			return nil, nil
		}
		return nil, err
	}
	return row, nil
}

func (s *Service) seal(plain string) (string, error) {
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawStdEncoding.EncodeToString(sealed), nil
}

func (s *Service) open(encoded string) (string, error) {
	raw, err := base64.RawStdEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("sealed cookie is truncated")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

// SaveSite updates the preferred upstream site without changing credentials.
func (s *Service) SaveSite(ctx context.Context, userID int64, site string) (string, error) {
	normalized, err := NormalizeSite(site)
	if err != nil {
		return "", err
	}
	row, err := s.setting(ctx, userID)
	if err != nil {
		return "", err
	}
	if row == nil {
		if _, err := s.db.UserSetting.Create().
			SetUserID(int(userID)).
			SetSite(normalized).
			Save(ctx); err != nil {
			return "", err
		}
		s.Invalidate(userID)
		return normalized, nil
	}
	if _, err := row.Update().SetSite(normalized).Save(ctx); err != nil {
		return "", err
	}
	s.Invalidate(userID)
	return normalized, nil
}
