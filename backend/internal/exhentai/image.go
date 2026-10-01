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
)

var nlReg = regexp.MustCompile(`nl\('(.+?)'\)`)

// maxImageBytes limits the size of a single image response to prevent OOM.
const maxImageBytes = 30 << 20 // 30 MiB

func ScrapePageImageURL(ctx context.Context, client *http.Client, pageURL string) (imgURL string, fallbackURL string, err error) {
	resp, err := httpGet(ctx, client, pageURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
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
		return nil, "", fmt.Errorf("HTTP %d: %w", resp.StatusCode, ErrNonOKStatus)
	}

	contentType = resp.Header.Get("Content-Type")
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}
