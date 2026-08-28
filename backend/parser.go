package main

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

var (
	reRating      = regexp.MustCompile(`Average:\s*([0-9]+(?:\.[0-9]+)?)`)
	rePageCount   = regexp.MustCompile(`(\d+)\s+pages`)
	rePostedAt    = regexp.MustCompile(`^(\d{4}-\d{2}-\d{2}\s+\d{2}:\d{2})$`)
	reThumbnail   = regexp.MustCompile(`url\(([^)]+)\)`)
	reRatingCount = regexp.MustCompile(`(\d+)`)
)

func ParseGallery(html []byte, id int64, token string) (*Gallery, error) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(string(html)))
	if err != nil {
		return nil, fmt.Errorf("failed to parse HTML: %w", err)
	}

	g := &Gallery{
		ID:    id,
		Token: token,
		Tags:  []Tag{},
	}

	title := doc.Find("#gn").First().Text()
	if title == "" {
		return nil, fmt.Errorf("gallery title not found")
	}
	g.Title = title

	g.TitleJPN = doc.Find("#gj").First().Text()

	g.Category = mapCategory(doc.Find("#gdc .cs").First().Text())

	g.Thumbnail = parseThumbnail(doc)

	g.Uploader = doc.Find("#gdn").First().Text()

	g.Tags = parseTags(doc)

	g.PostedAt, g.PageCount = parseMetadataTable(doc)

	g.Rating, g.RatingCount = parseRating(doc)

	return g, nil
}

func parseThumbnail(doc *goquery.Document) string {
	div := doc.Find("#gd1 div").First()
	style, exists := div.Attr("style")
	if !exists {
		return ""
	}

	match := reThumbnail.FindStringSubmatch(style)
	if len(match) > 1 {
		return match[1]
	}

	return ""
}

func parseRating(doc *goquery.Document) (float32, int) {
	var rating float32
	var ratingCount int

	label := doc.Find("#rating_label").First().Text()
	if match := reRating.FindStringSubmatch(label); len(match) > 1 {
		if v, err := strconv.ParseFloat(match[1], 32); err == nil {
			rating = float32(v)
		}
	}

	countText := doc.Find("#rating_count").First().Text()
	if match := reRatingCount.FindStringSubmatch(countText); len(match) > 1 {
		if v, err := strconv.Atoi(match[1]); err == nil {
			ratingCount = v
		}
	}

	return rating, ratingCount
}

func parseMetadataTable(doc *goquery.Document) (*time.Time, int) {
	var postedAt *time.Time
	var pageCount int

	doc.Find("#gdd tr").Each(func(_ int, row *goquery.Selection) {
		label := row.Find(".gdt1").First().Text()
		value := row.Find(".gdt2").First().Text()
		value = strings.TrimSpace(value)

		switch {
		case strings.HasPrefix(label, "Posted"):
			if match := rePostedAt.FindStringSubmatch(value); len(match) > 1 {
				if t, err := time.Parse("2006-01-02 15:04", match[1]); err == nil {
					postedAt = &t
				}
			}
		case strings.HasPrefix(label, "Length"):
			if match := rePageCount.FindStringSubmatch(value); len(match) > 1 {
				if v, err := strconv.Atoi(match[1]); err == nil {
					pageCount = v
				}
			}
		}
	})

	return postedAt, pageCount
}

func parseTags(doc *goquery.Document) []Tag {
	var tags []Tag

	doc.Find("#taglist .gt a").Each(func(_ int, s *goquery.Selection) {
		tagID, exists := s.Attr("id")
		if !exists {
			return
		}

		tagID = strings.TrimPrefix(tagID, "ta_")

		namespace, name, found := strings.Cut(tagID, ":")
		if !found {
			namespace = ""
			name = tagID
		}

		tags = append(tags, Tag{
			Namespace: namespace,
			Name:      name,
		})
	})

	if tags == nil {
		tags = []Tag{}
	}

	return tags
}

func mapCategory(raw string) GalleryCategory {
	raw = strings.ToLower(strings.TrimSpace(raw))
	switch raw {
	case "doujinshi":
		return CategoryDoujinshi
	case "manga":
		return CategoryManga
	case "artist cg":
		return CategoryArtistCG
	case "game cg":
		return CategoryGameCG
	case "western":
		return CategoryWestern
	case "image set":
		return CategoryImageSet
	default:
		return CategoryOther
	}
}
