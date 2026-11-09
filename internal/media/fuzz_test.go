package media

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"testing"
)

// FuzzDecode feeds Decode arbitrary bytes, including mutations of every
// supported format, and requires that it neither panics nor hangs. Valid
// seeds exercise each decoder; malformed seeds exercise the error paths.
func FuzzDecode(f *testing.F) {
	img := image.NewNRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.SetNRGBA(x, y, color.NRGBA{uint8(x * 16), uint8(y * 16), 128, uint8(x * 16)})
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		f.Fatalf("encode png: %v", err)
	}
	f.Add(buf.Bytes())

	buf.Reset()
	if err := jpeg.Encode(&buf, img, &jpeg.Options{Quality: 90}); err != nil {
		f.Fatalf("encode jpeg: %v", err)
	}
	f.Add(buf.Bytes())

	paletted := image.NewPaletted(image.Rect(0, 0, 16, 16), color.Palette{color.Black, color.White})
	buf.Reset()
	if err := gif.Encode(&buf, paletted, nil); err != nil {
		f.Fatalf("encode gif: %v", err)
	}
	f.Add(buf.Bytes())

	for _, name := range []string{"lossy-opaque.webp", "lossless-alpha.webp"} {
		data, err := os.ReadFile("testdata/" + name)
		if err != nil {
			f.Fatalf("read %s: %v", name, err)
		}
		f.Add(data)
	}

	f.Add(pngEntryFixture(8, 8))
	f.Add(icoOne(8, 8, 32, dib32Payload(8, 8,
		func(x, y int) uint8 { return 255 },
		func(x, y int) color.NRGBA { return color.NRGBA{255, 0, 0, 255} },
		nil, false)))
	rows := make([][]int, 8)
	for i := range rows {
		rows[i] = make([]int, 8)
	}
	f.Add(icoOne(8, 8, 8, dibPalettedPayload(8, 8, 8,
		[]color.NRGBA{{255, 0, 0, 255}, {0, 255, 0, 255}}, 2, rows, nil, false)))
	f.Add([]byte{0, 0, 1, 0, 2, 0, 1, 2, 3})
	f.Add(bytes.Repeat([]byte{0xff}, 64))

	f.Fuzz(func(t *testing.T, data []byte) {
		_, _ = Decode(data)
	})
}
