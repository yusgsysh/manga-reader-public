package membudget

import (
	"context"
	"errors"
	"testing"
	"time"
)

// withLimit sets a test budget and restores the default afterwards.
func withLimit(t *testing.T, n int64) {
	t.Helper()
	SetLimit(n)
	t.Cleanup(func() { SetLimit(defaultLimit) })
}

func TestParseBytes(t *testing.T) {
	cases := []struct {
		in   string
		want int64
	}{
		{"96MiB", 96 << 20},
		{"96 miB", 96 << 20},
		{"100000000", 100000000},
		{"128KB", 128 << 10},
		{"2GiB", 2 << 30},
		{"512B", 512},
	}
	for _, c := range cases {
		got, err := ParseBytes(c.in)
		if err != nil {
			t.Errorf("ParseBytes(%q): %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseBytes(%q) = %d, want %d", c.in, got, c.want)
		}
	}
	for _, bad := range []string{"", "abc", "-1MiB", "12.5MiB"} {
		if _, err := ParseBytes(bad); err == nil {
			t.Errorf("ParseBytes(%q): expected error", bad)
		}
	}
}

func TestAcquireBlocksWhenBudgetExhausted(t *testing.T) {
	withLimit(t, 1000)

	first, err := Acquire(context.Background(), 800)
	if err != nil {
		t.Fatalf("first Acquire: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(ctx, 400); err == nil {
		t.Fatal("expected second Acquire to block until ctx deadline")
	} else if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("second Acquire error = %v, want context deadline", err)
	}

	first.Release()
	second, err := Acquire(context.Background(), 400)
	if err != nil {
		t.Fatalf("Acquire after release: %v", err)
	}
	second.Release()
}

func TestAcquireClampsOversizedWeight(t *testing.T) {
	withLimit(t, 500)
	start := reserved.Load()

	// A weight above the budget is capped at 3/4 of it (see capWeight), so the
	// reservation succeeds and leaves headroom for other operations.
	l, err := Acquire(context.Background(), 5000)
	if err != nil {
		t.Fatalf("oversized Acquire: %v", err)
	}
	if got := reserved.Load() - start; got != 500/maxWeightDenom*maxWeightFraction {
		t.Errorf("reserved for oversized Acquire = %d, want %d", got, 500/maxWeightDenom*maxWeightFraction)
	}
	l.Release()
	l.Release() // idempotent
}

func TestLeaseResize(t *testing.T) {
	withLimit(t, 1000)

	l, err := Acquire(context.Background(), 100)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}

	// Grow: real payload is bigger than the estimate (capped at 3/4 of 1000).
	if err := l.Resize(context.Background(), 800); err != nil {
		t.Fatalf("Resize grow: %v", err)
	}
	blockedCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(blockedCtx, 300); err == nil {
		t.Fatal("expected Acquire to block while resized lease holds the budget")
	}

	// Shrink back: frees room again.
	if err := l.Resize(context.Background(), 200); err != nil {
		t.Fatalf("Resize shrink: %v", err)
	}
	other, err := Acquire(context.Background(), 700)
	if err != nil {
		t.Fatalf("Acquire after shrink: %v", err)
	}
	other.Release()

	l.Release()
	// Resize after release is a no-op, not a panic or a re-acquire.
	if err := l.Resize(context.Background(), 900); err != nil {
		t.Fatalf("Resize after release: %v", err)
	}
	l.Release()
}

// A grow that ctx cannot satisfy must leave the old reservation in place rather
// than dropping it, so the bytes the caller already holds stay accounted for.
func TestResizeGrowFailureKeepsOldReservation(t *testing.T) {
	withLimit(t, 1000)
	start := reserved.Load()

	l, err := Acquire(context.Background(), 700)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	// Fill the remaining budget so the grow has nowhere to go.
	blocker, err := Acquire(context.Background(), 300)
	if err != nil {
		t.Fatalf("blocker Acquire: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if err := l.Resize(ctx, 750); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Resize grow error = %v, want deadline exceeded", err)
	}
	if got := reserved.Load() - start; got != 1000 {
		t.Errorf("reserved after failed grow = %d, want 1000 (old lease kept)", got)
	}

	l.Release()
	if got := reserved.Load() - start; got != 300 {
		t.Errorf("reserved after releasing grown lease = %d, want 300 (blocker only)", got)
	}
	blocker.Release()
}

// Retain shares one reservation across readers: the budget is returned only
// after the last reference is released.
func TestLeaseRetainSharesReservation(t *testing.T) {
	withLimit(t, 1000)
	start := reserved.Load()

	l, err := Acquire(context.Background(), 700)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	l.Retain()

	l.Release()
	if got := reserved.Load() - start; got != 700 {
		t.Fatalf("reserved after first release = %d, want 700 (shared ref held)", got)
	}
	l.Release()
	if got := reserved.Load() - start; got != 0 {
		t.Fatalf("reserved after last release = %d, want 0", got)
	}
}

// Retaining after the last release must not revive the reservation or panic;
// the reservation is already back with the budget.
func TestLeaseRetainAfterRelease(t *testing.T) {
	withLimit(t, 1000)
	start := reserved.Load()

	l, err := Acquire(context.Background(), 300)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	l.Release()
	l.Retain()
	l.Release()
	if got := reserved.Load() - start; got != 0 {
		t.Fatalf("reserved after retain/release cycle = %d, want 0", got)
	}
}

func TestAcquireCanceledContext(t *testing.T) {
	withLimit(t, 100)

	held, err := Acquire(context.Background(), 100)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Acquire(ctx, 50); !errors.Is(err, context.Canceled) {
		t.Errorf("Acquire on canceled ctx error = %v, want context canceled", err)
	}
}

// TestReservedMirrorTracksLeases: the metrics mirror must follow Acquire,
// Resize and Release so the reserved-bytes gauge matches the semaphore instead
// of drifting (Resize releases the old weight before taking the new one).
func TestReservedMirrorTracksLeases(t *testing.T) {
	withLimit(t, 1000)

	start := reserved.Load()
	l, err := Acquire(context.Background(), 300)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	if got := reserved.Load() - start; got != 300 {
		t.Fatalf("reserved after Acquire = %d, want 300", got)
	}

	if err := l.Resize(context.Background(), 500); err != nil {
		t.Fatalf("Resize: %v", err)
	}
	if got := reserved.Load() - start; got != 500 {
		t.Fatalf("reserved after Resize = %d, want 500", got)
	}

	l.Release()
	if got := reserved.Load() - start; got != 0 {
		t.Fatalf("reserved after Release = %d, want 0", got)
	}
}

// TestWaitersMirrorReturnsToZero: a blocked Acquire that gives up on ctx
// deadline must still decrement the waiter gauge.
func TestWaitersMirrorReturnsToZero(t *testing.T) {
	withLimit(t, 1000)

	held, err := Acquire(context.Background(), 1000)
	if err != nil {
		t.Fatalf("Acquire: %v", err)
	}
	defer held.Release()

	// 1000 is capped to the single-weight ceiling, so ask for more than what is
	// left to force a real block.
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_, _ = Acquire(ctx, 500)

	if got := waiters.Load(); got != 0 {
		t.Fatalf("waiters after timed-out Acquire = %d, want 0", got)
	}
}
