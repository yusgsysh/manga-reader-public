package exhentai

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
)

const ExhentaiBase = "https://exhentai.org"

// CookieConfig stores cookie configuration.
type CookieConfig struct {
	IpbMemberID string
	IpbPassHash string
	Igneous     string
	SK          string
}

// LoadCookieConfig loads cookie configuration from environment variables.
func LoadCookieConfig() *CookieConfig {
	cfg := &CookieConfig{}

	if cookieStr := os.Getenv("EHENTAI_COOKIE"); cookieStr != "" {
		cfg.parseCookieString(cookieStr)
	}

	if v := os.Getenv("EHENTAI_COOKIE_IPB_MEMBER_ID"); v != "" {
		cfg.IpbMemberID = v
	}
	if v := os.Getenv("EHENTAI_COOKIE_IPB_PASS_HASH"); v != "" {
		cfg.IpbPassHash = v
	}
	if v := os.Getenv("EHENTAI_COOKIE_IGNEOUS"); v != "" {
		cfg.Igneous = v
	}
	if v := os.Getenv("EHENTAI_COOKIE_SK"); v != "" {
		cfg.SK = v
	}

	return cfg
}

func (c *CookieConfig) parseCookieString(s string) {
	pairs := strings.SplitSeq(s, ";")
	for pair := range pairs {
		pair = strings.TrimSpace(pair)
		if pair == "" {
			continue
		}
		parts := strings.SplitN(pair, "=", 2)
		if len(parts) != 2 {
			continue
		}
		key := strings.TrimSpace(parts[0])
		value := strings.TrimSpace(parts[1])
		switch key {
		case "ipb_member_id":
			c.IpbMemberID = value
		case "ipb_pass_hash":
			c.IpbPassHash = value
		case "igneous":
			c.Igneous = value
		case "sk":
			c.SK = value
		}
	}
}

// IsValid checks whether the required cookies are present.
func (c *CookieConfig) IsValid() bool {
	return c.IpbMemberID != "" && c.IpbPassHash != ""
}

// CreateHTTPClient creates an HTTP client with ExHentai cookies.
func CreateHTTPClient(cfg *CookieConfig) (*http.Client, error) {
	if !cfg.IsValid() {
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

	return &http.Client{Jar: jar}, nil
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
