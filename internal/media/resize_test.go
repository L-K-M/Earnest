package media

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func solidImage(w, h int, c color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.SetNRGBA(x, y, c)
		}
	}
	return img
}

func halfSplitImage(w, h int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			if x < w/2 {
				img.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{0, 0, 255, 255})
			}
		}
	}
	return img
}

func TestThumbnail(t *testing.T) {
	t.Run("400x300 opaque is 192x192 jpeg", func(t *testing.T) {
		enc, err := Thumbnail(halfSplitImage(400, 300))
		if err != nil {
			t.Fatalf("Thumbnail: %v", err)
		}
		if enc.Width != 192 || enc.Height != 192 {
			t.Fatalf("size = %dx%d, want 192x192", enc.Width, enc.Height)
		}
		if enc.ContentType != "image/jpeg" {
			t.Fatalf("content type = %q, want image/jpeg", enc.ContentType)
		}
		img, err := jpeg.Decode(bytes.NewReader(enc.Body))
		if err != nil {
			t.Fatalf("decode jpeg: %v", err)
		}
		if b := img.Bounds(); b.Dx() != 192 || b.Dy() != 192 {
			t.Fatalf("decoded bounds = %dx%d, want 192x192", b.Dx(), b.Dy())
		}
		// The centred crop is x 50..350; the source left half is red, so the
		// left half of the thumbnail must be red.
		// JPEG rounds flat colours slightly (255 becomes 254).
		if r, _, _, _ := img.At(0, 96).RGBA(); r>>8 < 250 {
			t.Errorf("left edge red channel = %d, want >= 250", r>>8)
		}
		if _, _, b, _ := img.At(191, 96).RGBA(); b>>8 < 250 {
			t.Errorf("right edge blue channel = %d, want >= 250", b>>8)
		}
	})

	t.Run("100x80 is 80x80", func(t *testing.T) {
		enc, err := Thumbnail(halfSplitImage(100, 80))
		if err != nil {
			t.Fatalf("Thumbnail: %v", err)
		}
		if enc.Width != 80 || enc.Height != 80 {
			t.Fatalf("size = %dx%d, want 80x80", enc.Width, enc.Height)
		}
		if enc.ContentType != "image/jpeg" {
			t.Fatalf("content type = %q, want image/jpeg", enc.ContentType)
		}
		img, err := jpeg.Decode(bytes.NewReader(enc.Body))
		if err != nil {
			t.Fatalf("decode jpeg: %v", err)
		}
		// The centred crop is x 10..90, so its left 40 pixels are red.
		if r, _, _, _ := img.At(0, 40).RGBA(); r>>8 < 250 {
			t.Errorf("left edge red channel = %d, want >= 250", r>>8)
		}
		if _, _, b, _ := img.At(79, 40).RGBA(); b>>8 < 250 {
			t.Errorf("right edge blue channel = %d, want >= 250", b>>8)
		}
	})

	t.Run("too small", func(t *testing.T) {
		if _, err := Thumbnail(halfSplitImage(63, 200)); err != ErrUnsuitable {
			t.Fatalf("err = %v, want ErrUnsuitable", err)
		}
	})

	t.Run("too elongated", func(t *testing.T) {
		if _, err := Thumbnail(halfSplitImage(400, 100)); err != ErrUnsuitable {
			t.Fatalf("err = %v, want ErrUnsuitable", err)
		}
	})

	t.Run("aspect boundary is suitable", func(t *testing.T) {
		enc, err := Thumbnail(halfSplitImage(64, 192))
		if err != nil {
			t.Fatalf("Thumbnail: %v", err)
		}
		if enc.Width != 64 || enc.Height != 64 {
			t.Fatalf("size = %dx%d, want 64x64", enc.Width, enc.Height)
		}
	})

	t.Run("transparency becomes png", func(t *testing.T) {
		img := image.NewNRGBA(image.Rect(0, 0, 100, 100))
		for y := 0; y < 100; y++ {
			for x := 0; x < 100; x++ {
				if x < 50 {
					img.SetNRGBA(x, y, color.NRGBA{0, 0, 0, 0})
				} else {
					img.SetNRGBA(x, y, color.NRGBA{255, 0, 0, 255})
				}
			}
		}
		enc, err := Thumbnail(img)
		if err != nil {
			t.Fatalf("Thumbnail: %v", err)
		}
		if enc.ContentType != "image/png" {
			t.Fatalf("content type = %q, want image/png", enc.ContentType)
		}
		if enc.Width != 100 || enc.Height != 100 {
			t.Fatalf("size = %dx%d, want 100x100", enc.Width, enc.Height)
		}
		out, err := png.Decode(bytes.NewReader(enc.Body))
		if err != nil {
			t.Fatalf("decode png: %v", err)
		}
		if got := nrgbaAt(out, 0, 0); got.A != 0 {
			t.Errorf("pixel (0,0) alpha = %d, want 0", got.A)
		}
		if got := nrgbaAt(out, 99, 99); got.A != 255 {
			t.Errorf("pixel (99,99) alpha = %d, want 255", got.A)
		}
	})

	t.Run("non-zero bounds min", func(t *testing.T) {
		base := halfSplitImage(200, 200)
		sub := base.SubImage(image.Rect(50, 40, 150, 140))
		enc, err := Thumbnail(sub)
		if err != nil {
			t.Fatalf("Thumbnail: %v", err)
		}
		if enc.Width != 100 || enc.Height != 100 {
			t.Fatalf("size = %dx%d, want 100x100", enc.Width, enc.Height)
		}
		img, err := jpeg.Decode(bytes.NewReader(enc.Body))
		if err != nil {
			t.Fatalf("decode jpeg: %v", err)
		}
		// Sub-image (0,0) is base (50,40): red. Base x=100 is the split, so
		// thumbnail x=50 is the boundary; sample away from it.
		if r, _, _, _ := img.At(0, 0).RGBA(); r>>8 < 250 {
			t.Errorf("pixel (0,0) red channel = %d, want >= 250", r>>8)
		}
		if _, _, b, _ := img.At(99, 99).RGBA(); b>>8 < 250 {
			t.Errorf("pixel (99,99) blue channel = %d, want >= 250", b>>8)
		}
		if r, _, _, _ := img.At(25, 50).RGBA(); r>>8 < 250 {
			t.Errorf("pixel (25,50) red channel = %d, want >= 250", r>>8)
		}
		if _, _, b, _ := img.At(75, 50).RGBA(); b>>8 < 250 {
			t.Errorf("pixel (75,50) blue channel = %d, want >= 250", b>>8)
		}
	})
}

