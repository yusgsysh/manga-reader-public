package main

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

const exhentaiBase = "https://exhentai.org"

// CookieConfig 存储 cookie 配置
type CookieConfig struct {
	IpbMemberID string
	IpbPassHash string
	Igneous     string
	SK          string
}

// LoadCookieConfig 从环境变量加载 cookie
func LoadCookieConfig() *CookieConfig {
	cfg := &CookieConfig{}

	// 支持两种环境变量格式
	// 方式一：完整 cookie 字符串 (如: "ipb_member_id=xxx; ipb_pass_hash=xxx")
	if cookieStr := os.Getenv("EHENTAI_COOKIE"); cookieStr != "" {
		cfg.parseCookieString(cookieStr)
	}

	// 方式二：单独环境变量
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

// parseCookieString 解析 cookie 字符串
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

// IsValid 检查必需的 cookie 是否存在
func (c *CookieConfig) IsValid() bool {
	return c.IpbMemberID != "" && c.IpbPassHash != ""
}

// CreateHTTPClient 创建带 cookie 的 HTTP 客户端
func CreateHTTPClient(cfg *CookieConfig) (*http.Client, error) {
	if !cfg.IsValid() {
		return nil, fmt.Errorf("missing required cookies: ipb_member_id and ipb_pass_hash")
	}

	jar, err := cookiejar.New(nil)
	if err != nil {
		return nil, err
	}

	u, _ := url.Parse(exhentaiBase)
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

// MakeRequest 发送 HTTP 请求
func MakeRequest(ctx context.Context, client *http.Client, reqURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", exhentaiBase+"/")

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
