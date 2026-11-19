package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/base64"
	json "encoding/json/v2"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/gin-gonic/gin"
	"github.com/go-webauthn/webauthn/protocol"
	"github.com/go-webauthn/webauthn/webauthn"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/passkey"
	"manga-reader/internal/ent/user"
	"manga-reader/internal/ident"
	"manga-reader/internal/requestid"
	"manga-reader/internal/web"
)

const (
	// passkeyCookieName carries only a random ceremony id. The challenge and
	// every other ceremony value stay server-side, so the cookie is an opaque
	// lookup key rather than trusted data.
	passkeyCookieName  = "manga_reader_passkey"
	passkeyCeremonyTTL = 5 * time.Minute
	passkeyMaxBody     = 64 << 10
	maxPasskeysPerUser = 20
	maxPasskeys        = 4096
	passkeyDefaultName = "通行密钥"
)

// passkeyCeremony is the server-side state between Begin* and Finish*. The
// WebAuthn object is kept with it so finish validation uses the exact RPID,
// origin and configuration the challenge was created with.
type passkeyCeremony struct {
	wa      *webauthn.WebAuthn
	session *webauthn.SessionData
	userID  ident.UUID
	expires time.Time
}

// passkeyUser adapts an account to the webauthn.User interface.
type passkeyUser struct {
	id          ident.UUID
	username    string
	credentials []webauthn.Credential
}

func (u *passkeyUser) WebAuthnID() []byte { return u.id[:] }
func (u *passkeyUser) WebAuthnName() string {
	return u.username
}
func (u *passkeyUser) WebAuthnDisplayName() string {
	return u.username
}
func (u *passkeyUser) WebAuthnCredentials() []webauthn.Credential {
	return u.credentials
}

type passkeyJSON struct {
	ID             ident.UUID `json:"id"`
	Name           string     `json:"name"`
	CreatedAt      string     `json:"created_at"`
	LastUsedAt     *string    `json:"last_used_at"`
	Transports     []string   `json:"transports"`
	BackupEligible bool       `json:"backup_eligible"`
	BackupState    bool       `json:"backup_state"`
	UserVerified   bool       `json:"user_verified"`
	CloneWarning   bool       `json:"clone_warning"`
}

func (s *Service) handlePasskeyList(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	rows, err := s.db.Passkey.Query().
		Where(passkey.UserIDEQ(principal.ID)).
		Order(ent.Asc(passkey.FieldCreatedAt)).
		All(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "list passkeys failed"})
		return
	}
	out := make([]passkeyJSON, 0, len(rows))
	for _, row := range rows {
		out = append(out, passkeyResponse(row))
	}
	web.JSON(c, http.StatusOK, gin.H{"passkeys": out})
}

func passkeyResponse(row *ent.Passkey) passkeyJSON {
	var lastUsed *string
	if row.LastUsedAt != nil {
		value := row.LastUsedAt.UTC().Format(time.RFC3339)
		lastUsed = &value
	}
	transports := row.Transports
	if transports == nil {
		transports = []string{}
	}
	return passkeyJSON{
		ID:             row.ID,
		Name:           row.Name,
		CreatedAt:      row.CreatedAt.UTC().Format(time.RFC3339),
		LastUsedAt:     lastUsed,
		Transports:     transports,
		BackupEligible: row.BackupEligible,
		BackupState:    row.BackupState,
		UserVerified:   row.UserVerified,
		CloneWarning:   row.CloneWarning,
	}
}

