package exhentai

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"manga-reader/internal/model"
)

const (
	ExhentaiURL = "https://exhentai.org"
	EhentaiURL  = "https://e-hentai.org"

	// upstreamDocTimeout bounds each HTML document fetch so a hung
	// upstream connection cannot stall a request indefinitely.
	upstreamDocTimeout = 15 * time.Second

	// maxImageBytes limits the size of a single image response to prevent OOM.
	maxImageBytes = 30 << 20 // 30 MiB
)

var foundReg = regexp.MustCompile(`Found(?: about)? ([\d,]+)\+? results?`)
var foundThousandsReg = regexp.MustCompile(`Found thousands of results`)
var starsReg = regexp.MustCompile(`background-position:(-?\d+)px (-\d+)px`)
var coverUrlReg = regexp.MustCompile(`url\(([^)]+)\)`)
var numReg = regexp.MustCompile(`Showing 1 - (\d+) of ([\d,]+) images?`)
var nlReg = regexp.MustCompile(`nl\('(.+?)'\)`)

func httpGet(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", ExhentaiURL+"/")
	return client.Do(req)
}

func httpGetDoc(ctx context.Context, client *http.Client, url string) (*goquery.Document, error) {
	ctx, cancel := context.WithTimeout(ctx, upstreamDocTimeout)
	defer cancel()

	resp, err := httpGet(ctx, client, url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	if sadPandaCheck(doc) {
		return nil, ErrSadPanda
	}
	if ipBannedCheck(doc) {
		return nil, ErrIPBanned
	}
	return doc, nil
}

func sadPandaCheck(doc *goquery.Document) bool {
	head := doc.Find("head")
	body := doc.Find("body")
	return head.Text() == "" && body.Text() == ""
}

func ipBannedCheck(doc *goquery.Document) bool {
	return strings.Contains(doc.Find("body").Text(), "This IP address has been temporarily banned")
}

func ParseStars(stars string) float64 {
	matches := starsReg.FindStringSubmatch(stars)
	if len(matches) == 0 {
		return 0
	}
	x, _ := strconv.Atoi(matches[1])
	y, _ := strconv.Atoi(matches[2])

	rating := 5.0 - float64(-x/16)
	switch y {
	case -1:
	case -21:
		rating -= 0.5
	default:
		rating -= float64(y+1) / 20.0
	}
	return rating
}

type SearchOptions struct {
	MinPages  *int
	MaxPages  *int
	MinRating *int

	HasTorrent      bool
	IncludeExpunged bool

	SearchName        bool
	SearchTags        bool
	SearchDescription bool

	IncludeLowPowerTags  bool
	IncludeDownvotedTags bool

	DisableLanguageFilter bool
	DisableUploaderFilter bool
	DisableTagFilter      bool
}

func BuildSearchQuery(keyword string, categories []string, opts *SearchOptions) url.Values {
	q := url.Values{}

	if len(categories) > 0 {
		catVal := BuildCategoryFilter(categories)
		if catVal != "" {
			q.Set("f_cats", catVal)
		}
	}
	if keyword != "" {
		q.Set("f_search", keyword)
	}

	if opts == nil {
		return q
	}

	advanced := false

	if opts.MinPages != nil {
		q.Set("f_spf", strconv.Itoa(*opts.MinPages))
		advanced = true
	}
	if opts.MaxPages != nil {
		q.Set("f_spt", strconv.Itoa(*opts.MaxPages))
		advanced = true
	}
	if opts.MinRating != nil {
		q.Set("f_sr", "on")
		q.Set("f_srdd", strconv.Itoa(*opts.MinRating))
		advanced = true
	}
	if opts.HasTorrent {
		q.Set("f_sto", "on")
		advanced = true
	}
	if opts.IncludeExpunged {
		q.Set("f_sh", "on")
		advanced = true
	}
	if opts.SearchName {
		q.Set("f_sname", "on")
		advanced = true
	}
	if opts.SearchTags {
		q.Set("f_stags", "on")
		advanced = true
	}
	if opts.SearchDescription {
		q.Set("f_sdesc", "on")
		advanced = true
	}
	if opts.IncludeLowPowerTags {
		q.Set("f_sdt1", "on")
		advanced = true
	}
	if opts.IncludeDownvotedTags {
		q.Set("f_sdt2", "on")
		advanced = true
	}
	if opts.DisableLanguageFilter {
		q.Set("f_sfl", "on")
		advanced = true
	}
	if opts.DisableUploaderFilter {
		q.Set("f_sfu", "on")
		advanced = true
	}
	if opts.DisableTagFilter {
		q.Set("f_sft", "on")
		advanced = true
	}

	if advanced {
		q.Set("advsearch", "1")
	}

	return q
}

func ScrapeSearch(ctx context.Context, client *http.Client, siteURL, keyword string, categories []string, page int, opts *SearchOptions) (total int, results []model.SearchResult, err error) {
	u, err := url.Parse(siteURL)
	if err != nil {
		return 0, nil, err
	}
	u.RawQuery = BuildSearchQuery(keyword, categories, opts).Encode()
	firstURL := u.String()

	doc, err := fetchListingDoc(ctx, client, firstURL, page)
	if err != nil {
		if errors.Is(err, errNoNextPage) {
			return 0, nil, fmt.Errorf("no results")
		}
		return 0, nil, err
	}

	noHits := doc.Find("body > div.ido > div:nth-child(2) > p").Text()
	if noHits != "" {
		return 0, nil, fmt.Errorf("no hits found: %s", noHits)
	}

	total, ok := parseSearchTotal(doc)
	if !ok && page > 0 {
		// The result count banner is expected on every page, but fall back to
		// page 0 (a single extra request) if it is missing so callers still get
		// a correct total_pages for pagination.
		if firstDoc, ferr := httpGetDoc(ctx, client, firstURL); ferr == nil {
			total, ok = parseSearchTotal(firstDoc)
		}
	}
	if !ok {
		return 0, nil, fmt.Errorf("could not parse result count")
	}
	if total == 0 {
		return 0, nil, fmt.Errorf("no results")
	}

	results, err = parseSearchResults(doc)
	return
}

// parseSearchTotal extracts the total result count from a search results page.
func parseSearchTotal(doc *goquery.Document) (int, bool) {
	foundResults := doc.Find("body > div.ido > div:nth-child(2) > div.searchtext > p").Text()
	matches := foundReg.FindStringSubmatch(foundResults)
	if len(matches) == 0 {
		if foundThousandsReg.MatchString(foundResults) {
			return 9999, true
		}
		return 0, false
	}
	totalStr := strings.ReplaceAll(matches[1], ",", "")
	total, err := strconv.Atoi(totalStr)
	if err != nil {
		return 0, false
	}
	return total, true
}

func extractNextURL(doc *goquery.Document) string {
	// Prefer the stable ids/rel the site exposes; fall back to the label text.
	for _, selector := range []string{"a#unext", "a#dnext", `a[rel="next"]`} {
		if href, ok := doc.Find(selector).First().Attr("href"); ok && href != "" {
			return href
		}
	}
	var nextURL string
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if nextURL == "" && s.Text() == "Next >" {
			nextURL, _ = s.Attr("href")
		}
	})
	return nextURL
}

