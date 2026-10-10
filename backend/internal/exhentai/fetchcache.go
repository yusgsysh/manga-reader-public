package exhentai

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"manga-reader/internal/metrics"
)

// Many endpoints derive different data from the same upstream page: the gallery
// HTML document carries details, the page list, the ".gpc" total and the
// torrent count; the torrents page carries the list and (via a POST) the
// Information view; the gdata API carries metadata. fetchOnce coalesces
// concurrent identical requests and shares the result for a short window so the
// same page is fetched at most once per window instead of once per derived
// endpoint.
const defaultFetchCacheTTL = 10 * time.Second

// fetchCacheTTL is a var so tests can shorten the sharing window.
var fetchCacheTTL = defaultFetchCacheTTL

var fetchGroup singleflight.Group

type fetchEntry struct {
	value   any
	expires time.Time
}

var (
	fetchCacheMu sync.Mutex
	fetchCache   = map[string]fetchEntry{}
)

// fetchCacheMaxEntries bounds the shared document cache. Values are full
// parsed upstream documents keyed by request URL, and query strings are
// user-controlled, so without a cap distinct URLs would pin memory forever.
const fetchCacheMaxEntries = 2048

// fetchOnce returns the cached value for key while fresh, otherwise runs fetch
// exactly once (concurrent callers wait on the same call) and caches the result
// for fetchCacheTTL. The fetch runs on a context detached from any single
// caller so one caller's cancellation cannot abort the shared request; it is
// still bounded by upstreamDocTimeout.
func fetchOnce(
	ctx context.Context,
	client *http.Client,
	key string,
	fetch func(ctx context.Context) (any, error),
) (any, error) {
	if value, ok := getFetchEntry(key); ok {
		metrics.ObserveFetchCache("hit")
		return value, nil
	}

	value, err, shared := fetchGroup.Do(key, func() (any, error) {
		if cached, ok := getFetchEntry(key); ok {
			metrics.ObserveFetchCache("hit")
			return cached, nil
		}
		metrics.ObserveFetchCache("miss")
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), upstreamDocTimeout)
		defer cancel()
		result, fetchErr := fetch(fetchCtx)
		if fetchErr != nil {
			return nil, fetchErr
		}
		setFetchEntry(key, result)
		return result, nil
	})
	// shared means this caller waited on an identical in-flight fetch.
	if shared {
		metrics.ObserveFetchCache("coalesced")
	}
	if err != nil {
		return nil, err
	}
	return value, nil
}

func getFetchEntry(key string) (any, bool) {
	fetchCacheMu.Lock()
	defer fetchCacheMu.Unlock()
	entry, ok := fetchCache[key]
	if !ok {
		return nil, false
	}
	if time.Now().After(entry.expires) {
		delete(fetchCache, key)
		// The gauge only observes inserts otherwise, so a cache that stops
		// receiving writes would keep reporting the last inserted size while
		// every entry has actually expired.
		metrics.SetFetchCacheEntries(len(fetchCache))
		return nil, false
	}
	return entry.value, true
}

func setFetchEntry(key string, value any) {
	fetchCacheMu.Lock()
	defer fetchCacheMu.Unlock()
	if len(fetchCache) >= fetchCacheMaxEntries {
		// Drop expired entries first; if the cache is still full of fresh
		// entries, reset it — refetching is cheap (short TTL) compared to
		// unbounded growth.
		now := time.Now()
		for k, e := range fetchCache {
			if now.After(e.expires) {
				delete(fetchCache, k)
			}
		}
		if len(fetchCache) >= fetchCacheMaxEntries {
			fetchCache = make(map[string]fetchEntry)
		}
	}
	fetchCache[key] = fetchEntry{value: value, expires: time.Now().Add(fetchCacheTTL)}
	metrics.SetFetchCacheEntries(len(fetchCache))
}

// fetchCacheKey namespaces a request by client (cookies) and request shape, so
// results are never shared across clients with different sessions.
func fetchCacheKey(client *http.Client, method, target string) string {
	return fmt.Sprintf("%p|%s|%s", client, method, target)
}
