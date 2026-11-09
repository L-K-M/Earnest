package media

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

func mustPNG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
	return buf.Bytes()
}

func mustJPEG(t *testing.T, img image.Image) []byte {
	t.Helper()
	var buf bytes.Buffer
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		t.Fatalf("encode jpeg: %v", err)
	}
	return buf.Bytes()
}

// nrgbaAt reads a pixel as non-premultiplied RGBA regardless of the concrete
// image type returned by a decoder.
func nrgbaAt(img image.Image, x, y int) color.NRGBA {
	if n, ok := img.(*image.NRGBA); ok {
		return n.NRGBAAt(x, y)
	}
	return color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
}

// pngHeaderOnly builds a PNG with a valid IHDR and no image data. It lets the
// pixel budget be exercised without allocating a pixel buffer.
func pngHeaderOnly(t *testing.T, width, height uint32) []byte {
	t.Helper()
	return pngHeaderDepth(t, width, height, 8)
}

func pngHeaderDepth(t *testing.T, width, height uint32, depth byte) []byte {
	t.Helper()
	chunk := make([]byte, 13)
	binary.BigEndian.PutUint32(chunk[0:4], width)
	binary.BigEndian.PutUint32(chunk[4:8], height)
	chunk[8] = depth // bit depth
	chunk[9] = 2     // truecolor
	chunk[10] = 0    // compression
	chunk[11] = 0    // filter
	chunk[12] = 0    // interlace

	out := append([]byte{}, pngSignature...)
	out = append(out, 0, 0, 0, 13)
	out = append(out, 'I', 'H', 'D', 'R')
	out = append(out, chunk...)
	var crc [4]byte
	binary.BigEndian.PutUint32(crc[:], crc32.ChecksumIEEE(out[len(pngSignature)+4:]))
	return append(out, crc[:]...)
}

func TestDecodeFormats(t *testing.T) {
	opaque := image.NewRGBA(image.Rect(0, 0, 12, 10))
	for y := 0; y < 10; y++ {
		for x := 0; x < 12; x++ {
			opaque.Set(x, y, color.RGBA{uint8(x * 20), uint8(y * 20), 128, 255})
		}
	}
	transparent := image.NewNRGBA(image.Rect(0, 0, 9, 7))
	for y := 0; y < 7; y++ {
		for x := 0; x < 9; x++ {
			transparent.SetNRGBA(x, y, color.NRGBA{uint8(x * 25), uint8(y * 30), 200, uint8(255 - x*10)})
		}
	}
	paletted := image.NewPaletted(image.Rect(0, 0, 8, 6), color.Palette{color.Black, color.White})
	for y := 0; y < 6; y++ {
		for x := 0; x < 8; x++ {
			paletted.SetColorIndex(x, y, uint8((x+y)%2))
		}
	}
	var gifBuf bytes.Buffer
	if err := gif.Encode(&gifBuf, paletted, nil); err != nil {
		t.Fatalf("encode gif: %v", err)
	}

	// The testdata WebP fixtures were produced from generated PNGs with:
	//   cwebp -q 80 -o testdata/lossy-opaque.webp opaque.png
	//   cwebp -lossless -o testdata/lossless-alpha.webp alpha.png
	webpLossy, err := os.ReadFile("testdata/lossy-opaque.webp")
	if err != nil {
		t.Fatalf("read lossy webp fixture: %v", err)
	}
	webpAlpha, err := os.ReadFile("testdata/lossless-alpha.webp")
	if err != nil {
		t.Fatalf("read alpha webp fixture: %v", err)
	}

	tests := []struct {
		name string
		data []byte
		w, h int
	}{
		{"jpeg", mustJPEG(t, opaque), 12, 10},
		{"png opaque", mustPNG(t, opaque), 12, 10},
		{"png alpha", mustPNG(t, transparent), 9, 7},
		{"gif", gifBuf.Bytes(), 8, 6},
		{"webp lossy", webpLossy, 64, 64},
		{"webp lossless alpha", webpAlpha, 64, 64},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			img, err := Decode(tt.data)
			if err != nil {
				t.Fatalf("Decode: %v", err)
			}
			if b := img.Bounds(); b.Dx() != tt.w || b.Dy() != tt.h {
				t.Fatalf("bounds = %dx%d, want %dx%d", b.Dx(), b.Dy(), tt.w, tt.h)
			}
		})
	}

	t.Run("png alpha pixel", func(t *testing.T) {
		img, err := Decode(mustPNG(t, transparent))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got := nrgbaAt(img, 8, 6); got != (color.NRGBA{200, 180, 200, 175}) {
			t.Fatalf("pixel = %+v, want {200 180 200 175}", got)
		}
	})

	t.Run("webp alpha pixel", func(t *testing.T) {
		img, err := Decode(webpAlpha)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got := nrgbaAt(img, 32, 0); got != (color.NRGBA{0, 255, 0, 255}) {
			t.Fatalf("top pixel = %+v, want opaque green", got)
		}
		if got := nrgbaAt(img, 32, 63); got.A != 0 {
			t.Fatalf("bottom pixel alpha = %d, want 0", got.A)
		}
	})

	t.Run("webp lossy opaque", func(t *testing.T) {
		img, err := Decode(webpLossy)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		// Lossy WebP stores limited-range YCbCr, so the exact source colour
		// is not recoverable; check the hue and that the image is opaque.
		got := nrgbaAt(img, 0, 0)
		if got.R < 200 || got.G > 80 || got.B > 80 || got.A != 255 {
			t.Fatalf("pixel = %+v, want opaque red", got)
		}
		if got := nrgbaAt(img, 63, 63); got.A != 255 {
			t.Fatalf("alpha = %d, want 255", got.A)
		}
	})
}

