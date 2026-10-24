package app

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"manga-reader/cmd/desktop-native/app/i18n"
	"manga-reader/internal/config"
)

// downloadSlots is how many of the store's pages download at once. The rest
// wait their turn, a page the frame shows first (see acquire), so that the
// visible page is not queued behind the window's pages on the shared
// transport's per-host connection limit.
const downloadSlots = 2

// ImageStore downloads, decodes and caches bitmaps in the background. The
// view never blocks on the network: it asks for a URL and gets the bitmap if
// it is ready, or nil while the first request is in flight. A completed load
// asks the window for another frame, so the bitmap appears without a refresh.
type ImageStore struct {
	mu      sync.Mutex
	entries map[string]*imageEntry
	// head and tail are the least and most recently used entries of the
	// access list. A hit moves its entry to the tail, so the head is what
	// eviction drops: a page the reader has come back to stays.
	head, tail *imageEntry
	max        int
	// wanted are the URLs the frame being built asked for (see BeginFrame):
	// the pictures it draws or lays a placeholder over. Eviction and Release
	// leave them alone, whatever their place in the access list, and their
	// downloads go first (see acquire).
	wanted map[string]bool
	// tokens numbers the downloads (see imageEntry.token).
	tokens int64
	// waiting are the downloads queued for a slot, the ones the frame being
	// built shows first, and running is how many hold one (see acquire).
	waiting []*downloadWaiter
	running int
	win     *mygo.Window
	http    *http.Client
	user    string
	pass    string
	token   string
}

