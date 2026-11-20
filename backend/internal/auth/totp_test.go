package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/pquerna/otp/totp"
)

type totpSetupResponse struct {
	Secret    string `json:"secret"`
	URL       string `json:"otpauth_url"`
	QRDataURL string `json:"qr_data_url"`
}

type totpEnableResponse struct {
	Enabled       bool     `json:"enabled"`
	RecoveryCodes []string `json:"recovery_codes"`
}

type mfaChallengeResponse struct {
	MFARequired bool   `json:"mfa_required"`
	MFAToken    string `json:"mfa_token"`
	ExpiresIn   int    `json:"expires_in"`
	Method      string `json:"method"`
}

func enableTOTP(t *testing.T, r http.Handler, token string) (string, []string) {
	t.Helper()
	w := doJSON(r, http.MethodPost, "/api/auth/totp/setup", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("setup = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var setup totpSetupResponse
	if err := json.Unmarshal(w.Body.Bytes(), &setup); err != nil {
		t.Fatalf("decode setup: %v", err)
	}
	if setup.Secret == "" || setup.URL == "" || !strings.HasPrefix(setup.QRDataURL, "data:image/png;base64,") {
		t.Fatalf("incomplete setup response: %+v", setup)
	}
	code, err := totp.GenerateCode(setup.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	body, _ := json.Marshal(map[string]string{"code": code})
	w = doJSON(r, http.MethodPost, "/api/auth/totp/enable", string(body), token)
	if w.Code != http.StatusOK {
		t.Fatalf("enable = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var enabled totpEnableResponse
	if err := json.Unmarshal(w.Body.Bytes(), &enabled); err != nil {
		t.Fatalf("decode enable: %v", err)
	}
	if !enabled.Enabled || len(enabled.RecoveryCodes) != recoveryCodes {
		t.Fatalf("enable response = %+v", enabled)
	}
	return setup.Secret, enabled.RecoveryCodes
}

func passwordLogin(t *testing.T, r http.Handler) (*mfaChallengeResponse, *http.Response) {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"username": "admin", "password": "password123"})
	w := doJSON(r, http.MethodPost, "/api/auth/login", string(body), "")
	if w.Code != http.StatusOK {
		t.Fatalf("login = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var challenge mfaChallengeResponse
	if err := json.Unmarshal(w.Body.Bytes(), &challenge); err != nil {
		t.Fatalf("decode login: %v", err)
	}
	return &challenge, w.Result()
}

func verifyTOTP(t *testing.T, r http.Handler, mfaToken, code string) *httptest.ResponseRecorder {
	t.Helper()
	body, _ := json.Marshal(map[string]string{"mfa_token": mfaToken, "code": code})
	return doJSON(r, http.MethodPost, "/api/auth/totp/verify", string(body), "")
}

func TestTOTPEnrollmentAndLogin(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)
	token, _ := login(t, r, "admin", "password123")
	if token == "" {
		t.Fatal("initial login failed")
	}

	w := doJSON(r, http.MethodGet, "/api/auth/totp", "", token)
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"enabled":false`) {
		t.Fatalf("status before enable = %d %s", w.Code, w.Body.String())
	}
	if w := doJSON(r, http.MethodPost, "/api/auth/totp/setup", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("setup without session = %d, want 401", w.Code)
	}

	secret, recoveryCodes := enableTOTP(t, r, token)
	w = doJSON(r, http.MethodGet, "/api/auth/totp", "", token)
	var status totpStatusJSON
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if !status.Enabled || status.RecoveryCodesRemaining != 10 {
		t.Fatalf("status after enable = %+v", status)
	}

	challenge, result := passwordLogin(t, r)
	if !challenge.MFARequired || challenge.MFAToken == "" || challenge.ExpiresIn <= 0 || challenge.Method != "totp" {
		t.Fatalf("challenge = %+v", challenge)
	}
	for _, cookie := range result.Cookies() {
		if cookie.Name == CookieName {
			t.Fatal("password step must not set a session cookie when TOTP is enabled")
		}
	}
	// The step-up token is signed for a different issuer and must not pass the
	// default-deny middleware as an access token.
	if got := doJSON(r, http.MethodGet, "/api/bookshelf", "", challenge.MFAToken); got.Code != http.StatusUnauthorized {
		t.Fatalf("mfa token on protected route = %d, want 401", got.Code)
	}

	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	wrong := "0" + code[1:]
	if wrong == code {
		wrong = "1" + code[1:]
	}
	if bad := verifyTOTP(t, r, challenge.MFAToken, wrong); bad.Code == http.StatusOK {
		t.Fatal("wrong code was accepted")
	}
	ok := verifyTOTP(t, r, challenge.MFAToken, code)
	if ok.Code != http.StatusOK {
		t.Fatalf("verify = %d, want 200 (%s)", ok.Code, ok.Body.String())
	}
	var session struct {
		AccessToken string `json:"access_token"`
		User        struct {
			Username string `json:"username"`
		} `json:"user"`
	}
	if err := json.Unmarshal(ok.Body.Bytes(), &session); err != nil || session.AccessToken == "" || session.User.Username != "admin" {
		t.Fatalf("verify response = %s", ok.Body.String())
	}
	if got := doJSON(r, http.MethodGet, "/api/bookshelf", "", session.AccessToken); got.Code != http.StatusOK {
		t.Fatalf("session from totp = %d, want 200", got.Code)
	}
	if len(recoveryCodes) != 10 {
		t.Fatalf("recovery codes = %d", len(recoveryCodes))
	}
}

func TestTOTPRecoveryCodeIsSingleUse(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)
	token, _ := login(t, r, "admin", "password123")
	_, codes := enableTOTP(t, r, token)

	challenge, _ := passwordLogin(t, r)
	first := verifyTOTP(t, r, challenge.MFAToken, codes[0])
	if first.Code != http.StatusOK {
		t.Fatalf("first recovery code = %d, want 200 (%s)", first.Code, first.Body.String())
	}

	challenge, _ = passwordLogin(t, r)
	reused := verifyTOTP(t, r, challenge.MFAToken, codes[0])
	if reused.Code != http.StatusUnauthorized {
		t.Fatalf("reused recovery code = %d, want 401", reused.Code)
	}
	second := verifyTOTP(t, r, challenge.MFAToken, codes[1])
	if second.Code != http.StatusOK {
		t.Fatalf("second recovery code = %d, want 200 (%s)", second.Code, second.Body.String())
	}

	w := doJSON(r, http.MethodGet, "/api/auth/totp", "", token)
	var status totpStatusJSON
	if err := json.Unmarshal(w.Body.Bytes(), &status); err != nil {
		t.Fatalf("decode status: %v", err)
	}
	if status.RecoveryCodesRemaining != 8 {
		t.Fatalf("remaining recovery codes = %d, want 8", status.RecoveryCodesRemaining)
	}
}

func TestTOTPDisableRequiresPasswordAndRestoresPasswordLogin(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)
	token, _ := login(t, r, "admin", "password123")
	secret, _ := enableTOTP(t, r, token)

	body, _ := json.Marshal(map[string]string{"password": "wrong-password"})
	if w := doJSON(r, http.MethodPost, "/api/auth/totp/disable", string(body), token); w.Code != http.StatusUnauthorized {
		t.Fatalf("disable with wrong password = %d, want 401", w.Code)
	}
	body, _ = json.Marshal(map[string]string{"password": "password123"})
	if w := doJSON(r, http.MethodPost, "/api/auth/totp/disable", string(body), token); w.Code != http.StatusUnauthorized {
		t.Fatalf("disable without code = %d, want 401", w.Code)
	}
	code, err := totp.GenerateCode(secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	body, _ = json.Marshal(map[string]string{"password": "password123", "code": code})
	w := doJSON(r, http.MethodPost, "/api/auth/totp/disable", string(body), token)
	if w.Code != http.StatusOK {
		t.Fatalf("disable = %d, want 200 (%s)", w.Code, w.Body.String())
	}

	// A fresh password login is complete again.
	body, _ = json.Marshal(map[string]string{"username": "admin", "password": "password123"})
	w = doJSON(r, http.MethodPost, "/api/auth/login", string(body), "")
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"access_token"`) {
		t.Fatalf("login after disable = %d %s", w.Code, w.Body.String())
	}
	// The old pending secret and recovery hashes are gone.
	row, err := svc.db.User.Query().Only(t.Context())
	if err != nil {
		t.Fatalf("load user: %v", err)
	}
	if row.TotpEnabled || row.TotpSecretCiphertext != "" || len(row.RecoveryCodeHashes) != 0 {
		t.Fatalf("totp fields after disable = enabled:%v secret:%q recovery:%d", row.TotpEnabled, row.TotpSecretCiphertext, len(row.RecoveryCodeHashes))
	}
}

func TestTOTPEnableRejectsWrongCodeAndSetupOverwritesPendingSecret(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)
	token, _ := login(t, r, "admin", "password123")

	w := doJSON(r, http.MethodPost, "/api/auth/totp/setup", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("setup = %d", w.Code)
	}
	var first totpSetupResponse
	if err := json.Unmarshal(w.Body.Bytes(), &first); err != nil {
		t.Fatalf("decode setup: %v", err)
	}
	w = doJSON(r, http.MethodPost, "/api/auth/totp/setup", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("second setup = %d", w.Code)
	}
	var second totpSetupResponse
	if err := json.Unmarshal(w.Body.Bytes(), &second); err != nil {
		t.Fatalf("decode second setup: %v", err)
	}
	if first.Secret == second.Secret {
		t.Fatal("setup did not rotate the pending secret")
	}
	code, err := totp.GenerateCode(second.Secret, time.Now())
	if err != nil {
		t.Fatalf("generate code: %v", err)
	}
	wrong := "0" + code[1:]
	if wrong == code {
		wrong = "1" + code[1:]
	}
	body, _ := json.Marshal(map[string]string{"code": wrong})
	if w := doJSON(r, http.MethodPost, "/api/auth/totp/enable", string(body), token); w.Code != http.StatusUnauthorized {
		t.Fatalf("enable with wrong code = %d, want 401", w.Code)
	}
	body, _ = json.Marshal(map[string]string{"code": code})
	if w := doJSON(r, http.MethodPost, "/api/auth/totp/enable", string(body), token); w.Code != http.StatusOK {
		t.Fatalf("enable with latest secret = %d, want 200 (%s)", w.Code, w.Body.String())
	}
}
