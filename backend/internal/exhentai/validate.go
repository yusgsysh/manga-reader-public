package exhentai

import (
	"fmt"
	"net"
	"net/url"
	"strings"
)

func GalleryURL(id, token string) string {
	return ExhentaiURL + "/g/" + id + "/" + token + "/"
}

func ValidateThumbnailURL(rawURL string) error {
	return validateThumbnailURL(rawURL)
}

func ValidatePageURL(rawURL string) error {
	return validatePageURL(rawURL)
}

// ValidatePageThumbnailURL validates a sprite image URL used by the page
// thumbnail crop API. Sprites are served from the e-hentai image CDN, which is
// a different host set than the gallery cover thumbnails.
func ValidatePageThumbnailURL(rawURL string) error {
	return validatePageThumbnailURL(rawURL)
}

var allowedThumbnailHosts = map[string]struct{}{
	"s.exhentai.org":  {},
	"ehgt.org":        {},
	"ul.e-hentai.org": {},
}

func validateThumbnailURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("missing url parameter")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url")
	}

	if u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("invalid url: missing host")
	}

	if isBlockedInternalHost(host) {
		return fmt.Errorf("unsupported URL: internal address not allowed")
	}

	if _, ok := allowedThumbnailHosts[host]; !ok {
		return fmt.Errorf("unsupported URL: domain not allowed")
	}

	return nil
}

// allowedPageThumbnailHosts lists the image CDN hosts that may serve gallery
// thumbnail sprites. Matching is by exact host or as a dot-suffixed subdomain.
var allowedPageThumbnailHosts = []string{
	"hath.network",
	"e-hentai.org",
	"exhentai.org",
	"ehgt.org",
}

func validatePageThumbnailURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("missing url parameter")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url")
	}

	if u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("invalid url: missing host")
	}

	if isBlockedInternalHost(host) {
		return fmt.Errorf("unsupported URL: internal address not allowed")
	}

	for _, allowed := range allowedPageThumbnailHosts {
		if host == allowed || strings.HasSuffix(host, "."+allowed) {
			return nil
		}
	}
	return fmt.Errorf("unsupported URL: domain not allowed")
}

func validatePageURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("missing url parameter")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url")
	}

	if u.Scheme != "https" {
		return fmt.Errorf("unsupported URL scheme: %s", u.Scheme)
	}

	host := u.Hostname()
	if host == "" {
		return fmt.Errorf("invalid url: missing host")
	}

	if isBlockedInternalHost(host) {
		return fmt.Errorf("unsupported URL: internal address not allowed")
	}

	allowed := false
	if host == "exhentai.org" || host == "e-hentai.org" {
		allowed = true
	}

	if !allowed {
		return fmt.Errorf("unsupported URL: domain not allowed")
	}

	return nil
}

func isBlockedInternalHost(host string) bool {
	if host == "localhost" || host == "127.0.0.1" || host == "::1" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsPrivate() || ip.IsLoopback() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
	}
	return false
}
