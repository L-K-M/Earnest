package media

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"testing"
)

func appendUint16(b []byte, v uint16) []byte {
	var tmp [2]byte
	binary.LittleEndian.PutUint16(tmp[:], v)
	return append(b, tmp[:]...)
}

func appendUint32(b []byte, v uint32) []byte {
	var tmp [4]byte
	binary.LittleEndian.PutUint32(tmp[:], v)
	return append(b, tmp[:]...)
}

func icoHeaderBytes(count int) []byte {
	b := appendUint16(nil, 0)
	b = appendUint16(b, 1)
	return appendUint16(b, uint16(count))
}

func icoEntryBytes(width, height int, bitCount uint16, size, offset uint32) []byte {
	b := []byte{byte(width), byte(height), 0, 0}
	b = appendUint16(b, 1) // planes
	b = appendUint16(b, bitCount)
	b = appendUint32(b, size)
	return appendUint32(b, offset)
}

type testICOEntry struct {
	width, height int
	bitCount      uint16
	payload       []byte
}

func buildICO(entries []testICOEntry) []byte {
	data := icoHeaderBytes(len(entries))
	offset := uint32(6 + 16*len(entries))
	for _, e := range entries {
		data = append(data, icoEntryBytes(e.width, e.height, e.bitCount, uint32(len(e.payload)), offset)...)
		offset += uint32(len(e.payload))
	}
	for _, e := range entries {
		data = append(data, e.payload...)
	}
	return data
}

func icoOne(width, height int, bitCount uint16, payload []byte) []byte {
	return buildICO([]testICOEntry{{width: width, height: height, bitCount: bitCount, payload: payload}})
}

func dibHeaderRaw(biSize uint32, width, height int32, bitCount uint16, compression, clrUsed uint32) []byte {
	b := make([]byte, 40)
	binary.LittleEndian.PutUint32(b[0:4], biSize)
	binary.LittleEndian.PutUint32(b[4:8], uint32(width))
	binary.LittleEndian.PutUint32(b[8:12], uint32(height))
	binary.LittleEndian.PutUint16(b[12:14], 1) // planes
	binary.LittleEndian.PutUint16(b[14:16], bitCount)
	binary.LittleEndian.PutUint32(b[16:20], compression)
	binary.LittleEndian.PutUint32(b[32:36], clrUsed)
	return b
}

func dibHeaderBytes(width, height int32, bitCount uint16, compression, clrUsed uint32) []byte {
	return dibHeaderRaw(40, width, height, bitCount, compression, clrUsed)
}

// paletteBytes stores colors as RGBQUAD: blue, green, red, reserved.
func paletteBytes(colors []color.NRGBA) []byte {
	var b []byte
	for _, c := range colors {
		b = append(b, c.B, c.G, c.R, 0)
	}
	return b
}

// rowsToBottomUp reverses row order; each row is already padded to 4 bytes.
func rowsToBottomUp(rows [][]byte) []byte {
	var out []byte
	for i := len(rows) - 1; i >= 0; i-- {
		out = append(out, rows[i]...)
	}
	return out
}

func andMaskRows(width, height int, transparent func(x, y int) bool) []byte {
	rowSize := (width + 31) / 32 * 4
	rows := make([][]byte, height)
	for y := 0; y < height; y++ {
		rows[y] = make([]byte, rowSize)
		for x := 0; x < width; x++ {
			if transparent(x, y) {
				rows[y][x/8] |= 0x80 >> uint(x%8)
			}
		}
	}
	return rowsToBottomUp(rows)
}

func dib32Payload(width, height int, alpha func(x, y int) uint8, rgb func(x, y int) color.NRGBA, mask []byte, withMask bool) []byte {
	xorRows := make([][]byte, height)
	for y := 0; y < height; y++ {
		row := make([]byte, width*4)
		for x := 0; x < width; x++ {
			c := rgb(x, y)
			row[x*4], row[x*4+1], row[x*4+2], row[x*4+3] = c.B, c.G, c.R, alpha(x, y)
		}
		xorRows[y] = row
	}
	payload := dibHeaderBytes(int32(width), int32(height*2), 32, 0, 0)
	payload = append(payload, rowsToBottomUp(xorRows)...)
	if withMask {
		payload = append(payload, mask...)
	}
	return payload
}

