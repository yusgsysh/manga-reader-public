package storage

import (
	"context"
	"testing"
)

func newLocalStorage(t *testing.T) *LocalStorage {
	t.Helper()
	s, err := NewLocalStorage(LocalConfig{Root: t.TempDir()})
	if err != nil {
		t.Fatalf("NewLocalStorage: %v", err)
	}
	return s
}

// The wrapper exists so handlers keep working across a settings change; it must
// route to whichever instance is current without losing in-flight calls.
func TestSwitchableRoutesToTheCurrentBackend(t *testing.T) {
	first := newLocalStorage(t)
	second := newLocalStorage(t)
	sw := NewSwitchable(first)

	ctx := context.Background()
	if err := sw.Put(ctx, "a/b.png", []byte("one"), "image/png", PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if data, _, err := sw.Get(ctx, "a/b.png"); err != nil || string(data) != "one" {
		t.Fatalf("Get before Replace = %q, %v", data, err)
	}

	sw.Replace(second)

	if _, _, err := sw.Get(ctx, "a/b.png"); !IsNotFound(err) {
		t.Errorf("Get after Replace = %v, want a miss in the new backend", err)
	}
	if err := sw.Put(ctx, "a/b.png", []byte("two"), "image/png", PutOptions{}); err != nil {
		t.Fatalf("Put after Replace: %v", err)
	}
	if data, _, err := sw.Get(ctx, "a/b.png"); err != nil || string(data) != "two" {
		t.Fatalf("Get after Replace = %q, %v", data, err)
	}

	// The abandoned instance still holds the first write: Replace swaps the
	// pointer, it does not migrate data.
	if data, _, err := first.Get(ctx, "a/b.png"); err != nil || string(data) != "one" {
		t.Errorf("original backend = %q, %v; want the data written before Replace", data, err)
	}
}

func TestSwitchableReplaceWithTheSameBackend(t *testing.T) {
	s := newLocalStorage(t)
	sw := NewSwitchable(s)
	sw.Replace(s)

	ctx := context.Background()
	if err := sw.Put(ctx, "k", []byte("v"), "text/plain", PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	if _, _, err := sw.Get(ctx, "k"); err != nil {
		t.Errorf("Get = %v, want nil", err)
	}
}

func TestSwitchableDeleteFollowsTheBackend(t *testing.T) {
	sw := NewSwitchable(newLocalStorage(t))
	ctx := context.Background()

	if err := sw.Put(ctx, "k", []byte("v"), "text/plain", PutOptions{}); err != nil {
		t.Fatalf("Put: %v", err)
	}
	sw.Replace(newLocalStorage(t))
	if ok, err := sw.Exists(ctx, "k"); err != nil || ok {
		t.Errorf("Exists after Replace = %v, %v; want false", ok, err)
	}
	// Deleting a key that is not in the current backend is still not an error.
	if err := sw.Delete(ctx, "k"); err != nil {
		t.Errorf("Delete = %v, want nil", err)
	}
}
