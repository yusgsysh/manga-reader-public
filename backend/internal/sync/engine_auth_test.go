package sync

import (
	"context"
	"strings"
	"testing"
)

// The sync server URL may embed Basic Auth credentials (user:pass@host) so
// the client can pass an edge-level Basic Auth in front of the server.
// Push errors must never contain the password: they are logged and persisted
// to sync_state.last_error (surfaced by GET /api/sync/status).
func TestPushErrorRedactsServerURLCredentials(t *testing.T) {
	e := newEngine(&Service{})

	t.Run("connection error", func(t *testing.T) {
		// 127.0.0.1:1 refuses instantly; no network needed.
		_, err := e.sendPush(context.Background(), ClientConfig{
			ServerURL: "http://alice:sup3rs3cret@127.0.0.1:1",
			Token:     "tok",
		}, PushRequest{})
		if err == nil {
			t.Fatal("expected push to fail")
		}
		if strings.Contains(err.Error(), "sup3rs3cret") {
			t.Fatalf("password leaked in error: %v", err)
		}
		if !strings.Contains(err.Error(), "push") {
			t.Fatalf("error lost its context: %v", err)
		}
	})

	t.Run("invalid url", func(t *testing.T) {
		// A control character makes url.Parse fail; parse errors echo the
		// raw URL, including the password, so they must not be wrapped.
		_, err := e.sendPush(context.Background(), ClientConfig{
			ServerURL: "http://alice:sup3rs3cret@host/\x7f",
			Token:     "tok",
		}, PushRequest{})
		if err == nil {
			t.Fatal("expected push to fail")
		}
		if strings.Contains(err.Error(), "sup3rs3cret") {
			t.Fatalf("password leaked in error: %v", err)
		}
	})
}