// errNoNextPage reports that a cursor-paginated listing has no further pages.
var errNoNextPage = errors.New("no next page")

// ExHentai paginates listings (front page, watched, popular, search) with a
// next=<gallery-id> cursor rather than ?page=N, so pages cannot be addressed
// directly. We remember each page's URL the first time we walk to it, which
// makes sequential access (the infinite-scroll case) cost one upstream request
// per page instead of re-walking from page 0 every time.
const listingCursorTTL = 10 * time.Minute

type listingCursor struct {
	url     string
	expires time.Time
}

var listingCursors = struct {
	mu sync.Mutex
	m  map[string]listingCursor
}{m: make(map[string]listingCursor)}

func listingCursorKey(firstURL string, page int) string {
	return firstURL + "\x00" + strconv.Itoa(page)
}

func getListingCursor(firstURL string, page int) (string, bool) {
	key := listingCursorKey(firstURL, page)
	listingCursors.mu.Lock()
	defer listingCursors.mu.Unlock()
	entry, ok := listingCursors.m[key]
	if !ok {
		return "", false
	}
	if time.Now().After(entry.expires) {
		delete(listingCursors.m, key)
		return "", false
	}
	return entry.url, true
}

func setListingCursor(firstURL string, page int, u string) {
	if u == "" {
		return
	}
	listingCursors.mu.Lock()
	defer listingCursors.mu.Unlock()
	if len(listingCursors.m) > 2000 {
		listingCursors.m = make(map[string]listingCursor)
	}
	listingCursors.m[listingCursorKey(firstURL, page)] = listingCursor{
		url:     u,
		expires: time.Now().Add(listingCursorTTL),
	}
}

