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

	l, err := Acquire(context.Background(), 5000)
	if err != nil {
		t.Fatalf("oversized Acquire: %v", err)
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

	// Grow: real payload is bigger than the estimate.
	l.Resize(800)
	blockedCtx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if _, err := Acquire(blockedCtx, 300); err == nil {
		t.Fatal("expected Acquire to block while resized lease holds 800")
	}

	// Shrink back: frees room again.
	l.Resize(200)
	other, err := Acquire(context.Background(), 700)
	if err != nil {
		t.Fatalf("Acquire after shrink: %v", err)
	}
	other.Release()

	l.Release()
	// Resize after release is a no-op, not a panic or a re-acquire.
	l.Resize(900)
	l.Release()
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
