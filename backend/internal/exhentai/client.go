package exhentai

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"manga-reader/internal/config"
)

const ExhentaiBase = "https://exhentai.org"

// CookieConfig stores cookie configuration.
type CookieConfig = config.CookieConfig

// LoadCookieConfig loads cookie configuration from environment variables.
// Deprecated: use config.Load() instead.
func LoadCookieConfig() *CookieConfig {
	cfg, err := config.Load()
	if err != nil {
		return &CookieConfig{}
	}
	return &cfg.Cookie
}

// IsValid checks whether the required cookies are present.
func IsValidCookieConfig(c *CookieConfig) bool {
	return c.IpbMemberID != "" && c.IpbPassHash != ""
}

// CreateHTTPClient creates an HTTP client with ExHentai cookies.
func CreateHTTPClient(cfg *CookieConfig) (*http.Client, error) {
	if !IsValidCookieConfig(cfg) {
		return nil, fmt.Errorf("missing required cookies: ipb_member_id and ipb_pass_hash")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	u, _ := url.Parse(ExhentaiBase)
	cookies := []*http.Cookie{
		{Name: "ipb_member_id", Value: cfg.IpbMemberID},
		{Name: "ipb_pass_hash", Value: cfg.IpbPassHash},
	}
	if cfg.Igneous != "" {
		cookies = append(cookies, &http.Cookie{Name: "igneous", Value: cfg.Igneous})
	}
	if cfg.SK != "" {
		cookies = append(cookies, &http.Cookie{Name: "sk", Value: cfg.SK})
	}
	jar.SetCookies(u, cookies)

	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.ResponseHeaderTimeout = 30 * time.Second
	transport.IdleConnTimeout = 90 * time.Second

	slog.Debug("http client created",
		"max_idle_conns", transport.MaxIdleConns,
		"max_idle_conns_per_host", transport.MaxIdleConnsPerHost,
		"max_conns_per_host", transport.MaxConnsPerHost,
	)

	return &http.Client{
		Jar:       jar,
		Transport: transport,
	}, nil
}

// MakeRequest sends an HTTP GET request with ExHentai headers.
func MakeRequest(ctx context.Context, client *http.Client, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", ExhentaiBase+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	return io.ReadAll(resp.Body)
}