// fetchListingDoc returns the parsed document for the given 0-indexed page of a
// cursor-paginated listing. Cursors discovered while walking are cached so a
// later request for the next page can jump straight to it.
func fetchListingDoc(ctx context.Context, client *http.Client, firstURL string, page int) (*goquery.Document, error) {
	if page <= 0 {
		doc, err := httpGetDoc(ctx, client, firstURL)
		if err != nil {
			return nil, err
		}
		setListingCursor(firstURL, 1, extractNextURL(doc))
		return doc, nil
	}

	if next, ok := getListingCursor(firstURL, page); ok {
		doc, err := httpGetDoc(ctx, client, next)
		if err != nil {
			return nil, err
		}
		setListingCursor(firstURL, page+1, extractNextURL(doc))
		return doc, nil
	}

	// No cached cursor for this page: walk from the first page, caching as we go.
	doc, err := httpGetDoc(ctx, client, firstURL)
	if err != nil {
		return nil, err
	}
	setListingCursor(firstURL, 1, extractNextURL(doc))
	for i := 1; i <= page; i++ {
		next, ok := getListingCursor(firstURL, i)
		if !ok {
			return nil, errNoNextPage
		}
		doc, err = httpGetDoc(ctx, client, next)
		if err != nil {
			return nil, err
		}
		setListingCursor(firstURL, i+1, extractNextURL(doc))
	}
	return doc, nil
}

