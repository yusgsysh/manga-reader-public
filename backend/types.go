package main

import (
	"strconv"
	"strings"
	"time"
)

// GalleryMetadata 来自官方 API 的元数据
type GalleryMetadata struct {
	GID          int       `json:"gid"`
	Token        string    `json:"token"`
	ArchiverKey  string    `json:"archiver_key"`
	Title        string    `json:"title"`
	TitleJpn     string    `json:"title_jpn"`
	Category     string    `json:"category"`
	Thumb        string    `json:"thumb"`
	Uploader     string    `json:"uploader"`
	Posted       string    `json:"posted"`       // unix timestamp string
	FileCount    string    `json:"filecount"`
	FileSize     int       `json:"filesize"`
	Expunged     bool      `json:"expunged"`
	Rating       string    `json:"rating"`
	TorrentCount string    `json:"torrentcount"`
	Tags         []string  `json:"tags"`          // ["namespace:tag", ...]
	ParentGId    string    `json:"parent_gid"`
	ParentKey    string    `json:"parent_key"`
	FirstGId     string    `json:"first_gid"`
	FirstKey     string    `json:"first_key"`
	Error        string    `json:"error,omitempty"`
}

// Gallery API 响应结构
type Gallery struct {
	ID          int64           `json:"id"`
	Token       string          `json:"token"`
	Title       string          `json:"title"`
	TitleJPN    string          `json:"title_jpn"`
	Category    GalleryCategory `json:"category"`
	Thumbnail   string          `json:"thumbnail"`
	PageCount   int             `json:"page_count"`
	Rating      float64         `json:"rating"`
	RatingCount int             `json:"rating_count"`
	Uploader    string          `json:"uploader"`
	PostedAt    *time.Time      `json:"posted_at,omitempty"`
	Tags        []Tag           `json:"tags"`
	FileSize    string          `json:"file_size,omitempty"`
	Expunged    bool            `json:"expunged,omitempty"`
}

type GalleryCategory string

const (
	CategoryDoujinshi GalleryCategory = "doujinshi"
	CategoryManga     GalleryCategory = "manga"
	CategoryArtistCG  GalleryCategory = "artistcg"
	CategoryGameCG    GalleryCategory = "gamecg"
	CategoryWestern   GalleryCategory = "western"
	CategoryImageSet  GalleryCategory = "image-set"
	CategoryCosplay   GalleryCategory = "cosplay"
	CategoryAsianPorn GalleryCategory = "asianporn"
	CategoryNonH      GalleryCategory = "non-h"
	CategoryMisc      GalleryCategory = "misc"
	CategoryOther     GalleryCategory = "other"
)

func MapCategory(raw string) GalleryCategory {
	switch strings.ToLower(strings.TrimSpace(raw)) {
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
	case "cosplay":
		return CategoryCosplay
	case "asian porn":
		return CategoryAsianPorn
	case "non-h":
		return CategoryNonH
	case "miscellaneous":
		return CategoryMisc
	default:
		return CategoryOther
	}
}

type Tag struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}

// ParseTags 将 "namespace:tag" 字符串数组转换为 Tag 数组
func ParseTags(rawTags []string) []Tag {
	tags := make([]Tag, 0, len(rawTags))
	for _, raw := range rawTags {
		parts := strings.SplitN(raw, ":", 2)
		if len(parts) == 2 {
			tags = append(tags, Tag{
				Namespace: parts[0],
				Name:      parts[1],
			})
		} else if len(parts) == 1 && parts[0] != "" {
			tags = append(tags, Tag{
				Namespace: "",
				Name:      parts[0],
			})
		}
	}
	return tags
}

// ConvertMetadataToGallery 将 API 元数据转换为 Gallery 响应
func ConvertMetadataToGallery(meta *GalleryMetadata) *Gallery {
	g := &Gallery{
		ID:        int64(meta.GID),
		Token:     meta.Token,
		Title:     meta.Title,
		TitleJPN:  meta.TitleJpn,
		Category:  MapCategory(meta.Category),
		Thumbnail: meta.Thumb,
		Uploader:  meta.Uploader,
		Tags:      ParseTags(meta.Tags),
		Expunged:  meta.Expunged,
	}

	// 解析 rating
	if v, err := strconv.ParseFloat(meta.Rating, 64); err == nil {
		g.Rating = v
	}

	// 解析 filecount 作为 pagecount
	if v, err := strconv.Atoi(meta.FileCount); err == nil {
		g.PageCount = v
	}

	// 解析 posted 时间戳
	if meta.Posted != "" {
		if ts, err := strconv.ParseInt(meta.Posted, 10, 64); err == nil {
			t := time.Unix(ts, 0)
			g.PostedAt = &t
		}
	}

	return g
}
