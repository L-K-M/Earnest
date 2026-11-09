// Package media decodes images fetched from publishers and re-encodes small
// copies for use as article thumbnails and feed favicons.
//
// Decode accepts only JPEG, PNG, GIF, WebP and ICO data, sniffed from the
// leading bytes rather than from any declared Content-Type. The encoded size
// and the decoded pixel count are bounded before pixel decoding starts, so a
// hostile publisher cannot exhaust memory. No decoder is registered with the
// image package: the format-specific decoders are called directly.
package media
