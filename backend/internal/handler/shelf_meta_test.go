package handler

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync"
	"testing"

	"manga-reader/internal/database"
	"manga-reader/internal/ent"
	"manga-reader/internal/gallerycache"
	"manga-reader/internal/model"
)

// metadataAPIServer answers the gdata API for the galleries its table maps,
// and "not found" for every other one, as the real endpoint does for an
// expunged or deleted gallery. It hands back its http.Client, which is what a
// Server talks upstream through.
func metadataAPIServer(t *testing.T, galleries map[string]model.GalleryMetadata) (*http.Client, *sync.Map) {
	t.Helper()
	requested := &sync.Map{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Method  string  `json:"method"`
			GIdList [][]any `json:"gidlist"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		var found []model.GalleryMetadata
		for _, pair := range body.GIdList {
			if len(pair) < 2 {
				continue
			}
			token, _ := pair[1].(string)
			id, _ := pair[0].(float64)
			key := strconv.FormatInt(int64(id), 10) + ":" + token
			requested.Store(key, true)
			if g, ok := galleries[key]; ok {
				found = append(found, g)
			}
		}
		if len(found) == 0 {
			_ = json.NewEncoder(w).Encode(map[string]any{
				"error": "Key missing, or duplicate data found.",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"gmetadata": found})
	}))
	t.Cleanup(srv.Close)
	client := srv.Client()
	client.Transport = &rewriteTransport{target: srv.URL, base: http.DefaultTransport}
	return client, requested
}

// rewriteTransport sends every request to the test server, whatever host the
// API names.
type rewriteTransport struct {
	target string
	base   http.RoundTripper
}

func (t *rewriteTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	u := *r.URL
	u.Scheme, u.Host = "http", strings.TrimPrefix(t.target, "http://")
	r2 := r.Clone(r.Context())
	r2.URL = &u
	return t.base.RoundTrip(r2)
}

// seedEmptyCacheRow writes the row a page-list walk leaves behind when the
// metadata write that should follow never lands: pages and thumbnails, no
// title, no cover, no page count.
func seedEmptyCacheRow(t *testing.T, client *ent.Client, galleryID int64, token string) {
	t.Helper()
	_, err := client.GalleryCache.Create().
		SetGalleryID(galleryID).
		SetToken(token).
		SetPages([]string{"https://img.example/p0.jpg"}).
		Save(t.Context())
	if err != nil {
		t.Fatalf("seed empty cache row: %v", err)
	}
	if row, found, err := gallerycache.Get(t.Context(), client, galleryID, token); err != nil || !found || !gallerycache.IsEmptyRow(row) {
		t.Fatalf("seeded row = %+v, found %v, err %v", row, found, err)
	}
}

// TestHandleRecentlyRead_FillsMissingMetadata covers the history card with a
// row that holds only pages: the list fetches the gallery's metadata instead
// of answering a card with no title, shows it in that same response, and
// persists it so the next request needs no upstream call.
func TestHandleRecentlyRead_FillsMissingMetadata(t *testing.T) {
	client := newTestDB(t)
	const (
		id    int64 = 4269137
		token       = "55fb2864ab12"
	)
	seedEmptyCacheRow(t, client, id, token)
	if _, err := client.ReadingProgress.Create().
		SetGalleryID(id).
		SetToken(token).
		SetCurrentPage(31).
		SetProgress(0.2).
		Save(t.Context()); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	upstream, requested := metadataAPIServer(t, map[string]model.GalleryMetadata{
		"4269137:" + token: {
			GID:       int(id),
			Token:     token,
			Title:     "A gallery read before its entry was cached",
			Category:  "Doujinshi",
			Thumb:     "https://img.example/thumb.jpg",
			FileCount: "148",
		},
	})

	server := &Server{DB: &database.DB{Client: client}, Client: upstream}
	r := setupTestRouter()
	r.GET("/api/recently-read", server.handleRecentlyRead)

	do := func() model.RecentlyReadResponse {
		req := httptest.NewRequest("GET", "/api/recently-read", nil)
		w := httptest.NewRecorder()
		r.ServeHTTP(w, req)
		if w.Code != http.StatusOK {
			t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
		}
		var resp model.RecentlyReadResponse
		if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
			t.Fatalf("unmarshal: %v", err)
		}
		if len(resp.Results) != 1 {
			t.Fatalf("results = %d, want 1", len(resp.Results))
		}
		return resp
	}

	first := do().Results[0]
	if first.Title != "A gallery read before its entry was cached" {
		t.Errorf("title = %q, want the fetched one", first.Title)
	}
	if first.Thumbnail != "https://img.example/thumb.jpg" {
		t.Errorf("thumbnail = %q", first.Thumbnail)
	}
	if first.Pages != 148 {
		t.Errorf("pages = %d, want 148", first.Pages)
	}
	if first.Reading.CurrentPage != 31 {
		t.Errorf("reading page = %d, want 31", first.Reading.CurrentPage)
	}
	if _, ok := requested.Load("4269137:" + token); !ok {
		t.Fatal("the empty row did not trigger an upstream fetch")
	}
	row, found, err := gallerycache.Get(t.Context(), client, id, token)
	if err != nil || !found || row.Title == "" {
		t.Fatalf("filled metadata was not persisted: row %+v, found %v, err %v", row, found, err)
	}

	// The second request has the row: it answers from the cache alone.
	requested.Delete("4269137:" + token)
	if got := do().Results[0].Title; got != "A gallery read before its entry was cached" {
		t.Errorf("second response title = %q", got)
	}
	if _, ok := requested.Load("4269137:" + token); ok {
		t.Error("a row with metadata was fetched upstream again")
	}
}

// TestHandleBookshelf_FillsMissingMetadata is the same for a shelf entry whose
// row holds nothing at all.
func TestHandleBookshelf_FillsMissingMetadata(t *testing.T) {
	client := newTestDB(t)
	const (
		id    int64 = 4267707
		token       = "0c38186dbe33"
	)
	if _, err := client.Bookshelf.Create().
		SetGalleryID(id).
		SetToken(token).
		Save(t.Context()); err != nil {
		t.Fatalf("seed bookshelf: %v", err)
	}
	seedEmptyCacheRow(t, client, id, token)
	upstream, _ := metadataAPIServer(t, map[string]model.GalleryMetadata{
		"4267707:" + token: {
			GID:       int(id),
			Token:     token,
			Title:     "Shelf entry",
			Category:  "Manga",
			FileCount: "31",
		},
	})

	server := &Server{DB: &database.DB{Client: client}, Client: upstream}
	r := setupTestRouter()
	r.GET("/api/bookshelf", server.handleBookshelfList)

	req := httptest.NewRequest("GET", "/api/bookshelf", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var resp model.BookshelfListResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("results = %d, want 1", len(resp.Results))
	}
	if got := resp.Results[0].Title; got != "Shelf entry" {
		t.Errorf("title = %q, want the fetched one", got)
	}
	if got := resp.Results[0].Pages; got != 31 {
		t.Errorf("pages = %d, want 31", got)
	}
}

// TestHandleRecentlyRead_DropsRowUpstreamForgot covers the other end: the
// gallery is gone upstream (expunged or deleted), so the fetch returns no
// metadata. The list still answers with the reading record, and the empty row
// is dropped so later requests neither ask again nor keep a blank card.
func TestHandleRecentlyRead_DropsRowUpstreamForgot(t *testing.T) {
	client := newTestDB(t)
	const (
		id    int64 = 4269137
		token       = "55fb2864ab12"
	)
	seedEmptyCacheRow(t, client, id, token)
	if _, err := client.ReadingProgress.Create().
		SetGalleryID(id).
		SetToken(token).
		SetCurrentPage(3).
		Save(t.Context()); err != nil {
		t.Fatalf("seed progress: %v", err)
	}
	upstream, _ := metadataAPIServer(t, nil)

	server := &Server{DB: &database.DB{Client: client}, Client: upstream}
	r := setupTestRouter()
	r.GET("/api/recently-read", server.handleRecentlyRead)

	req := httptest.NewRequest("GET", "/api/recently-read", nil)
	w := httptest.NewRecorder()
	r.ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("status = %d, body %s", w.Code, w.Body.String())
	}
	var resp model.RecentlyReadResponse
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].ID != id {
		t.Fatalf("results = %+v, want the reading record itself", resp.Results)
	}
	if n, err := client.ReadingProgress.Query().Count(t.Context()); err != nil || n != 1 {
		t.Fatalf("reading progress = %d, err %v, want it kept", n, err)
	}
	if row, found, err := gallerycache.Get(t.Context(), client, id, token); err != nil || found {
		t.Fatalf("empty row survived a fetch that found nothing: row %+v, found %v, err %v", row, found, err)
	}
}
