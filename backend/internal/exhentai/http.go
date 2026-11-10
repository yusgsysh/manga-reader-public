package exhentai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"manga-reader/internal/metrics"
)

const (
	ExhentaiURL = "https://exhentai.org"

	// upstreamDocTimeout bounds each HTML document fetch so a hung
	// upstream connection cannot stall a request indefinitely.
	upstreamDocTimeout = 15 * time.Second

	// maxDocBytes caps an upstream HTML/JSON body before parsing, so an
	// oversized or hostile gzip response cannot expand without bound.
	maxDocBytes = 16 << 20
)

func httpGet(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", ExhentaiURL+"/")
	return client.Do(req)
}

// httpGetDoc fetches and parses an upstream document. Identical GETs are
// coalesced and shared for a short window (see fetchOnce) so different
// endpoints deriving data from the same page trigger one upstream request.
func httpGetDoc(ctx context.Context, client *http.Client, target string) (*goquery.Document, error) {
	value, err := fetchOnce(ctx, client, fetchCacheKey(client, "GET", target), func(fetchCtx context.Context) (any, error) {
		return fetchDoc(fetchCtx, client, target)
	})
	if err != nil {
		return nil, err
	}
	return value.(*goquery.Document), nil
}

// httpGetDocDirect fetches a document without the short-lived dedup window. The
// single-page thumbnail fallback uses it so it never joins a stuck shared page
// walk; it always issues its own bounded request.
func httpGetDocDirect(ctx context.Context, client *http.Client, target string) (*goquery.Document, error) {
	fetchCtx, cancel := context.WithTimeout(ctx, upstreamDocTimeout)
	defer cancel()
	return fetchDoc(fetchCtx, client, target)
}

func fetchDoc(ctx context.Context, client *http.Client, target string) (*goquery.Document, error) {
	return doDoc(ctx, client, target, func(ctx context.Context) (*http.Response, error) {
		return httpGet(ctx, client, target)
	})
}

// igneousHealCooldown is how long a failed heal suppresses further retries. A
// stale igneous cookie heals on the first retry, but when the retry fails too the
// session problem is the credentials or the IP, and every request would
// otherwise fire twice.
const igneousHealCooldown = 10 * time.Minute

var healGate healGateState

// healGateState tracks when retries are allowed again after a failed heal.
type healGateState struct {
	mu           sync.Mutex
	blockedUntil time.Time
}

// allow reports whether a sad-panda heal may run now.
func (g *healGateState) allow() bool {
	g.mu.Lock()
	defer g.mu.Unlock()
	return !time.Now().Before(g.blockedUntil)
}

// record closes or reopens the gate depending on whether the heal worked.
func (g *healGateState) record(ok bool) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if ok {
		g.blockedUntil = time.Time{}
		return
	}
	g.blockedUntil = time.Now().Add(igneousHealCooldown)
}

// doDoc runs one document request and retries once without the igneous cookie
// when ExHentai answers with the empty "sad panda" page.
//
// A stale igneous cookie is worse than no igneous cookie: ExHentai validates it
// against the account's current session and serves the sad panda page when it no
// longer matches, even though ipb_member_id/ipb_pass_hash are still valid. That
// happens routinely after signing in again elsewhere, which rotates igneous and
// leaves the value in .env dead. Without the cookie the same credentials
// authenticate and the response's Set-Cookie installs a fresh igneous in the
// jar, so the deployment recovers without editing .env or restarting.
func doDoc(ctx context.Context, client *http.Client, target string, do func(context.Context) (*http.Response, error)) (*goquery.Document, error) {
	doc, err := doDocOnce(ctx, target, do)
	if !errors.Is(err, ErrSadPanda) || !healGate.allow() || !dropIgneous(client, target) {
		return doc, err
	}
	slog.Warn("exhentai rejected the igneous cookie; retrying without it",
		"url", target,
		"hint", "refresh EHENTAI_COOKIE_IGNEOUS to stop the extra round trip")
	healed, healErr := doDocOnce(ctx, target, do)
	healGate.record(healErr == nil)
	return healed, healErr
}

