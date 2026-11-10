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

// exhentaiCookieDomain scopes the session cookies to every *.exhentai.org
// host. Without it the jar stores them host-only for exhentai.org, and the
// setusertag API calls to s.exhentai.org go out unauthenticated (the server
// rejects them with errors like "target not exist").
const exhentaiCookieDomain = "exhentai.org"

// CookieConfig stores cookie configuration.
type CookieConfig = config.CookieConfig

// IsValid checks whether the required cookies are present.
func IsValidCookieConfig(c *CookieConfig) bool {
	return c.IpbMemberID != "" && c.IpbPassHash != ""
}

// validatingTransport guards every outbound request the production client
// makes: gallery pages, API calls, scraped image URLs and redirect targets all
// have to resolve to an allowlisted upstream host. Tests inject their own
// http.Client, so httptest servers keep working.
type validatingTransport struct {
	next http.RoundTripper
}

func (t validatingTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := validateUpstreamURL(req.URL.String()); err != nil {
		return nil, err
	}
	return t.next.RoundTrip(req)
}

func (t validatingTransport) CloseIdleConnections() {
	if closer, ok := t.next.(interface{ CloseIdleConnections() }); ok {
		closer.CloseIdleConnections()
	}
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
		{Name: "ipb_member_id", Value: cfg.IpbMemberID, Domain: exhentaiCookieDomain},
		{Name: "ipb_pass_hash", Value: cfg.IpbPassHash, Domain: exhentaiCookieDomain},
	}
	if cfg.Igneous != "" {
		cookies = append(cookies, &http.Cookie{Name: "igneous", Value: cfg.Igneous, Domain: exhentaiCookieDomain})
	}
	if cfg.SK != "" {
		cookies = append(cookies, &http.Cookie{Name: "sk", Value: cfg.SK, Domain: exhentaiCookieDomain})
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
	// Cookie lengths (never values) make a truncated paste or an ignored
	// .env entry visible without leaking the session: ipb_pass_hash is always 32
	// hex chars, and a missing igneous means the first request has to earn one.
	slog.Info("exhentai cookies loaded",
		"ipb_member_id_len", len(cfg.IpbMemberID),
		"ipb_pass_hash_len", len(cfg.IpbPassHash),
		"igneous_len", len(cfg.Igneous),
		"sk_len", len(cfg.SK),
	)

	return &http.Client{
		Jar:       jar,
		Transport: validatingTransport{next: metrics.NewTransport(transport)},
		// Re-validate every redirect target: the initial URL is allowlisted,
		// but a hostile/compromised upstream could redirect to localhost or a
		// cloud metadata endpoint.
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return fmt.Errorf("stopped after 10 redirects")
			}
			return validateUpstreamURL(req.URL.String())
		},
	}, nil
}
