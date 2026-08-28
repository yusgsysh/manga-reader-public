package main

import (
	"testing"
)

func TestDebugSkCookieDetection(t *testing.T) {
	t.Run("compact layout", func(t *testing.T) {
		html := `<table><tbody><tr></tr><tr><td class="gl1c glcat"></td><td class="gl2c"></td><td class="gl3c glname"></td><td class="gl4c glhide"></td></tr></tbody></table>`
		t.Logf("compact layout detected: %v", isCompactLayout(html))
	})

	t.Run("extended layout", func(t *testing.T) {
		html := `<table><tbody><tr></tr><tr><td class="gl1e"></td><td class="gl2e"></td></tr></tbody></table>`
		t.Logf("extended layout detected: %v", !isCompactLayout(html))
	})
}

func isCompactLayout(html string) bool {
	return contains(html, "gl1c")
}

func contains(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
