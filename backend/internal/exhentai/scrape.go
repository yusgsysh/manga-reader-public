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

// Listing navigation ("Jump/Seek") state is emitted by the site as plain JS
// variables. The cursor ids are embedded in the prevurl/nexturl values.
var (
	prevURLReg   = regexp.MustCompile(`var prevurl="([^"]*)"`)
	nextURLReg   = regexp.MustCompile(`var nexturl="([^"]*)"`)
	minDateReg   = regexp.MustCompile(`var mindate="([^"]*)"`)
	maxDateReg   = regexp.MustCompile(`var maxdate="([^"]*)"`)
	rangeMinReg  = regexp.MustCompile(`var rangemin=(\d+)`)
	rangeMaxReg  = regexp.MustCompile(`var rangemax=(\d+)`)
	rangeSpanReg = regexp.MustCompile(`var rangespan=(\d+)`)
)

// pageThumbStyleReg parses the inline style ExHentai uses to place a page
// thumbnail inside its sprite, e.g.
// "width:200px;height:282px;background:transparent url(https://...webp) -0px 0 no-repeat".
var pageThumbStyleReg = regexp.MustCompile(`width:(\d+)px;height:(\d+)px;background:[^;]*url\(([^)]+)\)\s*(-?\d+)px\s*(-?\d+)`)

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

// ListingNavOptions carries the upstream "Jump/Seek" query parameters. Seek is a
// date expression (YYYY, YY-MM or YYYY-MM-DD); Jump is a relative offset
// (e.g. 3d, 1w, 6m, 1y).
type ListingNavOptions struct {
	Seek string
	Jump string
}

// Active reports whether any navigation parameter is set. A nil receiver is not
// active, so callers can pass nil for a plain paginated request.
func (n *ListingNavOptions) Active() bool {
	return n != nil && (n.Seek != "" || n.Jump != "")
}

// Apply merges the navigation parameters into an existing query.
func (n *ListingNavOptions) Apply(q url.Values) {
	if n == nil {
		return
	}
	if n.Seek != "" {
		q.Set("seek", n.Seek)
	}
	if n.Jump != "" {
		q.Set("jump", n.Jump)
	}
}

// ListingNav is the navigation metadata parsed from a listing page. It lets
// clients build a Jump/Seek UI: Prev/Next are the neighbouring cursor ids and
// the date/range fields describe the seekable span.
type ListingNav struct {
	Prev      string `json:"prev"`
	Next      string `json:"next"`
	MinDate   string `json:"min_date"`
	MaxDate   string `json:"max_date"`
	RangeMin  int    `json:"range_min"`
	RangeMax  int    `json:"range_max"`
	RangeSpan int    `json:"range_span"`
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

func ScrapeSearch(ctx context.Context, client *http.Client, siteURL, keyword string, categories []string, page int, opts *SearchOptions, navOpts *ListingNavOptions) (total int, results []model.SearchResult, nav ListingNav, err error) {
	u, err := url.Parse(siteURL)
	if err != nil {
		return 0, nil, ListingNav{}, err
	}
	q := BuildSearchQuery(keyword, categories, opts)
	navOpts.Apply(q)
	u.RawQuery = q.Encode()
	firstURL := u.String()

	doc, err := fetchListingDoc(ctx, client, firstURL, page)
	if err != nil {
		if errors.Is(err, errNoNextPage) {
			return 0, nil, ListingNav{}, fmt.Errorf("no results")
		}
		return 0, nil, ListingNav{}, err
	}

	noHits := doc.Find("body > div.ido > div:nth-child(2) > p").Text()
	if noHits != "" {
		return 0, nil, ListingNav{}, fmt.Errorf("no hits found: %s", noHits)
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
		return 0, nil, ListingNav{}, fmt.Errorf("could not parse result count")
	}
	if total == 0 {
		return 0, nil, ListingNav{}, fmt.Errorf("no results")
	}

	results, err = parseSearchResults(doc)
	if err != nil {
		return 0, nil, ListingNav{}, err
	}
	return total, results, parseListingNav(doc), nil
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

// parseListingNav extracts the Jump/Seek navigation metadata from a listing
// page's inline script. Listings without a navigation bar (e.g. an empty
// watched feed, the popular page) yield the zero value.
func parseListingNav(doc *goquery.Document) ListingNav {
	script := doc.Find("script").Text()

	nav := ListingNav{
		Prev:    cursorFromURL(matchGroup(prevURLReg, script)),
		Next:    cursorFromURL(matchGroup(nextURLReg, script)),
		MinDate: matchGroup(minDateReg, script),
		MaxDate: matchGroup(maxDateReg, script),
	}
	nav.RangeMin = atoiOrZero(matchGroup(rangeMinReg, script))
	nav.RangeMax = atoiOrZero(matchGroup(rangeMaxReg, script))
	nav.RangeSpan = atoiOrZero(matchGroup(rangeSpanReg, script))
	return nav
}

func matchGroup(re *regexp.Regexp, s string) string {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return ""
	}
	return m[1]
}

func atoiOrZero(s string) int {
	n, _ := strconv.Atoi(s)
	return n
}

// cursorFromURL extracts the gid cursor from a prevurl/nexturl value.
func cursorFromURL(raw string) string {
	if raw == "" {
		return ""
	}
	u, err := url.Parse(raw)
	if err != nil {
		return ""
	}
	if v := u.Query().Get("next"); v != "" {
		return v
	}
	return u.Query().Get("prev")
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

// extractGalleryPages collects gallery page links and their sprite thumbnail
// geometry from a gallery document.
func extractGalleryPages(doc *goquery.Document) []model.CachedPage {
	var pages []model.CachedPage
	doc.Find("#gdt > a").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if href == "" {
			return
		}
		page := model.CachedPage{PageURL: href}
		if style, ok := s.Find("div[style]").First().Attr("style"); ok {
			if m := pageThumbStyleReg.FindStringSubmatch(style); len(m) == 6 {
				w, _ := strconv.Atoi(m[1])
				h, _ := strconv.Atoi(m[2])
				posX, _ := strconv.Atoi(m[4])
				posY, _ := strconv.Atoi(m[5])
				page.Thumbnail = &model.GalleryPageThumb{
					SpriteURL: m[3],
					X:         -posX,
					Y:         -posY,
					Width:     w,
					Height:    h,
				}
			}
		}
		pages = append(pages, page)
	})
	return pages
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
func StreamGalleryPages(ctx context.Context, client *http.Client, galleryURL string, emit func(total int, batch []model.CachedPage) error) error {
	doc, err := httpGetDoc(ctx, client, galleryURL)
	if err != nil {
		return err
	}

	total := galleryTotalImages(doc)
	if total <= 0 {
		return fmt.Errorf("cannot determine gallery total: %q counter missing or invalid", ".gpc")
	}
	first := extractGalleryPages(doc)
	if len(first) == 0 {
		return fmt.Errorf("cannot determine gallery total: no page links in the gallery document")
	}
	for i := range first {
		first[i].Index = i
	}
	if err := emit(total, first); err != nil {
		return err
	}
	received := len(first)

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
		batch := extractGalleryPages(pageDoc)
		if len(batch) == 0 {
			return fmt.Errorf("incomplete page list: thumbnail page %d is empty, got %d of %d pages", p, received, total)
		}
		for i := range batch {
			batch[i].Index = received + i
		}
		if err := emit(total, batch); err != nil {
			return err
		}
		received += len(batch)
	}

	if received != total {
		return fmt.Errorf("incomplete page list: got %d pages, want %d", received, total)
	}
	return nil
}

