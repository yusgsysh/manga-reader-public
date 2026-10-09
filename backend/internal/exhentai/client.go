package exhentai

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"time"

	"manga-reader/internal/config"
	"manga-reader/internal/metrics"
)

const ExhentaiBase = "https://exhentai.org"

// CookieConfig stores cookie configuration.
type CookieConfig = config.CookieConfig

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
	transport.MaxIdleConns = 50
	transport.MaxIdleConnsPerHost = 10
	transport.MaxConnsPerHost = 20
	transport.IdleConnTimeout = 90 * time.Second

	slog.Debug("http client created",
		"max_idle_conns", transport.MaxIdleConns,
		"max_idle_conns_per_host", transport.MaxIdleConnsPerHost,
		"max_conns_per_host", transport.MaxConnsPerHost,
	)

	return &http.Client{
		Jar:       jar,
		Transport: metrics.NewTransport(transport),
	}, nil
}
