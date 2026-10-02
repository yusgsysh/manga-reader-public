package model

import "time"

// GalleryPageThumb describes where a page thumbnail lives inside the gallery's
// sprite image. The site renders thumbnails with a CSS background-position, so
// the visible cell is (X, Y) with size (Width, Height) in sprite pixels.
type GalleryPageThumb struct {
	SpriteURL string `json:"sprite_url"`
	X         int    `json:"x"`
	Y         int    `json:"y"`
	Width     int    `json:"width"`
	Height    int    `json:"height"`
}

// GalleryPageRef is a lightweight page reference (URL + index) without
// thumbnail geometry. Used for page lists, prefill queues, reader, etc.
type GalleryPageRef struct {
	PageURL string `json:"page_url"`
	Index   int    `json:"index"`
}

// GalleryCacheSnapshot is the metadata written into gallery_cache. Sources
// (official metadata, scraped details, reading progress) fill the fields they
// can provide; empty/zero values are treated as "no update".
type GalleryCacheSnapshot struct {
	Title       string
	TitleJPN    string
	Category    string
	Thumbnail   string
	PageCount   int
	Rating      float64
	RatingCount int
	Uploader    string
	Posted      string
	PostedAt    *time.Time
	Language    string
	Translated  bool
	FileSize    string
	Favorited   int
	Expunged    bool
	Tags        []Tag
}