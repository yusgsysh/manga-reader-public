package model

import "time"

// Bookshelf is a bookshelf record in the database. It stores only the gallery
// reference; metadata is joined from gallery_cache when the list is served.
type Bookshelf struct {
	GalleryID int64     `json:"gallery_id"`
	Token     string    `json:"token"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

// ReadingProgress is a reading progress record in the database.
type ReadingProgress struct {
	GalleryID   int64   `json:"gallery_id"`
	Token       string  `json:"token"`
	CurrentPage int     `json:"current_page"`
	Progress    float64 `json:"progress"`
	Completed   bool    `json:"completed"`

	CreatedAt *time.Time `json:"created_at,omitempty"`
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

	CreatedAt time.Time        `json:"created_at"`
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
	CreatedAt   *time.Time `json:"created_at,omitempty"`
}

// BookshelfMutationResponse is the response for add/remove bookshelf operations.
type BookshelfMutationResponse struct {
	Success     bool `json:"success"`
	InBookshelf bool `json:"in_bookshelf"`
	// Offline is true when an add was satisfied from cached metadata because
	// the upstream ExHentai API was unreachable.
	Offline bool `json:"offline,omitempty"`
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

// RecentlyReadResponse is the paginated response for the recently read endpoint.
type RecentlyReadResponse struct {
	Page       int                `json:"page"`
	PageSize   int                `json:"page_size"`
	Total      int                `json:"total"`
	TotalPages int                `json:"total_pages"`
	Results    []RecentlyReadItem `json:"results"`
}
