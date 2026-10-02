package exhentai

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"

	"manga-reader/internal/model"
)

var coverUrlReg = regexp.MustCompile(`url\(([^)]+)\)`)
var numReg = regexp.MustCompile(`Showing 1 - (\d+) of ([\d,]+) images?`)
var torrentCountReg = regexp.MustCompile(`Torrent Download \((\d+)\)`)

// pageThumbStyleReg parses the inline style ExHentai uses to place a page
// thumbnail inside its sprite, e.g.
// "width:200px;height:282px;background:transparent url(https://...webp) -0px 0 no-repeat".
var pageThumbStyleReg = regexp.MustCompile(`width:(\d+)px;height:(\d+)px;background:[^;]*url\(([^)]+)\)\s*(-?\d+)px\s*(-?\d+)`)

func ScrapeGalleryDetails(ctx context.Context, client *http.Client, galleryURL string) (model.GalleryDetail, error) {
	doc, err := httpGetDoc(ctx, client, galleryURL)
	if err != nil {
		return model.GalleryDetail{}, err
	}

	var cover string
	doc.Find("#gd1 > div").Each(func(i int, sel *goquery.Selection) {
		style, exists := sel.Attr("style")
		if exists {
			matches := coverUrlReg.FindStringSubmatch(style)
			if len(matches) > 1 {
				cover = matches[1]
			}
		}
	})

	title := doc.Find("#gn").Text()
	titleJpn := doc.Find("#gj").Text()
	cat := doc.Find("#gdc > div").Text()
	uploader := doc.Find("#gdn > a:nth-child(1)").Text()

	gdd := doc.Find("#gdd > table > tbody")
	posted := gdd.Find("tr:nth-child(1) > td.gdt2").Text()
	parent := gdd.Find("tr:nth-child(2) > td.gdt2 > a").Text()
	parentId, _ := strconv.Atoi(parent)
	visible := gdd.Find("tr:nth-child(3) > td.gdt2").Text()
	langSel := gdd.Find("tr:nth-child(4) > td.gdt2").Clone()
	langSel.Find("span").Remove()
	language := strings.TrimSpace(langSel.Text())
	translated := gdd.Find("tr:nth-child(4) > td.gdt2 > span").Text()
	fileSize := gdd.Find("tr:nth-child(5) > td.gdt2").Text()
	length := gdd.Find("tr:nth-child(6) > td.gdt2").Text()
	length = strings.TrimSuffix(length, " pages")
	lengthNum, _ := strconv.Atoi(length)
	favorited := gdd.Find("tr:nth-child(7) > td.gdt2").Text()
	favorited = strings.TrimSuffix(favorited, " times")
	favoritedNum, _ := strconv.Atoi(favorited)

	ratingCountStr := doc.Find("#rating_count").Text()
	ratingCount, _ := strconv.Atoi(ratingCountStr)
	ratingStr := doc.Find("#rating_label").Text()
	ratingStr = strings.TrimPrefix(ratingStr, "Average: ")
	ratingStr = strings.TrimSpace(ratingStr)
	rating, _ := strconv.ParseFloat(ratingStr, 64)

	torrentCount := 0
	doc.Find("a[onclick*='gallerytorrents.php']").Each(func(_ int, s *goquery.Selection) {
		if m := torrentCountReg.FindStringSubmatch(s.Text()); len(m) == 2 {
			torrentCount, _ = strconv.Atoi(m[1])
		}
	})

	var tags []model.TagItem
	taglist := doc.Find("#taglist > table > tbody")
	taglist.Find("tr").Each(func(i int, s *goquery.Selection) {
		namespace := s.Find("td:nth-child(1)").Text()
		if namespace == "" {
			return
		}
		namespace = strings.TrimSuffix(namespace, ":")
		s.Find("td:nth-child(2) > div").Each(func(i int, s *goquery.Selection) {
			tag := s.Find("a").Text()
			if tag != "" {
				tags = append(tags, model.TagItem{Namespace: namespace, Name: tag})
			}
		})
	})

	domain := ""
	if strings.Contains(galleryURL, "exhentai.org") {
		domain = "exhentai.org"
	} else if strings.Contains(galleryURL, "e-hentai.org") {
		domain = "e-hentai.org"
	}

	gIdStr := ""
	gTokenStr := ""
	d, gId, gToken := ParseGalleryURL(galleryURL)
	if d != "" {
		domain = d
		gIdStr = gId
		gTokenStr = gToken
	}
	gIdNum, _ := strconv.Atoi(gIdStr)

	return model.GalleryDetail{
		Domain:       domain,
		GalleryID:    gIdNum,
		Token:        gTokenStr,
		Cover:        cover,
		Title:        title,
		TitleJpn:     titleJpn,
		Cat:          cat,
		Uploader:     uploader,
		Posted:       posted,
		Parent:       parentId,
		Visible:      visible,
		Language:     language,
		Translated:   translated,
		FileSize:     fileSize,
		Length:       lengthNum,
		Favorited:    favoritedNum,
		RatingCount:  ratingCount,
		Rating:       rating,
		TorrentCount: torrentCount,
		Tags:         tags,
	}, nil
}

