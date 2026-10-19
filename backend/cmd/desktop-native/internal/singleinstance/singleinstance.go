// Package singleinstance makes a desktop app start once per user. The first
// process owns a Unix-domain socket in the user's config directory; a later
// process asks that instance to focus its window and then exits, instead of
// starting a second backend on its own ports and database.
//
// The socket doubles as the lock: listening succeeds only for the first
// process, and a leftover socket from a crashed run is detected by probing it
// and removed before taking over.
package singleinstance

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

const (
	socketName     = "instance.sock"
	dialTimeout    = 2 * time.Second
	readTimeout    = 2 * time.Second
	focusRequest   = "focus\n"
	readBufferSize = 32
)

// Instance is the primary process's listener. Close it on shutdown so the
// next launch can take over immediately.
type Instance struct {
	ln   net.Listener
	path string

	closeOnce sync.Once
	closeErr  error
}

// Acquire tries to become the primary instance for the app state stored in
// dir.
//
// When it succeeds it returns primary=true and a listener that calls onFocus
// whenever another process asks to be brought forward. When another instance
// already owns the socket it asks that instance to focus and returns
// primary=false; the caller should exit.
func Acquire(dir string, onFocus func()) (*Instance, bool, error) {
	if dir == "" {
		return nil, false, fmt.Errorf("single instance directory is required")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, false, fmt.Errorf("create instance directory: %w", err)
	}
	path := filepath.Join(dir, socketName)

	ln, err := net.Listen("unix", path)
	if err != nil {
		// A live primary answers a dial; a stale socket file does not. Ask
		// the live one to focus and step aside either way.
		if conn, dialErr := net.DialTimeout("unix", path, dialTimeout); dialErr == nil {
			_, _ = conn.Write([]byte(focusRequest))
			_ = conn.Close()
			return nil, false, nil
		}
		if rmErr := os.Remove(path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) {
			return nil, false, fmt.Errorf("remove stale instance socket: %w", rmErr)
		}
		ln, err = net.Listen("unix", path)
		if err != nil {
			return nil, false, fmt.Errorf("listen instance socket: %w", err)
		}
	}
	// The socket only signals "focus"; keep it owner-only.
	_ = os.Chmod(path, 0o600)

	inst := &Instance{ln: ln, path: path}
	go inst.serve(onFocus)
	return inst, true, nil
}

// serve accepts focus requests until the listener is closed.
func (i *Instance) serve(onFocus func()) {
	for {
		conn, err := i.ln.Accept()
		if err != nil {
			return
		}
		_ = conn.SetReadDeadline(time.Now().Add(readTimeout))
		buf := make([]byte, readBufferSize)
		_, _ = conn.Read(buf)
		_ = conn.Close()
		if onFocus != nil {
			onFocus()
		}
	}
}

// Close stops listening and removes the socket file. It is safe to call more
// than once.
func (i *Instance) Close() error {
	if i == nil {
		return nil
	}
	i.closeOnce.Do(func() {
		if i.ln != nil {
			i.closeErr = i.ln.Close()
		}
		if rmErr := os.Remove(i.path); rmErr != nil && !errors.Is(rmErr, fs.ErrNotExist) && i.closeErr == nil {
			i.closeErr = rmErr
		}
	})
	return i.closeErr
}