func dib24Payload(width, height int, rgb func(x, y int) color.NRGBA, mask []byte, withMask bool) []byte {
	rowSize := (width*24 + 31) / 32 * 4
	xorRows := make([][]byte, height)
	for y := 0; y < height; y++ {
		row := make([]byte, rowSize)
		for x := 0; x < width; x++ {
			c := rgb(x, y)
			row[x*3], row[x*3+1], row[x*3+2] = c.B, c.G, c.R
		}
		xorRows[y] = row
	}
	payload := dibHeaderBytes(int32(width), int32(height*2), 24, 0, 0)
	payload = append(payload, rowsToBottomUp(xorRows)...)
	if withMask {
		payload = append(payload, mask...)
	}
	return payload
}

func packIndexes(indexes []int, bitCount int) []byte {
	rowSize := (len(indexes)*bitCount + 31) / 32 * 4
	row := make([]byte, rowSize)
	for i, idx := range indexes {
		switch bitCount {
		case 8:
			row[i] = byte(idx)
		case 4:
			if i%2 == 0 {
				row[i/2] |= byte(idx) << 4
			} else {
				row[i/2] |= byte(idx)
			}
		case 1:
			if idx != 0 {
				row[i/8] |= 0x80 >> uint(i%8)
			}
		}
	}
	return row
}

func dibPalettedPayload(width, height, bitCount int, palette []color.NRGBA, clrUsed uint32, rows [][]int, mask []byte, withMask bool) []byte {
	xorRows := make([][]byte, height)
	for y := range rows {
		xorRows[y] = packIndexes(rows[y], bitCount)
	}
	payload := dibHeaderBytes(int32(width), int32(height*2), uint16(bitCount), 0, clrUsed)
	payload = append(payload, paletteBytes(palette)...)
	payload = append(payload, rowsToBottomUp(xorRows)...)
	if withMask {
		payload = append(payload, mask...)
	}
	return payload
}

func pngEntryFixture(width, height int) []byte {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			var c color.NRGBA
			switch {
			case x < width/2 && y < height/2:
				c = color.NRGBA{255, 0, 0, 255}
			case x >= width/2 && y < height/2:
				c = color.NRGBA{0, 255, 0, 255}
			case x < width/2:
				c = color.NRGBA{0, 0, 255, 255}
			default:
				c = color.NRGBA{255, 255, 255, 255}
			}
			img.SetNRGBA(x, y, c)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		panic(err)
	}
	return buildICO([]testICOEntry{{width: width, height: height, bitCount: 32, payload: buf.Bytes()}})
}

