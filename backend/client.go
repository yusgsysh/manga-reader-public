package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
)

const exhentaiBase = "https://exhentai.org"

func parseCookieFile(filename string) ([]*http.Cookie, error) {
	data, err := os.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("failed to read cookie file: %w", err)
	}

	var cookieMap map[string]string
	if err := json.Unmarshal(data, &cookieMap); err != nil {
		return nil, fmt.Errorf("failed to parse JSON: %w", err)
	}

	var cookies []*http.Cookie
	for name, value := range cookieMap {
		if name == "" {
			continue
		}
		cookies = append(cookies, &http.Cookie{
			Name:  name,
			Value: value,
		})
	}

	return cookies, nil
}

func createClient(cookies []*http.Cookie) (*http.Client, error) {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create cookie jar: %w", err)
	}

	baseURL, _ := url.Parse(exhentaiBase)
	jar.SetCookies(baseURL, cookies)

	return &http.Client{Jar: jar}, nil
}

func makeRequest(client *http.Client, reqURL string) ([]byte, error) {
	req, err := http.NewRequest("GET", reqURL, nil)
	if err != nil {
		return nil, fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,image/webp,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("Referer", exhentaiBase+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		_ = body
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}

func makeGalleryURL(id int64, token string) string {
	return fmt.Sprintf("%s/g/%d/%s/", exhentaiBase, id, token)
}
