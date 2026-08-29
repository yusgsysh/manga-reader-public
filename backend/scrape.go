package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"

	"github.com/PuerkitoBio/goquery"
)

const exhentaiURL = "https://exhentai.org"
const ehentaiURL = "https://e-hentai.org"

var foundReg = regexp.MustCompile(`Found(?: about)? ([\d,]+)\+? results?`)
var foundThousandsReg = regexp.MustCompile(`Found thousands of results`)
var starsReg = regexp.MustCompile(`background-position:(-?\d+)px (-\d+)px`)
var coverUrlReg = regexp.MustCompile(`url\(([^)]+)\)`)
var numReg = regexp.MustCompile(`Showing 1 - (\d+) of ([\d,]+) images?`)

func httpGet(ctx context.Context, client *http.Client, url string) (*http.Response, error) {
	req, err := http.NewRequestWithContext(ctx, "GET", url, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", exhentaiURL+"/")
	return client.Do(req)
}

func httpGetDoc(ctx context.Context, client *http.Client, url string) (*goquery.Document, error) {
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

func parseStars(stars string) float64 {
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

type SearchResult struct {
	Domain   string
	GalleryID int
	Token    string
	Cat      string
	Cover    string
	Posted   string
	Rating   float64
	URL      string
	Title    string
	Tags     []string
	Uploader string
	Pages    int
}

func scrapeSearch(ctx context.Context, client *http.Client, siteURL, keyword string, categories []string, page int) (total int, results []SearchResult, err error) {
	u, err := url.Parse(siteURL)
	if err != nil {
		return 0, nil, err
	}

	querys := url.Values{}
	if len(categories) > 0 {
		catVal := buildCategoryFilter(categories)
		if catVal != "" {
			querys.Set("f_cats", catVal)
		}
	}
	if keyword != "" {
		querys.Set("f_search", keyword)
	}
	u.RawQuery = querys.Encode()

	// 先获取第一页以解析 total
	doc, err := httpGetDoc(ctx, client, u.String())
	if err != nil {
		return 0, nil, err
	}

	// 检查无结果
	noHits := doc.Find("body > div.ido > div:nth-child(2) > p").Text()
	if noHits != "" {
		return 0, nil, fmt.Errorf("no hits found: %s", noHits)
	}

	// 解析结果总数
	foundResults := doc.Find("body > div.ido > div:nth-child(2) > div.searchtext > p").Text()
	matches := foundReg.FindStringSubmatch(foundResults)
	if len(matches) == 0 {
		if foundThousandsReg.MatchString(foundResults) {
			total = 9999
		} else {
			return 0, nil, fmt.Errorf("could not parse result count from: %s", foundResults)
		}
	} else {
		totalStr := strings.ReplaceAll(matches[1], ",", "")
		total, _ = strconv.Atoi(totalStr)
	}
	if total == 0 {
		return 0, nil, fmt.Errorf("no results")
	}

	// EHentai 使用游标分页 (next=<gallery_id>)，page 参数无效
	// 对于 page > 0，需要依次跟随 next 链接
	for p := 0; p < page; p++ {
		nextURL := extractNextURL(doc)
		if nextURL == "" {
			return total, nil, nil
		}
		doc, err = httpGetDoc(ctx, client, nextURL)
		if err != nil {
			return 0, nil, err
		}
	}

	results, err = parseSearchResults(doc)
	return
}

// extractNextURL 从搜索结果页提取 "Next >" 链接
func extractNextURL(doc *goquery.Document) string {
	var nextURL string
	doc.Find("a").Each(func(i int, s *goquery.Selection) {
		if s.Text() == "Next >" {
			nextURL, _ = s.Attr("href")
		}
	})
	return nextURL
}

// parseSearchResults 从 goquery Document 解析搜索结果
func parseSearchResults(doc *goquery.Document) ([]SearchResult, error) {
	table := doc.Find("body > div.ido > div:nth-child(2) > table > tbody > tr")
	if table.Length() == 0 {
		return nil, fmt.Errorf("empty results table")
	}

	results := make([]SearchResult, 0, table.Length()-1)

	table.Each(func(i int, s *goquery.Selection) {
		gl1e := s.Find("td.gl1e")
		if gl1e.Length() == 0 {
			return
		}
		gURL, _ := gl1e.Find("div > a").Attr("href")

		gl2e := s.Find("td.gl2e")
		cat := gl2e.Find("div.gl3e > div.cn").Text()
		upTime := gl2e.Find("div.gl3e > div:nth-child(2)").Text()
		stars, _ := gl2e.Find("div.gl3e > div.ir").Attr("style")
		uploader := gl2e.Find("div.gl3e > div:nth-child(4) > a").Text()
		pagesStr := gl2e.Find("div.gl3e > div:nth-child(5)").Text()
		pagesStr = strings.TrimSuffix(pagesStr, " pages")
		pagesNum, _ := strconv.Atoi(pagesStr)

		glname := gl2e.Find("div.gl4e.glname")
		title := glname.Find("div.glink").Text()
		var tags []string
		glname.Find("table td:nth-child(2) > div").Each(func(i int, s *goquery.Selection) {
			tags = append(tags, s.AttrOr("title", s.Text()))
		})

		domain, gId, gToken := parseGalleryURL(gURL)
		gIdNum, _ := strconv.Atoi(gId)

		results = append(results, SearchResult{
			Domain:    domain,
			GalleryID: gIdNum,
			Token:     gToken,
			Cat:       cat,
			Cover:     gl1e.Find("div > a > img").AttrOr("src", ""),
			Posted:    upTime,
			Rating:    parseStars(stars),
			URL:       gURL,
			Title:     title,
			Tags:      tags,
			Uploader:  uploader,
			Pages:     pagesNum,
		})
	})
	return results, nil
}

func parseGalleryURL(u string) (domain, gId, gToken string) {
	u = strings.TrimSuffix(u, "/")
	splits := strings.Split(u, "/")
	for i, s := range splits {
		if s == "g" && i+2 < len(splits) {
			return splits[i-1], splits[i+1], splits[i+2]
		}
	}
	return "", "", ""
}

// buildCategoryFilter 将分类名列表转换为 f_cats 值
// 参考 EHentai-go 的 Category.Format(): 1023 ^ (selected categories bitmask)
func buildCategoryFilter(categories []string) string {
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

type GalleryDetail struct {
	Domain      string
	GalleryID   int
	Token       string
	Cover       string
	Title       string
	TitleJpn    string
	Cat         string
	Uploader    string
	Posted      string
	Parent      int
	Visible     string
	Language    string
	Translated  string
	FileSize    string
	Length      int
	Favorited   int
	RatingCount int
	Rating      float64
	Tags        []TagItem
	PageUrls    []string
}

type TagItem struct {
	Namespace string
	Name      string
}

func scrapeGalleryDetails(ctx context.Context, client *http.Client, galleryURL string) (GalleryDetail, error) {
	doc, err := httpGetDoc(ctx, client, galleryURL)
	if err != nil {
		return GalleryDetail{}, err
	}

	// cover
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

	var tags []TagItem
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
				tags = append(tags, TagItem{Namespace: namespace, Name: tag})
			}
		})
	})

	// 解析页数
	numImages := doc.Find(".gpc").Text()
	matches := numReg.FindStringSubmatch(numImages)
	var total int
	if len(matches) >= 3 {
		matches[2] = strings.ReplaceAll(matches[2], ",", "")
		total, _ = strconv.Atoi(matches[2])
	}

	// 收集页链接 (第一页)
	var pageUrls []string
	doc.Find("#gdt > a").Each(func(i int, s *goquery.Selection) {
		href, _ := s.Attr("href")
		if href != "" {
			pageUrls = append(pageUrls, href)
		}
	})

	// 多页画廊需要翻页获取更多页链接
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
			pageDoc.Find("#gdt > a").Each(func(i int, s *goquery.Selection) {
				href, _ := s.Attr("href")
				if href != "" {
					pageUrls = append(pageUrls, href)
				}
			})
		}
	}

	domain := ""
	if strings.Contains(galleryURL, "exhentai.org") {
		domain = "exhentai.org"
	} else if strings.Contains(galleryURL, "e-hentai.org") {
		domain = "e-hentai.org"
	}

	gIdStr := ""
	gTokenStr := ""
	d, gId, gToken := parseGalleryURL(galleryURL)
	if d != "" {
		domain = d
		gIdStr = gId
		gTokenStr = gToken
	}
	gIdNum, _ := strconv.Atoi(gIdStr)

	return GalleryDetail{
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
		PageUrls:    pageUrls,
	}, nil
}