func TestDecodeICO(t *testing.T) {
	red := color.NRGBA{255, 0, 0, 255}
	green := color.NRGBA{0, 255, 0, 255}
	blue := color.NRGBA{0, 0, 255, 255}
	white := color.NRGBA{255, 255, 255, 255}
	black := color.NRGBA{0, 0, 0, 255}
	const n = 8

	t.Run("png entry", func(t *testing.T) {
		img, err := Decode(pngEntryFixture(n, n))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if b := img.Bounds(); b.Dx() != n || b.Dy() != n {
			t.Fatalf("bounds = %dx%d, want %dx%d", b.Dx(), b.Dy(), n, n)
		}
		for _, tc := range []struct {
			x, y int
			want color.NRGBA
		}{
			{0, 0, red},
			{n - 1, 0, green},
			{0, n - 1, blue},
			{n - 1, n - 1, white},
		} {
			if got := nrgbaAt(img, tc.x, tc.y); got != tc.want {
				t.Errorf("pixel (%d,%d) = %+v, want %+v", tc.x, tc.y, got, tc.want)
			}
		}
	})

	t.Run("32bpp alpha", func(t *testing.T) {
		body := color.NRGBA{10, 20, 30, 255}
		alpha := func(x, y int) uint8 {
			switch {
			case x == 0 && y == 0:
				return 128
			case x == n-1 && y == n-1:
				return 0
			default:
				return 255
			}
		}
		// The mask marks (0,0) transparent, but 32bpp alpha wins because not
		// every alpha byte is zero.
		mask := andMaskRows(n, n, func(x, y int) bool { return x == 0 && y == 0 })
		payload := dib32Payload(n, n, alpha, func(x, y int) color.NRGBA { return body }, mask, true)
		img, err := Decode(icoOne(n, n, 32, payload))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		for _, tc := range []struct {
			x, y int
			want color.NRGBA
		}{
			{0, 0, color.NRGBA{10, 20, 30, 128}},
			{n - 1, n - 1, color.NRGBA{10, 20, 30, 0}},
			{0, n - 1, body},
			{n - 1, 0, body},
		} {
			if got := nrgbaAt(img, tc.x, tc.y); got != tc.want {
				t.Errorf("pixel (%d,%d) = %+v, want %+v", tc.x, tc.y, got, tc.want)
			}
		}
	})

	t.Run("32bpp zero alpha with mask", func(t *testing.T) {
		rgb := func(x, y int) color.NRGBA {
			switch {
			case x == 0 && y == 0:
				return green
			case x == 1 && y == 0:
				return red
			case x == 0 && y == 1:
				return blue
			default:
				return white
			}
		}
		mask := andMaskRows(n, n, func(x, y int) bool { return x == 0 && y == 0 })
		payload := dib32Payload(n, n, func(x, y int) uint8 { return 0 }, rgb, mask, true)
		img, err := Decode(icoOne(n, n, 32, payload))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got := nrgbaAt(img, 0, 0); got != (color.NRGBA{0, 255, 0, 0}) {
			t.Errorf("pixel (0,0) = %+v, want green with alpha 0", got)
		}
		if got := nrgbaAt(img, 1, 0); got != (color.NRGBA{255, 0, 0, 255}) {
			t.Errorf("pixel (1,0) = %+v, want opaque red", got)
		}
		if got := nrgbaAt(img, 0, 1); got != (color.NRGBA{0, 0, 255, 255}) {
			t.Errorf("pixel (0,1) = %+v, want opaque blue", got)
		}
	})

	t.Run("8bpp paletted with mask", func(t *testing.T) {
		palette := []color.NRGBA{red, green, blue, white}
		rows := make([][]int, n)
		for y := 0; y < n; y++ {
			rows[y] = make([]int, n)
			for x := 0; x < n; x++ {
				rows[y][x] = y % 4
			}
		}
		mask := andMaskRows(n, n, func(x, y int) bool { return x == 0 && y == 0 })
		payload := dibPalettedPayload(n, n, 8, palette, 4, rows, mask, true)
		img, err := Decode(icoOne(n, n, 8, payload))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		for _, tc := range []struct {
			x, y int
			want color.NRGBA
		}{
			{0, 0, color.NRGBA{255, 0, 0, 0}},
			{1, 0, red},
			{0, 1, green},
			{0, 2, blue},
			{0, 3, white},
		} {
			if got := nrgbaAt(img, tc.x, tc.y); got != tc.want {
				t.Errorf("pixel (%d,%d) = %+v, want %+v", tc.x, tc.y, got, tc.want)
			}
		}
	})

	t.Run("1bpp", func(t *testing.T) {
		palette := []color.NRGBA{white, black}
		rows := make([][]int, n)
		for y := 0; y < n; y++ {
			rows[y] = make([]int, n)
			for x := 0; x < n; x++ {
				rows[y][x] = (x + y) % 2
			}
		}
		payload := dibPalettedPayload(n, n, 1, palette, 0, rows, nil, false)
		img, err := Decode(icoOne(n, n, 1, payload))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got := nrgbaAt(img, 0, 0); got != white {
			t.Errorf("pixel (0,0) = %+v, want white", got)
		}
		if got := nrgbaAt(img, 1, 0); got != black {
			t.Errorf("pixel (1,0) = %+v, want black", got)
		}
		if got := nrgbaAt(img, 0, 1); got != black {
			t.Errorf("pixel (0,1) = %+v, want black", got)
		}
	})

	t.Run("4bpp", func(t *testing.T) {
		palette := make([]color.NRGBA, 16)
		for i := range palette {
			palette[i] = color.NRGBA{uint8(i * 16), uint8(255 - i*16), 0, 255}
		}
		rows := make([][]int, n)
		for y := 0; y < n; y++ {
			rows[y] = make([]int, n)
			for x := 0; x < n; x++ {
				rows[y][x] = (x + y) % 16
			}
		}
		payload := dibPalettedPayload(n, n, 4, palette, 0, rows, nil, false)
		img, err := Decode(icoOne(n, n, 4, payload))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		for _, tc := range []struct {
			x, y int
			want color.NRGBA
		}{
			{0, 0, palette[0]},
			{1, 0, palette[1]},
			{2, 1, palette[3]},
		} {
			if got := nrgbaAt(img, tc.x, tc.y); got != tc.want {
				t.Errorf("pixel (%d,%d) = %+v, want %+v", tc.x, tc.y, got, tc.want)
			}
		}
	})

	t.Run("24bpp", func(t *testing.T) {
		rgb := func(x, y int) color.NRGBA {
			switch {
			case x == 0 && y == 0:
				return red
			case x == n-1 && y == 0:
				return green
			case x == 0 && y == n-1:
				return blue
			default:
				return white
			}
		}
		payload := dib24Payload(n, n, rgb, nil, false)
		img, err := Decode(icoOne(n, n, 24, payload))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got := nrgbaAt(img, 0, 0); got != red {
			t.Errorf("pixel (0,0) = %+v, want red", got)
		}
		if got := nrgbaAt(img, n-1, 0); got != green {
			t.Errorf("pixel (7,0) = %+v, want green", got)
		}
		if got := nrgbaAt(img, 0, n-1); got != blue {
			t.Errorf("pixel (0,7) = %+v, want blue", got)
		}
	})

	t.Run("missing AND mask is opaque", func(t *testing.T) {
		payload := dib32Payload(n, n, func(x, y int) uint8 { return 0 }, func(x, y int) color.NRGBA { return red }, nil, false)
		img, err := Decode(icoOne(n, n, 32, payload))
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got := nrgbaAt(img, 0, 0); got.A != 255 {
			t.Fatalf("alpha = %d, want 255", got.A)
		}
	})

	t.Run("largest entry wins", func(t *testing.T) {
		small := dib32Payload(8, 8, func(x, y int) uint8 { return 255 }, func(x, y int) color.NRGBA { return red }, nil, false)
		green24 := dib24Payload(16, 16, func(x, y int) color.NRGBA { return green }, nil, false)
		rows := make([][]int, 16)
		for i := range rows {
			rows[i] = make([]int, 16)
		}
		blue8 := dibPalettedPayload(16, 16, 8, []color.NRGBA{blue}, 1, rows, nil, false)

		data := buildICO([]testICOEntry{
			{width: 8, height: 8, bitCount: 32, payload: small},
			{width: 16, height: 16, bitCount: 24, payload: green24},
			{width: 16, height: 16, bitCount: 8, payload: blue8},
		})
		img, err := Decode(data)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if b := img.Bounds(); b.Dx() != 16 || b.Dy() != 16 {
			t.Fatalf("bounds = %dx%d, want 16x16", b.Dx(), b.Dy())
		}
		if got := nrgbaAt(img, 0, 0); got != green {
			t.Fatalf("pixel = %+v, want green (24bpp entry should beat 8bpp)", got)
		}
	})

	t.Run("bit count breaks area ties", func(t *testing.T) {
		rows := make([][]int, 8)
		for i := range rows {
			rows[i] = make([]int, 8)
		}
		red8 := dibPalettedPayload(8, 8, 8, []color.NRGBA{red}, 1, rows, nil, false)
		green32 := dib32Payload(8, 8, func(x, y int) uint8 { return 255 }, func(x, y int) color.NRGBA { return green }, nil, false)
		data := buildICO([]testICOEntry{
			{width: 8, height: 8, bitCount: 8, payload: red8},
			{width: 8, height: 8, bitCount: 32, payload: green32},
		})
		img, err := Decode(data)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got := nrgbaAt(img, 0, 0); got != green {
			t.Fatalf("pixel = %+v, want green", got)
		}
	})

	t.Run("earlier entry breaks full ties", func(t *testing.T) {
		red32 := dib32Payload(8, 8, func(x, y int) uint8 { return 255 }, func(x, y int) color.NRGBA { return red }, nil, false)
		blue32 := dib32Payload(8, 8, func(x, y int) uint8 { return 255 }, func(x, y int) color.NRGBA { return blue }, nil, false)
		data := buildICO([]testICOEntry{
			{width: 8, height: 8, bitCount: 32, payload: red32},
			{width: 8, height: 8, bitCount: 32, payload: blue32},
		})
		img, err := Decode(data)
		if err != nil {
			t.Fatalf("Decode: %v", err)
		}
		if got := nrgbaAt(img, 0, 0); got != red {
			t.Fatalf("pixel = %+v, want red", got)
		}
	})

	t.Run("png entry pixel budget", func(t *testing.T) {
		entry := pngHeaderOnly(t, 8000, 8000)
		data := icoOne(8, 8, 32, entry)
		if _, err := Decode(data); err != ErrTooLarge {
			t.Fatalf("err = %v, want ErrTooLarge", err)
		}
	})
}

