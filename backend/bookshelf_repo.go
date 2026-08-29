package main

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

type BookshelfRepository struct {
	db *sql.DB
}

func NewBookshelfRepository(db *sql.DB) *BookshelfRepository {
	return &BookshelfRepository{db: db}
}

func (r *BookshelfRepository) Add(ctx context.Context, b *Bookshelf) error {
	now := time.Now().UTC()
	_, err := r.db.ExecContext(ctx,
		`INSERT OR IGNORE INTO bookshelf (gallery_id, token, title, title_jpn, category, thumbnail, page_count, added_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		b.GalleryID, b.Token, b.Title, b.TitleJPN, string(b.Category), b.Thumbnail, b.PageCount, now, now,
	)
	if err != nil {
		return fmt.Errorf("add bookshelf: %w", err)
	}
	return nil
}

func (r *BookshelfRepository) Remove(ctx context.Context, galleryID int64, token string) error {
	_, err := r.db.ExecContext(ctx,
		`DELETE FROM bookshelf WHERE gallery_id = ? AND token = ?`,
		galleryID, token,
	)
	if err != nil {
		return fmt.Errorf("remove bookshelf: %w", err)
	}
	return nil
}

func (r *BookshelfRepository) Exists(ctx context.Context, galleryID int64, token string) (bool, *time.Time, error) {
	var addedAt time.Time
	err := r.db.QueryRowContext(ctx,
		`SELECT added_at FROM bookshelf WHERE gallery_id = ? AND token = ?`,
		galleryID, token,
	).Scan(&addedAt)
	if err == sql.ErrNoRows {
		return false, nil, nil
	}
	if err != nil {
		return false, nil, fmt.Errorf("check bookshelf exists: %w", err)
	}
	return true, &addedAt, nil
}

func (r *BookshelfRepository) Get(ctx context.Context, galleryID int64, token string) (*Bookshelf, error) {
	b := &Bookshelf{}
	var category string
	err := r.db.QueryRowContext(ctx,
		`SELECT gallery_id, token, title, title_jpn, category, thumbnail, page_count, added_at, updated_at
		 FROM bookshelf WHERE gallery_id = ? AND token = ?`,
		galleryID, token,
	).Scan(&b.GalleryID, &b.Token, &b.Title, &b.TitleJPN, &category, &b.Thumbnail, &b.PageCount, &b.AddedAt, &b.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get bookshelf: %w", err)
	}
	b.Category = GalleryCategory(category)
	return b, nil
}

func (r *BookshelfRepository) Count(ctx context.Context) (int, error) {
	var count int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bookshelf`).Scan(&count); err != nil {
		return 0, fmt.Errorf("count bookshelf: %w", err)
	}
	return count, nil
}

