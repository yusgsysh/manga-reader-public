package handler

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	json "encoding/json/v2"

	"manga-reader/internal/database"
	"manga-reader/internal/ent/tagpreference"
	"manga-reader/internal/model"
)

func TestHandleTagSettingsGet_Empty(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/tag-settings", server.handleTagSettingsGet)

	req := httptest.NewRequest("GET", "/api/tag-settings", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}

	var resp model.TagSettingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Tags) != 0 {
		t.Errorf("tags = %v, want empty", resp.Tags)
	}
}

func TestHandleTagSettingsRoundTrip(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}

	r.GET("/api/tag-settings", server.handleTagSettingsGet)
	r.PUT("/api/tag-settings", server.handleTagSettingsPut)

	body := `{"tags":[` +
		`{"tag":"female:yuri","action":"like"},` +
		`{"tag":"tag:netorare","action":"dislike"},` +
		`{"tag":"artist:someone","action":"like"}` +
		`]}`
	req := httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("put status = %d, body = %s", w.Code, w.Body.String())
	}

	get := httptest.NewRequest("GET", "/api/tag-settings", nil)
	gw := httptest.NewRecorder()
	r.ServeHTTP(gw, get)

	if gw.Code != http.StatusOK {
		t.Fatalf("get status = %d", gw.Code)
	}
	var resp model.TagSettingsResponse
	if err := json.Unmarshal(gw.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Tags) != 3 {
		t.Fatalf("tags = %v, want 3 entries", resp.Tags)
	}
	// Ordered by tag asc: artist:someone, female:yuri, tag:netorare.
	wantOrder := []string{"artist:someone", "female:yuri", "tag:netorare"}
	for i, want := range wantOrder {
		if resp.Tags[i].Tag != want {
			t.Errorf("tags[%d].tag = %q, want %q", i, resp.Tags[i].Tag, want)
		}
	}
	if resp.Tags[1].Action != model.TagActionLike {
		t.Errorf("female:yuri action = %q, want like", resp.Tags[1].Action)
	}
	if resp.Tags[2].Action != model.TagActionDislike {
		t.Errorf("tag:netorare action = %q, want dislike", resp.Tags[2].Action)
	}
}

func TestHandleTagSettingsPut_ReplacesFullList(t *testing.T) {
	client := newTestDB(t)
	// Seed a stale row that the PUT should wipe.
	client.TagPreference.Create().SetTag("stale:tag").SetAction("like").Save(t.Context())

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	r.PUT("/api/tag-settings", server.handleTagSettingsPut)

	req := httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(`{"tags":[{"tag":"female:yuri","action":"dislike"}]}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	count, err := client.TagPreference.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 1 {
		t.Errorf("count = %d, want 1", count)
	}
	exists, err := client.TagPreference.Query().Where(tagpreference.Tag("stale:tag")).Exist(t.Context())
	if err != nil {
		t.Fatalf("exist: %v", err)
	}
	if exists {
		t.Error("stale:tag still present after full replace")
	}
}

func TestHandleTagSettingsPut_NormalizesAndDedupes(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	r.PUT("/api/tag-settings", server.handleTagSettingsPut)

	// Uppercase + surrounding whitespace get trimmed/lowercased; the duplicate
	// female:yuri keeps the last action (dislike).
	body := `{"tags":[` +
		`{"tag":"  Female:Yuri ","action":"like"},` +
		`{"tag":"female:yuri","action":"dislike"}` +
		`]}`
	req := httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(body))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	var resp model.TagSettingsResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Tags) != 1 {
		t.Fatalf("tags = %v, want 1 entry", resp.Tags)
	}
	if resp.Tags[0].Tag != "female:yuri" {
		t.Errorf("tag = %q, want female:yuri", resp.Tags[0].Tag)
	}
	if resp.Tags[0].Action != model.TagActionDislike {
		t.Errorf("action = %q, want dislike (last wins)", resp.Tags[0].Action)
	}
}

func TestHandleTagSettingsPut_Invalid(t *testing.T) {
	client := newTestDB(t)
	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	r.PUT("/api/tag-settings", server.handleTagSettingsPut)

	cases := []struct {
		name string
		body string
	}{
		{"empty tag", `{"tags":[{"tag":"","action":"like"}]}`},
		{"invalid action", `{"tags":[{"tag":"female:yuri","action":"meh"}]}`},
		{"bad json", `not-json`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(tc.body))
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d; body = %s", w.Code, http.StatusBadRequest, w.Body.String())
			}
			if !strings.Contains(w.Body.String(), "error") {
				t.Errorf("body = %s, want error field", w.Body.String())
			}
		})
	}

	// Nothing should have been persisted.
	count, err := client.TagPreference.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0 after invalid PUTs", count)
	}
}

func TestHandleTagSettingsPut_EmptyListClearsAll(t *testing.T) {
	client := newTestDB(t)
	client.TagPreference.Create().SetTag("female:yuri").SetAction("like").Save(t.Context())
	client.TagPreference.Create().SetTag("tag:netorare").SetAction("dislike").Save(t.Context())

	r := setupTestRouter()
	server := &Server{DB: &database.DB{Client: client}}
	r.PUT("/api/tag-settings", server.handleTagSettingsPut)

	req := httptest.NewRequest("PUT", "/api/tag-settings", bytes.NewBufferString(`{"tags":[]}`))
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}

	count, err := client.TagPreference.Query().Count(t.Context())
	if err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Errorf("count = %d, want 0", count)
	}
}
