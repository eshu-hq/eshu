// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package imagepreflight

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
)

func TestPreflightAcceptsSafeImageMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		body       []byte
		wantFormat string
	}{
		{name: "png", sourceName: "architecture.png", body: encodePNG(t, 2, 1), wantFormat: FormatPNG},
		{name: "jpeg", sourceName: "dashboard.jpg", body: encodeJPEG(t, 2, 1), wantFormat: FormatJPEG},
		{name: "gif", sourceName: "flow.gif", body: encodeGIF(t, 1), wantFormat: FormatGIF},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tt.sourceName, bytes.NewReader(tt.body), int64(len(tt.body)), Options{})
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			if got := result.Format; got != tt.wantFormat {
				t.Fatalf("Format = %q, want %q", got, tt.wantFormat)
			}
			if !result.Safe {
				t.Fatalf("Safe = false, want true; warnings=%#v", result.Warnings)
			}
			if result.Width == 0 || result.Height == 0 || result.PixelCount == 0 {
				t.Fatalf("image dimensions were not counted: %#v", result)
			}
		})
	}
}

func TestPreflightClassifiesUnsupportedMalformedAndLimits(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		body       []byte
		options    Options
		wantClass  WarningClass
	}{
		{name: "unsupported_format", sourceName: "diagram.bmp", body: []byte("ignored"), wantClass: WarningUnsupportedFormat},
		{name: "unsupported_webp_codec", sourceName: "screenshot.webp", body: webpHeader(), wantClass: WarningUnsupportedCodec},
		{name: "empty_image", sourceName: "empty.png", body: nil, wantClass: WarningMalformedMedia},
		{name: "corrupt_png", sourceName: "broken.png", body: []byte("not an image"), wantClass: WarningMalformedMedia},
		{name: "oversized_source", sourceName: "large.png", body: encodePNG(t, 1, 1), options: Options{MaxSourceBytes: 4}, wantClass: WarningResourceLimitExceeded},
		{name: "pixel_limit", sourceName: "huge.png", body: encodePNG(t, 2, 2), options: Options{MaxPixels: 3}, wantClass: WarningResourceLimitExceeded},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result, err := Preflight(context.Background(), tt.sourceName, bytes.NewReader(tt.body), int64(len(tt.body)), tt.options)
			if err != nil {
				t.Fatalf("Preflight() error = %v, want nil", err)
			}
			assertWarning(t, result, tt.wantClass)
		})
	}
}

func TestPreflightClassifiesMetadataSensitiveAndExternalMarkers(t *testing.T) {
	t.Parallel()

	body := append(encodePNG(t, 1, 1), []byte("Exif GPSLatitude credential_marker https://example.invalid/ref")...)
	result, err := Preflight(context.Background(), "screenshot.png", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningMetadataRedacted)
	assertWarning(t, result, WarningSensitiveValueRedacted)
	assertWarning(t, result, WarningExternalReferenceSkipped)
	if result.MetadataRedactionCount == 0 || result.SensitiveValueCount == 0 || result.ExternalReferenceCount == 0 {
		t.Fatalf("expected bounded marker counts, got %#v", result)
	}
}

func TestPreflightClassifiesAnimatedGIFFirstFrameOnly(t *testing.T) {
	t.Parallel()

	body := encodeGIF(t, 2)
	result, err := Preflight(context.Background(), "flow.gif", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	assertWarning(t, result, WarningPartialExtraction)
	if result.FrameCount < 2 {
		t.Fatalf("FrameCount = %d, want animated GIF frame count", result.FrameCount)
	}
}

func TestPreflightClassifiesCanceledContextAsTimeout(t *testing.T) {
	t.Parallel()

	body := encodePNG(t, 1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := Preflight(ctx, "architecture.png", bytes.NewReader(body), int64(len(body)), Options{})
	if err == nil {
		t.Fatal("Preflight() error = nil, want canceled context error")
	}
	assertWarning(t, result, WarningTimeout)
}

func TestPreflightResultJSONOmitsSourceAndImageContent(t *testing.T) {
	t.Parallel()

	body := append(encodePNG(t, 1, 1), []byte("member-name-must-not-leak https://example.invalid/link credential_marker")...)
	result, err := Preflight(context.Background(), "private-source-name.png", bytes.NewReader(body), int64(len(body)), Options{})
	if err != nil {
		t.Fatalf("Preflight() error = %v, want nil", err)
	}
	encoded, err := json.Marshal(result)
	if err != nil {
		t.Fatalf("Marshal() error = %v, want nil", err)
	}
	jsonText := string(encoded)
	for _, disallowed := range []string{"member-name-must-not-leak", "private-source-name", "example.invalid", "credential_marker"} {
		if strings.Contains(jsonText, disallowed) {
			t.Fatalf("result JSON leaked %q: %s", disallowed, jsonText)
		}
	}
}

func encodePNG(t *testing.T, width int, height int) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, solidImage(width, height)); err != nil {
		t.Fatalf("png.Encode() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

func encodeJPEG(t *testing.T, width int, height int) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, solidImage(width, height), nil); err != nil {
		t.Fatalf("jpeg.Encode() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

func encodeGIF(t *testing.T, frames int) []byte {
	t.Helper()

	palette := []color.Color{color.Black, color.White}
	images := make([]*image.Paletted, 0, frames)
	delays := make([]int, 0, frames)
	for i := 0; i < frames; i++ {
		frame := image.NewPaletted(image.Rect(0, 0, 2, 1), palette)
		frame.SetColorIndex(i%2, 0, 1)
		images = append(images, frame)
		delays = append(delays, 1)
	}
	var buffer bytes.Buffer
	if err := gif.EncodeAll(&buffer, &gif.GIF{Image: images, Delay: delays}); err != nil {
		t.Fatalf("gif.EncodeAll() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

func solidImage(width int, height int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: 0x22, G: 0x44, B: 0x66, A: 0xff})
		}
	}
	return img
}

func webpHeader() []byte {
	return []byte("RIFF\x10\x00\x00\x00WEBPVP8 \x00\x00\x00\x00")
}

func assertWarning(t *testing.T, result Result, class WarningClass) {
	t.Helper()

	for _, warning := range result.Warnings {
		if warning.Class == class && warning.Count > 0 {
			return
		}
	}
	t.Fatalf("missing warning %q in %#v", class, result.Warnings)
}