var nlReg = regexp.MustCompile(`nl\('(.+?)'\)`)

func scrapePageImageURL(ctx context.Context, client *http.Client, pageURL string) (imgURL string, fallbackURL string, err error) {
	resp, err := httpGet(ctx, client, pageURL)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
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

	// <a href="#" id="loadfail" onclick="return nl('SZF-483294')">Reload broken image</a>
	onclick, _ := doc.Find("#loadfail").Attr("onclick")
	fallbackURL = buildNlFallbackURL(pageURL, onclick)

	return img, fallbackURL, nil
}

func buildNlFallbackURL(pageURL, onclick string) string {
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

func proxyImage(ctx context.Context, client *http.Client, imgURL string) (data []byte, contentType string, err error) {
	req, err := http.NewRequestWithContext(ctx, "GET", imgURL, nil)
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/139.0.0.0 Safari/537.36")
	req.Header.Set("Referer", exhentaiURL+"/")

	resp, err := client.Do(req)
	if err != nil {
		return nil, "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, "", fmt.Errorf("HTTP %d", resp.StatusCode)
	}

	contentType = resp.Header.Get("Content-Type")
	data, err = io.ReadAll(resp.Body)
	if err != nil {
		return nil, "", err
	}
	return data, contentType, nil
}

func scrapeGalleryList(ctx context.Context, client *http.Client, listURL string, page int) (results []SearchResult, err error) {
	u, err := url.Parse(listURL)
	if err != nil {
		return nil, err
	}

	doc, err := httpGetDoc(ctx, client, u.String())
	if err != nil {
		return nil, err
	}

	for p := 0; p < page; p++ {
		nextURL := extractNextURL(doc)
		if nextURL == "" {
			return nil, nil
		}
		doc, err = httpGetDoc(ctx, client, nextURL)
		if err != nil {
			return nil, err
		}
	}

	results, err = parseGalleryListResults(doc)
	return
}

func parseGalleryListResults(doc *goquery.Document) ([]SearchResult, error) {
	table := doc.Find("table.itg.gltc > tbody > tr")
	if table.Length() == 0 {
		return nil, fmt.Errorf("empty gallery list")
	}

	results := make([]SearchResult, 0, table.Length())

	table.Each(func(i int, s *goquery.Selection) {
		gl3c := s.Find("td.gl3c.glname")
		if gl3c.Length() == 0 {
			return
		}
		a := gl3c.Find("a")
		gURL, _ := a.Attr("href")

		gl1c := s.Find("td.gl1c")
		cat := gl1c.Find("div.cn").Text()

		gl2c := s.Find("td.gl2c")
		stars, _ := gl2c.Find("div.ir").Attr("style")

		upTime := ""
		pagesNum := 0
		gl2c.Find("div").Each(func(i int, s *goquery.Selection) {
			text := s.Text()
			if len(text) >= 10 && text[4] == '-' && text[7] == '-' {
				if upTime == "" {
					upTime = text
				}
			}
		})
		pagesStr := ""
		gl2c.Find("div").Each(func(i int, s *goquery.Selection) {
			text := s.Text()
			if len(text) > 6 && text[len(text)-6:] == " pages" {
				pagesStr = text
			}
		})
		pagesStr = strings.TrimSuffix(pagesStr, " pages")
		pagesNum, _ = strconv.Atoi(pagesStr)

		title := a.Find("div.glink").Text()
		var tags []string
		a.Find("div > div.gt").Each(func(i int, s *goquery.Selection) {
			tags = append(tags, s.AttrOr("title", s.Text()))
		})

		gl4c := s.Find("td.gl4c")
		uploader := gl4c.Find("div:first-child > a").Text()

		domain, gId, gToken := parseGalleryURL(gURL)
		gIdNum, _ := strconv.Atoi(gId)

		coverImg := gl2c.Find("div.glthumb img")
		cover := coverImg.AttrOr("data-src", "")
		if cover == "" {
			cover = coverImg.AttrOr("src", "")
		}

		results = append(results, SearchResult{
			Domain:    domain,
			GalleryID: gIdNum,
			Token:     gToken,
			Cat:       cat,
			Cover:     cover,
			Posted:    upTime,
			Rating:    parseStars(stars),
			URL:       gURL,
			Title:     title,
			Tags:      tags,
			Uploader:  uploader,
			Pages:     pagesNum,
		})
	})
	return results, nil
}
