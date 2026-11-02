// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package picture

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"image"
	"io"
	"path/filepath"
	"sort"
	"strings"

	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
)

const (
	// FormatPNG identifies a PNG image.
	FormatPNG = "png"
	// FormatJPEG identifies a JPEG image.
	FormatJPEG = "jpeg"
	// FormatGIF identifies a GIF image.
	FormatGIF = "gif"
	// FormatWEBP identifies a WebP image.
	FormatWEBP = "webp"
)

const (
	defaultMaxSourceBytes = int64(25 << 20)
	defaultMaxPixels      = int64(50_000_000)
	maxReadBytes          = int64(25 << 20)
)

// WarningClass is a stable, low-cardinality image preflight failure class.
type WarningClass string

const (
	// WarningUnsupportedFormat marks image formats outside this preflight.
	WarningUnsupportedFormat WarningClass = "unsupported_format"
	// WarningUnsupportedCodec marks image codecs not yet handled by standard-library preflight.
	WarningUnsupportedCodec WarningClass = "unsupported_codec"
	// WarningMalformedMedia marks image containers that cannot be decoded as metadata.
	WarningMalformedMedia WarningClass = "malformed_media"
	// WarningResourceLimitExceeded marks source-byte or pixel-budget limits.
	WarningResourceLimitExceeded WarningClass = "resource_limit_exceeded"
	// WarningTimeout marks caller cancellation or deadline during preflight.
	WarningTimeout WarningClass = "timeout"
	// WarningExternalReferenceSkipped marks external references that cannot be followed.
	WarningExternalReferenceSkipped WarningClass = "external_reference_skipped"
	// WarningSensitiveValueRedacted marks sensitive-looking values detected in image metadata bytes.
	WarningSensitiveValueRedacted WarningClass = "sensitive_value_redacted"
	// WarningMetadataRedacted marks image metadata fields that must not be persisted.
	WarningMetadataRedacted WarningClass = "metadata_redacted"
	// WarningPartialExtraction marks bounded first-frame GIF handling.
	WarningPartialExtraction WarningClass = "partial_extraction"
)

// Options bounds image preflight work.
type Options struct {
	MaxSourceBytes int64
	MaxPixels      int64
}

// Warning records one bounded image preflight failure class.
type Warning struct {
	Class WarningClass `json:"class"`
	Count int          `json:"count"`
}

// Result summarizes metadata-only image preflight.
type Result struct {
	Format                 string    `json:"format"`
	Safe                   bool      `json:"safe"`
	Warnings               []Warning `json:"warnings,omitempty"`
	SourceBytes            int64     `json:"source_bytes"`
	Width                  int       `json:"width"`
	Height                 int       `json:"height"`
	PixelCount             int64     `json:"pixel_count"`
	FrameCount             int       `json:"frame_count,omitempty"`
	ExternalReferenceCount int       `json:"external_reference_count"`
	SensitiveValueCount    int       `json:"sensitive_value_count"`
	MetadataRedactionCount int       `json:"metadata_redaction_count"`
}

type recorder struct {
	result *Result
	seen   map[WarningClass]int
}