func TestDecodeICOMalformed(t *testing.T) {
	const n = 8
	valid32 := dib32Payload(n, n, func(x, y int) uint8 { return 255 }, func(x, y int) color.NRGBA { return color.NRGBA{255, 0, 0, 255} }, nil, false)

	solidRows := func(value int) [][]int {
		rows := make([][]int, n)
		for y := range rows {
			rows[y] = make([]int, n)
			for x := range rows[y] {
				rows[y][x] = value
			}
		}
		return rows
	}

	badReserved := appendUint16(nil, 1)
	badReserved = appendUint16(badReserved, 1)
	badReserved = appendUint16(badReserved, 1)
	badReserved = append(badReserved, icoEntryBytes(n, n, 32, uint32(len(valid32)), 22)...)
	badReserved = append(badReserved, valid32...)

	badType := appendUint16(nil, 0)
	badType = appendUint16(badType, 2)
	badType = appendUint16(badType, 1)
	badType = append(badType, icoEntryBytes(n, n, 32, uint32(len(valid32)), 22)...)
	badType = append(badType, valid32...)

	offsetPastEnd := icoHeaderBytes(1)
	offsetPastEnd = append(offsetPastEnd, icoEntryBytes(n, n, 32, 10, 1000)...)

	truncatedXOR := append(dibHeaderBytes(n, 2*n, 32, 0, 0), make([]byte, 10)...)
	fullXOR := dib32Payload(n, n, func(x, y int) uint8 { return 255 }, func(x, y int) color.NRGBA { return color.NRGBA{255, 0, 0, 255} }, nil, false)
	truncatedMask := append(append([]byte{}, fullXOR...), make([]byte, 10)...)
	truncatedPalette := append(dibHeaderBytes(n, 2*n, 8, 0, 256), make([]byte, 4)...)
	outOfRange := dibPalettedPayload(n, n, 8, []color.NRGBA{{255, 0, 0, 255}, {0, 255, 0, 255}}, 2, solidRows(5), nil, false)

	tests := []struct {
		name string
		data []byte
	}{
		{"short header", []byte{0, 0, 1}},
		{"bad reserved", badReserved},
		{"bad type", badType},
		{"truncated directory", append(icoHeaderBytes(2), make([]byte, 10)...)},
		{"offset past end", offsetPastEnd},
		{"huge count", icoHeaderBytes(300)},
		{"zero count", icoHeaderBytes(0)},
		{"short dib header", icoOne(n, n, 32, []byte{1, 2, 3})},
		{"small biSize", icoOne(n, n, 32, dibHeaderRaw(39, n, 2*n, 32, 0, 0))},
		{"width zero", icoOne(n, n, 32, dibHeaderBytes(0, 2*n, 32, 0, 0))},
		{"negative height", icoOne(n, n, 32, dibHeaderBytes(n, -2*n, 32, 0, 0))},
		{"zero height", icoOne(n, n, 32, dibHeaderBytes(n, 0, 32, 0, 0))},
		{"odd height", icoOne(n, n, 32, dibHeaderBytes(n, 2*n+1, 32, 0, 0))},
		{"height too large", icoOne(n, n, 32, dibHeaderBytes(n, 2*257, 32, 0, 0))},
		{"bitfields", icoOne(n, n, 32, dibHeaderBytes(n, 2*n, 32, 3, 0))},
		{"bad bit count", icoOne(n, n, 32, dibHeaderBytes(n, 2*n, 16, 0, 0))},
		{"palette index out of range", icoOne(n, n, 8, outOfRange)},
		{"truncated palette", icoOne(n, n, 8, truncatedPalette)},
		{"truncated xor", icoOne(n, n, 32, truncatedXOR)},
		{"truncated and mask", icoOne(n, n, 32, truncatedMask)},
		{"corrupt png payload", icoOne(n, n, 32, append(append([]byte{}, pngSignature...), 0, 0, 0, 0))},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := Decode(tt.data); err == nil {
				t.Fatal("Decode succeeded, want error")
			}
		})
	}
}

