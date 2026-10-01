package exhentai

import (
	"testing"
)

func TestValidatePageThumbnailURL(t *testing.T) {
	valid := []string{
		"https://zoycbewnml.hath.network/c2/nu/1-0.webp",
		"https://ehgt.org/a.webp",
		"https://sub.e-hentai.org/a.webp",
		"https://exhentai.org/a.webp",
	}
	for _, u := range valid {
		if err := ValidatePageThumbnailURL(u); err != nil {
			t.Errorf("ValidatePageThumbnailURL(%q) = %v, want nil", u, err)
		}
	}

	invalid := []string{
		"",
		"http://cdn.hath.network/a.webp",
		"https://evil.example.com/a.webp",
		"https://evilhath.network/a.webp",
		"https://localhost/a.webp",
		"https://192.168.1.1/a.webp",
	}
	for _, u := range invalid {
		if err := ValidatePageThumbnailURL(u); err == nil {
			t.Errorf("ValidatePageThumbnailURL(%q) = nil, want error", u)
		}
	}
}
