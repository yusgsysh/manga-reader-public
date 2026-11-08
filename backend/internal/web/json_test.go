package web

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
)

func newJSONRouter(handlers ...gin.HandlerFunc) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.POST("/json", handlers...)
	return r
}

func TestJSONUsesV2Encoding(t *testing.T) {
	r := newJSONRouter(func(c *gin.Context) {
		JSON(c, http.StatusOK, gin.H{
			"nil_map":   map[string]int(nil),
			"nil_slice": []int(nil),
		})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/json", nil))

	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", w.Code)
	}
	if got := w.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q", got)
	}
	// v2 marshals a nil map as {} and a nil slice as [], unlike v1's null.
	// Map keys are unordered in v2, so check the members independently.
	for _, member := range []string{`"nil_map":{}`, `"nil_slice":[]`} {
		if !strings.Contains(w.Body.String(), member) {
			t.Errorf("body = %s, want member %s", w.Body.String(), member)
		}
	}
}

func TestAbortWithStatusJSONSkipsNextHandler(t *testing.T) {
	next := false
	r := newJSONRouter(
		func(c *gin.Context) {
			AbortWithStatusJSON(c, http.StatusForbidden, gin.H{"error": "denied"})
		},
		func(c *gin.Context) { next = true },
	)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/json", nil))

	if w.Code != http.StatusForbidden || strings.TrimSpace(w.Body.String()) != `{"error":"denied"}` {
		t.Fatalf("status = %d, body = %s", w.Code, w.Body.String())
	}
	if next {
		t.Error("handler after AbortWithStatusJSON ran")
	}
}

func TestJSONMarshalErrorFailsResponse(t *testing.T) {
	r := newJSONRouter(func(c *gin.Context) {
		JSON(c, http.StatusOK, gin.H{"bad": func() {}})
	})
	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/json", nil))

	if w.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", w.Code)
	}
	if w.Body.Len() != 0 {
		t.Errorf("body = %q, want empty", w.Body.String())
	}
}

func TestBindJSON(t *testing.T) {
	r := newJSONRouter(func(c *gin.Context) {
		var got struct {
			Name string `json:"name"`
		}
		if err := BindJSON(c, &got); err != nil {
			JSON(c, http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		JSON(c, http.StatusOK, gin.H{"name": got.Name})
	})

	w := httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{"name":"ok"}`)))
	if w.Code != http.StatusOK || !strings.Contains(w.Body.String(), `"ok"`) {
		t.Fatalf("valid body: status = %d, body = %s", w.Code, w.Body.String())
	}

	for name, body := range map[string]string{
		"empty":      "",
		"whitespace": " \n\t ",
	} {
		w = httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(body)))
		if w.Code != http.StatusBadRequest || !strings.Contains(w.Body.String(), io.EOF.Error()) {
			t.Errorf("%s body: status = %d, body = %s", name, w.Code, w.Body.String())
		}
	}

	w = httptest.NewRecorder()
	r.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/json", strings.NewReader(`{"name":`)))
	if w.Code != http.StatusBadRequest {
		t.Errorf("truncated body: status = %d, body = %s", w.Code, w.Body.String())
	}
}