func TestIcon(t *testing.T) {
	t.Run("16x16 is not upscaled", func(t *testing.T) {
		enc, err := Icon(solidImage(16, 16, color.NRGBA{255, 0, 0, 255}))
		if err != nil {
			t.Fatalf("Icon: %v", err)
		}
		if enc.Width != 16 || enc.Height != 16 {
			t.Fatalf("size = %dx%d, want 16x16", enc.Width, enc.Height)
		}
		if enc.ContentType != "image/png" {
			t.Fatalf("content type = %q, want image/png", enc.ContentType)
		}
		img, err := png.Decode(bytes.NewReader(enc.Body))
		if err != nil {
			t.Fatalf("decode png: %v", err)
		}
		if got := nrgbaAt(img, 0, 0); got != (color.NRGBA{255, 0, 0, 255}) {
			t.Fatalf("pixel = %+v, want opaque red", got)
		}
	})

	t.Run("300x150 centred on 64x64", func(t *testing.T) {
		enc, err := Icon(solidImage(300, 150, color.NRGBA{0, 128, 255, 255}))
		if err != nil {
			t.Fatalf("Icon: %v", err)
		}
		if enc.Width != 64 || enc.Height != 64 {
			t.Fatalf("size = %dx%d, want 64x64", enc.Width, enc.Height)
		}
		if enc.ContentType != "image/png" {
			t.Fatalf("content type = %q, want image/png", enc.ContentType)
		}
		img, err := png.Decode(bytes.NewReader(enc.Body))
		if err != nil {
			t.Fatalf("decode png: %v", err)
		}
		if got := nrgbaAt(img, 0, 0); got.A != 0 {
			t.Errorf("pixel (0,0) alpha = %d, want 0", got.A)
		}
		if got := nrgbaAt(img, 32, 15); got.A != 0 {
			t.Errorf("pixel (32,15) alpha = %d, want 0", got.A)
		}
		if got := nrgbaAt(img, 32, 16); got != (color.NRGBA{0, 128, 255, 255}) {
			t.Errorf("pixel (32,16) = %+v, want content colour", got)
		}
		if got := nrgbaAt(img, 32, 47); got != (color.NRGBA{0, 128, 255, 255}) {
			t.Errorf("pixel (32,47) = %+v, want content colour", got)
		}
		if got := nrgbaAt(img, 32, 48); got.A != 0 {
			t.Errorf("pixel (32,48) alpha = %d, want 0", got.A)
		}
	})

	t.Run("zero size unsupported", func(t *testing.T) {
		if _, err := Icon(image.NewNRGBA(image.Rect(0, 0, 0, 10))); err != ErrUnsupported {
			t.Fatalf("err = %v, want ErrUnsupported", err)
		}
		if _, err := Icon(image.NewNRGBA(image.Rect(0, 0, 10, 0))); err != ErrUnsupported {
			t.Fatalf("err = %v, want ErrUnsupported", err)
		}
	})
}
