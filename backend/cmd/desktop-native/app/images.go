package app

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/internal/config"
)

// ImageStore downloads, decodes and caches bitmaps in the background. The
// view never blocks on the network: it asks for a URL and gets the bitmap if
// it is ready, or nil while the first request is in flight. A completed load
// asks the window for another frame, so the bitmap appears without a refresh.
type ImageStore struct {
	mu      sync.Mutex
	entries map[string]*imageEntry
	order   []string
	max     int
	win     *mygo.Window
	http    *http.Client
	user    string
	pass    string
	token   string
}

type imageEntry struct {
	bitmap   *ui.Bitmap
	err      string
	loading  bool
	failedAt time.Time
}

func NewImageStore(max int) *ImageStore {
	if max < 1 {
		max = 1
	}
	return &ImageStore{
		entries: make(map[string]*imageEntry),
		max:     max,
		// Pages can be large and upstream is slow; the per-request context
		// of the backend bounds the download, not this client.
		http: &http.Client{Timeout: 2 * time.Minute},
	}
}

func (s *ImageStore) SetWindow(w *mygo.Window) {
	s.mu.Lock()
	s.win = w
	s.mu.Unlock()
}

// SetAuth gives the image requests the same Basic Auth the API client uses.
func (s *ImageStore) SetAuth(user, password string) {
	s.mu.Lock()
	s.user, s.pass = user, password
	s.mu.Unlock()
}

// SetAuthToken gives the image requests the embedded server's per-run token.
func (s *ImageStore) SetAuthToken(token string) {
	s.mu.Lock()
	s.token = token
	s.mu.Unlock()
}

// Get returns the bitmap at url, starting the download the first time. It
// returns nil until the bitmap is ready, and after a failed download. A failed
// download is retried when the URL is asked for again after 30 seconds.
func (s *ImageStore) Get(url string) *ui.Bitmap {
	if url == "" {
		return nil
	}

	s.mu.Lock()
	e := s.entries[url]
	switch {
	case e == nil:
		e = &imageEntry{loading: true}
		s.entries[url] = e
		s.order = append(s.order, url)
		go s.load(url)
	case e.bitmap == nil && !e.loading &&
		(e.err == "" || time.Since(e.failedAt) > 30*time.Second):
		e.loading = true
		e.err = ""
		go s.load(url)
	}
	bitmap := e.bitmap
	s.mu.Unlock()
	return bitmap
}

// Err returns the last failure of url, or "" while it is loading or after a
// successful load. The view uses it to show a retry instead of a blank box.
func (s *ImageStore) Err(url string) string {
	if url == "" {
		return ""
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entries[url]; e != nil && !e.loading {
		return e.err
	}
	return ""
}

// Retry drops a failed entry so the next Get starts the download again.
func (s *ImageStore) Retry(url string) {
	if url == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entries[url]; e != nil && !e.loading && e.err != "" {
		delete(s.entries, url)
		for i, key := range s.order {
			if key == url {
				s.order = append(s.order[:i], s.order[i+1:]...)
				break
			}
		}
	}
}

func (s *ImageStore) load(url string) {
	bitmap, err := s.fetch(url)

	s.mu.Lock()
	if e := s.entries[url]; e != nil {
		e.loading = false
		e.bitmap = bitmap
		if err != nil {
			e.err = err.Error()
			e.failedAt = time.Now()
		} else {
			e.err = ""
		}
	}
	s.evictLocked()
	win := s.win
	s.mu.Unlock()

	if win != nil {
		win.Update(func() {})
	}
}

func (s *ImageStore) fetch(url string) (*ui.Bitmap, error) {
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
	s.mu.Lock()
	user, pass, token := s.user, s.pass, s.token
	s.mu.Unlock()
	if user != "" {
		req.SetBasicAuth(user, pass)
	}
	if token != "" {
		req.Header.Set(config.AuthHeader, token)
	}
	resp, err := s.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("图片请求失败：%s", resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	return ui.DecodeBitmap(data)
}

// evictLocked drops the oldest bitmaps until the cache is within its limit.
// Entries still downloading are kept: they are the ones the latest frames
// asked for.
func (s *ImageStore) evictLocked() {
	for len(s.entries) > s.max {
		removed := false
		for i, key := range s.order {
			e := s.entries[key]
			if e != nil && e.loading {
				continue
			}
			delete(s.entries, key)
			s.order = append(s.order[:i], s.order[i+1:]...)
			removed = true
			break
		}
		if !removed {
			return
		}
	}
}
