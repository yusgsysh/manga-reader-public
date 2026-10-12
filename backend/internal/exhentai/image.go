package exhentai

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"

	"github.com/PuerkitoBio/goquery"

	"manga-reader/internal/metrics"
)

var nlReg = regexp.MustCompile(`nl\('(.+?)'\)`)

// MaxImageBytes limits the size of a single image response to prevent OOM.
const MaxImageBytes = 30 << 20 // 30 MiB

func ScrapePageImageURL(ctx context.Context, client *http.Client, pageURL string) (imgURL string, fallbackURL string, err error) {
	resp, err := httpGet(ctx, client, pageURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	// A non-OK status here means the page URL itself is gone (e.g. a stale
	// cached page URL after a gallery was replaced). Surface it as a status
	// error so callers can classify it as permanent and refresh the gallery
	// cache, instead of the generic "could not find image src" parse failure.
	if resp.StatusCode != http.StatusOK {
		metrics.ObserveClassification(metrics.Endpoint(pageURL), metrics.OutcomeHTTPError, resp.StatusCode)
		return "", "", &httpStatusError{code: resp.StatusCode}
	}

	body, err := io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes))
	if err != nil {
		return "", "", err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}

	img, ok := doc.Find("#img").Attr("src")
	if !ok || img == "" {
		return "", "", fmt.Errorf("could not find image src")
	}

	onclick, _ := doc.Find("#loadfail").Attr("onclick")
	fallbackURL = BuildNlFallbackURL(pageURL, onclick)

	return img, fallbackURL, nil
}

func BuildNlFallbackURL(pageURL, onclick string) string {
	if onclick == "" {
		return ""
	}
	matches := nlReg.FindStringSubmatch(onclick)
	if len(matches) == 0 {
		return ""
	}
	u, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	nl := matches[1]
	if u.RawQuery != "" {
		u.RawQuery += "&nl=" + nl
	} else {
		u.RawQuery = "nl=" + nl
	}
	return u.String()
}

const MaxNlRetries = 2

func FetchPageImage(ctx context.Context, client *http.Client, pageURL string) (data []byte, contentType string, err error) {
	imgURL, fallbackURL, err := ScrapePageImageURL(ctx, client, pageURL)
	if err != nil {
		return nil, "", err
	}

	data, contentType, err = ProxyImage(ctx, client, imgURL)
	if err != nil && fallbackURL != "" {
		for range MaxNlRetries {
			metrics.ObserveUpstreamRetry(metrics.Endpoint(pageURL), "nl_fallback")
			imgURL, fallbackURL, err = ScrapePageImageURL(ctx, client, fallbackURL)
			if err != nil {
				break
			}
			data, contentType, err = ProxyImage(ctx, client, imgURL)
			if err == nil {
				break
			}
		}
	}
	return data, contentType, err
}

// ImageStream is an upstream image whose body has not been read into memory
// yet, so callers can proxy it to a client at constant memory. Body must be
// closed by the caller. Size is the upstream Content-Length, or -1 when
// unknown.
type ImageStream struct {
	Body        io.ReadCloser
	ContentType string
	Size        int64
}

// imageBody pairs a (possibly prefixed) reader with the upstream body closer.
type imageBody struct {
	io.Reader
	io.Closer
}

// ProxyImageStream opens imgURL without buffering the image. A leading probe
// read makes an empty body fail before the caller commits response headers,
// so retry strategies (e.g. the nl fallback) still apply; everything after
// the probe streams through.
func ProxyImageStream(ctx context.Context, client *http.Client, imgURL string) (*ImageStream, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imgURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", ExhentaiURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != http.StatusOK {
		resp.Body.Close()
		metrics.ObserveClassification(metrics.Endpoint(imgURL), metrics.OutcomeHTTPError, resp.StatusCode)
		return nil, &httpStatusError{code: resp.StatusCode}
	}

	probe := make([]byte, 512)
	n, err := io.ReadFull(resp.Body, probe)
	if n == 0 {
		resp.Body.Close()
		if err == nil || err == io.EOF {
			err = fmt.Errorf("empty image body from upstream")
		}
		return nil, err
	}

	return &ImageStream{
		Body: &imageBody{
			Reader: io.MultiReader(bytes.NewReader(probe[:n]), resp.Body),
			Closer: resp.Body,
		},
		ContentType: resp.Header.Get("Content-Type"),
		Size:        resp.ContentLength,
	}, nil
}

// FetchPageImageStream is the streaming counterpart of FetchPageImage: it
// scrapes the page URL (buffered, small) and returns the image body without
// reading it, keeping the same nl-fallback retry semantics for failures
// detected before the caller starts writing a response.
func FetchPageImageStream(ctx context.Context, client *http.Client, pageURL string) (*ImageStream, error) {
	imgURL, fallbackURL, err := ScrapePageImageURL(ctx, client, pageURL)
	if err != nil {
		return nil, err
	}

	stream, err := ProxyImageStream(ctx, client, imgURL)
	if err != nil && fallbackURL != "" {
		for range MaxNlRetries {
			metrics.ObserveUpstreamRetry(metrics.Endpoint(pageURL), "nl_fallback")
			imgURL, fallbackURL, err = ScrapePageImageURL(ctx, client, fallbackURL)
			if err != nil {
				break
			}
			stream, err = ProxyImageStream(ctx, client, imgURL)
			if err == nil {
				break
			}
		}
	}
	return stream, err
}

func ProxyImage(ctx context.Context, client *http.Client, imgURL string) (data []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imgURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", ExhentaiURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		metrics.ObserveClassification(metrics.Endpoint(imgURL), metrics.OutcomeHTTPError, resp.StatusCode)
		return nil, "", &httpStatusError{code: resp.StatusCode}
	}

	contentType = resp.Header.Get("Content-Type")
	data, err = io.ReadAll(io.LimitReader(resp.Body, MaxImageBytes))
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}
