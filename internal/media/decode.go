package media

import (
	"bytes"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"io"

	"golang.org/x/image/webp"
)

const (
	// MaxSourceBytes is the largest accepted encoded input.
	MaxSourceBytes = 8 << 20
	// MaxSourcePixels bounds width*height and MaxDecodedBytes the estimated
	// size of the decoded pixels. Both are checked from the image header
	// before any pixel buffer is allocated. Feeds do link camera originals
	// (26 megapixels is common), which fit; a 16-bit PNG of that size does
	// not.
	MaxSourcePixels = 50_000_000
	MaxDecodedBytes = 128 << 20
	// ThumbnailSize is the side length of a full-size article thumbnail.
	ThumbnailSize = 192
	// MinThumbnailSide is the smallest source side that can yield a usable
	// thumbnail. Smaller sources are refused rather than upscaled.
	MinThumbnailSide = 64
	// MaxThumbnailAspect bounds how elongated a thumbnail source may be: the
	// long side may be at most this many times the short side.
	MaxThumbnailAspect = 3
	// IconSize is the largest side of a stored feed icon.
	IconSize = 64
)

var (
	// ErrUnsupported reports input that is not a supported raster format, or
	// an image that cannot be used for the requested output (for example, a
	// zero-sized icon).
	ErrUnsupported = errors.New("media: unsupported image format")
	// ErrTooLarge reports input that exceeds the encoded size or pixel budget.
	ErrTooLarge = errors.New("media: image too large")
	// ErrUnsuitable reports a source that is too small or too elongated to
	// crop into an article thumbnail.
	ErrUnsuitable = errors.New("media: image unsuitable as a thumbnail")
)

var (
	pngSignature = []byte{0x89, 'P', 'N', 'G', '\r', '\n', 0x1a, '\n'}
	icoSignature = []byte{0x00, 0x00, 0x01, 0x00}
)

// Decode decodes a single image from data. The format is sniffed from the
// leading bytes; a declared Content-Type is never consulted.
//
// JPEG, PNG, the first frame of a GIF, WebP and ICO are accepted. Everything
// else returns ErrUnsupported. Input larger than MaxSourceBytes, or whose
// header declares more than MaxSourcePixels pixels, returns ErrTooLarge
// before any pixel buffer is allocated. Decoder failures on corrupt data are
// returned wrapped; a decoder panic is converted into an error because the
// input comes from untrusted publishers.
func Decode(data []byte) (image.Image, error) {
	if len(data) > MaxSourceBytes {
		return nil, ErrTooLarge
	}
	switch {
	case bytes.HasPrefix(data, []byte{0xff, 0xd8, 0xff}):
		return decodeStandard("jpeg", data, jpeg.DecodeConfig, jpeg.Decode)
	case bytes.HasPrefix(data, pngSignature):
		return decodeStandard("png", data, png.DecodeConfig, png.Decode)
	case bytes.HasPrefix(data, []byte("GIF87a")), bytes.HasPrefix(data, []byte("GIF89a")):
		return decodeStandard("gif", data, gif.DecodeConfig, gif.Decode)
	case isWebP(data):
		img, err := decodeStandard("webp", data, webp.DecodeConfig, webp.Decode)
		if err != nil {
			return nil, err
		}
		expandVideoRange(img)
		return img, nil
	case bytes.HasPrefix(data, icoSignature):
		return decodeICOChecked(data)
	default:
		return nil, ErrUnsupported
	}
}

// expandVideoRange fixes lossy WebP colours in place. VP8 stores video-range
// BT.601 samples (luma 16 to 235, chroma 16 to 240), but image.YCbCr's colour
// model assumes the full JPEG range, so read as-is a thumbnail comes out
// washed out: white as 235, black as 16. Stretching the planes to full range
// makes the standard conversion produce exactly the video-range result, and
// keeps the image in YCbCr, which the scaler reads quickly and which needs
// no second pixel buffer. Lossless WebP decodes to NRGBA and is untouched.
func expandVideoRange(img image.Image) {
	var ycc *image.YCbCr
	switch m := img.(type) {
	case *image.YCbCr:
		ycc = m
	case *image.NYCbCrA:
		ycc = &m.YCbCr
	default:
		return
	}
	for i, v := range ycc.Y {
		ycc.Y[i] = lumaFull[v]
	}
	for i, v := range ycc.Cb {
		ycc.Cb[i] = chromaFull[v]
	}
	for i, v := range ycc.Cr {
		ycc.Cr[i] = chromaFull[v]
	}
}

// lumaFull and chromaFull map video-range samples to full range:
// Y' = (Y-16)*255/219 and C' = (C-128)*255/224 + 128, rounded and clamped.
var lumaFull, chromaFull = func() (luma, chroma [256]uint8) {
	for i := range 256 {
		luma[i] = uint8(min(max((float64(i)-16)*255/219+0.5, 0), 255))
		chroma[i] = uint8(min(max((float64(i)-128)*255/224+128.5, 0), 255))
	}
	return
}()

// decodedBytes estimates the memory a decoder allocates for the image a
// header describes. YCbCr is counted as 4:4:4, the largest JPEG layout.
func decodedBytes(cfg image.Config) uint64 {
	perPixel := uint64(8)
	switch cfg.ColorModel {
	case color.GrayModel:
		perPixel = 1
	case color.Gray16Model:
		perPixel = 2
	case color.YCbCrModel:
		perPixel = 3
	case color.RGBAModel, color.NRGBAModel, color.NYCbCrAModel, color.CMYKModel:
		perPixel = 4
	default:
		if _, ok := cfg.ColorModel.(color.Palette); ok {
			perPixel = 1
		}
	}
	return uint64(cfg.Width) * uint64(cfg.Height) * perPixel
}

// checkBudget rejects a header whose image is empty or would exceed the
// pixel or memory budget.
func checkBudget(cfg image.Config) error {
	if cfg.Width <= 0 || cfg.Height <= 0 {
		return ErrUnsupported
	}
	if uint64(cfg.Width)*uint64(cfg.Height) > MaxSourcePixels || decodedBytes(cfg) > MaxDecodedBytes {
		return ErrTooLarge
	}
	return nil
}

func isWebP(data []byte) bool {
	return len(data) >= 12 &&
		bytes.Equal(data[:4], []byte("RIFF")) &&
		bytes.Equal(data[8:12], []byte("WEBP"))
}

type configDecoder func(io.Reader) (image.Config, error)
type imageDecoder func(io.Reader) (image.Image, error)

// decodeStandard reads the dimensions with the format-specific config
// decoder, rejects inputs over the pixel budget, then decodes. The recover
// keeps a decoder bug from taking down the process.
func decodeStandard(name string, data []byte, config configDecoder, decode imageDecoder) (img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("media: %s decoder panicked: %v", name, r)
		}
	}()

	cfg, cerr := config(bytes.NewReader(data))
	if cerr != nil {
		return nil, fmt.Errorf("media: decode %s config: %w", name, cerr)
	}
	if err := checkBudget(cfg); err != nil {
		return nil, err
	}
	img, err = decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("media: decode %s: %w", name, err)
	}
	return img, nil
}

// decodeICOChecked turns a panic in the hand-written ICO parser into an
// error. The parser bounds-checks every slice, but untrusted input gets
// defense in depth.
func decodeICOChecked(data []byte) (img image.Image, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("media: ico decoder panicked: %v", r)
		}
	}()
	return decodeICO(data)
}
