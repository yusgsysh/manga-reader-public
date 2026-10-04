package exhentai

import (
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"sync"
	"time"

	"manga-reader/internal/config"
)

const ExhentaiBase = "https://exhentai.org"

// CookieConfig stores cookie configuration.
type CookieConfig = config.CookieConfig

// IsValidCookieConfig checks whether the required cookies are present.
func IsValidCookieConfig(c *CookieConfig) bool {
	return c.IpbMemberID != "" && c.IpbPassHash != ""
}

// HotJar is a cookie jar whose contents can be replaced while the process is
// running. The settings page uses it to swap ExHentai credentials without
// rebuilding the http.Client — and therefore without disturbing in-flight
// requests or the connection pool.
type HotJar struct {
	mu  sync.RWMutex
	jar *cookiejar.Jar
}

var _ http.CookieJar = (*HotJar)(nil)

// NewHotJar builds a jar preloaded with cfg. An incomplete cfg is accepted: the
// server must be able to start before the user has configured cookies, since
// the settings page is how they get configured.
func NewHotJar(cfg CookieConfig) *HotJar {
	h := &HotJar{}
	_ = h.Replace(cfg)
	return h
}

// Replace builds a fresh jar from cfg and swaps it in. Cookies previously
// received from upstream are discarded, which is what a credential change
// wants.
func (h *HotJar) Replace(cfg CookieConfig) error {
	jar, err := cookiejar.New(nil)
	if err != nil {
		return err
	}

	u, _ := url.Parse(ExhentaiBase)
	var cookies []*http.Cookie
	for _, c := range []struct{ name, value string }{
		{"ipb_member_id", cfg.IpbMemberID},
		{"ipb_pass_hash", cfg.IpbPassHash},
		{"igneous", cfg.Igneous},
		{"sk", cfg.SK},
	} {
		if c.value != "" {
			cookies = append(cookies, &http.Cookie{Name: c.name, Value: c.value})
		}
	}
	if len(cookies) > 0 {
		jar.SetCookies(u, cookies)
	}

	h.mu.Lock()
	h.jar = jar
	h.mu.Unlock()
	return nil
}

func (h *HotJar) current() *cookiejar.Jar {
	h.mu.RLock()
	jar := h.jar
	h.mu.RUnlock()
	return jar
}

func (h *HotJar) Cookies(u *url.URL) []*http.Cookie {
	return h.current().Cookies(u)
}

func (h *HotJar) SetCookies(u *url.URL, cookies []*http.Cookie) {
	h.current().SetCookies(u, cookies)
}

// NewHTTPClient creates an HTTP client whose cookie jar can be replaced later
// through the returned HotJar.
func NewHTTPClient(cfg CookieConfig) (*http.Client, *HotJar) {
	jar := NewHotJar(cfg)

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
		Transport: transport,
	}, jar
}
