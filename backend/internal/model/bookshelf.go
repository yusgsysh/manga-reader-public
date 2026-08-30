package model

import "time"

// Bookshelf is a bookshelf record in the database.
type Bookshelf struct {
	GalleryID int64           `json:"gallery_id"`
	Token     string          `json:"token"`
	Title     string          `json:"title"`
	TitleJPN  string          `json:"title_jpn"`
	Category  GalleryCategory `json:"category"`
	Thumbnail string          `json:"thumbnail"`
	PageCount int             `json:"page_count"`

	AddedAt   time.Time `json:"added_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReadingProgress is a reading progress record in the database.
type ReadingProgress struct {
	GalleryID   int64   `json:"gallery_id"`
	Token       string  `json:"token"`
	CurrentPage int     `json:"current_page"`
	Progress    float64 `json:"progress"`
	Completed   bool    `json:"completed"`

	StartedAt *time.Time `json:"started_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// BookshelfItem is a single item in the bookshelf list response.
type BookshelfItem struct {
	ID        int64           `json:"id"`
	Token     string          `json:"token"`
	Title     string          `json:"title"`
	TitleJPN  string          `json:"title_jpn"`
	Category  GalleryCategory `json:"category"`
	Thumbnail string          `json:"thumbnail"`
	Pages     int             `json:"pages"`

	AddedAt   time.Time        `json:"added_at"`
	UpdatedAt time.Time        `json:"updated_at"`
	Reading   *ReadingProgress `json:"reading,omitempty"`
}

// BookshelfListResponse is the paginated bookshelf list response.
type BookshelfListResponse struct {
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	Total      int             `json:"total"`
	TotalPages int             `json:"total_pages"`
	Results    []BookshelfItem `json:"results"`
}

// BookshelfStatus is the response for bookshelf status queries.
type BookshelfStatus struct {
	InBookshelf bool       `json:"in_bookshelf"`
	AddedAt     *time.Time `json:"added_at,omitempty"`
}

// BookshelfMutationResponse is the response for add/remove bookshelf operations.
type BookshelfMutationResponse struct {
	Success     bool `json:"success"`
	InBookshelf bool `json:"in_bookshelf"`
}

// UpdateReadingProgressRequest is the request body for updating reading progress.
type UpdateReadingProgressRequest struct {
	CurrentPage int     `json:"current_page"`
	Progress    float64 `json:"progress"`
	Completed   bool    `json:"completed"`
}

// RecentlyReadItem is a single item in the recently read list.
type RecentlyReadItem struct {
	ID        int64           `json:"id"`
	Token     string          `json:"token"`
	Title     string          `json:"title"`
	TitleJPN  string          `json:"title_jpn"`
	Category  GalleryCategory `json:"category"`
	Thumbnail string          `json:"thumbnail"`
	Pages     int             `json:"pages"`

	Reading ReadingProgress `json:"reading"`
}

// RecentlyReadResponse is the response for the recently read endpoint.
type RecentlyReadResponse struct {
	Results []RecentlyReadItem `json:"results"`
}

// GalleryToBookshelf converts a Gallery to a Bookshelf snapshot.
func GalleryToBookshelf(g *Gallery) *Bookshelf {
	if g == nil {
		return nil
	}

	return &Bookshelf{
		GalleryID: g.ID,
		Token:     g.Token,
		Title:     g.Title,
		TitleJPN:  g.TitleJPN,
		Category:  g.Category,
		Thumbnail: g.Thumbnail,
		PageCount: g.PageCount,
	}
}

// BookshelfToItem converts a Bookshelf to a BookshelfItem for API responses.
func BookshelfToItem(b *Bookshelf, progress *ReadingProgress) BookshelfItem {
	item := BookshelfItem{
		ID:        b.GalleryID,
		Token:     b.Token,
		Title:     b.Title,
		TitleJPN:  b.TitleJPN,
		Category:  b.Category,
		Thumbnail: b.Thumbnail,
		Pages:     b.PageCount,
		AddedAt:   b.AddedAt,
		UpdatedAt: b.UpdatedAt,
	}

	if progress != nil {
		item.Reading = progress
	}

	return item
}
