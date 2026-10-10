// Package metrics exposes Prometheus instrumentation for upstream ExHentai
// traffic. The registry is package-private so tests can gather isolated
// snapshots; production mounts Handler at /metrics behind Basic Auth.
package metrics

import (
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Outcomes classify what happened to an upstream exchange. Transport-level
// recording only knows ok/error; content-level signals (sad panda, IP ban)
// are layered on via ObserveClassification.
const (
	OutcomeOK    = "ok"
	OutcomeError = "error"

	OutcomeSadPanda  = "sad_panda"
	OutcomeIPBanned  = "ip_banned"
	OutcomeHTTPError = "http_error"
)

var (
	registry = prometheus.NewRegistry()

	requestsTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "exhentai_upstream_requests_total",
		Help: "Upstream requests by endpoint, method, status code and transport-level outcome.",
	}, []string{"endpoint", "method", "status_code", "outcome"})

	requestDuration = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    "exhentai_upstream_request_duration_seconds",
		Help:    "Upstream request latency from send to response headers.",
		Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 20, 30},
	}, []string{"endpoint", "method"})

	responseBytes = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "exhentai_upstream_response_bytes_total",
		Help: "Upstream response body bytes observed at the transport layer (may undercount truncated bodies).",
	}, []string{"endpoint"})

	classificationTotal = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "exhentai_upstream_classification_total",
		Help: "Content-level classification of upstream documents (sad panda, IP ban, HTTP status errors).",
	}, []string{"endpoint", "outcome"})

	fetchCacheRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "exhentai_fetchcache_requests_total",
		Help: "Shared document fetches by cache result: hit, miss, or coalesced onto an in-flight fetch.",
	}, []string{"result"})

	fetchCacheEntries = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "exhentai_fetchcache_entries",
		Help: "Current number of entries in the short-lived shared document cache.",
	})

	imageCacheRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "image_cache_requests_total",
		Help: "MinIO read-through image cache lookups by object kind and result.",
	}, []string{"kind", "result"})

	galleryCacheRequests = prometheus.NewCounterVec(prometheus.CounterOpts{
		Name: "gallery_cache_requests_total",
		Help: "Gallery metadata cache lookups by kind and result.",
	}, []string{"kind", "result"})
)

func init() {
	registry.MustRegister(
		requestsTotal,
		requestDuration,
		responseBytes,
		classificationTotal,
		fetchCacheRequests,
		fetchCacheEntries,
		imageCacheRequests,
		galleryCacheRequests,
	)
}

// Handler serves the Prometheus text exposition format for this package's
// private registry. Mount it behind the app's Basic Auth middleware.
func Handler() http.Handler {
	return promhttp.HandlerFor(registry, promhttp.HandlerOpts{})
}

// Endpoint classifies an upstream URL into a bounded label value.
func Endpoint(rawURL string) string {
	if strings.Contains(rawURL, "api.e-hentai.org") {
		return "api"
	}
	// Strip query for path matching; search/listing pages are same-host with
	// distinguishing query params.
	path := rawURL
	if idx := strings.IndexByte(path, '?'); idx >= 0 {
		path = path[:idx]
	}
	switch {
	case strings.Contains(path, "/s/"):
		return "page_image"
	case strings.Contains(path, "/g/"):
		return "gallery"
	case strings.Contains(rawURL, "f_search="):
		return "search"
	case strings.HasSuffix(path, "/watched") || strings.HasSuffix(path, "/popular"):
		return "listing"
	case strings.Contains(path, ".jpg") || strings.Contains(path, ".png") ||
		strings.Contains(path, ".webp") || strings.Contains(path, ".gif") ||
		strings.Contains(path, "hath"):
		return "image_cdn"
	case strings.HasSuffix(path, "/") || strings.HasSuffix(path, "/exhentai.org"):
		return "listing"
	default:
		return "other"
	}
}

// Transport records one upstream exchange. Wrap it around the shared
// http.Client transport so every exhentai request is counted.
type Transport struct {
	base http.RoundTripper
}

// NewTransport wraps base (nil means http.DefaultTransport) with metrics.
func NewTransport(base http.RoundTripper) *Transport {
	if base == nil {
		base = http.DefaultTransport
	}
	return &Transport{base: base}
}

func (t *Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	endpoint := classifyRequest(req)
	method := req.Method
	start := time.Now()

	resp, err := t.base.RoundTrip(req)

	statusCode := "0"
	outcome := OutcomeError
	if err == nil {
		statusCode = strconv.Itoa(resp.StatusCode)
		outcome = OutcomeOK
		requestsTotal.WithLabelValues(endpoint, method, statusCode, outcome).Inc()
		requestDuration.WithLabelValues(endpoint, method).Observe(time.Since(start).Seconds())
		// ContentLength is the announced size; actual body length is counted
		// by callers that buffer (readDocument etc.) and would double-count
		// here, so only record the announced value when non-negative.
		if resp.ContentLength >= 0 {
			responseBytes.WithLabelValues(endpoint).Add(float64(resp.ContentLength))
		}
		return resp, err
	}

	requestsTotal.WithLabelValues(endpoint, method, statusCode, outcome).Inc()
	requestDuration.WithLabelValues(endpoint, method).Observe(time.Since(start).Seconds())
	return nil, err
}

// classifyRequest picks the endpoint label from the request URL without
// depending on the full raw string (avoids unbounded label values).
func classifyRequest(req *http.Request) string {
	u := req.URL
	if u == nil {
		return "other"
	}
	return Endpoint(u.String())
}

// ObserveClassification records a content-level verdict about a completed
// upstream exchange (sad panda page, IP ban page, non-200 status).
func ObserveClassification(endpoint, outcome string) {
	classificationTotal.WithLabelValues(endpoint, outcome).Inc()
}

// ObserveFetchCache records a shared-fetch cache lookup. result is one of
// "hit", "miss", "coalesced".
func ObserveFetchCache(result string) {
	fetchCacheRequests.WithLabelValues(result).Inc()
}

// SetFetchCacheEntries updates the gauge for live cache size.
func SetFetchCacheEntries(n int) {
	fetchCacheEntries.Set(float64(n))
}

// ObserveImageCache records a MinIO read-through image cache lookup. kind is
// one of "page_image", "thumbnail", "sprite"; result is "hit" or "miss".
func ObserveImageCache(kind, result string) {
	imageCacheRequests.WithLabelValues(kind, result).Inc()
}

// ObserveGalleryCache records a gallery metadata cache lookup. kind is one of
// "gallery", "details", "pages", "page_thumb"; result is "hit" or "miss".
func ObserveGalleryCache(kind, result string) {
	galleryCacheRequests.WithLabelValues(kind, result).Inc()
}

// ResetForTest clears all metric values so tests start from zero. Not for
// production use.
func ResetForTest() {
	requestsTotal.Reset()
	requestDuration.Reset()
	responseBytes.Reset()
	classificationTotal.Reset()
	fetchCacheRequests.Reset()
	fetchCacheEntries.Set(0)
	imageCacheRequests.Reset()
	galleryCacheRequests.Reset()
}
