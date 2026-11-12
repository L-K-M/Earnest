package media_test

import (
	"bytes"
	"image"
	"image/color"
	"testing"

	"github.com/L-K-M/Earnest/internal/media"
)

func TestFeaturePreservesCompositionAndDoesNotUpscale(t *testing.T) {
	for _, size := range []image.Point{{2400, 1600}, {600, 900}, {100, 80}} {
		src := image.NewNRGBA(image.Rect(0, 0, size.X, size.Y))
		src.SetNRGBA(0, 0, color.NRGBA{255, 0, 0, 255})
		got, err := media.Feature(src)
		if err != nil {
			t.Fatal(err)
		}
		long := min(1200, max(size.X, size.Y))
		wantW, wantH := size.X*long/max(size.X, size.Y), size.Y*long/max(size.X, size.Y)
		if got.Width != wantW || got.Height != wantH {
			t.Fatalf("dimensions %+v", got)
		}
		decoded, _, err := image.Decode(bytes.NewReader(got.Body))
		if err != nil {
			t.Fatal(err)
		}
		if decoded.Bounds().Dx() != wantW || decoded.Bounds().Dy() != wantH {
			t.Fatal("encoded dimensions")
		}
	}
}
