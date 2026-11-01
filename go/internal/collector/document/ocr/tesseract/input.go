// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package tesseract

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	_ "image/gif"  // register GIF header decoding for DecodeConfig
	_ "image/jpeg" // register JPEG header decoding for DecodeConfig
	_ "image/png"  // register PNG header decoding for DecodeConfig

	"github.com/eshu-hq/eshu/go/internal/collector/document/ocr"
)

// formatWebP labels WebP input. PNG, JPEG, and GIF labels come from
// image.DecodeConfig and match the picture preflight format names.
const formatWebP = "webp"

// validatedImage is an image that passed every pre-exec check.
type validatedImage struct {
	format string
	width  int
	height int
}

// validateInput enforces byte, format, and pixel caps before any exec. Only
// headers are parsed; pixel data is decoded by the sandboxed subprocess.
func (e *Engine) validateInput(img ocr.Image) (validatedImage, error) {
	if len(img.Body) == 0 {
		return validatedImage{}, failure(OutcomeInputRejected, "empty image body", nil)
	}
	if int64(len(img.Body)) > e.cfg.MaxInputBytes {
		return validatedImage{}, failure(OutcomeInputRejected,
			fmt.Sprintf("input %d bytes exceeds cap %d", len(img.Body), e.cfg.MaxInputBytes), nil)
	}
	if img.FrameIndex != 0 {
		return validatedImage{}, failure(OutcomeInputRejected, "only frame 0 is recognized", nil)
	}
	format, width, height, err := sniffImage(img.Body)
	if err != nil {
		return validatedImage{}, failure(OutcomeInputRejected, "image header", err)
	}
	if img.Format != "" && img.Format != format {
		return validatedImage{}, failure(OutcomeInputRejected,
			fmt.Sprintf("declared format %q does not match %q bytes", img.Format, format), nil)
	}
	if width <= 0 || height <= 0 {
		return validatedImage{}, failure(OutcomeInputRejected,
			fmt.Sprintf("degenerate dimensions %dx%d", width, height), nil)
	}
	if pixels := int64(width) * int64(height); pixels > e.cfg.MaxPixels {
		return validatedImage{}, failure(OutcomeInputRejected,
			fmt.Sprintf("pixels %d exceed cap %d", pixels, e.cfg.MaxPixels), nil)
	}
	return validatedImage{format: format, width: width, height: height}, nil
}

var errUnsupportedFormat = errors.New("unsupported image format")

// sniffImage identifies the format by magic bytes and reads dimensions from
// the header. PNG, JPEG, and GIF use the standard library DecodeConfig;
// WebP uses a bounded RIFF header parser because the module carries no WebP
// decoder and the subprocess decodes pixels.
func sniffImage(body []byte) (string, int, int, error) {
	switch {
	case bytes.HasPrefix(body, []byte("\x89PNG\r\n\x1a\n")),
		bytes.HasPrefix(body, []byte("\xff\xd8\xff")),
		bytes.HasPrefix(body, []byte("GIF87a")),
		bytes.HasPrefix(body, []byte("GIF89a")):
		cfg, format, err := image.DecodeConfig(bytes.NewReader(body))
		if err != nil {
			return "", 0, 0, errors.New("malformed raster header")
		}
		return format, cfg.Width, cfg.Height, nil
	case len(body) >= 12 && bytes.Equal(body[0:4], []byte("RIFF")) && bytes.Equal(body[8:12], []byte("WEBP")):
		width, height, err := webpDimensions(body)
		if err != nil {
			return "", 0, 0, err
		}
		return formatWebP, width, height, nil
	default:
		return "", 0, 0, errUnsupportedFormat
	}
}

// webpDimensions reads the canvas size from the first chunk of a WebP
// container: VP8X (extended), VP8L (lossless), or VP8 (lossy key frame).
func webpDimensions(body []byte) (int, int, error) {
	if len(body) < 20 {
		return 0, 0, errors.New("truncated webp header")
	}
	fourcc := string(body[12:16])
	size := binary.LittleEndian.Uint32(body[16:20])
	payload := body[20:]
	if uint64(size) > uint64(len(payload)) {
		return 0, 0, errors.New("truncated webp chunk")
	}
	payload = payload[:size]
	switch fourcc {
	case "VP8X":
		if len(payload) < 10 {
			return 0, 0, errors.New("short VP8X chunk")
		}
		width := 1 + int(uint32(payload[4])|uint32(payload[5])<<8|uint32(payload[6])<<16)
		height := 1 + int(uint32(payload[7])|uint32(payload[8])<<8|uint32(payload[9])<<16)
		return width, height, nil
	case "VP8L":
		if len(payload) < 5 || payload[0] != 0x2f {
			return 0, 0, errors.New("bad VP8L signature")
		}
		bits := binary.LittleEndian.Uint32(payload[1:5])
		return 1 + int(bits&0x3fff), 1 + int((bits>>14)&0x3fff), nil
	case "VP8 ":
		if len(payload) < 10 || payload[0]&1 != 0 || !bytes.Equal(payload[3:6], []byte{0x9d, 0x01, 0x2a}) {
			return 0, 0, errors.New("bad VP8 key frame header")
		}
		width := int(binary.LittleEndian.Uint16(payload[6:8]) & 0x3fff)
		height := int(binary.LittleEndian.Uint16(payload[8:10]) & 0x3fff)
		return width, height, nil
	default:
		return 0, 0, errors.New("unknown webp chunk")
	}
}