func TestDecodeUnsupported(t *testing.T) {
	avif := append([]byte{0x00, 0x00, 0x00, 0x1c}, []byte("ftypavif")...)
	tests := []struct {
		name string
		data []byte
	}{
		{"empty", nil},
		{"svg", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="10" height="10"></svg>`)},
		{"html", []byte(`<!doctype html><html><body>hi</body></html>`)},
		{"xml", []byte(`<?xml version="1.0"?><root/>`)},
		{"avif", avif},
		{"bmp", []byte("BM\x00\x00\x00\x00\x00\x00\x00\x00")},
		{"tiff le", []byte("II*\x00")},
		{"tiff be", []byte("MM\x00*")},
		{"text", []byte("definitely not an image")},
		// gif.DecodeConfig reports the logical screen size without
		// validating it, so the zero side must be caught by Decode.
		{"zero side gif", append([]byte("GIF89a"), 0, 0, 0, 0, 0, 0, 0)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decode(tt.data); err != ErrUnsupported {
				t.Fatalf("err = %v, want ErrUnsupported", err)
			}
		})
	}
}

func TestDecodeTooLarge(t *testing.T) {
	t.Run("encoded bytes", func(t *testing.T) {
		data := make([]byte, MaxSourceBytes+1)
		if _, err := Decode(data); err != ErrTooLarge {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})
	t.Run("declared pixels", func(t *testing.T) {
		if _, err := Decode(pngHeaderOnly(t, 8000, 8000)); err != ErrTooLarge {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})
	t.Run("decoded bytes", func(t *testing.T) {
		// 20 megapixels is under the pixel cap, but at 16 bits per channel
		// it would need 160 MB.
		if _, err := Decode(pngHeaderDepth(t, 4500, 4500, 16)); err != ErrTooLarge {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})
	t.Run("pixel limit boundary", func(t *testing.T) {
		// A 26-megapixel camera original fits in 8-bit colour; the truncated
		// body then fails for a different reason.
		_, err := Decode(pngHeaderOnly(t, 6240, 4160))
		if err == ErrTooLarge {
			t.Fatal("26M pixels reported as too large")
		}
		if err == nil {
			t.Fatal("truncated PNG accepted")
		}
	})
}

func TestDecodeCorrupt(t *testing.T) {
	opaque := image.NewRGBA(image.Rect(0, 0, 8, 8))
	for i := range opaque.Pix {
		opaque.Pix[i] = 0x80
	}
	pngData := mustPNG(t, opaque)
	jpegData := mustJPEG(t, opaque)
	webpData, err := os.ReadFile("testdata/lossy-opaque.webp")
	if err != nil {
		t.Fatalf("read webp fixture: %v", err)
	}
	icoData := icoOne(8, 8, 32, []byte{0x00, 0x00, 0x01, 0x00, 0x40})

	tests := []struct {
		name string
		data []byte
	}{
		{"png truncated", pngData[:len(pngData)-1]},
		{"png header only", pngData[:20]},
		{"jpeg truncated", jpegData[:len(jpegData)/2]},
		{"webp truncated", webpData[:len(webpData)-1]},
		{"webp header only", webpData[:12]},
		{"ico truncated payload", icoData[:len(icoData)-1]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decode(tt.data); err == nil {
				t.Fatal("Decode succeeded, want error")
			}
		})
	}
}

func TestDecodeGIFFirstFrame(t *testing.T) {
	red := color.RGBA{255, 0, 0, 255}
	blue := color.RGBA{0, 0, 255, 255}
	palette := color.Palette{red, blue}
	frame1 := image.NewPaletted(image.Rect(0, 0, 8, 8), palette)
	frame2 := image.NewPaletted(image.Rect(0, 0, 8, 8), palette)
	for i := range frame2.Pix {
		frame2.Pix[i] = 1
	}
	var buf bytes.Buffer
	if err := gif.EncodeAll(&buf, &gif.GIF{
		Image:    []*image.Paletted{frame1, frame2},
		Delay:    []int{10, 10},
		Disposal: []byte{gif.DisposalNone, gif.DisposalNone},
		Config: image.Config{
			ColorModel: palette,
			Width:      8,
			Height:     8,
		},
	}); err != nil {
		t.Fatalf("encode gif: %v", err)
	}

	img, err := Decode(buf.Bytes())
	if err != nil {
		t.Fatalf("Decode: %v", err)
	}
	if got := nrgbaAt(img, 0, 0); got != (color.NRGBA{255, 0, 0, 255}) {
		t.Fatalf("first frame pixel = %+v, want red", got)
	}
}

// Regression: lossy WebP is video range. Without the conversion white decoded
// as 235 and black as 16. The fixture is four 16-pixel columns (white, black,
// red 220,30,40 and blue 30,120,220) made with cwebp -q 95; the expected
// values are dwebp's decoding of the same file.
func TestDecodeLossyWebPUsesFullRange(t *testing.T) {
	data, err := os.ReadFile("testdata/lossy-range.webp")
	if err != nil {
		t.Fatal(err)
	}
	img, err := Decode(data)
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		x       int
		r, g, b uint8
	}{{8, 255, 255, 255}, {24, 0, 0, 0}, {40, 221, 30, 40}, {56, 30, 121, 220}} {
		c := color.NRGBAModel.Convert(img.At(tc.x, 32)).(color.NRGBA)
		if absDiff(c.R, tc.r) > 2 || absDiff(c.G, tc.g) > 2 || absDiff(c.B, tc.b) > 2 || c.A != 0xff {
			t.Errorf("x=%d got %v, want about (%d,%d,%d)", tc.x, c, tc.r, tc.g, tc.b)
		}
	}
}

func absDiff(a, b uint8) uint8 {
	if a > b {
		return a - b
	}
	return b - a
}
