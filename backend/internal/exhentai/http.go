package exhentai

import (
	"context"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

const (
	ExhentaiURL = "https://exhentai.org"

	// upstreamDocTimeout bounds each HTML document fetch so a hung
	// upstream connection cannot stall a request indefinitely.
	upstreamDocTimeout = 15 * time.Second
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
	resp, err := httpGet(ctx, client, target)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	if sadPandaCheck(doc) {
		return nil, ErrSadPanda
	}
	if ipBannedCheck(doc) {
		return nil, ErrIPBanned
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
	req, err := http.NewRequestWithContext(ctx, "POST", target, strings.NewReader(form.Encode()))
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", ExhentaiURL+"/")
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	if sadPandaCheck(doc) {
		return nil, ErrSadPanda
	}
	if ipBannedCheck(doc) {
		return nil, ErrIPBanned
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
