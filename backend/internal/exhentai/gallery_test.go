package exhentai

import (
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
)

func TestExtractGalleryPages_ThumbnailSprite(t *testing.T) {
	const html = `<html><body><div id="gdt" class="gt200">
	<a href="https://exhentai.org/s/aaa/1-1"><div><div title="Page 1: 01.png" style="width:200px;height:282px;background:transparent url(https://cdn.hath.network/x/1-0.webp) -0px 0 no-repeat"></div><div>Page 1</div></div></a>
	<a href="https://exhentai.org/s/bbb/1-2"><div><div title="Page 2: 02.png" style="width:200px;height:282px;background:transparent url(https://cdn.hath.network/x/1-0.webp) -200px 0 no-repeat"></div><div>Page 2</div></div></a>
	<a href="https://exhentai.org/s/ccc/1-3"><div><div>Page 3</div></div></a>
	</div></body></html>`

	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	if err != nil {
		t.Fatal(err)
	}
	pageURLs, thumbnails := extractGalleryPages(doc)

	if len(pageURLs) != 3 {
		t.Fatalf("pageURLs = %d, want 3", len(pageURLs))
	}
	if pageURLs[0] != "https://exhentai.org/s/aaa/1-1" {
		t.Errorf("pageURLs[0] = %q", pageURLs[0])
	}
	thumb := thumbnails[1]
	if thumb.SpriteURL == "" {
		t.Fatal("page[1].Thumbnail is empty, want sprite geometry")
	}
	if thumb.SpriteURL != "https://cdn.hath.network/x/1-0.webp" {
		t.Errorf("sprite = %q", thumb.SpriteURL)
	}
	if thumb.X != 200 || thumb.Y != 0 || thumb.Width != 200 || thumb.Height != 282 {
		t.Errorf("thumb = %+v, want x=200 y=0 w=200 h=282", thumb)
	}
	if thumbnails[2].SpriteURL != "" {
		t.Errorf("page without sprite should have empty thumbnail, got %+v", thumbnails[2])
	}
}

func TestGalleryPagesWalkBudget(t *testing.T) {
	cases := []struct {
		total int
		want  time.Duration
	}{
		{total: 0, want: time.Minute},
		{total: 10, want: time.Minute},
		{total: 60, want: time.Minute},        // 30s base + 30s
		{total: 120, want: 90 * time.Second},  // 30s + 60s
		{total: 500, want: 280 * time.Second}, // 30s + 250s
		{total: 2000, want: 10 * time.Minute}, // capped at 10m
	}
	for _, tc := range cases {
		if got := galleryPagesWalkBudget(tc.total); got != tc.want {
			t.Errorf("galleryPagesWalkBudget(%d) = %s, want %s", tc.total, got, tc.want)
		}
	}
}

func TestScrapeGalleryPageThumb(t *testing.T) {
	var srv *httptest.Server
	srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		switch r.URL.Query().Get("p") {
		case "", "0":
			fmt.Fprint(w, `<html><body><div class="gpc">Showing 1 - 2 of 4 images</div><div id="gdt">
			<a href="https://exhentai.org/s/a/1-1"><div><div style="width:4px;height:4px;background:transparent url(https://cdn.hath.network/1-0.webp) -0px 0 no-repeat"></div></div></a>
			<a href="https://exhentai.org/s/b/1-2"><div><div style="width:4px;height:4px;background:transparent url(https://cdn.hath.network/1-0.webp) -4px 0 no-repeat"></div></div></a>
			</div></body></html>`)
		case "1":
			fmt.Fprint(w, `<html><body><div class="gpc">Showing 3 - 4 of 4 images</div><div id="gdt">
			<a href="https://exhentai.org/s/c/1-3"><div><div style="width:4px;height:4px;background:transparent url(https://cdn.hath.network/1-1.webp) -0px 0 no-repeat"></div></div></a>
			<a href="https://exhentai.org/s/d/1-4"><div><div style="width:4px;height:4px;background:transparent url(https://cdn.hath.network/1-1.webp) -4px 0 no-repeat"></div></div></a>
			</div></body></html>`)
		default:
			fmt.Fprint(w, `<html><body></body></html>`)
		}
	}))
	t.Cleanup(srv.Close)

	thumb, found, err := ScrapeGalleryPageThumb(t.Context(), srv.Client(), srv.URL+"/g/1/tok/", 3)
	if err != nil {
		t.Fatalf("ScrapeGalleryPageThumb: %v", err)
	}
	if !found {
		t.Fatal("expected page 3 thumbnail")
	}
	if !strings.HasSuffix(thumb.SpriteURL, "1-1.webp") || thumb.X != 4 {
		t.Errorf("thumb = %+v, want sprite 1-1.webp at x=4", thumb)
	}

	if _, found, _ := ScrapeGalleryPageThumb(t.Context(), srv.Client(), srv.URL+"/g/1/tok/", 4); found {
		t.Error("index beyond total should not be found")
	}
}
