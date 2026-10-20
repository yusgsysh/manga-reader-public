package assets

import (
	"bytes"
	"image/png"
	"testing"
)

func TestIconIsAValidSquarePNG(t *testing.T) {
	img, err := png.Decode(bytes.NewReader(PNG))
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	b := img.Bounds()
	if b.Dx() != b.Dy() {
		t.Fatalf("icon is %dx%d, want square", b.Dx(), b.Dy())
	}
	if b.Dx() < 128 {
		t.Fatalf("icon is only %dpx, too small for a desktop icon", b.Dx())
	}
}