// extractGalleryPages collects gallery page links and their sprite thumbnail
// geometry from a gallery document.
// Returns page URLs and their corresponding thumbnail geometries in parallel slices.
func extractGalleryPages(doc *goquery.Document) (pageURLs []string, thumbnails []model.GalleryPageThumb) {
	doc.Find("#gdt > a").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if href == "" {
			return
		}
		pageURLs = append(pageURLs, href)
		var thumb model.GalleryPageThumb
		hasThumb := false
		if style, ok := s.Find("div[style]").First().Attr("style"); ok {
			if m := pageThumbStyleReg.FindStringSubmatch(style); len(m) == 6 {
				w, _ := strconv.Atoi(m[1])
				h, _ := strconv.Atoi(m[2])
				posX, _ := strconv.Atoi(m[4])
				posY, _ := strconv.Atoi(m[5])
				thumb = model.GalleryPageThumb{
					SpriteURL: m[3],
					X:         -posX,
					Y:         -posY,
					Width:     w,
					Height:    h,
				}
				hasThumb = true
			}
		}
		if hasThumb {
			thumbnails = append(thumbnails, thumb)
		} else {
			thumbnails = append(thumbnails, model.GalleryPageThumb{})
		}
	})
	return
}

// galleryTotalImages parses the total image count from the ".gpc" counter.
func galleryTotalImages(doc *goquery.Document) int {
	matches := numReg.FindStringSubmatch(doc.Find(".gpc").Text())
	if len(matches) < 3 {
		return 0
	}
	matches[2] = strings.ReplaceAll(matches[2], ",", "")
	total, _ := strconv.Atoi(matches[2])
	return total
}

// galleryPagesWalkBudget returns how long the pagination walk (?p=N) may take
// for a gallery whose ".gpc" total is known. It grows with the page count
// (30s base + 0.5s per page) and is clamped to [1m, 10m] so a large gallery
// is not cut off early while a stuck walk still terminates.
func galleryPagesWalkBudget(total int) time.Duration {
	d := 30*time.Second + time.Duration(total)*500*time.Millisecond
	if d < time.Minute {
		return time.Minute
	}
	if d > 10*time.Minute {
		return 10 * time.Minute
	}
	return d
}

// StreamGalleryPages fetches the full list of gallery pages (page URL plus
// sprite thumbnail geometry), walking the paginated thumbnail list when the
// gallery has more pages than fit on the first thumbnail page. Each batch is
// handed to emit as soon as it is scraped so callers can forward pages
// incrementally; total is the image count parsed from ".gpc" and is passed to
// every emit call. The pagination walk is bounded by galleryPagesWalkBudget,
// computed from that total.
//
// total == 0 means the parse failed, not an empty gallery: ExHentai galleries
// always contain at least one image, so an unparsable ".gpc" counter (or a
// document without page links) fails the scrape before anything is emitted,
// instead of letting an unknown total masquerade as a verified complete list.
//
// The returned error is nil only when the scrape is verifiably complete:
// every thumbnail page fetched without error and exactly total pages
// received. An empty thumbnail page while pages are still missing, a received
// count that overshoots total, or an emit failure (caller went away) all fail
// the scrape. Partial results are never reported as success.
func StreamGalleryPages(ctx context.Context, client *http.Client, galleryURL string, emit func(total int, pageURLs []string, thumbnails []model.GalleryPageThumb) error) error {
	doc, err := httpGetDoc(ctx, client, galleryURL)
	if err != nil {
		return err
	}

	total := galleryTotalImages(doc)
	if total <= 0 {
		return fmt.Errorf("cannot determine gallery total: %q counter missing or invalid", ".gpc")
	}
	firstURLs, firstThumbs := extractGalleryPages(doc)
	if len(firstURLs) == 0 {
		return fmt.Errorf("cannot determine gallery total: no page links in the gallery document")
	}
	if err := emit(total, firstURLs, firstThumbs); err != nil {
		return err
	}
	received := len(firstURLs)

	// The total parsed from the first document bounds the pagination walk: a
	// gallery with many pages gets more time, but a hanging or abusive walk
	// cannot run forever. The first document and the caller's cache write are
	// not covered by this budget.
	walkCtx, cancelWalk := context.WithTimeout(ctx, galleryPagesWalkBudget(total))
	defer cancelWalk()

	// Walk ?p=N until every declared page has arrived. Each thumbnail page
	// only advances the cursor, so the batch size may vary without skipping
	// or repeating pages; an empty batch while pages are still missing means
	// the upstream list disagrees with total and cannot be trusted.
	for p := 1; received < total; p++ {
		u, _ := url.Parse(galleryURL)
		u.RawQuery = fmt.Sprintf("p=%d", p)
		pageDoc, err := httpGetDoc(walkCtx, client, u.String())
		if err != nil {
			return err
		}
		batchURLs, batchThumbs := extractGalleryPages(pageDoc)
		if len(batchURLs) == 0 {
			return fmt.Errorf("incomplete page list: thumbnail page %d is empty, got %d of %d pages", p, received, total)
		}
		if err := emit(total, batchURLs, batchThumbs); err != nil {
			return err
		}
		received += len(batchURLs)
	}

	if received != total {
		return fmt.Errorf("incomplete page list: got %d pages, want %d", received, total)
	}
	return nil
}