type imageEntry struct {
	url      string
	bitmap   *ui.Bitmap
	err      string
	loading  bool
	failedAt time.Time
	// token names the download the entry is waiting on. A download started
	// again while an older one is still running takes a new one, and the
	// older one's result is dropped (see load).
	token int64
	// cancel ends a download the view no longer wants (see Release).
	cancel context.CancelFunc
	prev   *imageEntry
	next   *imageEntry
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

// touchLocked moves e to the most recently used end of the access list.
func (s *ImageStore) touchLocked(e *imageEntry) {
	if s.tail == e {
		return
	}
	s.unlinkLocked(e)
	e.prev, e.next = s.tail, nil
	if s.tail != nil {
		s.tail.next = e
	} else {
		s.head = e
	}
	s.tail = e
}

// BeginFrame starts the frame the view is building: from here on, the URLs it
// asks Get for are the ones it shows. Recency alone is not enough to decide
// what to keep: the reader asks for the pages of its window once a frame, the
// whole window at that, and with a window as wide as the store's limit keeping
// it current empties the capacity, so the page on screen could be the first
// evicted, disappear for the frame it takes to fetch again, and come back: a
// blink between the gradient stand-in and the picture, over and over.
//
// A view that is not drawn (a window hidden, a frame that changes nothing)
// asks nothing, and only the access list keeps the cache within its limit.
func (s *ImageStore) BeginFrame() {
	s.mu.Lock()
	s.wanted = make(map[string]bool, 8)
	s.mu.Unlock()
}

// Showed marks url as one the frame being built shows, which Get does for
// every URL the view asks for. It is for the page a frame draws after asking
// for it in some other way: it keeps the page until the next BeginFrame.
func (s *ImageStore) Showed(url string) {
	if url == "" {
		return
	}
	s.mu.Lock()
	if s.wanted == nil {
		s.wanted = make(map[string]bool, 8)
	}
	s.wanted[url] = true
	s.mu.Unlock()
}

// unlinkLocked takes e out of the access list, as touchLocked moves it
// elsewhere. Its neighbors close the gap.
func (s *ImageStore) unlinkLocked(e *imageEntry) {
	if e.prev != nil {
		e.prev.next = e.next
	} else if s.head == e {
		s.head = e.next
	}
	if e.next != nil {
		e.next.prev = e.prev
	} else if s.tail == e {
		s.tail = e.prev
	}
	e.prev, e.next = nil, nil
}

// removeLocked drops e from both the map and the access list.
func (s *ImageStore) removeLocked(e *imageEntry) {
	s.unlinkLocked(e)
	delete(s.entries, e.url)
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

// startLocked starts e's download in the background, under a token no earlier
// download of the same page holds.
func (s *ImageStore) startLocked(e *imageEntry) {
	s.tokens++
	e.token = s.tokens
	go s.load(e.url)
}

// Get returns the bitmap at url, starting the download the first time. It
// returns nil until the bitmap is ready, and after a failed download. A failed
// download is retried when the URL is asked for again after 30 seconds.
//
// It is also what marks the URL as one the frame being built shows (see
// BeginFrame): the view asks for exactly the pictures it draws or lays a
// placeholder over, so what it asked for is what the store must keep.
func (s *ImageStore) Get(url string) *ui.Bitmap {
	if url == "" {
		return nil
	}

	s.mu.Lock()
	if s.wanted != nil {
		s.wanted[url] = true
	}
	e := s.entries[url]
	switch {
	case e == nil:
		e = &imageEntry{url: url, loading: true}
		s.entries[url] = e
		s.touchLocked(e)
		s.startLocked(e)
	case e.bitmap == nil && !e.loading &&
		(e.err == "" || time.Since(e.failedAt) > 30*time.Second):
		e.loading = true
		e.err = ""
		s.touchLocked(e)
		s.startLocked(e)
	default:
		s.touchLocked(e)
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

// Loading reports whether url's first bytes are still arriving: its download
// started and has neither finished nor failed. The view reads it to lay a
// loading indicator over a picture's placeholder until the picture appears.
func (s *ImageStore) Loading(url string) bool {
	if url == "" {
		return false
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[url]
	return e != nil && e.loading && e.bitmap == nil
}

// Release drops an entry that the view no longer wants, which ends its
// download. The reader calls it for pages that scrolled out of its preload
// window: turning through a gallery quickly queues one fetch per page
// passed, and those downloads would otherwise keep competing with the page
// now on screen. An entry that finished loading is kept, and one another
// part of the view is showing is left to Get's next use.
func (s *ImageStore) Release(url string) {
	if url == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.wanted[url] {
		// A frame is showing it: a window that moved on must not take the
		// picture off the screen (see BeginFrame).
		return
	}
	e := s.entries[url]
	if e == nil || !e.loading {
		return
	}
	// A cancelled entry is removed rather than marked failed: a later Get
	// for the page starts it fresh, without waiting out the retry delay.
	// The handle cancels without the lock: Release holds it already.
	if e.cancel != nil {
		e.cancel()
	}
	s.removeLocked(e)
}

// Retry drops a failed entry so the next Get starts the download again.
func (s *ImageStore) Retry(url string) {
	if url == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if e := s.entries[url]; e != nil && !e.loading && e.err != "" {
		s.removeLocked(e)
	}
}

func (s *ImageStore) load(url string) {
	ctx, cancel, token, err := s.begin(url)
	if err != nil {
		// Released between the Get that queued it and this goroutine: the
		// entry is gone, so its result would have nowhere to go.
		return
	}
	// The download's end is taken before its context is cancelled: cancel
	// while the picture is being handed over (see fetch) would turn a page
	// that arrived into a failed one.
	bitmap, err := s.fetch(ctx, url)

	s.mu.Lock()
	cancel()
	// Only the download the entry is waiting on writes back. A page released
	// and asked for again while this one was still running has a newer
	// download, and this is the cancelled one's context error, not an answer.
	if e := s.entries[url]; e != nil && e.loading && e.token == token {
		e.loading = false
		e.bitmap = bitmap
		e.cancel = nil
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

// begin gives the download of url its turn in the entry, or reports that the
// entry is gone. The entry's cancel ends that download and forgets it, so a
// page released and asked for again while its first download was still
// running is not ended by the first one's cancel handle.
func (s *ImageStore) begin(url string) (context.Context, context.CancelFunc, int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	e := s.entries[url]
	if e == nil || !e.loading {
		return nil, nil, 0, fmt.Errorf(i18n.T("Discarded %s"), url)
	}
	ctx, cancel := context.WithCancel(context.Background())
	token := e.token
	// The handle ends this download and forgets itself, but only if the entry
	// is still waiting on this one, and without taking the lock: callers
	// write the store under it already (Release).
	e.cancel = func() {
		cancel()
		if e.token == token {
			e.cancel = nil
		}
	}
	return ctx, cancel, token, nil
}

// downloadWaiter is a download queued for one of the store's slots. wanted
// records whether it was queued as one the frame being built shows, which is
// the order the queue keeps: those go first.
type downloadWaiter struct {
	url    string
	ready  chan struct{}
	wanted bool
}

// acquire queues url for a download slot, ahead of the downloads not shown in
// the frame being built, and waits for one. It returns the function that frees
// the slot, or ctx's error when the page was released while it waited.
//
// The store fetches through a few slots of its own rather than one goroutine
// per page: a page asked for as the reader turned must not hold the visible
// page's turn, which is what a shared transport's per-host connection limit
// does when it queues them. A frame showing a page puts it ahead of the pages
// it does not show (see Showed), so the picture on screen arrives first and
// the window's other pages follow.
func (s *ImageStore) acquire(ctx context.Context, url string) (func(), error) {
	s.mu.Lock()
	w := &downloadWaiter{url: url, ready: make(chan struct{}), wanted: s.wanted[url]}
	pos := len(s.waiting)
	if w.wanted {
		// Ahead of the downloads the frame does not show, behind the ones it
		// does.
		pos = 0
		for i, q := range s.waiting {
			if !q.wanted {
				pos = i
				break
			}
		}
	}
	s.waiting = append(s.waiting, nil)
	copy(s.waiting[pos+1:], s.waiting[pos:])
	s.waiting[pos] = w
	s.dispatchLocked()
	s.mu.Unlock()

	select {
	case <-w.ready:
		return func() { s.release() }, nil
	case <-ctx.Done():
		s.mu.Lock()
		s.dequeueLocked(w)
		s.mu.Unlock()
		return nil, ctx.Err()
	}
}

// dispatchLocked hands the free slots to the downloads at the front of the
// queue, the ones shown in the frame being built first.
func (s *ImageStore) dispatchLocked() {
	for s.running < downloadSlots && len(s.waiting) > 0 {
		w := s.waiting[0]
		s.waiting = s.waiting[1:]
		s.running++
		close(w.ready)
	}
}

// dequeueLocked drops w from the queue, if it is still there.
func (s *ImageStore) dequeueLocked(w *downloadWaiter) {
	for i, q := range s.waiting {
		if q == w {
			s.waiting = append(s.waiting[:i], s.waiting[i+1:]...)
			return
		}
	}
}

// release frees a download slot for the next queued download.
func (s *ImageStore) release() {
	s.mu.Lock()
	s.running--
	s.dispatchLocked()
	s.mu.Unlock()
}

func (s *ImageStore) fetch(ctx context.Context, url string) (*ui.Bitmap, error) {
	s.mu.Lock()
	user, pass, token := s.user, s.pass, s.token
	s.mu.Unlock()

	done, err := s.acquire(ctx, url)
	if err != nil {
		return nil, err
	}
	defer done()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, err
	}
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
		return nil, fmt.Errorf(i18n.T("Image request failed: %s"), resp.Status)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, 64<<20))
	if err != nil {
		return nil, err
	}
	bitmap, err := ui.DecodeBitmap(data)
	if ctx.Err() != nil {
		// The page was released while its picture decoded: the caller is
		// about to cancel this context (see load), so a picture it reports
		// would be dropped as if the download had never come back. Report
		// the release instead, which is what it had already decided to do.
		return nil, ctx.Err()
	}
	return bitmap, err
}

// evictLocked drops the least recently used bitmaps until the cache is
// within its limit. A download still arriving is kept while the frame being
// built asked for it (BeginFrame): it is one of the pages the view is about to
// draw, and the reader keeps its whole window current, which can fill the
// store, so the page on screen must not be the first to go. A hit moves an
// entry to the most recently used end (touchLocked), so pages the reader came
// back to stay.
//
// The download that finished is the one that calls this, and it is not one of
// those: the frames since asked it to stop, or it would have been drawn and
// not asked for again. Keeping every loading entry would do instead, but
// nothing else would bound the store: seven window pages and the visible one
// in a store of six, plus every page a request released while it was being
// fetched, which the next frame asks for and starts again. The store then
// holds every page passed since it last asked for too few, and each one's
// download competes with the page on screen.
func (s *ImageStore) evictLocked() {
	for e := s.head; e != nil && len(s.entries) > s.max; {
		next := e.next
		if !s.stickyLocked(e) {
			s.removeLocked(e)
		}
		e = next
	}
}

// stickyLocked reports whether eviction must leave e alone: the frame being
// built asked for it, whether or not its picture has arrived.
func (s *ImageStore) stickyLocked(e *imageEntry) bool {
	return s.wanted[e.url]
}
