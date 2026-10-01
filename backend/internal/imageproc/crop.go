// Package imageproc provides small image processing helpers used by the
// ExHentai image proxies (e.g. cropping a page thumbnail out of a sprite).
package imageproc

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/draw"

	"github.com/gen2brain/webp"
)

// DefaultCropQuality is the WebP quality used for cropped page thumbnails.
const DefaultCropQuality = 80

// ErrOutOfBounds is returned when the requested crop rectangle is empty or not
// fully contained in the source image.
var ErrOutOfBounds = errors.New("crop rectangle out of bounds")

// alphaThreshold is the minimum alpha considered part of the visible thumbnail.
// ExHentai letterboxes page thumbnails inside their sprite cell with fully
// transparent padding; pixels below this value are treated as padding.
const alphaThreshold = 8

// CropWEBP decodes a WebP image, crops it to rect and re-encodes the result as
// a WebP image. rect is expressed in the sprite's pixel coordinates (as parsed
// from the site's CSS background-position). rect must be fully contained in the
// sprite bounds.
//
// Fully transparent padding around the cell content is trimmed so the returned
// thumbnail tightly bounds the page image. This keeps consumers from rendering
// the transparent letterbox as a solid (white) border.
func CropWEBP(data []byte, rect image.Rectangle) ([]byte, error) {
	src, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode sprite: %w", err)
	}

	if rect.Empty() || !rect.In(src.Bounds()) {
		return nil, fmt.Errorf("%w: %v not in %v", ErrOutOfBounds, rect, src.Bounds())
	}

	cell := toNRGBA(src, rect)
	if bounds := opaqueBounds(cell); !bounds.Empty() && bounds != cell.Bounds() {
		cell = cropNRGBA(cell, bounds)
	}

	var out bytes.Buffer
	if err := webp.Encode(&out, cell, webp.Options{Quality: DefaultCropQuality}); err != nil {
		return nil, fmt.Errorf("encode thumbnail: %w", err)
	}
	return out.Bytes(), nil
}

// toNRGBA copies the given region of src into a new opaquely-backed NRGBA image.
func toNRGBA(src image.Image, rect image.Rectangle) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), src, rect.Min, draw.Src)
	return dst
}

// opaqueBounds returns the smallest rectangle containing pixels whose alpha is
// at or above alphaThreshold. It returns an empty rectangle when the image is
// fully transparent.
func opaqueBounds(img *image.NRGBA) image.Rectangle {
	b := img.Bounds()
	minX, minY, maxX, maxY := b.Max.X, b.Max.Y, b.Min.X, b.Min.Y

	for y := b.Min.Y; y < b.Max.Y; y++ {
		row := img.Pix[y*img.Stride : y*img.Stride+b.Dx()*4]
		for x := 0; x < b.Dx(); x++ {
			if row[x*4+3] >= alphaThreshold {
				px := b.Min.X + x
				if px < minX {
					minX = px
				}
				if px >= maxX {
					maxX = px + 1
				}
				if y < minY {
					minY = y
				}
				if y+1 > maxY {
					maxY = y + 1
				}
			}
		}
	}

	if minX >= maxX || minY >= maxY {
		return image.Rectangle{}
	}
	return image.Rect(minX, minY, maxX, maxY)
}

// cropNRGBA returns a copy of img restricted to rect.
func cropNRGBA(img *image.NRGBA, rect image.Rectangle) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), img, rect.Min, draw.Src)
	return dst
}
