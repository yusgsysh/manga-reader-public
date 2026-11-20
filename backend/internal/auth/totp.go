package auth

import (
	"bytes"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base32"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"image/png"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/pquerna/otp"
	"github.com/pquerna/otp/totp"

	"manga-reader/internal/config"
	"manga-reader/internal/ent"
	"manga-reader/internal/ent/user"
	"manga-reader/internal/ident"
	"manga-reader/internal/requestid"
	"manga-reader/internal/web"
)

const (
	// mfaTokenTTL is how long the password step remains usable while the user
	// fetches a code. It is much shorter than a session.
	mfaTokenTTL = 5 * time.Minute

	totpPeriod     = 30
	totpSkew       = 1
	totpDigits     = otp.DigitsSix
	recoveryCodes  = 10
	recoveryBytes  = 10 // 80 bits -> 16 base32 characters
	recoveryGroups = 4

	// A six-digit TOTP has only one million possibilities. Ten attempts may
	// burst, then one attempt per five seconds; combined with the five-minute
	// MFA token this keeps brute force impractical without locking out a user
	// who mistypes once or twice.
	totpRate  = 0.2
	totpBurst = 10.0
)

func deriveTOTPKey(jwtSecret string) []byte {
	sum := sha256.Sum256([]byte("manga-reader/totp/v1\x00" + jwtSecret))
	return sum[:]
}

func (s *Service) sealTOTPSecret(secret string) (string, error) {
	block, err := aes.NewCipher(s.totpKey)
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
	sealed := gcm.Seal(nonce, nonce, []byte(secret), nil)
	return base64.RawURLEncoding.EncodeToString(sealed), nil
}

func (s *Service) openTOTPSecret(encoded string) (string, error) {
	raw, err := base64.RawURLEncoding.DecodeString(encoded)
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(s.totpKey)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("totp secret ciphertext is truncated")
	}
	nonce, ciphertext := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}

func validTOTPCode(secret, code string) bool {
	code = strings.TrimSpace(code)
	if len(code) != totpDigits.Length() {
		return false
	}
	for _, r := range code {
		if r < '0' || r > '9' {
			return false
		}
	}
	ok, err := totp.ValidateCustom(code, secret, time.Now().UTC(), totp.ValidateOpts{
		Period:    totpPeriod,
		Skew:      totpSkew,
		Digits:    totpDigits,
		Algorithm: otp.AlgorithmSHA1,
	})
	return err == nil && ok
}

func (s *Service) recoveryHash(code string) string {
	key := sha256.Sum256([]byte("manga-reader/totp-recovery/v1\x00" + s.cfg.JWTSecret))
	mac := hmac.New(sha256.New, key[:])
	mac.Write([]byte(normalizeRecoveryCode(code)))
	return hex.EncodeToString(mac.Sum(nil))
}

func normalizeRecoveryCode(code string) string {
	code = strings.ToUpper(strings.TrimSpace(code))
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 'A' && r <= 'Z', r >= '0' && r <= '9':
			return r
		default:
			return -1
		}
	}, code)
}

// newRecoveryCodes returns the display form (groups separated by dashes) and
// the stored hashes. Only the display form is ever sent to the user, once.
func (s *Service) newRecoveryCodes() ([]string, []string, error) {
	plain := make([]string, 0, recoveryCodes)
	hashes := make([]string, 0, recoveryCodes)
	encoding := base32.StdEncoding.WithPadding(base32.NoPadding)
	for range recoveryCodes {
		buf := make([]byte, recoveryBytes)
		if _, err := rand.Read(buf); err != nil {
			return nil, nil, err
		}
		raw := encoding.EncodeToString(buf)
		groups := make([]string, 0, recoveryGroups)
		for i := 0; i < len(raw); i += 4 {
			groups = append(groups, raw[i:i+4])
		}
		code := strings.Join(groups, "-")
		plain = append(plain, code)
		hashes = append(hashes, s.recoveryHash(code))
	}
	return plain, hashes, nil
}

type totpStatusJSON struct {
	Enabled                bool    `json:"enabled"`
	ConfirmedAt            *string `json:"confirmed_at"`
	RecoveryCodesRemaining int     `json:"recovery_codes_remaining"`
}

func totpStatus(u *ent.User) totpStatusJSON {
	var confirmed *string
	if u.TotpConfirmedAt != nil {
		value := u.TotpConfirmedAt.UTC().Format(time.RFC3339)
		confirmed = &value
	}
	return totpStatusJSON{
		Enabled:                u.TotpEnabled,
		ConfirmedAt:            confirmed,
		RecoveryCodesRemaining: len(u.RecoveryCodeHashes),
	}
}

func (s *Service) handleTOTPStatus(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	u, err := s.db.User.Get(c.Request.Context(), principal.ID)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read totp status failed"})
		return
	}
	web.JSON(c, http.StatusOK, totpStatus(u))
}

