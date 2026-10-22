package app

import (
	"image"
	"image/color"
	"math"
	"sync"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// placeholderTone is one placeholder scheme: two gradient ends and a soft
// highlight colour. The tones are muted and harmonious, so a screen of
// stand-in covers reads as calm art rather than clashing stripes.
type placeholderTone struct {
	top, bottom, glow color.RGBA
}

// palette is the set of placeholder tones. Real covers come from the image
// store; these keep the shell and the reader usable before the backend client
// is connected.
var palette = []placeholderTone{
	{rgb(0x3b4c6b), rgb(0x1e2637), rgb(0x6f8bc4)}, // indigo dusk
	{rgb(0x5a3d5c), rgb(0x2b1e2e), rgb(0xb07ab4)}, // plum
	{rgb(0x6b3f3f), rgb(0x2e1d1d), rgb(0xc08080)}, // rosewood
	{rgb(0x2f5a4d), rgb(0x162b25), rgb(0x6fb8a0)}, // pine
	{rgb(0x5c5230), rgb(0x2a2617), rgb(0xb5a866)}, // olive gold
	{rgb(0x3a4f63), rgb(0x1a2733), rgb(0x7fb0cf)}, // slate blue
	{rgb(0x6b4a2f), rgb(0x2e2015), rgb(0xc79a66)}, // amber
	{rgb(0x2a5a60), rgb(0x143034), rgb(0x6fc2cb)}, // teal
	{rgb(0x5a2f4f), rgb(0x2a1725), rgb(0xb06fa0)}, // magenta
	{rgb(0x455a2f), rgb(0x1f2a15), rgb(0x9fd16f)}, // moss
	{rgb(0x5c3333), rgb(0x2a1717), rgb(0xc47c7c)}, // brick
	{rgb(0x33456b), rgb(0x171f33), rgb(0x7c98d4)}, // royal blue
}

func rgb(v uint32) color.RGBA {
	return color.RGBA{uint8(v >> 16), uint8(v >> 8), uint8(v), 0xff}
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

// gradientBitmap paints a deterministic placeholder: a soft diagonal gradient
// between a tone's two ends, with two broad, low-contrast glows for depth and
// a gentle vignette. It is smooth and calm — no hard bands or noise — so a
// screen of stand-in covers looks like intentional art while the real covers
// download. The angle, glow positions and tone vary with seed, so each card
// differs and page turns visibly change.
//
// The per-pixel work is a linear gradient plus two radial glows and a
// vignette: additions and multiplications only, cheap enough to keep the
// cold start well under budget even for the whole cover cache.
func gradientBitmap(seed, w, h int) *ui.Bitmap {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	tone := palette[seed%len(palette)]
	s := float64(seed)

	// Gradient runs along an angle that walks around with the seed.
	ang := s * 0.7
	ax, ay := math.Cos(ang), math.Sin(ang)

	// Two broad glows, placed deterministically in the frame.
	g1x := (0.30 + 0.25*math.Sin(s*1.3)) * float64(w)
	g1y := (0.28 + 0.25*math.Cos(s*0.9)) * float64(h)
	g2x := (0.72 + 0.20*math.Cos(s*1.7)) * float64(w)
	g2y := (0.70 + 0.20*math.Sin(s*1.1)) * float64(h)
	r1 := 0.55 * float64(w)
	r2 := 0.65 * float64(w)

	fw, fh := float64(w), float64(h)
	// span normalises the projection so the gradient covers the whole frame.
	span := math.Abs(ax)*fw + math.Abs(ay)*fh

	for y := 0; y < h; y++ {
		fy := float64(y)
		for x := 0; x < w; x++ {
			fx := float64(x)

			// Base diagonal gradient, 0..1 across the frame.
			t := ((fx-fw/2)*ax+(fy-fh/2)*ay)/span + 0.5
			t = clamp01(t)
			// Ease so the midtones stay gentle rather than a flat ramp.
			t = t * t * (3 - 2*t)

			rr := lerp(tone.top.R, tone.bottom.R, t)
			gg := lerp(tone.top.G, tone.bottom.G, t)
			bb := lerp(tone.top.B, tone.bottom.B, t)

			// Two soft glows, each falling off smoothly (1 - d²)².
			rr, gg, bb = addGlow(rr, gg, bb, fx, fy, g1x, g1y, r1, tone.glow, 0.30)
			rr, gg, bb = addGlow(rr, gg, bb, fx, fy, g2x, g2y, r2, tone.glow, 0.20)

			// Gentle vignette: darken toward the corners.
			dx := (fx/fw - 0.5) * 1.4
			dy := (fy/fh - 0.5) * 1.4
			vig := 1 - 0.22*clamp01(dx*dx+dy*dy)
			rr = uint8(float64(rr) * vig)
			gg = uint8(float64(gg) * vig)
			bb = uint8(float64(bb) * vig)

			i := img.PixOffset(x, y)
			img.Pix[i+0] = rr
			img.Pix[i+1] = gg
			img.Pix[i+2] = bb
			img.Pix[i+3] = 0xff
		}
	}
	return ui.NewBitmap(img)
}

// addGlow blends colour c toward glow where (x,y) lies within radius rad of
// (cx,cy), weighted by a smooth falloff scaled by strength.
func addGlow(cr, cg, cb uint8, x, y, cx, cy, rad float64, glow color.RGBA, strength float64) (uint8, uint8, uint8) {
	dx := (x - cx) / rad
	dy := (y - cy) / rad
	d2 := dx*dx + dy*dy
	if d2 >= 1 {
		return cr, cg, cb
	}
	f := 1 - d2
	f = f * f * strength
	cr = lerp(cr, glow.R, f)
	cg = lerp(cg, glow.G, f)
	cb = lerp(cb, glow.B, f)
	return cr, cg, cb
}

func clamp01(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 1 {
		return 1
	}
	return v
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
