package handler

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"manga-reader/internal/database"
)

func TestDevUpstreamDownToggle(t *testing.T) {
	mockServer := newMockServer(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, mockGalleryDetailHTML(12345, "Dev", 3))
	})
	defer mockServer.Close()

	client := newTestDB(t)
	server := &Server{
		Client:   newMockClient(mockServer.URL),
		DB:       &database.DB{Client: client},
		devTools: true,
	}
	r := setupMockRouter(server)

	do := func(method, path, body string) *httptest.ResponseRecorder {
		var req *http.Request
		if body != "" {
			req = httptest.NewRequest(method, path, strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
		} else {
			req = httptest.NewRequest(method, path, nil)
		}
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		return w
	}

	// Upstream is reachable before the switch is flipped.
	if w := do("GET", "/api/gallery/12345/tok12345/pages", ""); w.Code != http.StatusOK {
		t.Fatalf("online pages before down = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	// Flip the outage switch.
	if w := do("PUT", "/api/dev/upstream-down", `{"down":true}`); w.Code != http.StatusOK {
		t.Fatalf("toggle down = %d, want 200. body: %s", w.Code, w.Body.String())
	}
	if w := do("GET", "/api/dev/upstream-down", ""); !strings.Contains(w.Body.String(), `"down":true`) {
		t.Fatalf("state = %s, want down:true", w.Body.String())
	}

	// Upstream-backed endpoints now fail...
	if w := do("GET", "/api/gallery/12345/tok12345/pages", ""); w.Code != http.StatusBadGateway {
		t.Fatalf("online pages while down = %d, want 502", w.Code)
	}

	// ...while the local cache still answers (the fallback target).
	if w := do("GET", "/api/gallery-cache/12345/tok12345/pages", ""); w.Code != http.StatusOK {
		t.Fatalf("cache pages while down = %d, want 200. body: %s", w.Code, w.Body.String())
	}

	// Flip it back.
	if w := do("PUT", "/api/dev/upstream-down", `{"down":false}`); w.Code != http.StatusOK {
		t.Fatalf("toggle up = %d, want 200", w.Code)
	}
	if w := do("GET", "/api/gallery/12345/tok12345/pages", ""); w.Code != http.StatusOK {
		t.Fatalf("online pages after up = %d, want 200. body: %s", w.Code, w.Body.String())
	}
}

func TestDevRoutesDisabledByDefault(t *testing.T) {
	server := &Server{Client: &http.Client{}}
	r := setupMockRouter(server)

	req := httptest.NewRequest("GET", "/api/dev/upstream-down", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("dev route without DevTools = %d, want 404", w.Code)
	}
}