// Regression: a 32bpp entry with real alpha and no AND mask keeps its alpha,
// and a directory entry whose size disagrees with the DIB does not matter.
func TestICOAlphaWithoutMaskAndDirectoryMismatch(t *testing.T) {
	const w, h = 4, 2
	payload := dib32Payload(w, h, func(x, y int) uint8 {
		if x == 0 {
			return 0
		}
		return 128
	}, func(x, y int) color.NRGBA { return color.NRGBA{10, 20, 30, 255} }, nil, false)
	img, err := Decode(icoOne(16, 16, 32, payload))
	if err != nil {
		t.Fatal(err)
	}
	if b := img.Bounds(); b.Dx() != w || b.Dy() != h {
		t.Fatalf("bounds %v, want the DIB's %dx%d", b, w, h)
	}
	nrgba := img.(*image.NRGBA)
	if got := nrgba.NRGBAAt(0, 0); got.A != 0 {
		t.Fatalf("pixel (0,0) alpha %d, want 0", got.A)
	}
	if got := nrgba.NRGBAAt(1, 1); got != (color.NRGBA{10, 20, 30, 128}) {
		t.Fatalf("pixel (1,1) = %v", got)
	}
	// All-zero alpha without a mask is an opaque image.
	opaque := dib32Payload(w, h, func(x, y int) uint8 { return 0 }, func(x, y int) color.NRGBA { return color.NRGBA{10, 20, 30, 255} }, nil, false)
	img, err = Decode(icoOne(w, h, 32, opaque))
	if err != nil {
		t.Fatal(err)
	}
	if got := img.(*image.NRGBA).NRGBAAt(2, 1); got.A != 0xff {
		t.Fatalf("opaque pixel alpha %d", got.A)
	}
}
