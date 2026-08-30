package main

import (
	"fmt"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"os"
	"strings"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <url> [page]\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s https://exhentai.org/\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s https://exhentai.org/watched\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s https://exhentai.org/popular\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s https://exhentai.org/popular?page=2\n", os.Args[0])
		os.Exit(1)
	}

	targetURL := os.Args[1]

	// Load cookies from environment variables (no content printing)
	memberID := os.Getenv("EHENTAI_COOKIE_IPB_MEMBER_ID")
	passHash := os.Getenv("EHENTAI_COOKIE_IPB_PASS_HASH")
	igneous := os.Getenv("EHENTAI_COOKIE_IGNEOUS")
	sk := os.Getenv("EHENTAI_COOKIE_SK")

	// Also support EHENTAI_COOKIE="ipb_member_id=xxx; ipb_pass_hash=xxx"
	if cookieStr := os.Getenv("EHENTAI_COOKIE"); cookieStr != "" && memberID == "" {
		pairs := strings.SplitSeq(cookieStr, ";")
		for pair := range pairs {
			pair = strings.TrimSpace(pair)
			parts := strings.SplitN(pair, "=", 2)
			if len(parts) != 2 {
				continue
			}
			key := strings.TrimSpace(parts[0])
			value := strings.TrimSpace(parts[1])
			switch key {
			case "ipb_member_id":
				memberID = value
			case "ipb_pass_hash":
				passHash = value
			case "igneous":
				igneous = value
			case "sk":
				sk = value
			}
		}
	}

	if memberID == "" || passHash == "" {
		fmt.Fprintln(os.Stderr, "Error: missing required cookies")
		fmt.Fprintln(os.Stderr, "Set EHENTAI_COOKIE or EHENTAI_COOKIE_IPB_MEMBER_ID + EHENTAI_COOKIE_IPB_PASS_HASH")
		os.Exit(1)
	}

	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(targetURL)
	cookies := []*http.Cookie{
		{Name: "ipb_member_id", Value: memberID},
		{Name: "ipb_pass_hash", Value: passHash},
	}
	if igneous != "" {
		cookies = append(cookies, &http.Cookie{Name: "igneous", Value: igneous})
	}
	if sk != "" {
		cookies = append(cookies, &http.Cookie{Name: "sk", Value: sk})
	}
	jar.SetCookies(u, cookies)

	client := &http.Client{Jar: jar}

	req, _ := http.NewRequest("GET", targetURL, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", "https://exhentai.org/")

	resp, err := client.Do(req)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error fetching %s: %v\n", targetURL, err)
		os.Exit(1)
	}
	defer resp.Body.Close()

	fmt.Fprintf(os.Stderr, "HTTP %d, Content-Type: %s\n", resp.StatusCode, resp.Header.Get("Content-Type"))

	body, _ := io.ReadAll(resp.Body)
	fmt.Print(string(body))
}
