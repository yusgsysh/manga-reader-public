package singleinstance

import (
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

func TestAcquirePrimaryThenSecondaryFocusesPrimary(t *testing.T) {
	dir := t.TempDir()

	var focused atomic.Int32
	inst, primary, err := Acquire(dir, func() { focused.Add(1) })
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if !primary {
		t.Fatal("first Acquire should be primary")
	}
	defer func() { _ = inst.Close() }()

	second, primary2, err := Acquire(dir, func() {})
	if err != nil {
		t.Fatalf("second Acquire: %v", err)
	}
	if primary2 {
		t.Fatal("second Acquire must not be primary")
	}
	if second != nil {
		_ = second.Close()
		t.Fatal("secondary Acquire should not return an instance")
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && focused.Load() == 0 {
		time.Sleep(5 * time.Millisecond)
	}
	if focused.Load() == 0 {
		t.Fatal("primary was never asked to focus")
	}
}

func TestAcquireAfterCloseBecomesPrimary(t *testing.T) {
	dir := t.TempDir()

	inst, primary, err := Acquire(dir, nil)
	if err != nil || !primary {
		t.Fatalf("first Acquire = (%v, %v), want primary", inst, err)
	}
	if err := inst.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	// The socket file is gone, so the path is free again.
	if _, err := os.Stat(filepath.Join(dir, socketName)); !os.IsNotExist(err) {
		t.Fatalf("socket still present after Close: %v", err)
	}

	again, primary2, err := Acquire(dir, nil)
	if err != nil {
		t.Fatalf("re-Acquire: %v", err)
	}
	if !primary2 {
		t.Fatal("Acquire after Close should be primary")
	}
	defer func() { _ = again.Close() }()
}

// A crashed run leaves a socket file behind; the next launch must take over
// instead of refusing to start.
func TestAcquireReplacesStaleSocketFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, socketName)
	if err := os.WriteFile(path, []byte("stale"), 0o600); err != nil {
		t.Fatalf("seed stale socket file: %v", err)
	}

	inst, primary, err := Acquire(dir, nil)
	if err != nil {
		t.Fatalf("Acquire over stale file: %v", err)
	}
	if !primary {
		t.Fatal("Acquire should take over a stale socket file")
	}
	defer func() { _ = inst.Close() }()
}

func TestAcquireRequiresDir(t *testing.T) {
	if _, _, err := Acquire("", nil); err == nil {
		t.Fatal("Acquire(\"\") = nil error, want error")
	}
}
