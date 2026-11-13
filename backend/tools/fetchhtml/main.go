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

// fetchhtml is a debugging helper: it fetches one ExHentai page with a session
// supplied on the command line, so scrapers can be checked against the real
// upstream HTML. The session is per user now and never read from the
// environment; pass it explicitly as a cookie header, e.g.
//
//	EH_COOKIE='ipb_member_id=...; ipb_pass_hash=...' go run ./tools/fetchhtml https://exhentai.org/
func main() {
	if len(os.Args) < 2 {
		fmt.Fprintf(os.Stderr, "Usage: %s <url>\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "Set EH_COOKIE to the browser cookie header (never commit or log it).\n")
		fmt.Fprintf(os.Stderr, "Examples:\n")
		fmt.Fprintf(os.Stderr, "  %s https://exhentai.org/\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s https://exhentai.org/watched\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s https://exhentai.org/popular\n", os.Args[0])
		fmt.Fprintf(os.Stderr, "  %s https://exhentai.org/popular?page=2\n", os.Args[0])
		os.Exit(1)
	}

	targetURL := os.Args[1]

	// The session is a per-user secret, so it is passed in the environment of
	// this one command (EH_COOKIE) rather than configured on the server.
	raw := strings.TrimSpace(os.Getenv("EH_COOKIE"))
	if raw == "" {
		fmt.Fprintln(os.Stderr, "Error: EH_COOKIE is empty")
		fmt.Fprintln(os.Stderr, "Example: EH_COOKIE='ipb_member_id=1; ipb_pass_hash=...'")
		os.Exit(1)
	}

	jar, _ := cookiejar.New(nil)
	u, _ := url.Parse(targetURL)
	var cookies []*http.Cookie
	for pair := range strings.SplitSeq(raw, ";") {
		pair = strings.TrimSpace(pair)
		name, value, found := strings.Cut(pair, "=")
		if !found {
			continue
		}
		cookies = append(cookies, &http.Cookie{
			Name:  strings.TrimSpace(name),
			Value: strings.TrimSpace(value),
		})
	}
	if len(cookies) == 0 {
		fmt.Fprintln(os.Stderr, "Error: EH_COOKIE has no k=v pairs")
		os.Exit(1)
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