// ScrapeGalleryPages fetches the full list of gallery pages. It reports an
// error instead of returning a partial list, so callers only ever cache a
// complete result.
func ScrapeGalleryPages(ctx context.Context, client *http.Client, galleryURL string) ([]string, []model.GalleryPageThumb, error) {
	var pageURLs []string
	var thumbnails []model.GalleryPageThumb
	if err := StreamGalleryPages(ctx, client, galleryURL, func(_ int, urls []string, thumbs []model.GalleryPageThumb) error {
		pageURLs = append(pageURLs, urls...)
		thumbnails = append(thumbnails, thumbs...)
		return nil
	}); err != nil {
		return nil, nil, err
	}
	return pageURLs, thumbnails, nil
}

// ScrapeGalleryTotal fetches the gallery document and returns the ".gpc" image
// count without walking the thumbnail pages. It is a lightweight freshness probe
// used to detect a page-count change against the cache.
func ScrapeGalleryTotal(ctx context.Context, client *http.Client, galleryURL string) (int, error) {
	doc, err := httpGetDoc(ctx, client, galleryURL)
	if err != nil {
		return 0, err
	}
	total := galleryTotalImages(doc)
	if total <= 0 {
		return 0, fmt.Errorf("cannot determine gallery total: %q counter missing or invalid", ".gpc")
	}
	return total, nil
}

// PageURLGalleryID extracts the gallery id from an ExHentai page URL of the form
// https://exhentai.org/s/<hash>/<gallery-id>-<page>. It maps a failed cached page
// URL back to its gallery so the cache can be refreshed.
func PageURLGalleryID(rawURL string) (int64, bool) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return 0, false
	}
	seg := u.Path
	if i := strings.LastIndex(seg, "/"); i >= 0 {
		seg = seg[i+1:]
	}
	dash := strings.LastIndex(seg, "-")
	if dash <= 0 {
		return 0, false
	}
	id, err := strconv.ParseInt(seg[:dash], 10, 64)
	if err != nil || id <= 0 {
		return 0, false
	}
	return id, true
}

// ScrapeGalleryPageThumb returns the sprite thumbnail geometry for a single
// gallery page index. Unlike ScrapeGalleryPages it fetches only the thumbnail
// page that contains the requested index instead of walking the whole gallery.
func ScrapeGalleryPageThumb(ctx context.Context, client *http.Client, galleryURL string, index int) (model.GalleryPageThumb, bool, error) {
	if index < 0 {
		return model.GalleryPageThumb{}, false, nil
	}

	doc, err := httpGetDoc(ctx, client, galleryURL)
	if err != nil {
		return model.GalleryPageThumb{}, false, err
	}

	if total := galleryTotalImages(doc); total > 0 && index >= total {
		return model.GalleryPageThumb{}, false, nil
	}

	firstURLs, firstThumbs := extractGalleryPages(doc)
	perPage := len(firstURLs)
	if perPage == 0 {
		return model.GalleryPageThumb{}, false, nil
	}
	if index < perPage {
		if index < len(firstThumbs) && firstThumbs[index].SpriteURL != "" {
			return firstThumbs[index], true, nil
		}
		return model.GalleryPageThumb{}, false, nil
	}

	u, err := url.Parse(galleryURL)
	if err != nil {
		return model.GalleryPageThumb{}, false, err
	}
	u.RawQuery = fmt.Sprintf("p=%d", index/perPage)
	pageDoc, err := httpGetDoc(ctx, client, u.String())
	if err != nil {
		return model.GalleryPageThumb{}, false, err
	}
	_, batchThumbs := extractGalleryPages(pageDoc)
	if idx := index % perPage; idx < len(batchThumbs) && batchThumbs[idx].SpriteURL != "" {
		return batchThumbs[idx], true, nil
	}
	return model.GalleryPageThumb{}, false, nil
}
