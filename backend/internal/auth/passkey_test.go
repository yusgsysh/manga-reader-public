package auth

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"

	"manga-reader/internal/config"
	"manga-reader/internal/ent/passkey"
)

func TestPasskeyLoginBeginIsPublicAndSetsCeremonyCookie(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)

	w := doJSON(r, http.MethodPost, "/api/auth/passkey/login/begin", "", "")
	if w.Code != http.StatusOK {
		t.Fatalf("begin = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var res struct {
		PublicKey struct {
			Challenge string `json:"challenge"`
			RPID      string `json:"rpId"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode begin: %v", err)
	}
	if res.PublicKey.Challenge == "" {
		t.Fatal("challenge is empty")
	}
	if res.PublicKey.RPID != "example.com" {
		t.Fatalf("rpId = %q, want request host", res.PublicKey.RPID)
	}
	cookie := findCookie(w, passkeyCookieName)
	if cookie == nil || !cookie.HttpOnly || cookie.Value == "" {
		t.Fatalf("ceremony cookie = %+v", cookie)
	}
	if cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("SameSite = %v, want Lax", cookie.SameSite)
	}
}

func TestPasskeyLoginBeginRejectsMismatchedOrigin(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)

	req := httptest.NewRequest(http.MethodPost, "/api/auth/passkey/login/begin", nil)
	req.Header.Set("Origin", "https://evil.example")
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("begin with foreign origin = %d, want 400 (%s)", w.Code, w.Body.String())
	}
}

func TestPasskeyLoginFinishWithoutCeremonyIsUnauthorized(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)

	w := doJSON(r, http.MethodPost, "/api/auth/passkey/login/finish", `{}`, "")
	if w.Code != http.StatusUnauthorized {
		t.Fatalf("finish without ceremony = %d, want 401 (%s)", w.Code, w.Body.String())
	}
}

func TestPasskeyRegisterRequiresSessionAndCeremony(t *testing.T) {
	svc, _ := newTestService(t)
	r := newTestRouter(svc)

	if w := doJSON(r, http.MethodPost, "/api/auth/passkeys/register/begin", "", ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("begin without session = %d, want 401", w.Code)
	}
	if w := doJSON(r, http.MethodPost, "/api/auth/passkeys/register/finish", `{}`, ""); w.Code != http.StatusUnauthorized {
		t.Fatalf("finish without session = %d, want 401", w.Code)
	}

	token, _ := login(t, r, "admin", "password123")
	w := doJSON(r, http.MethodPost, "/api/auth/passkeys/register/begin", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("begin = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var res struct {
		PublicKey struct {
			Challenge              string `json:"challenge"`
			AuthenticatorSelection struct {
				ResidentKey string `json:"residentKey"`
			} `json:"authenticatorSelection"`
			User struct {
				ID string `json:"id"`
			} `json:"user"`
		} `json:"publicKey"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &res); err != nil {
		t.Fatalf("decode begin: %v", err)
	}
	if res.PublicKey.Challenge == "" || res.PublicKey.User.ID == "" {
		t.Fatalf("incomplete creation options: %s", w.Body.String())
	}
	if res.PublicKey.AuthenticatorSelection.ResidentKey != "required" {
		t.Fatalf("residentKey = %q, want required (discoverable passkey)", res.PublicKey.AuthenticatorSelection.ResidentKey)
	}
	if findCookie(w, passkeyCookieName) == nil {
		t.Fatal("register begin did not set a ceremony cookie")
	}

	if w := doJSON(r, http.MethodPost, "/api/auth/passkeys/register/finish", `{}`, token); w.Code != http.StatusBadRequest {
		t.Fatalf("finish without ceremony = %d, want 400 (%s)", w.Code, w.Body.String())
	}
}

func TestPasskeyListRenameAndDeleteAreUserScoped(t *testing.T) {
	svc, client := newTestService(t)
	r := newTestRouter(svc)
	admin, err := client.User.Query().Only(t.Context())
	if err != nil {
		t.Fatalf("load admin: %v", err)
	}
	other, err := client.User.Create().
		SetUsername("other").
		SetPasswordHash("not-used").
		SetRole(config.RoleUser).
		SetStatus(config.StatusActive).
		Save(t.Context())
	if err != nil {
		t.Fatalf("create other user: %v", err)
	}
	mine, err := client.Passkey.Create().
		SetUserID(admin.ID).
		SetCredentialID([]byte("mine")).
		SetPublicKey([]byte("pk")).
		SetCredentialJSON([]byte("{}")).
		SetName("Laptop").
		Save(t.Context())
	if err != nil {
		t.Fatalf("create passkey: %v", err)
	}
	theirs, err := client.Passkey.Create().
		SetUserID(other.ID).
		SetCredentialID([]byte("theirs")).
		SetPublicKey([]byte("pk")).
		SetCredentialJSON([]byte("{}")).
		SetName("Phone").
		Save(t.Context())
	if err != nil {
		t.Fatalf("create other passkey: %v", err)
	}

	token, _ := login(t, r, "admin", "password123")

	w := doJSON(r, http.MethodGet, "/api/auth/passkeys", "", token)
	if w.Code != http.StatusOK {
		t.Fatalf("list = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	var list struct {
		Passkeys []passkeyJSON `json:"passkeys"`
	}
	if err := json.Unmarshal(w.Body.Bytes(), &list); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(list.Passkeys) != 1 || list.Passkeys[0].ID != mine.ID {
		t.Fatalf("list = %+v, want only the caller's passkey", list.Passkeys)
	}

	w = doJSON(r, http.MethodPatch, "/api/auth/passkeys/"+mine.ID.String(), `{"name":"Desktop"}`, token)
	if w.Code != http.StatusOK {
		t.Fatalf("rename = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	updated, err := client.Passkey.Get(t.Context(), mine.ID)
	if err != nil || updated.Name != "Desktop" {
		t.Fatalf("renamed passkey = %+v, err=%v", updated, err)
	}

	// Another account's credential is indistinguishable from a missing one.
	if w := doJSON(r, http.MethodPatch, "/api/auth/passkeys/"+theirs.ID.String(), `{"name":"Hijacked"}`, token); w.Code != http.StatusNotFound {
		t.Fatalf("rename other user's passkey = %d, want 404", w.Code)
	}
	if w := doJSON(r, http.MethodDelete, "/api/auth/passkeys/"+theirs.ID.String(), "", token); w.Code != http.StatusNotFound {
		t.Fatalf("delete other user's passkey = %d, want 404", w.Code)
	}

	if w := doJSON(r, http.MethodDelete, "/api/auth/passkeys/"+mine.ID.String(), "", token); w.Code != http.StatusOK {
		t.Fatalf("delete = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	if n, err := client.Passkey.Query().Where(passkey.IDEQ(mine.ID)).Count(t.Context()); err != nil || n != 0 {
		t.Fatalf("deleted passkey still present: n=%d err=%v", n, err)
	}
}

func TestPasskeyRenameValidatesName(t *testing.T) {
	svc, client := newTestService(t)
	r := newTestRouter(svc)
	admin, err := client.User.Query().Only(t.Context())
	if err != nil {
		t.Fatalf("load admin: %v", err)
	}
	row, err := client.Passkey.Create().
		SetUserID(admin.ID).
		SetCredentialID([]byte("id")).
		SetPublicKey([]byte("pk")).
		SetCredentialJSON([]byte("{}")).
		Save(t.Context())
	if err != nil {
		t.Fatalf("create passkey: %v", err)
	}
	token, _ := login(t, r, "admin", "password123")

	if w := doJSON(r, http.MethodPatch, "/api/auth/passkeys/"+row.ID.String(), `{"name":"   "}`, token); w.Code != http.StatusBadRequest {
		t.Fatalf("blank name = %d, want 400", w.Code)
	}
	long := strings.Repeat("设", 100)
	if w := doJSON(r, http.MethodPatch, "/api/auth/passkeys/"+row.ID.String(), `{"name":"`+long+`"}`, token); w.Code != http.StatusOK {
		t.Fatalf("long name = %d, want 200 (%s)", w.Code, w.Body.String())
	}
	updated, err := client.Passkey.Get(t.Context(), row.ID)
	if err != nil {
		t.Fatalf("load passkey: %v", err)
	}
	if got := len([]rune(updated.Name)); got != 64 {
		t.Fatalf("name length = %d runes, want 64", got)
	}
}

func findCookie(w *httptest.ResponseRecorder, name string) *http.Cookie {
	for _, cookie := range w.Result().Cookies() {
		if cookie.Name == name {
			return cookie
		}
	}
	return nil
}

// Keep gin's test mode set for the package even when this file's tests run
// before auth_test.go's router helper.
func init() {
	gin.SetMode(gin.TestMode)
}