func (r *BookshelfRepository) List(ctx context.Context, page, pageSize int) (*BookshelfListResponse, error) {
	var total int
	if err := r.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM bookshelf`).Scan(&total); err != nil {
		return nil, fmt.Errorf("count bookshelf: %w", err)
	}

	totalPages := (total + pageSize - 1) / pageSize
	offset := page * pageSize

	rows, err := r.db.QueryContext(ctx,
		`SELECT gallery_id, token, title, title_jpn, category, thumbnail, page_count, added_at, updated_at
		 FROM bookshelf ORDER BY added_at DESC LIMIT ? OFFSET ?`,
		pageSize, offset,
	)
	if err != nil {
		return nil, fmt.Errorf("list bookshelf: %w", err)
	}
	defer rows.Close()

	items := make([]BookshelfItem, 0)
	for rows.Next() {
		var b Bookshelf
		var category string
		if err := rows.Scan(&b.GalleryID, &b.Token, &b.Title, &b.TitleJPN, &category, &b.Thumbnail, &b.PageCount, &b.AddedAt, &b.UpdatedAt); err != nil {
			return nil, fmt.Errorf("scan bookshelf: %w", err)
		}
		b.Category = GalleryCategory(category)
		item := BookshelfToItem(&b, nil)
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate bookshelf: %w", err)
	}

	return &BookshelfListResponse{
		Page:       page,
		PageSize:   len(items),
		Total:      total,
		TotalPages: totalPages,
		Results:    items,
	}, nil
}

type ReadingProgressRepository struct {
	db *sql.DB
}

func NewReadingProgressRepository(db *sql.DB) *ReadingProgressRepository {
	return &ReadingProgressRepository{db: db}
}

func (r *ReadingProgressRepository) Get(ctx context.Context, galleryID int64, token string) (*ReadingProgress, error) {
	p := &ReadingProgress{}
	var completed int
	var startedAt, updatedAt sql.NullTime
	err := r.db.QueryRowContext(ctx,
		`SELECT gallery_id, token, current_page, progress, completed, started_at, updated_at
		 FROM reading_progress WHERE gallery_id = ? AND token = ?`,
		galleryID, token,
	).Scan(&p.GalleryID, &p.Token, &p.CurrentPage, &p.Progress, &completed, &startedAt, &updatedAt)
	if err == sql.ErrNoRows {
		return &ReadingProgress{
			GalleryID: galleryID,
			Token:     token,
		}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("get reading progress: %w", err)
	}
	p.Completed = completed == 1
	if startedAt.Valid {
		p.StartedAt = &startedAt.Time
	}
	if updatedAt.Valid {
		p.UpdatedAt = &updatedAt.Time
	}
	return p, nil
}

func (r *ReadingProgressRepository) Upsert(ctx context.Context, galleryID int64, token string, req *UpdateReadingProgressRequest) (*ReadingProgress, error) {
	if req.Completed {
		req.Progress = 1
	}

	now := time.Now().UTC()

	var exists bool
	err := r.db.QueryRowContext(ctx,
		`SELECT EXISTS(SELECT 1 FROM reading_progress WHERE gallery_id = ? AND token = ?)`,
		galleryID, token,
	).Scan(&exists)
	if err != nil {
		return nil, fmt.Errorf("check reading progress: %w", err)
	}

	completedInt := 0
	if req.Completed {
		completedInt = 1
	}

	if exists {
		_, err = r.db.ExecContext(ctx,
			`UPDATE reading_progress SET current_page = ?, progress = ?, completed = ?, updated_at = ?
			 WHERE gallery_id = ? AND token = ?`,
			req.CurrentPage, req.Progress, completedInt, now, galleryID, token,
		)
		if err != nil {
			return nil, fmt.Errorf("update reading progress: %w", err)
		}
	} else {
		_, err = r.db.ExecContext(ctx,
			`INSERT INTO reading_progress (gallery_id, token, current_page, progress, completed, started_at, updated_at)
			 VALUES (?, ?, ?, ?, ?, ?, ?)`,
			galleryID, token, req.CurrentPage, req.Progress, completedInt, now, now,
		)
		if err != nil {
			return nil, fmt.Errorf("insert reading progress: %w", err)
		}
	}

	return r.Get(ctx, galleryID, token)
}

func (r *ReadingProgressRepository) DeleteBefore(ctx context.Context, cutoff time.Time) (int64, error) {
	res, err := r.db.ExecContext(ctx,
		`DELETE FROM reading_progress WHERE updated_at < ?`,
		cutoff,
	)
	if err != nil {
		return 0, fmt.Errorf("delete reading progress before cutoff: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reading progress deleted rows: %w", err)
	}
	return n, nil
}

func (r *ReadingProgressRepository) DeleteAll(ctx context.Context) (int64, error) {
	res, err := r.db.ExecContext(ctx, `DELETE FROM reading_progress`)
	if err != nil {
		return 0, fmt.Errorf("delete all reading progress: %w", err)
	}
	n, err := res.RowsAffected()
	if err != nil {
		return 0, fmt.Errorf("reading progress deleted rows: %w", err)
	}
	return n, nil
}

func (r *ReadingProgressRepository) ListRecentlyRead(ctx context.Context, limit int) ([]RecentlyReadItem, error) {
	rows, err := r.db.QueryContext(ctx,
		`SELECT
			COALESCE(b.gallery_id, rp.gallery_id) AS gallery_id,
			COALESCE(b.token, rp.token) AS token,
			COALESCE(b.title, '') AS title,
			COALESCE(b.title_jpn, '') AS title_jpn,
			COALESCE(b.category, '') AS category,
			COALESCE(b.thumbnail, '') AS thumbnail,
			COALESCE(b.page_count, 0) AS page_count,
			rp.current_page,
			rp.progress,
			rp.completed,
			rp.started_at,
			rp.updated_at
		 FROM reading_progress rp
		 LEFT JOIN bookshelf b ON rp.gallery_id = b.gallery_id AND rp.token = b.token
		 ORDER BY rp.updated_at DESC
		 LIMIT ?`,
		limit,
	)
	if err != nil {
		return nil, fmt.Errorf("list recently read: %w", err)
	}
	defer rows.Close()

	items := make([]RecentlyReadItem, 0)
	for rows.Next() {
		var item RecentlyReadItem
		var category string
		var completed int
		var startedAt, updatedAt sql.NullTime
		if err := rows.Scan(
			&item.ID, &item.Token, &item.Title, &item.TitleJPN, &category, &item.Thumbnail, &item.Pages,
			&item.Reading.CurrentPage, &item.Reading.Progress, &completed, &startedAt, &updatedAt,
		); err != nil {
			return nil, fmt.Errorf("scan recently read: %w", err)
		}
		item.Category = GalleryCategory(category)
		item.Reading.GalleryID = item.ID
		item.Reading.Token = item.Token
		item.Reading.Completed = completed == 1
		if startedAt.Valid {
			item.Reading.StartedAt = &startedAt.Time
		}
		if updatedAt.Valid {
			item.Reading.UpdatedAt = &updatedAt.Time
		}
		items = append(items, item)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate recently read: %w", err)
	}

	return items, nil
}