func parseSearchResults(doc *goquery.Document) ([]model.SearchResult, error) {
	table := doc.Find("table.itg.gltc > tbody > tr")
	isThumbnail := false
	isExtended := false
	if table.Length() == 0 {
		table = doc.Find("table.itg.gltm > tbody > tr")
		isThumbnail = true
	}
	if table.Length() == 0 {
		table = doc.Find("table.itg.glte > tbody > tr")
		isExtended = true
	}
	if table.Length() == 0 {
		table = doc.Find("body > div.ido > div:nth-child(2) > table > tbody > tr")
	}
	if table.Length() == 0 {
		return nil, fmt.Errorf("empty results table")
	}

	results := make([]model.SearchResult, 0, table.Length())

	table.Each(func(i int, s *goquery.Selection) {
		var gURL, cat, title, cover, upTime, uploader, pagesStr string
		var tags []string
		var stars string

		if isExtended {
			gl1e := s.Find("td.gl1e")
			if gl1e.Length() == 0 {
				return
			}
			gl2e := s.Find("td.gl2e")
			if gl2e.Length() == 0 {
				return
			}

			gl3e := gl2e.Find("div.gl3e")
			cat = gl3e.Find("div.cn").Text()

			gl3e.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) >= 10 && text[4] == '-' && text[7] == '-' {
					if upTime == "" {
						upTime = text
					}
				}
			})
			stars, _ = gl3e.Find("div.ir").Attr("style")

			gl3e.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) > 6 && text[len(text)-6:] == " pages" {
					pagesStr = text
				}
			})

			gl3e.Find("div > a").Each(func(i int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				if strings.Contains(href, "/uploader/") && uploader == "" {
					uploader = a.Text()
				}
			})

			coverImg := gl1e.Find("img")
			cover = coverImg.AttrOr("src", "")

			gl2e.Find("a").Each(func(i int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				if strings.Contains(href, "/g/") && gURL == "" {
					gURL = href
				}
			})

			gl4e := gl2e.Find("div.gl4e.glname")
			title = gl4e.Find("div.glink").Text()
			gl4e.Find("table tr").Each(func(i int, s *goquery.Selection) {
				s.Find("div").Each(func(i int, s *goquery.Selection) {
					tag := s.AttrOr("title", "")
					if tag == "" {
						tag = s.Text()
					}
					if tag != "" {
						tags = append(tags, tag)
					}
				})
			})
		} else if isThumbnail {
			gl3m := s.Find("td.gl3m.glname")
			if gl3m.Length() == 0 {
				return
			}
			a := gl3m.Find("a")
			gURL, _ = a.Attr("href")
			title = a.Find("div.glink").Text()

			gl1m := s.Find("td.gl1m")
			cat = gl1m.Find("div.cs").Text()

			gl2m := s.Find("td.gl2m")
			stars, _ = gl2m.Find("div.ir").Attr("style")

			gl2m.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) >= 10 && text[4] == '-' && text[7] == '-' {
					if upTime == "" {
						upTime = text
					}
				}
			})
			gl2m.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) > 6 && text[len(text)-6:] == " pages" {
					pagesStr = text
				}
			})

			gl5m := s.Find("td.gl5m")
			uploader = gl5m.Find("div:first-child > a").Text()

			coverImg := gl2m.Find("div.glthumb img")
			cover = coverImg.AttrOr("data-src", "")
			if cover == "" {
				cover = coverImg.AttrOr("src", "")
			}
		} else {
			gl3c := s.Find("td.gl3c.glname")
			if gl3c.Length() == 0 {
				return
			}
			a := gl3c.Find("a")
			gURL, _ = a.Attr("href")
			title = a.Find("div.glink").Text()
			a.Find("div.gt").Each(func(i int, s *goquery.Selection) {
				tags = append(tags, s.AttrOr("title", s.Text()))
			})

			gl1c := s.Find("td.gl1c")
			cat = gl1c.Find("div.cn").Text()

			gl2c := s.Find("td.gl2c")
			stars, _ = gl2c.Find("div.ir").Attr("style")

			gl2c.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) >= 10 && text[4] == '-' && text[7] == '-' {
					if upTime == "" {
						upTime = text
					}
				}
			})
			gl2c.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) > 6 && text[len(text)-6:] == " pages" {
					pagesStr = text
				}
			})

			gl4c := s.Find("td.gl4c")
			uploader = gl4c.Find("div:first-child > a").Text()

			coverImg := gl2c.Find("div.glthumb img")
			cover = coverImg.AttrOr("data-src", "")
			if cover == "" {
				cover = coverImg.AttrOr("src", "")
			}
		}

		pagesStr = strings.TrimSuffix(pagesStr, " pages")
		pagesNum, _ := strconv.Atoi(pagesStr)

		domain, gId, gToken := ParseGalleryURL(gURL)
		gIdNum, _ := strconv.Atoi(gId)

		results = append(results, model.SearchResult{
			Domain:    domain,
			GalleryID: gIdNum,
			Token:     gToken,
			Cat:       cat,
			Cover:     cover,
			Posted:    upTime,
			Rating:    ParseStars(stars),
			URL:       gURL,
			Title:     title,
			Tags:      tags,
			Uploader:  uploader,
			Pages:     pagesNum,
		})
	})
	return results, nil
}

func ParseGalleryURL(u string) (domain, gId, gToken string) {
	u = strings.TrimSuffix(u, "/")
	splits := strings.Split(u, "/")
	for i, s := range splits {
		if s == "g" && i+2 < len(splits) {
			return splits[i-1], splits[i+1], splits[i+2]
		}
	}
	return "", "", ""
}

