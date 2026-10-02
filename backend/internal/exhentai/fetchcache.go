package exhentai

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
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
		return value, nil
	}

	value, err, _ := fetchGroup.Do(key, func() (any, error) {
		if cached, ok := getFetchEntry(key); ok {
			return cached, nil
		}
		fetchCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), upstreamDocTimeout)
		defer cancel()
		result, fetchErr := fetch(fetchCtx)
		if fetchErr != nil {
			return nil, fetchErr
		}
		setFetchEntry(key, result)
		return result, nil
	})
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
		return nil, false
	}
	return entry.value, true
}

func setFetchEntry(key string, value any) {
	fetchCacheMu.Lock()
	defer fetchCacheMu.Unlock()
	fetchCache[key] = fetchEntry{value: value, expires: time.Now().Add(fetchCacheTTL)}
}

// fetchCacheKey namespaces a request by client (cookies) and request shape, so
// results are never shared across clients with different sessions.
func fetchCacheKey(client *http.Client, method, target string) string {
	return fmt.Sprintf("%p|%s|%s", client, method, target)
}
