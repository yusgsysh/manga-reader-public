package model

import "time"

// CachedPage is a single gallery page reference stored in the gallery cache.
type CachedPage struct {
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