func (s *Service) handleTOTPSetup(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	ctx := c.Request.Context()
	u, err := s.db.User.Get(ctx, principal.ID)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read user failed"})
		return
	}
	if u.TotpEnabled {
		web.JSON(c, http.StatusConflict, gin.H{"error": "two-factor authentication is already enabled"})
		return
	}
	key, err := totp.Generate(totp.GenerateOpts{
		Issuer:      s.cfg.TOTPIssuer,
		AccountName: u.Username,
		Period:      totpPeriod,
		Digits:      totpDigits,
		Algorithm:   otp.AlgorithmSHA1,
	})
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "generate totp secret failed"})
		return
	}
	sealed, err := s.sealTOTPSecret(key.Secret())
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "store totp secret failed"})
		return
	}
	if err := s.db.User.UpdateOneID(u.ID).
		SetTotpSecretCiphertext(sealed).
		SetTotpEnabled(false).
		ClearTotpConfirmedAt().
		SetRecoveryCodeHashes([]string{}).
		Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "store totp secret failed"})
		return
	}

	// The QR image embeds the same secret as otpauth_url; it is generated
	// server-side so the secret never needs a third-party QR service.
	qrURL := ""
	if img, err := key.Image(240, 240); err == nil {
		var buf bytes.Buffer
		if err := png.Encode(&buf, img); err == nil {
			qrURL = "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
		}
	}
	web.JSON(c, http.StatusOK, gin.H{
		"secret":       key.Secret(),
		"otpauth_url":  key.URL(),
		"qr_data_url":  qrURL,
		"issuer":       s.cfg.TOTPIssuer,
		"account_name": u.Username,
	})
}

func (s *Service) handleTOTPEnable(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var body struct {
		Code string `json:"code"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	ctx := c.Request.Context()
	u, err := s.db.User.Get(ctx, principal.ID)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read user failed"})
		return
	}
	if u.TotpEnabled {
		web.JSON(c, http.StatusConflict, gin.H{"error": "two-factor authentication is already enabled"})
		return
	}
	if u.TotpSecretCiphertext == "" {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "start totp setup first"})
		return
	}
	secret, err := s.openTOTPSecret(u.TotpSecretCiphertext)
	if err != nil {
		slog.Error("open totp secret failed", "request_id", requestid.From(c), "user_id", u.ID, "error", err)
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read totp secret failed"})
		return
	}
	if !validTOTPCode(secret, body.Code) {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid verification code"})
		return
	}

	plain, hashes, err := s.newRecoveryCodes()
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "generate recovery codes failed"})
		return
	}
	now := time.Now()
	if err := s.db.User.UpdateOneID(u.ID).
		SetTotpEnabled(true).
		SetTotpConfirmedAt(now).
		SetRecoveryCodeHashes(hashes).
		Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "enable totp failed"})
		return
	}
	slog.Info("totp enabled", "request_id", requestid.From(c), "actor_user_id", u.ID)
	web.JSON(c, http.StatusOK, gin.H{
		"enabled":        true,
		"confirmed_at":   now.UTC().Format(time.RFC3339),
		"recovery_codes": plain,
	})
}

func (s *Service) handleTOTPDisable(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	ctx := c.Request.Context()
	u, err := s.db.User.Get(ctx, principal.ID)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read user failed"})
		return
	}
	if !VerifyPassword(u.PasswordHash, body.Password) {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid password"})
		return
	}
	// Disabling is a security downgrade, so an enabled second factor must be
	// presented too (TOTP code or one recovery code). A pending, unconfirmed
	// secret can be discarded with the password alone.
	if u.TotpEnabled {
		secret, err := s.openTOTPSecret(u.TotpSecretCiphertext)
		if err != nil {
			web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read totp secret failed"})
			return
		}
		if !validTOTPCode(secret, body.Code) {
			used, err := s.consumeRecoveryCode(ctx, u.ID, body.Code)
			if err != nil || !used {
				web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid password or verification code"})
				return
			}
		}
	}
	if err := s.db.User.UpdateOneID(u.ID).
		SetTotpSecretCiphertext("").
		SetTotpEnabled(false).
		ClearTotpConfirmedAt().
		SetRecoveryCodeHashes([]string{}).
		Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "disable totp failed"})
		return
	}
	slog.Info("totp disabled", "request_id", requestid.From(c), "actor_user_id", u.ID)
	web.JSON(c, http.StatusOK, gin.H{"enabled": false})
}

func (s *Service) handleTOTPRegenerateRecoveryCodes(c *gin.Context) {
	principal, ok := PrincipalFrom(c)
	if !ok {
		web.AbortWithStatusJSON(c, http.StatusUnauthorized, gin.H{"error": "unauthorized"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var body struct {
		Password string `json:"password"`
		Code     string `json:"code"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	ctx := c.Request.Context()
	u, err := s.db.User.Get(ctx, principal.ID)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read user failed"})
		return
	}
	if !u.TotpEnabled || u.TotpSecretCiphertext == "" {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "two-factor authentication is not enabled"})
		return
	}
	if !VerifyPassword(u.PasswordHash, body.Password) {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid password"})
		return
	}
	secret, err := s.openTOTPSecret(u.TotpSecretCiphertext)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "read totp secret failed"})
		return
	}
	if !validTOTPCode(secret, body.Code) {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid verification code"})
		return
	}
	plain, hashes, err := s.newRecoveryCodes()
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "generate recovery codes failed"})
		return
	}
	if err := s.db.User.UpdateOneID(u.ID).
		SetRecoveryCodeHashes(hashes).
		Exec(ctx); err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "save recovery codes failed"})
		return
	}
	slog.Info("totp recovery codes regenerated", "request_id", requestid.From(c), "actor_user_id", u.ID)
	web.JSON(c, http.StatusOK, gin.H{"recovery_codes": plain})
}