// Preflight classifies an image source without extracting OCR text or pixels.
func Preflight(ctx context.Context, sourceName string, reader io.ReaderAt, size int64, options Options) (Result, error) {
	if reader == nil {
		return Result{}, fmt.Errorf("reader must not be nil")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	opts := normalizeOptions(options)
	result := Result{
		Format:      formatForSource(sourceName),
		Safe:        true,
		SourceBytes: size,
	}
	rec := recorder{result: &result, seen: map[WarningClass]int{}}

	if err := ctx.Err(); err != nil {
		rec.warn(WarningTimeout)
		return rec.finalize(), err
	}
	if result.Format == "" {
		rec.warn(WarningUnsupportedFormat)
		return rec.finalize(), nil
	}
	if size < 0 || size > opts.MaxSourceBytes || size > maxReadBytes {
		rec.warn(WarningResourceLimitExceeded)
		return rec.finalize(), nil
	}
	body, ok := readExact(reader, size, &rec)
	if !ok {
		return rec.finalize(), nil
	}
	rec.classifyImage(ctx, body, opts)
	return rec.finalize(), nil
}

func normalizeOptions(options Options) Options {
	if options.MaxSourceBytes <= 0 {
		options.MaxSourceBytes = defaultMaxSourceBytes
	}
	if options.MaxPixels <= 0 {
		options.MaxPixels = defaultMaxPixels
	}
	return options
}

func readExact(reader io.ReaderAt, size int64, rec *recorder) ([]byte, bool) {
	body := make([]byte, size)
	n, err := reader.ReadAt(body, 0)
	if err != nil && (err != io.EOF || n != len(body)) {
		rec.warn(WarningResourceLimitExceeded)
		return nil, false
	}
	return body, true
}

func (r *recorder) classifyImage(ctx context.Context, body []byte, options Options) {
	if err := ctx.Err(); err != nil {
		r.warn(WarningTimeout)
		return
	}
	r.scanMetadataMarkers(body)
	if r.result.Format == FormatWEBP {
		width, height, err := webpDimensions(body)
		if err != nil {
			r.warn(WarningMalformedMedia)
			return
		}
		pixels, ok := pixelCount(width, height, options.MaxPixels)
		if !ok {
			r.warn(WarningResourceLimitExceeded)
			return
		}
		r.result.Width = width
		r.result.Height = height
		r.result.PixelCount = pixels
		return
	}
	config, decodedFormat, err := image.DecodeConfig(bytes.NewReader(body))
	if err != nil {
		r.warn(WarningMalformedMedia)
		return
	}
	if !formatMatches(r.result.Format, decodedFormat) {
		r.warn(WarningMalformedMedia)
		return
	}
	r.result.Width = config.Width
	r.result.Height = config.Height
	pixels, ok := pixelCount(config.Width, config.Height, options.MaxPixels)
	if !ok {
		r.warn(WarningResourceLimitExceeded)
		return
	}
	r.result.PixelCount = pixels
	if r.result.Format == FormatGIF {
		frames, ok := countGIFFrames(body)
		if !ok {
			r.warn(WarningMalformedMedia)
			return
		}
		r.result.FrameCount = frames
		if frames > 1 {
			r.warn(WarningPartialExtraction)
		}
	}
}

func (r *recorder) scanMetadataMarkers(body []byte) {
	lower := strings.ToLower(string(body))
	if hasAny(lower, "http://", "https://", "ftp://", "file://") {
		r.result.ExternalReferenceCount++
		r.warn(WarningExternalReferenceSkipped)
	}
	if hasAny(lower, "credential_marker", "secret_marker", "token_marker", "password", "api_key", "private_key") {
		r.result.SensitiveValueCount++
		r.warn(WarningSensitiveValueRedacted)
	}
	if hasAny(lower, "exif", "xmp", "gps", "artist", "software", "usercomment", "image description", "camera", "serial") {
		r.result.MetadataRedactionCount++
		r.warn(WarningMetadataRedacted)
	}
}

func (r *recorder) warn(class WarningClass) {
	if count, ok := r.seen[class]; ok {
		r.seen[class] = count + 1
		for i := range r.result.Warnings {
			if r.result.Warnings[i].Class == class {
				r.result.Warnings[i].Count++
				break
			}
		}
	} else {
		r.seen[class] = 1
		r.result.Warnings = append(r.result.Warnings, Warning{Class: class, Count: 1})
	}
	r.result.Safe = false
}

func (r *recorder) finalize() Result {
	if len(r.result.Warnings) > 0 {
		r.result.Safe = false
		sort.Slice(r.result.Warnings, func(left, right int) bool {
			return r.result.Warnings[left].Class < r.result.Warnings[right].Class
		})
	}
	return *r.result
}

func formatForSource(sourceName string) string {
	switch strings.ToLower(filepath.Ext(sourceName)) {
	case ".png":
		return FormatPNG
	case ".jpg", ".jpeg":
		return FormatJPEG
	case ".gif":
		return FormatGIF
	case ".webp":
		return FormatWEBP
	default:
		return ""
	}
}

func formatMatches(expected string, decoded string) bool {
	return expected == decoded
}

func pixelCount(width int, height int, limit int64) (int64, bool) {
	if width <= 0 || height <= 0 || limit <= 0 {
		return 0, false
	}
	w := int64(width)
	h := int64(height)
	if w > limit/h {
		return 0, false
	}
	return w * h, true
}

// webpDimensions reads the canvas size from the first chunk of a WebP
// container: VP8X (extended), VP8L (lossless), or VP8 (lossy key frame).
// It mirrors sniffImage in document/ocr/tesseract/input.go: a bounded header
// parse with no decoder dependency, so the preflight stays standard-library
// only (#9780).
func webpDimensions(body []byte) (int, int, error) {
	if len(body) < 12 || !bytes.Equal(body[0:4], []byte("RIFF")) || !bytes.Equal(body[8:12], []byte("WEBP")) {
		return 0, 0, errors.New("not a riff webp container")
	}
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
			return 0, 0, errors.New("short vp8x chunk")
		}
		width := 1 + int(uint32(payload[4])|uint32(payload[5])<<8|uint32(payload[6])<<16)
		height := 1 + int(uint32(payload[7])|uint32(payload[8])<<8|uint32(payload[9])<<16)
		return width, height, nil
	case "VP8L":
		if len(payload) < 5 || payload[0] != 0x2f {
			return 0, 0, errors.New("bad vp8l signature")
		}
		bits := binary.LittleEndian.Uint32(payload[1:5])
		return 1 + int(bits&0x3fff), 1 + int((bits>>14)&0x3fff), nil
	case "VP8 ":
		if len(payload) < 10 || payload[0]&1 != 0 || !bytes.Equal(payload[3:6], []byte{0x9d, 0x01, 0x2a}) {
			return 0, 0, errors.New("bad vp8 key frame header")
		}
		width := int(binary.LittleEndian.Uint16(payload[6:8]) & 0x3fff)
		height := int(binary.LittleEndian.Uint16(payload[8:10]) & 0x3fff)
		return width, height, nil
	default:
		return 0, 0, errors.New("unknown webp chunk")
	}
}