func BuildCategoryFilter(categories []string) string {
	var cat uint
	for _, c := range categories {
		switch strings.ToUpper(strings.ReplaceAll(strings.ReplaceAll(strings.TrimSpace(c), "-", ""), "_", "")) {
		case "MISC":
			cat |= 1 << 0
		case "DOUJINSHI":
			cat |= 1 << 1
		case "MANGA":
			cat |= 1 << 2
		case "ARTISTCG":
			cat |= 1 << 3
		case "GAMECG":
			cat |= 1 << 4
		case "IMAGESET":
			cat |= 1 << 5
		case "COSPLAY":
			cat |= 1 << 6
		case "ASIANPORN":
			cat |= 1 << 7
		case "NONH":
			cat |= 1 << 8
		case "WESTERN":
			cat |= 1 << 9
		}
	}
	if cat == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(1023^cat), 10)
}

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
		Domain:      domain,
		GalleryID:   gIdNum,
		Token:       gTokenStr,
		Cover:       cover,
		Title:       title,
		TitleJpn:    titleJpn,
		Cat:         cat,
		Uploader:    uploader,
		Posted:      posted,
		Parent:      parentId,
		Visible:     visible,
		Language:    language,
		Translated:  translated,
		FileSize:    fileSize,
		Length:      lengthNum,
		Favorited:   favoritedNum,
		RatingCount: ratingCount,
		Rating:      rating,
		Tags:        tags,
	}, nil
}

// extractPageURLs collects gallery page links from a gallery document.
func extractPageURLs(doc *goquery.Document) []string {
	var urls []string
	doc.Find("#gdt > a").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if href != "" {
			urls = append(urls, href)
		}
	})
	return urls
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

// ScrapeGalleryPageURLs fetches the full list of gallery page URLs,
// walking the paginated thumbnail list when the gallery has more pages
// than fit on the first thumbnail page.
func ScrapeGalleryPageURLs(ctx context.Context, client *http.Client, galleryURL string) ([]string, error) {
	doc, err := httpGetDoc(ctx, client, galleryURL)
	if err != nil {
		return nil, err
	}

	pageUrls := extractPageURLs(doc)
	total := galleryTotalImages(doc)

	if total > len(pageUrls) && len(pageUrls) > 0 {
		end := len(pageUrls)
		pages := total / end
		if total%end != 0 {
			pages++
		}
		for p := 1; p < pages; p++ {
			u, _ := url.Parse(galleryURL)
			u.RawQuery = fmt.Sprintf("p=%d", p)
			pageDoc, err := httpGetDoc(ctx, client, u.String())
			if err != nil {
				break
			}
			pageUrls = append(pageUrls, extractPageURLs(pageDoc)...)
		}
	}

	return pageUrls, nil
}

func ScrapePageImageURL(ctx context.Context, client *http.Client, pageURL string) (imgURL string, fallbackURL string, err error) {
	resp, err := httpGet(ctx, client, pageURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return "", "", err
	}

	doc, err := goquery.NewDocumentFromReader(bytes.NewReader(body))
	if err != nil {
		return "", "", err
	}

	img, ok := doc.Find("#img").Attr("src")
	if !ok || img == "" {
		return "", "", fmt.Errorf("could not find image src")
	}

	onclick, _ := doc.Find("#loadfail").Attr("onclick")
	fallbackURL = BuildNlFallbackURL(pageURL, onclick)

	return img, fallbackURL, nil
}

func BuildNlFallbackURL(pageURL, onclick string) string {
	if onclick == "" {
		return ""
	}
	matches := nlReg.FindStringSubmatch(onclick)
	if len(matches) == 0 {
		return ""
	}
	u, err := url.Parse(pageURL)
	if err != nil {
		return ""
	}
	nl := matches[1]
	if u.RawQuery != "" {
		u.RawQuery += "&nl=" + nl
	} else {
		u.RawQuery = "nl=" + nl
	}
	return u.String()
}

