package exhentai

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"manga-reader/internal/metrics"
)

func TestFetchOnce_CoalescesConcurrent(t *testing.T) {
	var calls atomic.Int32
	start := make(chan struct{})
	fetch := func(context.Context) (any, error) {
		calls.Add(1)
		<-start
		return "value", nil
	}

	client := &http.Client{}
	key := t.Name()

	const n = 8
	var wg sync.WaitGroup
	results := make([]any, n)
	for i := range n {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, err := fetchOnce(context.Background(), client, key, fetch)
			if err != nil {
				t.Errorf("fetchOnce: %v", err)
				return
			}
			results[i] = v
		}(i)
	}
	close(start)
	wg.Wait()

	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1 (coalesced)", got)
	}
	for i, v := range results {
		if v != "value" {
			t.Errorf("result[%d] = %v, want value", i, v)
		}
	}
}

func TestFetchOnce_ReusesWithinTTL(t *testing.T) {
	var calls atomic.Int32
	fetch := func(context.Context) (any, error) {
		return calls.Add(1), nil
	}

	client := &http.Client{}
	key := t.Name() + "-reuse"
	for range 3 {
		if _, err := fetchOnce(context.Background(), client, key, fetch); err != nil {
			t.Fatalf("fetchOnce: %v", err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1 (fresh entries reused)", got)
	}
}

func TestFetchOnce_ExpiresAfterTTL(t *testing.T) {
	prev := fetchCacheTTL
	fetchCacheTTL = 20 * time.Millisecond
	t.Cleanup(func() { fetchCacheTTL = prev })

	var calls atomic.Int32
	fetch := func(context.Context) (any, error) {
		return calls.Add(1), nil
	}

	client := &http.Client{}
	key := t.Name() + "-expire"
	if _, err := fetchOnce(context.Background(), client, key, fetch); err != nil {
		t.Fatalf("fetchOnce: %v", err)
	}
	if _, err := fetchOnce(context.Background(), client, key, fetch); err != nil {
		t.Fatalf("fetchOnce: %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("fetch calls = %d, want 1 before expiry", got)
	}

	time.Sleep(50 * time.Millisecond)
	if _, err := fetchOnce(context.Background(), client, key, fetch); err != nil {
		t.Fatalf("fetchOnce: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("fetch calls = %d, want 2 after expiry", got)
	}
}

func TestFetchOnce_DistinctKeysDoNotShare(t *testing.T) {
	var calls atomic.Int32
	fetch := func(context.Context) (any, error) {
		return calls.Add(1), nil
	}

	client := &http.Client{}
	if _, err := fetchOnce(context.Background(), client, t.Name()+"-a", fetch); err != nil {
		t.Fatalf("fetchOnce: %v", err)
	}
	if _, err := fetchOnce(context.Background(), client, t.Name()+"-b", fetch); err != nil {
		t.Fatalf("fetchOnce: %v", err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("fetch calls = %d, want 2 (distinct keys)", got)
	}
}

// TestFetchCacheIsBounded: values are parsed upstream documents keyed by
// request URL (user-controlled query strings), so an uncapped map would pin
// memory forever as distinct URLs are fetched.
func TestFetchCacheIsBounded(t *testing.T) {
	fetchCacheMu.Lock()
	fetchCache = make(map[string]fetchEntry)
	fetchCacheMu.Unlock()
	t.Cleanup(func() {
		fetchCacheMu.Lock()
		fetchCache = make(map[string]fetchEntry)
		fetchCacheMu.Unlock()
	})

	for i := range fetchCacheMaxEntries + 64 {
		setFetchEntry(fmt.Sprintf("bounded-test:%d", i), i)
	}

	fetchCacheMu.Lock()
	n := len(fetchCache)
	fetchCacheMu.Unlock()
	if n > fetchCacheMaxEntries {
		t.Fatalf("fetchCache holds %d entries, want <= %d", n, fetchCacheMaxEntries)
	}
}

// TestFetchCacheEntriesGaugeFollowsExpiry: the gauge is only written when an
// entry is inserted, so the lazy expiry delete must refresh it too. Otherwise
// a cache that stops receiving writes keeps reporting its last inserted size
// long after every entry has expired.
func TestFetchCacheEntriesGaugeFollowsExpiry(t *testing.T) {
	metrics.ResetForTest()
	prev := fetchCacheTTL
	fetchCacheTTL = 20 * time.Millisecond
	t.Cleanup(func() { fetchCacheTTL = prev })

	fetchCacheMu.Lock()
	fetchCache = make(map[string]fetchEntry)
	fetchCacheMu.Unlock()
	metrics.SetFetchCacheEntries(0)
	t.Cleanup(func() {
		fetchCacheMu.Lock()
		fetchCache = make(map[string]fetchEntry)
		fetchCacheMu.Unlock()
		metrics.SetFetchCacheEntries(0)
	})

	setFetchEntry("gauge-expire-test", "value")
	if want := "exhentai_fetchcache_entries 1"; !strings.Contains(gatherMetrics(t), want) {
		t.Fatalf("after insert: expected %q, got:\n%s", want, gatherMetrics(t))
	}

	time.Sleep(50 * time.Millisecond)
	if _, ok := getFetchEntry("gauge-expire-test"); ok {
		t.Fatal("entry should have expired")
	}
	if want := "exhentai_fetchcache_entries 0"; !strings.Contains(gatherMetrics(t), want) {
		t.Fatalf("after expiry: expected %q, got:\n%s", want, gatherMetrics(t))
	}
}

func gatherMetrics(t *testing.T) string {
	t.Helper()
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	metrics.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("metrics handler status = %d", rec.Code)
	}
	return rec.Body.String()
}
