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
	"sync/atomic"
	"time"

	"golang.org/x/sync/semaphore"

	"manga-reader/internal/metrics"
)

// DefaultWeight is the reservation used for page-image downloads before a
// payload's real size is known. It roughly matches the typical page image
// size, so the estimate only needs to hold until the operation can resize.
const DefaultWeight = 8 << 20

// ThumbnailWeight is the reservation used for thumbnail downloads before the
// real size is known. Thumbnails are far smaller than page images, so pricing
// them at DefaultWeight would reserve ~8 MiB for a few-hundred-KB object and
// throttle thumbnail concurrency for no reason.
const ThumbnailWeight = 512 << 10

// defaultLimit is the default total budget, overridable with
// MANGA_READER_MEM_BUDGET (bytes, or a value with a KiB/MiB/GiB — or
// KB/MB/GB — suffix).
const defaultLimit = 96 << 20

// maxWeightFraction/maxWeightDenom cap a single reservation at 3/4 of the
// total budget so one oversized payload cannot reserve the whole budget and
// starve every other operation. The excess is deliberately left unaccounted:
// the operation still allocates its full payload, it just reserves this share.
const (
	maxWeightFraction = 3
	maxWeightDenom    = 4
)

// capWeight bounds weight to a fraction of limit, never below 1 byte.
func capWeight(weight, limit int64) int64 {
	if limit > 0 {
		if ceiling := limit / maxWeightDenom * maxWeightFraction; weight > ceiling {
			weight = ceiling
		}
	}
	if weight < 1 {
		weight = 1
	}
	return weight
}

var (
	stateMu sync.Mutex
	limit   int64 = defaultLimit
	sem           = semaphore.NewWeighted(defaultLimit)

	// reserved and waiters mirror the semaphore's internal state for the
	// metrics gauges: semaphore.Weighted does not expose current usage.
	reserved atomic.Int64
	waiters  atomic.Int64
)

// addReserved adjusts the reserved-bytes mirror and refreshes the gauge.
func addReserved(delta int64) {
	metrics.SetMemBudgetReserved(reserved.Add(delta))
}

func init() {
	metrics.SetMemBudgetLimit(limit)
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
	metrics.SetMemBudgetLimit(n)
}

// Limit returns the current total budget in bytes.
func Limit() int64 {
	stateMu.Lock()
	defer stateMu.Unlock()
	return limit
}

// Reserved returns the bytes currently reserved from the budget. It mirrors the
// reserved-bytes metric and is intended for diagnostics and tests.
func Reserved() int64 {
	return reserved.Load()
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

// Lease is a reference-counted byte reservation. Acquire creates it with one
// reference; Retain adds a reference for another caller sharing the same
// payload, and the reservation returns to the budget only when the last
// reference is released. This lets concurrent readers of one shared buffer
// (singleflight waiters, cells of the same sprite) be charged once instead of
// once per reader. Resize, Retain and Release are serialized internally.
type Lease struct {
	mu     sync.Mutex
	sem    *semaphore.Weighted
	weight int64
	refs   int64
}

// Acquire reserves weight bytes of the budget, waiting until ctx is done if
// the budget is exhausted. weight <= 0 reserves DefaultWeight. Weights are
// capped at a fraction of the total budget (see capWeight) so a single
// oversized operation cannot reserve the whole budget and starve the rest.
func Acquire(ctx context.Context, weight int64) (*Lease, error) {
	s, lim := current()
	if weight <= 0 {
		weight = DefaultWeight
	}
	weight = capWeight(weight, lim)
	metrics.SetMemBudgetWaiters(int(waiters.Add(1)))
	start := time.Now()
	err := s.Acquire(ctx, weight)
	metrics.ObserveMemBudgetWait(time.Since(start))
	metrics.SetMemBudgetWaiters(int(waiters.Add(-1)))
	if err != nil {
		return nil, err
	}
	addReserved(weight)
	return &Lease{sem: s, weight: weight, refs: 1}, nil
}

// Retain adds a reference for another caller sharing this reservation. It is a
// no-op on a nil or already-released lease, so a caller that lost the race
// against the last Release behaves safely: its read window simply goes
// unaccounted rather than reviving a reservation the budget already returned.
func (l *Lease) Retain() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.weight == 0 {
		return
	}
	l.refs++
}

// Resize re-reserves at a new weight once the real payload size is known.
// Growing acquires only the difference while the old reservation is still
// held, so the lease is never dropped below its current weight: the budget
// undercounts this lease by at most the pending delta instead of the whole
// reservation. A grow that ctx cannot satisfy leaves the old (smaller)
// reservation in place and returns the error — the caller is already holding
// the bytes, so blocking forever or dropping the reservation are both worse.
// Shrinking returns the difference immediately.
func (l *Lease) Resize(ctx context.Context, weight int64) error {
	if l == nil {
		return nil
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.weight == 0 {
		return nil // already released
	}
	if weight <= 0 {
		weight = 1
	}
	weight = capWeight(weight, Limit())
	switch {
	case weight > l.weight:
		delta := weight - l.weight
		metrics.SetMemBudgetWaiters(int(waiters.Add(1)))
		start := time.Now()
		err := l.sem.Acquire(ctx, delta)
		metrics.ObserveMemBudgetWait(time.Since(start))
		metrics.SetMemBudgetWaiters(int(waiters.Add(-1)))
		if err != nil {
			return err
		}
		addReserved(delta)
		l.weight = weight
	case weight < l.weight:
		delta := l.weight - weight
		addReserved(-delta)
		l.sem.Release(delta)
		l.weight = weight
	}
	return nil
}

// Release drops one reference; the reservation returns to the budget when the
// last reference is gone. It is idempotent and nil-safe.
func (l *Lease) Release() {
	if l == nil {
		return
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.weight == 0 {
		return
	}
	l.refs--
	if l.refs > 0 {
		return
	}
	addReserved(-l.weight)
	l.sem.Release(l.weight)
	l.weight = 0
}