const MaxNlRetries = 2

func FetchPageImage(ctx context.Context, client *http.Client, pageURL string) (data []byte, contentType string, err error) {
	imgURL, fallbackURL, err := ScrapePageImageURL(ctx, client, pageURL)
	if err != nil {
		return nil, "", err
	}

	data, contentType, err = ProxyImage(ctx, client, imgURL)
	if err != nil && fallbackURL != "" {
		for range MaxNlRetries {
			imgURL, fallbackURL, err = ScrapePageImageURL(ctx, client, fallbackURL)
			if err != nil {
				break
			}
			data, contentType, err = ProxyImage(ctx, client, imgURL)
			if err == nil {
				break
			}
		}
	}
	return data, contentType, err
}

func ProxyImage(ctx context.Context, client *http.Client, imgURL string) (data []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imgURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", ExhentaiURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d: %w", resp.StatusCode, ErrNonOKStatus)
	}

	contentType = resp.Header.Get("Content-Type")
	data, err = io.ReadAll(io.LimitReader(resp.Body, maxImageBytes))
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

func ScrapeGalleryList(ctx context.Context, client *http.Client, listURL string, page int, opts *SearchOptions) (results []model.SearchResult, err error) {
	u, err := url.Parse(listURL)
	if err != nil {
		return nil, err
	}

	if opts != nil {
		existing := u.Query()
		newParams := BuildSearchQuery("", nil, opts)
		for k, vs := range newParams {
			for _, v := range vs {
				existing.Set(k, v)
			}
		}
		u.RawQuery = existing.Encode()
	}

	doc, err := fetchListingDoc(ctx, client, u.String(), page)
	if err != nil {
		if errors.Is(err, errNoNextPage) {
			return []model.SearchResult{}, nil
		}
		return nil, err
	}

	results, err = parseGalleryListResults(doc)
	if err != nil && err.Error() == "empty gallery list" {
		return []model.SearchResult{}, nil
	}
	return
}

