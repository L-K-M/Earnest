package media

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"image"
	"image/color"
	"image/png"
)

const (
	icoHeaderLen    = 6
	icoEntryLen     = 16
	icoMaxEntries   = 256
	icoMaxDimension = 256
	dibHeaderLen    = 40
)

// icoEntry is one ICONDIRENTRY. width and height already expand the 0-means-256
// convention.
type icoEntry struct {
	width, height int
	bitCount      uint16
	offset        uint32
	size          uint32
}

// decodeICO decodes the largest entry of an ICO container. Entries are ranked
// by width*height, then by bit count, then by position; the first entry wins
// a full tie.
func decodeICO(data []byte) (image.Image, error) {
	entries, err := icoEntries(data)
	if err != nil {
		return nil, err
	}
	best := entries[0]
	for _, e := range entries[1:] {
		area, bestArea := e.width*e.height, best.width*best.height
		if area > bestArea || (area == bestArea && e.bitCount > best.bitCount) {
			best = e
		}
	}
	return decodeICOEntry(data, best)
}

func icoEntries(data []byte) ([]icoEntry, error) {
	if len(data) < icoHeaderLen {
		return nil, fmt.Errorf("media: ico: short header")
	}
	if binary.LittleEndian.Uint16(data[0:2]) != 0 {
		return nil, fmt.Errorf("media: ico: reserved word is not zero")
	}
	if binary.LittleEndian.Uint16(data[2:4]) != 1 {
		return nil, fmt.Errorf("media: ico: type is not 1")
	}
	count := int(binary.LittleEndian.Uint16(data[4:6]))
	if count < 1 || count > icoMaxEntries {
		return nil, fmt.Errorf("media: ico: entry count %d out of range", count)
	}
	dirEnd := uint64(icoHeaderLen) + uint64(count)*icoEntryLen
	if dirEnd > uint64(len(data)) {
		return nil, fmt.Errorf("media: ico: truncated directory")
	}

	entries := make([]icoEntry, count)
	for i := range entries {
		b := data[icoHeaderLen+i*icoEntryLen : icoHeaderLen+(i+1)*icoEntryLen]
		e := icoEntry{
			width:    int(b[0]),
			height:   int(b[1]),
			bitCount: binary.LittleEndian.Uint16(b[6:8]),
			size:     binary.LittleEndian.Uint32(b[8:12]),
			offset:   binary.LittleEndian.Uint32(b[12:16]),
		}
		if e.width == 0 {
			e.width = icoMaxDimension
		}
		if e.height == 0 {
			e.height = icoMaxDimension
		}
		// uint64 arithmetic: offset and size are uint32 and must not wrap.
		if uint64(e.offset)+uint64(e.size) > uint64(len(data)) {
			return nil, fmt.Errorf("media: ico: entry %d payload outside data", i)
		}
		entries[i] = e
	}
	return entries, nil
}

func decodeICOEntry(data []byte, e icoEntry) (image.Image, error) {
	if uint64(e.width)*uint64(e.height) > MaxSourcePixels {
		return nil, ErrTooLarge
	}
	payload := data[e.offset : uint64(e.offset)+uint64(e.size)]
	if bytes.HasPrefix(payload, pngSignature) {
		return decodeICOPNG(payload)
	}
	return decodeICODIB(payload)
}

// decodeICOPNG decodes a PNG payload embedded in an ICO entry. The pixel
// budget is checked from the PNG header before decoding.
func decodeICOPNG(payload []byte) (image.Image, error) {
	cfg, err := png.DecodeConfig(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("media: ico: png entry config: %w", err)
	}
	if err := checkBudget(cfg); err != nil {
		return nil, err
	}
	img, err := png.Decode(bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("media: ico: png entry: %w", err)
	}
	return img, nil
}

