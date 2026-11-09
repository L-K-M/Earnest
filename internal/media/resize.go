package media

import (
	"bytes"
	"fmt"
	"image"
	"image/jpeg"
	"image/png"

	xdraw "golang.org/x/image/draw"
)

// Encoded is a re-encoded copy of a decoded image, ready to store.
type Encoded struct {
	Body        []byte
	ContentType string
	Width       int
	Height      int
}

const jpegQuality = 82

// Thumbnail centre-crops img to a square and scales it down to a small
// article thumbnail. Sources smaller than MinThumbnailSide on either side, or
// whose long side exceeds MaxThumbnailAspect times the short side, are
// unsuitable and return ErrUnsuitable.
//
// Fully opaque results are encoded as JPEG; anything with transparency is
// encoded as PNG. Width and Height describe the encoded image.
func Thumbnail(img image.Image) (Encoded, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	short, long := min(w, h), max(w, h)
	if short < MinThumbnailSide || long > MaxThumbnailAspect*short {
		return Encoded{}, ErrUnsuitable
	}

	side := min(ThumbnailSize, short)
	// Largest centred square of the source. Integer division floors the
	// offset, which is the conventional centred crop for odd differences.
	x0 := b.Min.X + (w-short)/2
	y0 := b.Min.Y + (h-short)/2
	crop := image.Rect(x0, y0, x0+short, y0+short)

	dst := image.NewNRGBA(image.Rect(0, 0, side, side))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), img, crop, xdraw.Src, nil)

	opaque := true
	for i := 3; i < len(dst.Pix); i += 4 {
		if dst.Pix[i] != 0xff {
			opaque = false
			break
		}
	}

	var buf bytes.Buffer
	if opaque {
		if err := jpeg.Encode(&buf, dst, &jpeg.Options{Quality: jpegQuality}); err != nil {
			return Encoded{}, fmt.Errorf("media: encode thumbnail jpeg: %w", err)
		}
		return Encoded{Body: buf.Bytes(), ContentType: "image/jpeg", Width: side, Height: side}, nil
	}
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, dst); err != nil {
		return Encoded{}, fmt.Errorf("media: encode thumbnail png: %w", err)
	}
	return Encoded{Body: buf.Bytes(), ContentType: "image/png", Width: side, Height: side}, nil
}

// Icon scales img down to fit within IconSize x IconSize, preserving the
// aspect ratio and never upscaling, and draws it centred on a transparent
// square canvas. The result is always PNG so the transparency survives.
// Width and Height describe the square canvas.
func Icon(img image.Image) (Encoded, error) {
	b := img.Bounds()
	w, h := b.Dx(), b.Dy()
	if w <= 0 || h <= 0 {
		return Encoded{}, ErrUnsupported
	}

	long := max(w, h)
	scaledLong := min(long, IconSize)
	tw := w * scaledLong / long
	th := h * scaledLong / long
	if tw < 1 {
		tw = 1
	}
	if th < 1 {
		th = 1
	}

	canvas := image.NewNRGBA(image.Rect(0, 0, scaledLong, scaledLong))
	x0 := (scaledLong - tw) / 2
	y0 := (scaledLong - th) / 2
	xdraw.CatmullRom.Scale(canvas, image.Rect(x0, y0, x0+tw, y0+th), img, b, xdraw.Src, nil)

	var buf bytes.Buffer
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	if err := enc.Encode(&buf, canvas); err != nil {
		return Encoded{}, fmt.Errorf("media: encode icon png: %w", err)
	}
	return Encoded{Body: buf.Bytes(), ContentType: "image/png", Width: scaledLong, Height: scaledLong}, nil
}