func (s *Service) handlePasskeyRename(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	id, err := ident.Parse(c.Param("id"))
	if err != nil || id.IsNil() {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid passkey id"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 4<<10)
	var body struct {
		Name string `json:"name"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	name := truncateRunes(strings.TrimSpace(body.Name), 64)
	if name == "" {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "name is required"})
		return
	}
	n, err := s.db.Passkey.Update().
		Where(passkey.IDEQ(id), passkey.UserIDEQ(principal.ID)).
		SetName(name).
		Save(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "rename passkey failed"})
		return
	}
	if n == 0 {
		web.JSON(c, http.StatusNotFound, gin.H{"error": "passkey not found"})
		return
	}
	row, err := s.db.Passkey.Query().
		Where(passkey.IDEQ(id), passkey.UserIDEQ(principal.ID)).
		Only(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read passkey failed"})
		return
	}
	web.JSON(c, http.StatusOK, gin.H{"passkey": passkeyResponse(row)})
}

func (s *Service) handlePasskeyDelete(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	id, err := ident.Parse(c.Param("id"))
	if err != nil || id.IsNil() {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid passkey id"})
		return
	}
	n, err := s.db.Passkey.Delete().
		Where(passkey.IDEQ(id), passkey.UserIDEQ(principal.ID)).
		Exec(c.Request.Context())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "delete passkey failed"})
		return
	}
	if n == 0 {
		web.JSON(c, http.StatusNotFound, gin.H{"error": "passkey not found"})
		return
	}
	slog.Info("passkey deleted",
		"request_id", requestid.From(c),
		"actor_user_id", principal.ID,
		"passkey_id", id,
	)
	web.JSON(c, http.StatusOK, gin.H{"success": true})
}

func (s *Service) handlePasskeyRegisterBegin(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	if !s.allowLogin(c.ClientIP()) {
		c.Header("Retry-After", "60")
		web.JSON(c, http.StatusTooManyRequests, gin.H{"error": "too many requests"})
		return
	}
	wa, origin, err := s.newWebAuthn(c)
	if err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	u, credentialRows, err := s.loadPasskeyUser(c.Request.Context(), principal.ID)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "load passkeys failed"})
		return
	}
	exclusions := make([]protocol.CredentialDescriptor, 0, len(credentialRows))
	for _, cred := range u.credentials {
		exclusions = append(exclusions, cred.Descriptor())
	}
	creation, session, err := wa.BeginRegistration(
		u,
		webauthn.WithResidentKeyRequirement(protocol.ResidentKeyRequirementRequired),
		webauthn.WithExclusions(exclusions),
		webauthn.WithExtensions(webauthn.WithExtensionCredProps()),
		webauthn.WithRegistrationOrigin(origin),
	)
	if err != nil {
		slog.Warn("passkey registration begin failed",
			"request_id", requestid.From(c),
			"user_id", principal.ID,
			"error", err,
		)
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "passkey registration is not available"})
		return
	}
	if err := s.storePasskeyCeremony(c, &passkeyCeremony{
		wa:      wa,
		session: session,
		userID:  principal.ID,
	}); err != nil {
		web.JSON(c, http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}
	web.JSON(c, http.StatusOK, creation)
}

func (s *Service) handlePasskeyRegisterFinish(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ceremony, ok := s.takePasskeyCeremony(c)
	if !ok || ceremony.userID != principal.ID {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "passkey ceremony expired, please try again"})
		return
	}

	u, _, err := s.loadPasskeyUser(c.Request.Context(), principal.ID)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "load passkeys failed"})
		return
	}
	if len(u.credentials) >= maxPasskeysPerUser {
		web.JSON(c, http.StatusConflict, gin.H{"error": fmt.Sprintf("at most %d passkeys are allowed", maxPasskeysPerUser)})
		return
	}

	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, passkeyMaxBody)
	credential, err := ceremony.wa.FinishRegistration(u, *ceremony.session, c.Request)
	if err != nil {
		slog.Warn("passkey registration failed",
			"request_id", requestid.From(c),
			"user_id", principal.ID,
			"error", err,
		)
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "passkey registration failed"})
		return
	}

	name := truncateRunes(strings.TrimSpace(c.Query("name")), 64)
	if name == "" {
		name = passkeyDefaultName
	}
	encoded, err := json.Marshal(credential)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "store passkey failed"})
		return
	}

	row, err := s.db.Passkey.Create().
		SetUserID(principal.ID).
		SetCredentialID(credential.ID).
		SetPublicKey(credential.PublicKey).
		SetAttestationType(credential.AttestationType).
		SetAttestationFormat(credential.AttestationFormat).
		SetTransports(transportStrings(credential.Transport)).
		SetAaguid(credential.Authenticator.AAGUID).
		SetSignCount(int64(credential.Authenticator.SignCount)).
		SetCloneWarning(credential.Authenticator.CloneWarning).
		SetBackupEligible(credential.Flags.BackupEligible).
		SetBackupState(credential.Flags.BackupState).
		SetUserPresent(credential.Flags.UserPresent).
		SetUserVerified(credential.Flags.UserVerified).
		SetName(name).
		SetCredentialJSON(encoded).
		Save(c.Request.Context())
	if err != nil {
		if ent.IsConstraintError(err) {
			web.JSON(c, http.StatusConflict, gin.H{"error": "this passkey is already registered"})
			return
		}
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "store passkey failed"})
		return
	}

	slog.Info("passkey registered",
		"request_id", requestid.From(c),
		"actor_user_id", principal.ID,
		"passkey_id", row.ID,
	)
	web.JSON(c, http.StatusCreated, gin.H{"passkey": passkeyResponse(row)})
}

func (s *Service) handlePasskeyLoginBegin(c *gin.Context) {
	if !s.allowLogin(c.ClientIP()) {
		c.Header("Retry-After", "60")
		web.JSON(c, http.StatusTooManyRequests, gin.H{"error": "too many login attempts"})
		return
	}
	wa, origin, err := s.newWebAuthn(c)
	if err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	assertion, session, err := wa.BeginDiscoverableLogin(
		webauthn.WithUserVerification(protocol.VerificationPreferred),
		webauthn.WithLoginOrigin(origin),
	)
	if err != nil {
		slog.Warn("passkey login begin failed",
			"request_id", requestid.From(c),
			"error", err,
		)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "passkey login is not available"})
		return
	}
	if err := s.storePasskeyCeremony(c, &passkeyCeremony{
		wa:      wa,
		session: session,
	}); err != nil {
		web.JSON(c, http.StatusTooManyRequests, gin.H{"error": err.Error()})
		return
	}
	web.JSON(c, http.StatusOK, assertion)
}

func (s *Service) handlePasskeyLoginFinish(c *gin.Context) {
	if !s.allowLogin(c.ClientIP()) {
		c.Header("Retry-After", "60")
		web.JSON(c, http.StatusTooManyRequests, gin.H{"error": "too many login attempts"})
		return
	}
	ceremony, ok := s.takePasskeyCeremony(c)
	if !ok {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "passkey login failed"})
		return
	}

	ctx := c.Request.Context()
	var matched *ent.Passkey
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, passkeyMaxBody)
	_, credential, err := ceremony.wa.FinishPasskeyLogin(
		func(rawID, userHandle []byte) (webauthn.User, error) {
			row, err := s.db.Passkey.Query().
				Where(passkey.CredentialIDEQ(rawID)).
				Only(ctx)
			if err != nil {
				return nil, err
			}
			matched = row
			account, err := s.db.User.Query().
				Where(user.IDEQ(row.UserID), user.StatusEQ(config.StatusActive)).
				Only(ctx)
			if err != nil {
				return nil, err
			}
			// userHandle is the WebAuthnID bytes issued at registration. When
			// the authenticator returns one it must belong to the same owner
			// as the credential, otherwise the assertion is inconsistent.
			if len(userHandle) > 0 && !bytes.Equal(userHandle, account.ID[:]) {
				return nil, errors.New("passkey user handle mismatch")
			}
			passkeyAccount, _, err := s.loadPasskeyUserWithAccount(ctx, account)
			if err != nil {
				return nil, err
			}
			return passkeyAccount, nil
		},
		*ceremony.session,
		c.Request,
	)
	if err != nil {
		slog.Info("passkey login failed",
			"request_id", requestid.From(c),
			"error", err,
		)
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "passkey login failed"})
		return
	}
	if matched == nil {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "passkey login failed"})
		return
	}

	account, err := s.db.User.Get(ctx, matched.UserID)
	if err != nil || account.Status != config.StatusActive {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "passkey login failed"})
		return
	}
	encoded, err := json.Marshal(credential)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}
	if err := s.db.Passkey.UpdateOneID(matched.ID).
		SetCredentialJSON(encoded).
		SetSignCount(int64(credential.Authenticator.SignCount)).
		SetCloneWarning(credential.Authenticator.CloneWarning).
		SetBackupState(credential.Flags.BackupState).
		SetUserPresent(credential.Flags.UserPresent).
		SetUserVerified(credential.Flags.UserVerified).
		SetLastUsedAt(time.Now()).
		Exec(ctx); err != nil {
		// The assertion itself is valid. Do not fail the login just because
		// the counter/last-used bookkeeping could not be persisted.
		slog.Warn("update passkey after login failed",
			"request_id", requestid.From(c),
			"passkey_id", matched.ID,
			"error", err,
		)
	}
	if credential.Authenticator.CloneWarning {
		slog.Warn("passkey signature counter did not advance",
			"request_id", requestid.From(c),
			"passkey_id", matched.ID,
			"user_id", account.ID,
		)
	}

	token, expires, err := s.tokens.issue(account.ID, account.Username, account.Role, account.TokenVersion)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}
	s.setCookie(c, token, expires)
	slog.Info("user login",
		"request_id", requestid.From(c),
		"username", account.Username,
		"result", "success",
		"method", "passkey",
	)
	web.JSON(c, http.StatusOK, s.sessionResponse(token, account))
}

// loadPasskeyUser loads one account and its credential records. A record that
// cannot be decoded is skipped (and logged) instead of locking the account out
// of every other passkey.
func (s *Service) loadPasskeyUser(ctx context.Context, id ident.UUID) (*passkeyUser, []*ent.Passkey, error) {
	account, err := s.db.User.Get(ctx, id)
	if err != nil {
		return nil, nil, err
	}
	u, rows, err := s.loadPasskeyUserWithAccount(ctx, account)
	if err != nil {
		return nil, nil, err
	}
	return u, rows, nil
}

func (s *Service) loadPasskeyUserWithAccount(ctx context.Context, account *ent.User) (*passkeyUser, []*ent.Passkey, error) {
	rows, err := s.db.Passkey.Query().
		Where(passkey.UserIDEQ(account.ID)).
		Order(ent.Asc(passkey.FieldCreatedAt)).
		All(ctx)
	if err != nil {
		return nil, nil, err
	}
	u := &passkeyUser{id: account.ID, username: account.Username}
	for _, row := range rows {
		var credential webauthn.Credential
		if err := json.Unmarshal(row.CredentialJSON, &credential); err != nil {
			slog.Warn("skip unreadable passkey record", "passkey_id", row.ID, "error", err)
			continue
		}
		u.credentials = append(u.credentials, credential)
	}
	return u, rows, nil
}

// newWebAuthn builds the relying party for this request. RPID/RPOrigins can be
// pinned by the operator; otherwise they follow the browser origin, which is
// what a same-origin deployment needs with no configuration at all.
func (s *Service) newWebAuthn(c *gin.Context) (*webauthn.WebAuthn, string, error) {
	origin, requestHost, err := requestOrigin(c)
	if err != nil {
		return nil, "", err
	}
	cfg := s.cfg.WebAuthn
	rpid := cfg.RPID
	if len(cfg.RPOrigins) > 0 {
		allowed := false
		for _, candidate := range cfg.RPOrigins {
			if strings.EqualFold(candidate, origin) {
				allowed = true
				break
			}
		}
		if !allowed {
			return nil, "", fmt.Errorf("passkey origin %q is not allowed", origin)
		}
	} else if !strings.EqualFold(originHost(origin), requestHost) {
		return nil, "", errors.New("passkey request origin does not match the server host")
	}
	if rpid == "" {
		rpid = originHost(origin)
	}
	if rpid == "" || !hostMatchesRPID(originHost(origin), rpid) {
		return nil, "", errors.New("passkey relying party id does not match the request origin")
	}
	wa, err := webauthn.New(&webauthn.Config{
		RPID:          rpid,
		RPDisplayName: cfg.RPDisplayName,
		RPOrigins:     []string{origin},
	})
	if err != nil {
		return nil, "", fmt.Errorf("passkey configuration is invalid: %w", err)
	}
	return wa, origin, nil
}

// requestOrigin resolves the browser origin. The Origin header is preferred
// (it is what the WebAuthn client data will contain); the request host is the
// fallback for non-browser clients and for same-origin requests that omit it.
func requestOrigin(c *gin.Context) (origin, requestHost string, err error) {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwarded := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwarded != "" {
		if first, _, _ := strings.Cut(forwarded, ","); strings.EqualFold(strings.TrimSpace(first), "https") {
			scheme = "https"
		}
	}
	requestHost = hostname(c.Request.Host)
	raw := strings.TrimSpace(c.GetHeader("Origin"))
	if raw == "" {
		raw = scheme + "://" + c.Request.Host
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
		return "", "", errors.New("invalid request origin")
	}
	// Origin is a serialized origin: no path, query or fragment.
	origin = u.Scheme + "://" + u.Host
	return origin, requestHost, nil
}

func originHost(origin string) string {
	u, err := url.Parse(origin)
	if err != nil {
		return ""
	}
	return strings.ToLower(u.Hostname())
}

func hostname(hostport string) string {
	if host, _, err := net.SplitHostPort(hostport); err == nil {
		return strings.ToLower(host)
	}
	return strings.ToLower(strings.Trim(hostport, "[]"))
}

// hostMatchesRPID implements the WebAuthn domain rule: the origin host must
// equal the RP ID or be a subdomain of it.
func hostMatchesRPID(host, rpid string) bool {
	host = strings.ToLower(host)
	rpid = strings.ToLower(rpid)
	return host == rpid || strings.HasSuffix(host, "."+rpid)
}

func (s *Service) storePasskeyCeremony(c *gin.Context, ceremony *passkeyCeremony) error {
	now := time.Now()
	key, err := randomCeremonyID()
	if err != nil {
		return errors.New("could not start passkey ceremony")
	}
	ceremony.expires = now.Add(passkeyCeremonyTTL)
	ceremony.session.Expires = ceremony.expires

	s.passkeyMu.Lock()
	// Opportunistic cleanup; the map only lives for five minutes per entry.
	for id, existing := range s.passkeyCeremonies {
		if now.After(existing.expires) {
			delete(s.passkeyCeremonies, id)
		}
	}
	if len(s.passkeyCeremonies) >= maxPasskeys {
		s.passkeyMu.Unlock()
		return errors.New("too many passkey ceremonies in progress")
	}
	s.passkeyCeremonies[key] = ceremony
	s.passkeyMu.Unlock()

	http.SetCookie(c.Writer, &http.Cookie{
		Name:     passkeyCookieName,
		Value:    key,
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   int(passkeyCeremonyTTL / time.Second),
		Expires:  ceremony.expires,
	})
	return nil
}

// takePasskeyCeremony consumes the ceremony exactly once. A failed finish
// therefore cannot be retried with the same challenge, and the cookie is
// cleared in every outcome.
func (s *Service) takePasskeyCeremony(c *gin.Context) (*passkeyCeremony, bool) {
	key, err := c.Cookie(passkeyCookieName)
	http.SetCookie(c.Writer, &http.Cookie{
		Name:     passkeyCookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		Secure:   s.cookieSecure(c),
		SameSite: http.SameSiteLaxMode,
		Expires:  time.Unix(1, 0),
		MaxAge:   -1,
	})
	if err != nil || key == "" {
		return nil, false
	}
	s.passkeyMu.Lock()
	ceremony, ok := s.passkeyCeremonies[key]
	delete(s.passkeyCeremonies, key)
	s.passkeyMu.Unlock()
	if !ok || time.Now().After(ceremony.expires) {
		return nil, false
	}
	return ceremony, true
}

func randomCeremonyID() (string, error) {
	var buf [32]byte
	if _, err := rand.Read(buf[:]); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(buf[:]), nil
}

func transportStrings(in []protocol.AuthenticatorTransport) []string {
	out := make([]string, 0, len(in))
	for _, transport := range in {
		out = append(out, string(transport))
	}
	return out
}

func truncateRunes(value string, limit int) string {
	if limit <= 0 || utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	return string(runes[:limit])
}
