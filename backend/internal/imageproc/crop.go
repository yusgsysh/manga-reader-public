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

// CropWEBP decodes a WebP image, crops it to rect and re-encodes the result as
// a WebP image. rect is expressed in the sprite's pixel coordinates (as parsed
// from the site's CSS background-position). rect must be fully contained in the
// sprite bounds.
func CropWEBP(data []byte, rect image.Rectangle) ([]byte, error) {
	src, err := webp.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode sprite: %w", err)
	}

	if rect.Empty() || !rect.In(src.Bounds()) {
		return nil, fmt.Errorf("%w: %v not in %v", ErrOutOfBounds, rect, src.Bounds())
	}

	dst := image.NewRGBA(image.Rect(0, 0, rect.Dx(), rect.Dy()))
	draw.Draw(dst, dst.Bounds(), src, rect.Min, draw.Src)

	var out bytes.Buffer
	if err := webp.Encode(&out, dst, webp.Options{Quality: DefaultCropQuality}); err != nil {
		return nil, fmt.Errorf("encode thumbnail: %w", err)
	}
	return out.Bytes(), nil
}
