package storage

import (
	"context"
	"sync"
)

// Switchable wraps a Storage so it can be replaced while requests are in
// flight. Handlers hold the wrapper for the lifetime of the process; a settings
// change swaps the implementation underneath them, and every in-progress call
// still completes against whichever instance it started with.
type Switchable struct {
	mu    sync.RWMutex
	inner Storage
}

var _ Storage = (*Switchable)(nil)

// NewSwitchable wraps initial. initial must not be nil.
func NewSwitchable(initial Storage) *Switchable {
	return &Switchable{inner: initial}
}

// Replace swaps the active implementation.
func (s *Switchable) Replace(next Storage) {
	s.mu.Lock()
	s.inner = next
	s.mu.Unlock()
}

// current copies the pointer out so the call runs without holding the lock.
func (s *Switchable) current() Storage {
	s.mu.RLock()
	inner := s.inner
	s.mu.RUnlock()
	return inner
}

func (s *Switchable) Get(ctx context.Context, key string) ([]byte, string, error) {
	return s.current().Get(ctx, key)
}

func (s *Switchable) Put(ctx context.Context, key string, data []byte, contentType string, opts PutOptions) error {
	return s.current().Put(ctx, key, data, contentType, opts)
}

func (s *Switchable) Delete(ctx context.Context, key string) error {
	return s.current().Delete(ctx, key)
}

func (s *Switchable) Exists(ctx context.Context, key string) (bool, error) {
	return s.current().Exists(ctx, key)
}