func parseGalleryListResults(doc *goquery.Document) ([]model.SearchResult, error) {
	table := doc.Find("table.itg.gltc > tbody > tr")
	isThumbnail := false
	isExtended := false
	if table.Length() == 0 {
		table = doc.Find("table.itg.gltm > tbody > tr")
		isThumbnail = true
	}
	if table.Length() == 0 {
		table = doc.Find("table.itg.glte > tbody > tr")
		isExtended = true
	}
	if table.Length() == 0 {
		return nil, fmt.Errorf("empty gallery list")
	}

	results := make([]model.SearchResult, 0, table.Length())

	table.Each(func(i int, s *goquery.Selection) {
		var gURL, cat, title, cover, upTime, uploader, pagesStr string
		var tags []string
		var stars string

		if isExtended {
			gl1e := s.Find("td.gl1e")
			if gl1e.Length() == 0 {
				return
			}
			gl2e := s.Find("td.gl2e")
			if gl2e.Length() == 0 {
				return
			}

			gl3e := gl2e.Find("div.gl3e")
			cat = gl3e.Find("div.cn").Text()

			gl3e.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) >= 10 && text[4] == '-' && text[7] == '-' {
					if upTime == "" {
						upTime = text
					}
				}
			})
			stars, _ = gl3e.Find("div.ir").Attr("style")

			gl3e.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) > 6 && text[len(text)-6:] == " pages" {
					pagesStr = text
				}
			})

			gl3e.Find("div > a").Each(func(i int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				if strings.Contains(href, "/uploader/") && uploader == "" {
					uploader = a.Text()
				}
			})

			coverImg := gl1e.Find("img")
			cover = coverImg.AttrOr("src", "")

			gl2e.Find("a").Each(func(i int, a *goquery.Selection) {
				href, _ := a.Attr("href")
				if strings.Contains(href, "/g/") && gURL == "" {
					gURL = href
				}
			})

			gl4e := gl2e.Find("div.gl4e.glname")
			title = gl4e.Find("div.glink").Text()
			gl4e.Find("table tr").Each(func(i int, s *goquery.Selection) {
				s.Find("div").Each(func(i int, s *goquery.Selection) {
					tag := s.AttrOr("title", "")
					if tag == "" {
						tag = s.Text()
					}
					if tag != "" {
						tags = append(tags, tag)
					}
				})
			})
		} else if isThumbnail {
			gl3m := s.Find("td.gl3m.glname")
			if gl3m.Length() == 0 {
				return
			}
			a := gl3m.Find("a")
			gURL, _ = a.Attr("href")
			title = a.Find("div.glink").Text()

			gl1m := s.Find("td.gl1m")
			cat = gl1m.Find("div.cs").Text()

			gl2m := s.Find("td.gl2m")
			stars, _ = gl2m.Find("div.ir").Attr("style")

			gl2m.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) >= 10 && text[4] == '-' && text[7] == '-' {
					if upTime == "" {
						upTime = text
					}
				}
			})
			gl2m.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) > 6 && text[len(text)-6:] == " pages" {
					pagesStr = text
				}
			})

			gl5m := s.Find("td.gl5m")
			uploader = gl5m.Find("div:first-child > a").Text()

			coverImg := gl2m.Find("div.glthumb img")
			cover = coverImg.AttrOr("data-src", "")
			if cover == "" {
				cover = coverImg.AttrOr("src", "")
			}
		} else {
			gl3c := s.Find("td.gl3c.glname")
			if gl3c.Length() == 0 {
				return
			}
			a := gl3c.Find("a")
			gURL, _ = a.Attr("href")
			title = a.Find("div.glink").Text()
			a.Find("div.gt").Each(func(i int, s *goquery.Selection) {
				tags = append(tags, s.AttrOr("title", s.Text()))
			})

			gl1c := s.Find("td.gl1c")
			cat = gl1c.Find("div.cn").Text()

			gl2c := s.Find("td.gl2c")
			stars, _ = gl2c.Find("div.ir").Attr("style")

			gl2c.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) >= 10 && text[4] == '-' && text[7] == '-' {
					if upTime == "" {
						upTime = text
					}
				}
			})
			gl2c.Find("div").Each(func(i int, s *goquery.Selection) {
				text := s.Text()
				if len(text) > 6 && text[len(text)-6:] == " pages" {
					pagesStr = text
				}
			})

			gl4c := s.Find("td.gl4c")
			uploader = gl4c.Find("div:first-child > a").Text()

			coverImg := gl2c.Find("div.glthumb img")
			cover = coverImg.AttrOr("data-src", "")
			if cover == "" {
				cover = coverImg.AttrOr("src", "")
			}
		}

		pagesStr = strings.TrimSuffix(pagesStr, " pages")
		pagesNum, _ := strconv.Atoi(pagesStr)

		domain, gId, gToken := ParseGalleryURL(gURL)
		gIdNum, _ := strconv.Atoi(gId)

		results = append(results, model.SearchResult{
			Domain:    domain,
			GalleryID: gIdNum,
			Token:     gToken,
			Cat:       cat,
			Cover:     cover,
			Posted:    upTime,
			Rating:    ParseStars(stars),
			URL:       gURL,
			Title:     title,
			Tags:      tags,
			Uploader:  uploader,
			Pages:     pagesNum,
		})
	})
	return results, nil
}

func GalleryURL(id, token string) string {
	return ExhentaiURL + "/g/" + id + "/" + token + "/"
}

func ValidateThumbnailURL(rawURL string) error {
	return validateThumbnailURL(rawURL)
}

func ValidatePageURL(rawURL string) error {
	return validatePageURL(rawURL)
}

func IsBlockedInternalHost(host string) bool {
	return isBlockedInternalHost(host)
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

func validatePageURL(rawURL string) error {
	if rawURL == "" {
		return fmt.Errorf("missing url parameter")
	}

	u, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("invalid url")
	}

	if u.Scheme != "https" && u.Scheme != "http" {
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