// decodeICODIB decodes the BITMAPINFOHEADER, palette, XOR bitmap and AND mask
// of an ICO entry into an *image.NRGBA. The XOR bitmap holds biHeight/2 rows,
// stored bottom-up and padded to 4 bytes; the AND mask, when present, holds
// the same number of 1-bpp rows. The DIB's own dimensions win over the
// directory entry's, which some icon writers get wrong.
func decodeICODIB(payload []byte) (*image.NRGBA, error) {
	if len(payload) < dibHeaderLen {
		return nil, fmt.Errorf("media: ico: short DIB header")
	}
	biSize := binary.LittleEndian.Uint32(payload[0:4])
	biWidth := int32(binary.LittleEndian.Uint32(payload[4:8]))
	biHeight := int32(binary.LittleEndian.Uint32(payload[8:12]))
	biBitCount := binary.LittleEndian.Uint16(payload[14:16])
	biCompression := binary.LittleEndian.Uint32(payload[16:20])
	biClrUsed := binary.LittleEndian.Uint32(payload[32:36])

	if biSize < dibHeaderLen {
		return nil, fmt.Errorf("media: ico: DIB header size %d too small", biSize)
	}
	if biWidth < 1 || biWidth > icoMaxDimension {
		return nil, fmt.Errorf("media: ico: DIB width %d out of range", biWidth)
	}
	// The DIB height counts both the XOR rows and the AND mask rows, so it
	// is twice the icon's height.
	if biHeight <= 0 || biHeight%2 != 0 || biHeight > 2*icoMaxDimension {
		return nil, fmt.Errorf("media: ico: DIB height %d out of range", biHeight)
	}
	switch biBitCount {
	case 1, 4, 8, 24, 32:
	default:
		return nil, fmt.Errorf("media: ico: bit count %d unsupported", biBitCount)
	}
	if biCompression != 0 {
		return nil, fmt.Errorf("media: ico: compression %d unsupported", biCompression)
	}

	width := int(biWidth)
	height := int(biHeight / 2)
	headerLen := uint64(biSize)
	if headerLen > uint64(len(payload)) {
		return nil, fmt.Errorf("media: ico: DIB header exceeds payload")
	}

	var palette []color.NRGBA
	xorOffset := headerLen
	if biBitCount <= 8 {
		nColors := uint64(1) << biBitCount
		if biClrUsed > 0 && uint64(biClrUsed) <= nColors {
			nColors = uint64(biClrUsed)
		}
		paletteEnd := headerLen + nColors*4
		if paletteEnd > uint64(len(payload)) {
			return nil, fmt.Errorf("media: ico: truncated palette")
		}
		palette = make([]color.NRGBA, nColors)
		for i := range palette {
			p := payload[headerLen+uint64(i)*4:]
			// RGBQUAD is stored blue, green, red, reserved.
			palette[i] = color.NRGBA{R: p[2], G: p[1], B: p[0], A: 0xff}
		}
		xorOffset = paletteEnd
	}

	xorRowSize := (uint64(width)*uint64(biBitCount) + 31) / 32 * 4
	xorBytes := xorRowSize * uint64(height)
	if xorOffset+xorBytes > uint64(len(payload)) {
		return nil, fmt.Errorf("media: ico: truncated XOR bitmap")
	}
	xorEnd := xorOffset + xorBytes

	andRowSize := (uint64(width) + 31) / 32 * 4
	andBytes := andRowSize * uint64(height)
	// A mask that is absent leaves the image opaque; a partly present mask is
	// corrupt rather than partially transparent.
	hasMask := false
	if uint64(len(payload)) > xorEnd {
		if xorEnd+andBytes > uint64(len(payload)) {
			return nil, fmt.Errorf("media: ico: truncated AND mask")
		}
		hasMask = true
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	anyAlpha := false
	for y := 0; y < height; y++ {
		rowStart := xorOffset + uint64(height-1-y)*xorRowSize
		row := payload[rowStart : rowStart+xorRowSize]
		for x := 0; x < width; x++ {
			var c color.NRGBA
			switch biBitCount {
			case 32:
				i := x * 4
				c = color.NRGBA{R: row[i+2], G: row[i+1], B: row[i], A: row[i+3]}
				if c.A != 0 {
					anyAlpha = true
				}
			case 24:
				i := x * 3
				c = color.NRGBA{R: row[i+2], G: row[i+1], B: row[i], A: 0xff}
			case 8:
				if int(row[x]) >= len(palette) {
					return nil, fmt.Errorf("media: ico: palette index %d out of range", row[x])
				}
				c = palette[row[x]]
			case 4:
				var idx byte
				if x%2 == 0 {
					idx = row[x/2] >> 4
				} else {
					idx = row[x/2] & 0x0f
				}
				if int(idx) >= len(palette) {
					return nil, fmt.Errorf("media: ico: palette index %d out of range", idx)
				}
				c = palette[idx]
			case 1:
				idx := (row[x/8] >> (7 - uint(x%8))) & 1
				if int(idx) >= len(palette) {
					return nil, fmt.Errorf("media: ico: palette index %d out of range", idx)
				}
				c = palette[idx]
			}
			img.SetNRGBA(x, y, c)
		}
	}

	// 32bpp alpha wins unless every alpha byte is zero: older writers leave
	// it unset, so the pixels are opaque and the AND mask, if any, supplies
	// transparency.
	if biBitCount == 32 && !anyAlpha {
		for i := 3; i < len(img.Pix); i += 4 {
			img.Pix[i] = 0xff
		}
	}
	if hasMask && (biBitCount != 32 || !anyAlpha) {
		applyANDMask(img, payload[xorEnd:xorEnd+andBytes], width, height, int(andRowSize))
	}
	return img, nil
}

// applyANDMask sets alpha from a bottom-up 1-bpp mask: a set bit is
// transparent. RGB is preserved; only alpha changes.
func applyANDMask(img *image.NRGBA, mask []byte, width, height, rowSize int) {
	for y := 0; y < height; y++ {
		row := (height - 1 - y) * rowSize
		for x := 0; x < width; x++ {
			c := img.NRGBAAt(x, y)
			if mask[row+x/8]&(0x80>>uint(x%8)) != 0 {
				c.A = 0
			} else {
				c.A = 0xff
			}
			img.SetNRGBA(x, y, c)
		}
	}
}
