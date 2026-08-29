package main

import "time"

// Bookshelf 是数据库中的书架记录
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

// ReadingProgress 是数据库中的阅读进度记录
type ReadingProgress struct {
	GalleryID   int64      `json:"gallery_id"`
	Token       string     `json:"token"`
	CurrentPage int        `json:"current_page"`
	Progress    float64    `json:"progress"`
	Completed   bool       `json:"completed"`

	StartedAt *time.Time `json:"started_at,omitempty"`
	UpdatedAt *time.Time `json:"updated_at,omitempty"`
}

// BookshelfItem 是书架列表中的单个项目
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

// BookshelfListResponse 是书架列表的分页响应
type BookshelfListResponse struct {
	Page       int             `json:"page"`
	PageSize   int             `json:"page_size"`
	Total      int             `json:"total"`
	TotalPages int             `json:"total_pages"`
	Results    []BookshelfItem `json:"results"`
}

// BookshelfStatus 是收藏状态查询的响应
type BookshelfStatus struct {
	InBookshelf bool       `json:"in_bookshelf"`
	AddedAt     *time.Time `json:"added_at,omitempty"`
}

// BookshelfMutationResponse 是收藏/取消收藏的响应
type BookshelfMutationResponse struct {
	Success     bool `json:"success"`
	InBookshelf bool `json:"in_bookshelf"`
}

// UpdateReadingProgressRequest 是更新阅读进度的请求体
type UpdateReadingProgressRequest struct {
	CurrentPage int     `json:"current_page"`
	Progress    float64 `json:"progress"`
	Completed   bool    `json:"completed"`
}

// RecentlyReadItem 是最近阅读列表中的单个项目
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

// RecentlyReadResponse 是最近阅读的响应
type RecentlyReadResponse struct {
	Results []RecentlyReadItem `json:"results"`
}

// GalleryToBookshelf 将 Gallery 转换为 Bookshelf 快照
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

// BookshelfToItem 将 Bookshelf 转换为 API 响应中的 BookshelfItem
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