// dropIgneous expires the igneous cookie in the client's jar so the next request
// authenticates with ipb_member_id/ipb_pass_hash alone. It reports whether a
// cookie was actually present: without one the sad panda is about the
// credentials, and retrying only doubles upstream traffic.
func dropIgneous(client *http.Client, target string) bool {
	if client == nil || client.Jar == nil {
		return false
	}
	u, err := url.Parse(target)
	if err != nil || u.Host == "" {
		return false
	}
	present := false
	for _, c := range client.Jar.Cookies(u) {
		if c.Name == "igneous" {
			present = true
			break
		}
	}
	if !present {
		return false
	}
	// Expire both shapes: the domain cookie the production client seeds for
	// *.exhentai.org, and a host-only cookie another client may have set.
	expired := []*http.Cookie{
		{Name: "igneous", Value: "", Path: "/", Domain: exhentaiCookieDomain, MaxAge: -1, Expires: time.Unix(1, 0)},
		{Name: "igneous", Value: "", Path: "/", MaxAge: -1, Expires: time.Unix(1, 0)},
	}
	client.Jar.SetCookies(u, expired)
	return true
}

// doDocOnce issues one request and classifies the response document.
func doDocOnce(ctx context.Context, target string, do func(context.Context) (*http.Response, error)) (*goquery.Document, error) {
	resp, err := do(ctx)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := readDocument(resp.Body)
	if err != nil {
		return nil, err
	}

	// Content heuristics run before the status check so the specific auth
	// failure pages keep their dedicated error types.
	if sadPandaCheck(doc) {
		metrics.ObserveClassification(metrics.Endpoint(target), metrics.OutcomeSadPanda, resp.StatusCode)
		return nil, ErrSadPanda
	}
	if ipBannedCheck(doc) {
		metrics.ObserveClassification(metrics.Endpoint(target), metrics.OutcomeIPBanned, resp.StatusCode)
		return nil, ErrIPBanned
	}
	if resp.StatusCode != http.StatusOK {
		metrics.ObserveClassification(metrics.Endpoint(target), metrics.OutcomeHTTPError, resp.StatusCode)
		return nil, &httpStatusError{code: resp.StatusCode}
	}
	return doc, nil
}

// httpPostFormDoc POSTs an application/x-www-form-urlencoded form and parses the
// response document, applying the same User-Agent/Referer and auth-failure
// checks as httpGetDoc. Identical POSTs share one upstream request for a short
// window.
func httpPostFormDoc(ctx context.Context, client *http.Client, target string, form url.Values) (*goquery.Document, error) {
	key := fetchCacheKey(client, "POST", target+"\n"+form.Encode())
	value, err := fetchOnce(ctx, client, key, func(fetchCtx context.Context) (any, error) {
		return postFormDoc(fetchCtx, client, target, form)
	})
	if err != nil {
		return nil, err
	}
	return value.(*goquery.Document), nil
}

func postFormDoc(ctx context.Context, client *http.Client, target string, form url.Values) (*goquery.Document, error) {
	return doDoc(ctx, client, target, func(ctx context.Context) (*http.Response, error) {
		req, err := http.NewRequestWithContext(ctx, "POST", target, strings.NewReader(form.Encode()))
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
		req.Header.Set("Referer", ExhentaiURL+"/")
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		return client.Do(req)
	})
}

// readDocument parses an upstream body, enforcing maxDocBytes so a decompression
// bomb or oversized page cannot be buffered without limit.
func readDocument(body io.Reader) (*goquery.Document, error) {
	data, err := io.ReadAll(io.LimitReader(body, maxDocBytes))
	if err != nil {
		return nil, err
	}
	if len(data) == maxDocBytes {
		return nil, fmt.Errorf("upstream document exceeds %d bytes", maxDocBytes)
	}
	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(data))
	if err != nil {
		return nil, err
	}
	return doc, nil
}

func sadPandaCheck(doc *goquery.Document) bool {
	head := doc.Find("head")
	body := doc.Find("body")
	return head.Text() == "" && body.Text() == ""
}

func ipBannedCheck(doc *goquery.Document) bool {
	return strings.Contains(doc.Find("body").Text(), "This IP address has been temporarily banned")
}
