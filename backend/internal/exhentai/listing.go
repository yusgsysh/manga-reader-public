package exhentai

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/PuerkitoBio/goquery"

	"manga-reader/internal/model"
	"manga-reader/internal/ttl"
)

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
// per page instead of re-walking from page 0 every time. The cursor lifetime
// lives in internal/ttl.

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
		expires: time.Now().Add(ttl.ListingCursor),
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

func ScrapeGalleryList(ctx context.Context, client *http.Client, listURL string, page int, categories []string, opts *SearchOptions, navOpts *ListingNavOptions) (results []model.SearchResult, nav ListingNav, err error) {
	u, err := url.Parse(listURL)
	if err != nil {
		return nil, ListingNav{}, err
	}

	existing := u.Query()
	if opts != nil {
		newParams := BuildSearchQuery("", categories, opts)
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
