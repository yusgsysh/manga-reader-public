package main

import "time"

type Gallery struct {
	ID          int64           `json:"id"`
	Token       string          `json:"token"`
	Title       string          `json:"title"`
	TitleJPN    string          `json:"title_jpn"`
	Category    GalleryCategory `json:"category"`
	Thumbnail   string          `json:"thumbnail"`
	PageCount   int             `json:"page_count"`
	Rating      float32         `json:"rating"`
	RatingCount int             `json:"rating_count"`
	Uploader    string          `json:"uploader"`
	PostedAt    *time.Time      `json:"posted_at,omitempty"`
	Tags        []Tag           `json:"tags"`
}

type GalleryCategory string

const (
	CategoryDoujinshi GalleryCategory = "doujinshi"
	CategoryManga     GalleryCategory = "manga"
	CategoryArtistCG  GalleryCategory = "artistcg"
	CategoryGameCG    GalleryCategory = "gamecg"
	CategoryWestern   GalleryCategory = "western"
	CategoryImageSet  GalleryCategory = "image-set"
	CategoryOther     GalleryCategory = "other"
)

type Tag struct {
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
}