// ScrapeGalleryPages fetches the full list of gallery pages. It reports an
// error instead of returning a partial list, so callers only ever cache a
// complete result.
func ScrapeGalleryPages(ctx context.Context, client *http.Client, galleryURL string) ([]model.CachedPage, error) {
	var pages []model.CachedPage
	if err := StreamGalleryPages(ctx, client, galleryURL, func(_ int, batch []model.CachedPage) error {
		pages = append(pages, batch...)
		return nil
	}); err != nil {
		return nil, err
	}
	return pages, nil
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

	first := extractGalleryPages(doc)
	perPage := len(first)
	if perPage == 0 {
		return model.GalleryPageThumb{}, false, nil
	}
	if index < perPage {
		return pageThumbAt(first, index)
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
	return pageThumbAt(extractGalleryPages(pageDoc), index%perPage)
}

func pageThumbAt(pages []model.CachedPage, index int) (model.GalleryPageThumb, bool, error) {
	if index < 0 || index >= len(pages) || pages[index].Thumbnail == nil {
		return model.GalleryPageThumb{}, false, nil
	}
	return *pages[index].Thumbnail, true, nil
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

func ScrapeGalleryList(ctx context.Context, client *http.Client, listURL string, page int, opts *SearchOptions, navOpts *ListingNavOptions) (results []model.SearchResult, nav ListingNav, err error) {
	u, err := url.Parse(listURL)
	if err != nil {
		return nil, ListingNav{}, err
	}

	existing := u.Query()
	if opts != nil {
		newParams := BuildSearchQuery("", nil, opts)
		for k, vs := range newParams {
			for _, v := range vs {
				existing.Set(k, v)
			}
		}
	}
	navOpts.Apply(existing)
	u.RawQuery = existing.Encode()

	doc, err := fetchListingDoc(ctx, client, u.String(), page)
	if err != nil {
		if errors.Is(err, errNoNextPage) {
			return []model.SearchResult{}, ListingNav{}, nil
		}
		return nil, ListingNav{}, err
	}

	results, err = parseGalleryListResults(doc)
	if err != nil && err.Error() == "empty gallery list" {
		return []model.SearchResult{}, parseListingNav(doc), nil
	}
	if err != nil {
		return nil, ListingNav{}, err
	}
	return results, parseListingNav(doc), nil
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

// ValidatePageThumbnailURL validates a sprite image URL used by the page
// thumbnail crop API. Sprites are served from the e-hentai image CDN, which is
// a different host set than the gallery cover thumbnails.
func ValidatePageThumbnailURL(rawURL string) error {
	return validatePageThumbnailURL(rawURL)
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
