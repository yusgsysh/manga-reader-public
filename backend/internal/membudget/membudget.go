// Package membudget bounds how many bytes of image payloads a single process
// buffers at once. Image proxies naturally want to hold whole images in
// memory (download → store → respond, sprite decode → crop), and without a
// ceiling the sum of concurrent requests is what makes RSS spike under
// load. The budget is a weighted semaphore over bytes: each operation that
// retains a buffer reserves its size (estimated before it is known, resized
// once known) and returns it when the buffer goes out of scope.
package membudget

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"golang.org/x/sync/semaphore"
)

// DefaultWeight is the reservation used before a payload's real size is
// known. It roughly matches the typical page image / sprite size, so the
// estimate only needs to hold until the operation can resize.
const DefaultWeight = 8 << 20

// defaultLimit is the default total budget, overridable with
// MANGA_READER_MEM_BUDGET (bytes, or a value with a KiB/MiB/GiB — or
// KB/MB/GB — suffix).
const defaultLimit = 96 << 20

var (
	stateMu sync.Mutex
	limit   int64 = defaultLimit
	sem           = semaphore.NewWeighted(defaultLimit)
)

func init() {
	if v := strings.TrimSpace(os.Getenv("MANGA_READER_MEM_BUDGET")); v != "" {
		n, err := ParseBytes(v)
		if err != nil {
			fmt.Fprintf(os.Stderr, "membudget: ignoring invalid MANGA_READER_MEM_BUDGET %q: %v\n", v, err)
			return
		}
		SetLimit(n)
	}
}

// SetLimit replaces the global budget. Call it only before traffic is
// served (startup or tests): replacing the semaphore would strand waiters
// queued on the previous one.
func SetLimit(n int64) {
	if n <= 0 {
		return
	}
	stateMu.Lock()
	defer stateMu.Unlock()
	limit = n
	sem = semaphore.NewWeighted(n)
}

// Limit returns the current total budget in bytes.
func Limit() int64 {
	stateMu.Lock()
	defer stateMu.Unlock()
	return limit
}

func current() (*semaphore.Weighted, int64) {
	stateMu.Lock()
	defer stateMu.Unlock()
	return sem, limit
}

// ParseBytes parses a byte count such as "96MiB", "100000000" or "128KB".
func ParseBytes(v string) (int64, error) {
	s := strings.TrimSpace(v)
	if s == "" {
		return 0, fmt.Errorf("empty value")
	}
	lower := strings.ToLower(s)
	mult := int64(1)
	for _, suf := range []struct {
		s string
		m int64
	}{
		{"kib", 1 << 10}, {"mib", 1 << 20}, {"gib", 1 << 30},
		{"kb", 1 << 10}, {"mb", 1 << 20}, {"gb", 1 << 30},
		{"b", 1},
	} {
		if strings.HasSuffix(lower, suf.s) {
			mult = suf.m
			lower = strings.TrimSpace(strings.TrimSuffix(lower, suf.s))
			break
		}
	}
	n, err := strconv.ParseInt(lower, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("not a byte count: %w", err)
	}
	if n < 0 {
		return 0, fmt.Errorf("negative value")
	}
	return n * mult, nil
}

// Lease is a byte reservation held until Release. It is safe for use from a
// single goroutine's scope (acquire and release happen on the request path);
// Resize and Release are serialized internally.
type Lease struct {
	mu     sync.Mutex
	sem    *semaphore.Weighted
	weight int64
	ctx    context.Context
}

// Acquire reserves weight bytes of the budget, waiting until ctx is done if
// the budget is exhausted. weight <= 0 reserves DefaultWeight; weights above
// the total budget are clamped so a single oversized operation can still
// run once everything else has released.
func Acquire(ctx context.Context, weight int64) (*Lease, error) {
	s, lim := current()
	if weight <= 0 {
		weight = DefaultWeight
	}
	if weight > lim {
		weight = lim
	}
	if err := s.Acquire(ctx, weight); err != nil {
		return nil, err
	}
	return &Lease{sem: s, weight: weight, ctx: ctx}, nil
}

// Resize re-reserves at a new weight once the real payload size is known.
// The old reservation is released first — otherwise two full reservations
// held at once could deadlock the budget — so the budget undercounts this
// lease by at most the old weight between the two calls. A resize that
// cannot be satisfied (ctx gone) drops the reservation entirely: the caller
// is already aborting and will Release harmlessly.
func (l *Lease) Resize(weight int64) {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.weight == 0 {
		return // already released
	}
	if weight <= 0 {
		weight = 1
	}
	if _, lim := current(); weight > lim {
		weight = lim
	}
	if weight == l.weight {
		return
	}
	s := l.sem
	old := l.weight
	l.weight = 0
	s.Release(old)
	// Deliberately not l.ctx: a request that finished while the payload was
	// being measured must still account for the bytes it is about to write.
	if err := s.Acquire(context.Background(), weight); err != nil {
		return
	}
	l.weight = weight
}

// Release returns the reservation. It is idempotent and nil-safe.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.weight == 0 {
		return
	}
	l.sem.Release(l.weight)
	l.weight = 0
}