func (s *Service) handleTOTPVerify(c *gin.Context) {
	if !s.allowTOTP(c.ClientIP()) {
		c.Header("Retry-After", "30")
		web.JSON(c, http.StatusTooManyRequests, gin.H{"error": "too many verification attempts"})
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, 8<<10)
	var body struct {
		MFAToken string `json:"mfa_token"`
		Code     string `json:"code"`
	}
	if err := web.BindJSON(c, &body); err != nil {
		web.JSON(c, http.StatusBadRequest, gin.H{"error": "invalid request body"})
		return
	}
	claims, err := s.mfaTokens.parse(strings.TrimSpace(body.MFAToken))
	if err != nil {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid or expired verification token"})
		return
	}
	uid, err := ident.Parse(claims.Subject)
	if err != nil || uid.IsNil() {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid or expired verification token"})
		return
	}
	ctx := c.Request.Context()
	u, err := s.db.User.Query().
		Where(user.IDEQ(uid), user.StatusEQ(config.StatusActive)).
		Only(ctx)
	if err != nil || u.TokenVersion != claims.TokenVersion || !u.TotpEnabled || u.TotpSecretCiphertext == "" {
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid or expired verification token"})
		return
	}
	secret, err := s.openTOTPSecret(u.TotpSecretCiphertext)
	if err != nil {
		slog.Error("open totp secret failed", "request_id", requestid.From(c), "user_id", u.ID, "error", err)
		web.JSON(c, http.StatusUnauthorized, gin.H{"error": "verification failed"})
		return
	}

	method := ""
	if validTOTPCode(secret, body.Code) {
		method = "totp"
	} else {
		used, err := s.consumeRecoveryCode(ctx, u.ID, body.Code)
		if err != nil {
			slog.Error("consume recovery code failed", "request_id", requestid.From(c), "user_id", u.ID, "error", err)
			web.JSON(c, http.StatusUnauthorized, gin.H{"error": "verification failed"})
			return
		}
		if !used {
			web.JSON(c, http.StatusUnauthorized, gin.H{"error": "invalid verification code"})
			return
		}
		method = "recovery_code"
	}

	token, expires, err := s.tokens.issue(u.ID, u.Username, u.Role, u.TokenVersion)
	if err != nil {
		web.JSON(c, http.StatusInternalServerError, gin.H{"error": "login failed"})
		return
	}
	s.setCookie(c, token, expires)
	slog.Info("user login",
		"request_id", requestid.From(c),
		"username", u.Username,
		"result", "success",
		"method", method,
	)
	web.JSON(c, http.StatusOK, s.sessionResponse(token, u))
}

// consumeRecoveryCode atomically verifies and removes one unused code. The
// mutex covers the read-modify-write so two concurrent requests cannot redeem
// the same code.
func (s *Service) consumeRecoveryCode(ctx context.Context, userID ident.UUID, code string) (bool, error) {
	normalized := normalizeRecoveryCode(code)
	if len(normalized) != recoveryBytes*8/5 {
		return false, nil
	}
	want := s.recoveryHash(normalized)

	s.totpMu.Lock()
	defer s.totpMu.Unlock()

	u, err := s.db.User.Get(ctx, userID)
	if err != nil {
		return false, err
	}
	matched := -1
	for i, stored := range u.RecoveryCodeHashes {
		if subtle.ConstantTimeCompare([]byte(stored), []byte(want)) == 1 {
			matched = i
		}
	}
	if matched < 0 {
		return false, nil
	}
	remaining := make([]string, 0, len(u.RecoveryCodeHashes)-1)
	remaining = append(remaining, u.RecoveryCodeHashes[:matched]...)
	remaining = append(remaining, u.RecoveryCodeHashes[matched+1:]...)
	if err := s.db.User.UpdateOneID(userID).SetRecoveryCodeHashes(remaining).Exec(ctx); err != nil {
		return false, err
	}
	return true, nil
}
