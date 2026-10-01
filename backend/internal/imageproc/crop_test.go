package imageproc

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/gen2brain/webp"
)

// makeSprite builds a 3-cell sprite (each 4x4) where every cell has a distinct
// solid colour, encoded as WebP.
func makeSprite(t *testing.T) []byte {
	t.Helper()

	src := image.NewRGBA(image.Rect(0, 0, 12, 4))
	colors := []color.RGBA{
		{R: 255, A: 255},
		{G: 255, A: 255},
		{B: 255, A: 255},
	}
	for cell, c := range colors {
		for y := range 4 {
			for x := range 4 {
				src.SetRGBA(cell*4+x, y, c)
			}
		}
	}

	var buf bytes.Buffer
	if err := webp.Encode(&buf, src, webp.Options{Lossless: true}); err != nil {
		t.Fatalf("encode sprite: %v", err)
	}
	return buf.Bytes()
}

func TestCropWEBP(t *testing.T) {
	sprite := makeSprite(t)

	out, err := CropWEBP(sprite, image.Rect(4, 0, 8, 4))
	if err != nil {
		t.Fatalf("CropWEBP: %v", err)
	}

	img, err := webp.Decode(bytes.NewReader(out))
	if err != nil {
		t.Fatalf("decode cropped: %v", err)
	}
	if got := img.Bounds().Size(); got.X != 4 || got.Y != 4 {
		t.Fatalf("cropped size = %v, want 4x4", got)
	}

	r, g, b, _ := img.At(1, 1).RGBA()
	// Lossy WebP uses chroma subsampling, so allow some colour bleed from the
	// neighbouring cells; the green cell must still dominate.
	if g < 0x8000 || g < 4*r || g < 4*b {
		t.Errorf("cropped pixel = (%d,%d,%d), want green cell", r, g, b)
	}
}

func TestCropWEBP_OutOfBounds(t *testing.T) {
	sprite := makeSprite(t)

	if _, err := CropWEBP(sprite, image.Rect(8, 0, 20, 4)); err == nil {
		t.Error("expected out-of-bounds error")
	}
	if _, err := CropWEBP(sprite, image.Rect(0, 0, 0, 0)); err == nil {
		t.Error("expected empty-rect error")
	}
}

func TestCropWEBP_InvalidData(t *testing.T) {
	if _, err := CropWEBP([]byte("not a webp"), image.Rect(0, 0, 1, 1)); err == nil {
		t.Error("expected decode error for invalid data")
	}
}
