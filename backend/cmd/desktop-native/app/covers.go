package app

import (
	"image"
	"image/color"
	"math"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// palette is the set of placeholder cover colors. Real covers come from the
// image store; these keep the shell and the reader usable before the backend
// client is connected.
var palette = []color.RGBA{
	{0x2f, 0x48, 0x5f, 0xff}, {0x5b, 0x3a, 0x6b, 0xff}, {0x74, 0x35, 0x3f, 0xff},
	{0x2d, 0x5a, 0x4a, 0xff}, {0x4a, 0x4d, 0x2a, 0xff}, {0x3d, 0x3f, 0x6b, 0xff},
	{0x6b, 0x4a, 0x2a, 0xff}, {0x2a, 0x5f, 0x66, 0xff}, {0x5f, 0x2a, 0x53, 0xff},
	{0x38, 0x53, 0x2a, 0xff}, {0x53, 0x2a, 0x2a, 0xff}, {0x2a, 0x3f, 0x6b, 0xff},
}

// CoverCache holds the placeholder covers, made once. Indexing by gallery ID
// keeps a gallery's cover stable across frames.
type CoverCache struct {
	bitmaps []*ui.Bitmap
}

func NewCoverCache() *CoverCache {
	const variants = 24
	c := &CoverCache{bitmaps: make([]*ui.Bitmap, 0, variants)}
	for i := 0; i < variants; i++ {
		c.bitmaps = append(c.bitmaps, gradientBitmap(i, 300, 400))
	}
	return c
}

func (c *CoverCache) Get(galleryID int) *ui.Bitmap {
	if len(c.bitmaps) == 0 {
		return nil
	}
	return c.bitmaps[(galleryID-1)%len(c.bitmaps)]
}

// PageCache holds decoded reader pages. It is deliberately small and evicts in
// insertion order: the reader only needs the current page and its neighbours,
// and a gallery can have thousands of pages.
type PageCache struct {
	mu      sync.Mutex
	entries map[[2]int]*ui.Bitmap
	order   [][2]int
	max     int
	// pending marks the placeholders being painted in the background.
	pending map[[2]int]bool
	win     *mygo.Window
}

func NewPageCache(max int) *PageCache {
	if max < 1 {
		max = 1
	}
	return &PageCache{entries: make(map[[2]int]*ui.Bitmap), max: max}
}

func (p *PageCache) Get(galleryID, page int) *ui.Bitmap {
	key := [2]int{galleryID, page}

	p.mu.Lock()
	if b, ok := p.entries[key]; ok {
		p.mu.Unlock()
		return b
	}
	if p.pending == nil {
		p.pending = make(map[[2]int]bool)
	}
	// A 900x1300 gradient is a million sine evaluations: build it off the
	// frame, and let this frame show its plain placeholder instead.
	if !p.pending[key] {
		p.pending[key] = true
		go p.build(key)
	}
	p.mu.Unlock()
	return nil
}

// build paints a placeholder in the background and asks for a frame.
func (p *PageCache) build(key [2]int) {
	b := gradientBitmap(key[0]*1000+key[1], 900, 1300)

	p.mu.Lock()
	delete(p.pending, key)
	if _, ok := p.entries[key]; !ok {
		p.entries[key] = b
		p.order = append(p.order, key)
		for len(p.order) > p.max {
			oldest := p.order[0]
			p.order = p.order[1:]
			delete(p.entries, oldest)
		}
	}
	win := p.win
	p.mu.Unlock()

	if win != nil {
		win.Update(func() {})
	}
}

// SetWindow lets the cache ask for a frame once a placeholder is ready.
func (p *PageCache) SetWindow(w *mygo.Window) {
	p.mu.Lock()
	p.win = w
	p.mu.Unlock()
}

// gradientBitmap paints a deterministic placeholder: a vertical gradient, a
// sine ribbon and page bands. The shape and colours vary with seed, so frames
// and page turns visibly change.
func gradientBitmap(seed, w, h int) *ui.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	base := palette[seed%len(palette)]
	accent := palette[(seed*7+5)%len(palette)]
	phase := float64(seed%17) / 17 * 2 * math.Pi

	for y := 0; y < h; y++ {
		fy := float64(y) / float64(h)
		for x := 0; x < w; x++ {
			fx := float64(x) / float64(w)
			u := 0.30*fx + 0.55*fy
			u += 0.15 * (0.5 + 0.5*math.Sin(fx*6+fy*4+phase))
			r := lerp(base.R, accent.R, u)
			g := lerp(base.G, accent.G, u)
			bl := lerp(base.B, accent.B, u)

			// Wide bands make page turns and cover variety obvious.
			band := int(fy * 9)
			if band%2 == 0 {
				r, g, bl = r*9/10, g*9/10, bl*9/10
			}

			i := img.PixOffset(x, y)
			img.Pix[i+0] = r
			img.Pix[i+1] = g
			img.Pix[i+2] = bl
			img.Pix[i+3] = 0xff
		}
	}
	return ui.NewBitmap(img)
}

func lerp(a, b uint8, t float64) uint8 {
	v := float64(a)*(1-t) + float64(b)*t
	if v < 0 {
		v = 0
	}
	if v > 255 {
		v = 255
	}
	return uint8(v)
}
