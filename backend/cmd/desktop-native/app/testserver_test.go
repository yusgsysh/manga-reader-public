package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"sync"
	"testing"
)

// apiURL serves handler over a plain TCP listener on the loopback interface
// and returns its base URL. The listener and server are stopped with the
// test.
//
// The tests that drive the app use this instead of httptest.NewServer for one
// reason: on a machine with a single usable CPU (a small CI runner, or a
// binary under -race) the test's own goroutine must never block inside
// net/http. There, the runtime may take the thread running the test goroutine
// for the network poller while that goroutine sits in a read, and the reads
// of everything else then wait for it: a fake server built on httptest stops
// accepting connections until the test's goroutine comes back, which a
// server that is being read from has no reason to do.
func apiURL(t *testing.T, handler http.Handler) string {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	srv := &http.Server{Handler: handler, BaseContext: func(net.Listener) context.Context {
		return context.Background()
	}}
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) {
			t.Logf("test server: %v", err)
		}
	}()
	t.Cleanup(func() { _ = srv.Close() })
	return "http://" + ln.Addr().String()
}

// responseGate holds the handler's responses until Close opens it, so a test
// can see which pages are being fetched before any of them arrive.
//
// The tests assert on the app's own state (the store's entries, the reader's
// window), never on a handler goroutine: with a single usable CPU the
// goroutines of a plain listener wait to be scheduled even while the test's
// own goroutine spins, so an assertion that needs them needs waiting.
type responseGate struct {
	mu     sync.Mutex
	seen   map[string]int
	closed chan struct{}
	once   sync.Once
}

func newResponseGate() *responseGate {
	return &responseGate{
		seen:   map[string]int{},
		closed: make(chan struct{}),
	}
}

// serve is a handler for the image endpoints: it counts the page asked for
// (by its url query) and holds the response until the gate opens or the
// request ends.
func (g *responseGate) serve(w http.ResponseWriter, r *http.Request) {
	who := r.URL.Query().Get("url")
	if who == "" {
		who = r.URL.Path
	}
	g.mu.Lock()
	g.seen[who]++
	g.mu.Unlock()
	select {
	case <-g.closed:
	case <-r.Context().Done():
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(fakeImagePNG)
}

// Close opens the gate: the requests still held answer at once.
func (g *responseGate) Close() {
	g.once.Do(func() { close(g.closed) })
}
