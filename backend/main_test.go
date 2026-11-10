package main

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
)

func TestParseLogLevel(t *testing.T) {
	tests := []struct {
		input    string
		expected slog.Level
	}{
		{"", slog.LevelWarn},
		{"debug", slog.LevelDebug},
		{"DEBUG", slog.LevelDebug},
		{"info", slog.LevelInfo},
		{"INFO", slog.LevelInfo},
		{"warn", slog.LevelWarn},
		{"WARN", slog.LevelWarn},
		{"warning", slog.LevelWarn},
		{"WARNING", slog.LevelWarn},
		{"error", slog.LevelError},
		{"ERROR", slog.LevelError},
		{"invalid", slog.LevelWarn},
		{"  warn  ", slog.LevelWarn},
		{"  DEBUG  ", slog.LevelDebug},
		{"unknown", slog.LevelWarn},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := parseLogLevel(tt.input)
			if got != tt.expected {
				t.Errorf("parseLogLevel(%q) = %v, want %v", tt.input, got, tt.expected)
			}
		})
	}
}

func newCSRFRouter() *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.New()
	r.Use(csrfGuard(nil))
	r.GET("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	r.POST("/x", func(c *gin.Context) { c.Status(http.StatusNoContent) })
	return r
}

func TestCSRFGuard(t *testing.T) {
	r := newCSRFRouter()
	tests := []struct {
		name   string
		method string
		origin string
		site   string
		want   int
	}{
		{"same-origin POST", http.MethodPost, "https://manga.example.com", "", http.StatusNoContent},
		{"cross-origin POST", http.MethodPost, "https://evil.example", "", http.StatusForbidden},
		{"same-site subdomain POST", http.MethodPost, "https://sub.manga.example.com", "", http.StatusForbidden},
		{"no origin header POST (CLI)", http.MethodPost, "", "", http.StatusNoContent},
		{"cross-site fetch metadata", http.MethodPost, "", "cross-site", http.StatusForbidden},
		{"same-origin fetch metadata", http.MethodPost, "", "same-origin", http.StatusNoContent},
		{"GET is never blocked", http.MethodGet, "https://evil.example", "cross-site", http.StatusNoContent},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "https://manga.example.com/x", nil)
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.site != "" {
				req.Header.Set("Sec-Fetch-Site", tc.site)
			}
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.want {
				t.Fatalf("%s = %d, want %d", tc.name, w.Code, tc.want)
			}
		})
	}
}

func TestCORSOriginSetIncludesAllowlistAndViteDev(t *testing.T) {
	t.Setenv("MANGA_READER_CORS_ORIGINS", "https://app.example.com")
	set := corsOriginSet()
	if _, ok := set["https://app.example.com"]; !ok {
		t.Error("explicit allowlist entry missing")
	}
	if _, ok := set["http://localhost:5173"]; !ok {
		t.Error("Vite dev origin missing")
	}
}
