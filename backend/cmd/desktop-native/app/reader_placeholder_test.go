package app

import (
	"testing"
	"time"

	"github.com/egoist/mygo/ui"
)

// TestPageCacheKeepsPlaceholdersTheFrameDraws covers a spread's placeholders:
// a wide window draws three groups of two pages, six in all, while the cache
// holds four. Eviction used to keep only the four most recently inserted, so
// two of the six were dropped, rebuilt in the background and dropped again on
// the next frame: the pages blinked between their gradient stand-in and the
// plain loading box for as long as their pictures were downloading.
func TestPageCacheKeepsPlaceholdersTheFrameDraws(t *testing.T) {
	p := NewPageCache(4)
	pages := []int{2, 3, 4, 5, 6, 7}

	// A frame asks for the pages it draws and reports the gradients it has.
	frame := func() map[int]*ui.Bitmap {
		p.BeginFrame()
		got := make(map[int]*ui.Bitmap, len(pages))
		for _, page := range pages {
			if b := p.Get(1, page); b != nil {
				got[page] = b
			}
		}
		return got
	}

	// The gradients are painted in the background: give them the frames a
	// reader would.
	deadline := time.Now().Add(5 * time.Second)
	var first map[int]*ui.Bitmap
	for time.Now().Before(deadline) {
		first = frame()
		if len(first) == len(pages) {
			break
		}
		time.Sleep(time.Millisecond)
	}
	if len(first) != len(pages) {
		t.Fatalf("the frame kept %d of %d placeholders: eviction dropped the pages on screen",
			len(first), len(pages))
	}

	// The pages the frame draws keep the gradients already shown, so none is
	// rebuilt and the stand-in never blinks.
	for n := 0; n < 3; n++ {
		next := frame()
		for _, page := range pages {
			if next[page] != first[page] {
				t.Fatalf("frame %d: page %d lost its gradient (present=%v), so the page would blink",
					n, page+1, next[page] != nil)
			}
		}
	}
}

// TestReaderSpreadKeepsItsPlaceholders is the same blink seen through the
// reader: the stage lays out six pages in a spread, and every frame must find
// the same gradients while the pictures are still being fetched.
func TestReaderSpreadKeepsItsPlaceholders(t *testing.T) {
	gate := newResponseGate()
	defer gate.Close()
	srv := blockingImageServer(t, gate)
	client := NewClient(srv)
	client.UseImageCache(true)
	a := New(client)
	a.readerSettings.double = true
	a.readerSettings.hasCover = false
	tt := ui.NewTester(a.View, 1280, 860)
	waitText(t, tt, "长画廊")
	a.router.Push("/reader/1/tok1")
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) && !a.reader.loaded {
		tt.Frame()
		time.Sleep(2 * time.Millisecond)
	}
	if !a.reader.loaded {
		t.Fatal("the reader did not load")
	}
	// Read from a pair with a group on either side: [2 3] [4 5] [6 7].
	a.update(func() { a.goToPage(&a.reader, 4) })
	drawn := []int{2, 3, 4, 5, 6, 7}
	placeholders := func() map[int]*ui.Bitmap {
		a.pageCache.mu.Lock()
		defer a.pageCache.mu.Unlock()
		got := make(map[int]*ui.Bitmap, len(drawn))
		for _, page := range drawn {
			if b := a.pageCache.entries[[2]int{int(a.reader.id), page}]; b != nil {
				got[page] = b
			}
		}
		return got
	}

	var first map[int]*ui.Bitmap
	waitFor(t, tt, "the spread's placeholders", func() bool {
		first = placeholders()
		return len(first) == len(drawn)
	})
	for n := 0; n < 3; n++ {
		tt.Frame()
		next := placeholders()
		for _, page := range drawn {
			if next[page] != first[page] {
				t.Fatalf("frame %d: page %d lost its placeholder (present=%v), so the spread would blink",
					n, page+1, next[page] != nil)
			}
		}
	}
}
