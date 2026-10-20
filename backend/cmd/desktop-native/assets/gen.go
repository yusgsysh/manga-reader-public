//go:build ignore

// gen.go draws the icon and writes icon.png.

package main

// gen.go draws the icon and writes icon.png. Run: go generate ./assets
//
// The mark is an open book: two rounded page blocks leaning away from a
// center spine, on a rounded-square tile in the app's accent blue. Drawn
// with Go's image packages, so no design tool is needed to change it.

import (
	"image"
	"image/color"
	"image/draw"
	"image/png"
	"math"
	"os"
)

const size = 512

func main() {
	img := image.NewRGBA(image.Rect(0, 0, size, size))

	// Rounded-square tile, in a deep blue that reads on both light and dark
	// docks and taskbars.
	tile := color.RGBA{0x22, 0x5a, 0xd5, 0xff}
	drawRoundedRect(img, img.Bounds(), 96, tile)

	// The book: two page blocks and a lighter spine.
	pages := color.RGBA{0xf5, 0xf7, 0xfa, 0xff}
	shade := color.RGBA{0xd9, 0xdf, 0xe8, 0xff}
	cx := size / 2

	// Left page: a block leaning toward the spine, drawn as a sheared
	// rounded rectangle.
	drawPage(img, cx-14, 150, 150, 220, -0.18, pages)
	// Right page mirrors it.
	drawPage(img, cx+14, 150, 150, 220, 0.18, pages)
	// The spine between them, slightly darker so the fold reads.
	drawRoundedRect(img, image.Rect(cx-10, 146, cx+10, 386), 10, shade)

	out, err := os.Create("icon.png")
	if err != nil {
		panic(err)
	}
	defer out.Close()
	if err := png.Encode(out, img); err != nil {
		panic(err)
	}
}

// drawRoundedRect fills r with a rounded rectangle of the given corner
// radius (a superseded distance field; simple and exact enough at 512px).
func drawRoundedRect(img draw.Image, r image.Rectangle, radius int, col color.RGBA) {
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			if insideRounded(x, y, r, radius) {
				img.Set(x, y, col)
			}
		}
	}
}

func insideRounded(x, y int, r image.Rectangle, radius int) bool {
	dx, dy := 0, 0
	if x < r.Min.X+radius {
		dx = r.Min.X + radius - x
	} else if x >= r.Max.X-radius {
		dx = x - (r.Max.X - radius) + 1
	}
	if y < r.Min.Y+radius {
		dy = r.Min.Y + radius - y
	} else if y >= r.Max.Y-radius {
		dy = y - (r.Max.Y - radius) + 1
	}
	if dx <= 0 || dy <= 0 {
		return true
	}
	return dx*dx+dy*dy <= radius*radius
}

// drawPage fills a rounded rect rotated around its top-left area by slope
// (radians): each row is offset horizontally by slope of its distance from
// the middle, giving the leaning-page look.
func drawPage(img draw.Image, x, y, w, h int, slope float64, col color.RGBA) {
	for row := 0; row < h; row++ {
		t := float64(row)/float64(h) - 0.5
		off := int(math.Round(slope * t * float64(h)))
		r := image.Rect(x+off, y+row, x+off+w, y+row+1)
		drawRoundedRect(img, r, 4, col)
	}
}