func hasAny(text string, needles ...string) bool {
	for _, needle := range needles {
		if strings.Contains(text, needle) {
			return true
		}
	}
	return false
}

func countGIFFrames(body []byte) (int, bool) {
	if len(body) < 13 || !bytes.HasPrefix(body, []byte("GIF")) {
		return 0, false
	}
	offset := 13
	packed := body[10]
	if packed&0x80 != 0 {
		offset += 3 * (1 << ((packed & 0x07) + 1))
	}
	frames := 0
	for offset < len(body) {
		switch body[offset] {
		case 0x2c:
			frames++
			offset += 10
			if offset > len(body) {
				return frames, false
			}
			localPacked := body[offset-1]
			if localPacked&0x80 != 0 {
				offset += 3 * (1 << ((localPacked & 0x07) + 1))
			}
			if offset >= len(body) {
				return frames, false
			}
			offset++
			next, ok := skipGIFSubBlocks(body, offset)
			if !ok {
				return frames, false
			}
			offset = next
		case 0x21:
			offset += 2
			next, ok := skipGIFSubBlocks(body, offset)
			if !ok {
				return frames, false
			}
			offset = next
		case 0x3b:
			return frames, true
		default:
			return frames, false
		}
	}
	return frames, false
}

func skipGIFSubBlocks(body []byte, offset int) (int, bool) {
	for offset < len(body) {
		blockSize := int(body[offset])
		offset++
		if blockSize == 0 {
			return offset, true
		}
		offset += blockSize
		if offset > len(body) {
			return offset, false
		}
	}
	return offset, false
}
