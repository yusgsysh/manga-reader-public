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

	"github.com/PuerkitoBio/goquery"

	"manga-reader/internal/model"
)

var foundReg = regexp.MustCompile(`Found(?: about)? ([\d,]+)\+? results?`)
var foundThousandsReg = regexp.MustCompile(`Found thousands of results`)

type SearchOptions struct {
	Tags []string

	MinPages  *int
	MaxPages  *int
	MinRating *int

	HasTorrent      bool
	IncludeExpunged bool

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

	if opts == nil {
		if keyword != "" {
			q.Set("f_search", keyword)
		}
		return q
	}

	advanced := false

	searchTerms := make([]string, 0, len(opts.Tags)+1)
	if keyword != "" {
		searchTerms = append(searchTerms, keyword)
	}
	for _, tag := range opts.Tags {
		tag = strings.TrimSpace(tag)
		if tag != "" {
			searchTerms = append(searchTerms, "tag:"+tag)
		}
	}
	if len(searchTerms) > 0 {
		q.Set("f_search", strings.Join(searchTerms, " "))
	}
	if len(opts.Tags) > 0 {
		advanced = true
	}

	// ExHentai treats empty/zero page bounds as "unset" (search_presubmit
	// disables them before submit), so only forward positive values.
	if opts.MinPages != nil && *opts.MinPages > 0 {
		q.Set("f_spf", strconv.Itoa(*opts.MinPages))
		advanced = true
	}
	if opts.MaxPages != nil && *opts.MaxPages > 0 {
		q.Set("f_spt", strconv.Itoa(*opts.MaxPages))
		advanced = true
	}
	if opts.MinRating != nil {
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
			if page == 0 {
				return 0, []model.SearchResult{}, ListingNav{}, nil
			}
			firstDoc, ferr := httpGetDoc(ctx, client, firstURL)
			if ferr != nil {
				return 0, nil, ListingNav{}, ferr
			}
			total, ok := parseSearchTotal(firstDoc)
			if !ok || total == 0 {
				return 0, []model.SearchResult{}, ListingNav{}, nil
			}
			return total, []model.SearchResult{}, ListingNav{}, nil
		}
		return 0, nil, ListingNav{}, err
	}

	noHits := doc.Find("body > div.ido > div:nth-child(2) > p").Text()
	if noHits != "" {
		// A "no results" page is a successful empty outcome, not an error.
		return 0, []model.SearchResult{}, ListingNav{}, nil
	}

	total, ok := parseSearchTotal(doc)
	if !ok && page > 0 {
		if firstDoc, ferr := httpGetDoc(ctx, client, firstURL); ferr == nil {
			total, ok = parseSearchTotal(firstDoc)
		}
	}
	if !ok {
		return 0, nil, ListingNav{}, fmt.Errorf("could not parse result count")
	}
	if total == 0 {
		return 0, []model.SearchResult{}, ListingNav{}, nil
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
