// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocr

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/preflight/picture"
	"github.com/eshu-hq/eshu/go/internal/facts/docs"
)

func TestLocalEngineRecognizesRenderedFixture(t *testing.T) {
	t.Parallel()

	body := renderTextPNG(t, "HI 42")
	engine := NewLocalEngine(LocalOptions{})

	result, err := engine.Recognize(context.Background(), Image{
		SourceName: "docs/fixture.png",
		Format:     picture.FormatPNG,
		Body:       body,
	})
	if err != nil {
		t.Fatalf("Recognize() error = %v, want nil", err)
	}
	if result.EngineName == "" || result.EngineVersion == "" {
		t.Fatalf("Recognize() result missing engine identity: %+v", result)
	}
	if result.Language == "" {
		t.Fatalf("Recognize() result missing language: %+v", result)
	}
	got := make([]string, 0, len(result.Regions))
	for _, region := range result.Regions {
		got = append(got, region.Text)
		if region.Confidence <= 0 || region.Confidence > 1 {
			t.Fatalf("region %q confidence = %v, want (0,1]", region.Text, region.Confidence)
		}
	}
	want := []string{"HI", "42"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("Recognize() regions = %q, want %q", got, want)
	}
	for _, region := range result.Regions {
		for _, bound := range []float64{region.Bounds.X, region.Bounds.Y, region.Bounds.Width, region.Bounds.Height} {
			if bound < 0 || bound > 1 {
				t.Fatalf("region %q has out-of-range bound %v: %+v", region.Text, bound, region.Bounds)
			}
		}
	}
}

func TestLocalEngineDeterministic(t *testing.T) {
	t.Parallel()

	body := renderTextPNG(t, "OCR 123")
	engine := NewLocalEngine(LocalOptions{})

	first, err := engine.Recognize(context.Background(), Image{SourceName: "docs/a.png", Format: picture.FormatPNG, Body: body})
	if err != nil {
		t.Fatalf("first Recognize() error = %v, want nil", err)
	}
	second, err := engine.Recognize(context.Background(), Image{SourceName: "docs/a.png", Format: picture.FormatPNG, Body: body})
	if err != nil {
		t.Fatalf("second Recognize() error = %v, want nil", err)
	}
	if len(first.Regions) == 0 {
		t.Fatalf("first Recognize() regions = 0, want > 0")
	}
	if len(first.Regions) != len(second.Regions) {
		t.Fatalf("region count changed between runs: %d vs %d", len(first.Regions), len(second.Regions))
	}
	for i := range first.Regions {
		if first.Regions[i] != second.Regions[i] {
			t.Fatalf("region %d differs between runs: %+v vs %+v", i, first.Regions[i], second.Regions[i])
		}
	}
}

func TestLocalEngineDecodesJPEGAndFirstFrameGIF(t *testing.T) {
	t.Parallel()

	engine := NewLocalEngine(LocalOptions{})
	for name, body := range map[string][]byte{
		"jpeg": encodeJPEG(t, 8, 8),
		"gif":  encodeGIF(t, 2),
	} {
		result, err := engine.Recognize(context.Background(), Image{SourceName: "docs/codec." + name, Body: body})
		if err != nil {
			t.Fatalf("Recognize(%s) error = %v, want nil", name, err)
		}
		if result.EngineName == "" {
			t.Fatalf("Recognize(%s) result missing engine identity: %+v", name, result)
		}
	}
}

func TestLocalEngineRejectsOverCapInput(t *testing.T) {
	t.Parallel()

	body := renderTextPNG(t, "HI")
	engine := NewLocalEngine(LocalOptions{MaxInputBytes: 8})

	if _, err := engine.Recognize(context.Background(), Image{SourceName: "docs/big.png", Format: picture.FormatPNG, Body: body}); err == nil {
		t.Fatalf("Recognize() with over-cap body error = nil, want non-nil")
	}

	tiny := NewLocalEngine(LocalOptions{MaxPixels: 4})
	if _, err := tiny.Recognize(context.Background(), Image{SourceName: "docs/big.png", Format: picture.FormatPNG, Body: body}); err == nil {
		t.Fatalf("Recognize() with over-cap pixels error = nil, want non-nil")
	}

	if _, err := engine.Recognize(context.Background(), Image{SourceName: "docs/empty.png", Format: picture.FormatPNG}); err == nil {
		t.Fatalf("Recognize() with empty body error = nil, want non-nil")
	}

	capped := NewLocalEngine(LocalOptions{MaxRegions: 1})
	twoWords := renderTextPNG(t, "AB CD")
	if _, err := capped.Recognize(context.Background(), Image{SourceName: "docs/two.png", Format: picture.FormatPNG, Body: twoWords}); err == nil {
		t.Fatalf("Recognize() with over-cap regions error = nil, want non-nil")
	}
}

func TestLocalEngineRespectsCancellation(t *testing.T) {
	t.Parallel()

	body := renderTextPNG(t, "HI")
	engine := NewLocalEngine(LocalOptions{})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := engine.Recognize(ctx, Image{SourceName: "docs/cancel.png", Format: picture.FormatPNG, Body: body}); err == nil {
		t.Fatalf("Recognize() with canceled context error = nil, want non-nil")
	}

	expired := NewLocalEngine(LocalOptions{Timeout: time.Nanosecond})
	if _, err := expired.Recognize(context.Background(), Image{SourceName: "docs/timeout.png", Format: picture.FormatPNG, Body: body}); err == nil {
		t.Fatalf("Recognize() with expired timeout error = nil, want non-nil")
	}
}

func TestLocalEngineSkipsUnknownGlyphs(t *testing.T) {
	t.Parallel()

	body := solidBlockPNG(t, 40, 24)
	engine := NewLocalEngine(LocalOptions{})

	result, err := engine.Recognize(context.Background(), Image{SourceName: "docs/blob.png", Format: picture.FormatPNG, Body: body})
	if err != nil {
		t.Fatalf("Recognize() error = %v, want nil", err)
	}
	if len(result.Regions) != 0 {
		t.Fatalf("Recognize() regions = %v, want none for a template-free blob", result.Regions)
	}
}

func BenchmarkLocalEngineRecognize(b *testing.B) {
	body := renderTextPNG(b, "OCR 123")
	engine := NewLocalEngine(LocalOptions{})
	img := Image{SourceName: "docs/bench.png", Format: picture.FormatPNG, Body: body}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := engine.Recognize(context.Background(), img); err != nil {
			b.Fatalf("Recognize() error = %v, want nil", err)
		}
	}
}

// fixtureTB is the minimal testing surface shared by tests and benchmarks
// that render image fixtures.
type fixtureTB interface {
	Helper()
	Fatalf(string, ...any)
}

// renderTextPNG draws text with the engine template set at a fixed scale so
// recognition tests exercise segmentation and matching instead of the font
// data itself. Glyph advance is one cell; words split on wide gaps.
func renderTextPNG(t fixtureTB, text string) []byte {
	t.Helper()

	const scale = 4
	const cell = scale
	const glyphAdvance = 6 * cell
	const spaceAdvance = 3 * glyphAdvance
	const pad = 2 * cell

	width := 2 * pad
	for _, glyph := range text {
		if glyph == ' ' {
			width += spaceAdvance
			continue
		}
		if _, ok := localFontRows[glyph]; !ok {
			t.Fatalf("renderTextPNG() unsupported glyph %q", glyph)
		}
		width += glyphAdvance
	}
	height := 7*cell + 2*pad

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.White)
		}
	}
	x := pad
	for _, glyph := range text {
		if glyph == ' ' {
			x += spaceAdvance
			continue
		}
		cells := localFontCells(glyph)
		for cy := 0; cy < 7; cy++ {
			for cx := 0; cx < 5; cx++ {
				if !cells[cy][cx] {
					continue
				}
				for dy := 0; dy < cell; dy++ {
					for dx := 0; dx < cell; dx++ {
						img.Set(x+cx*cell+dx, pad+cy*cell+dy, color.Black)
					}
				}
			}
		}
		x += glyphAdvance
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("png.Encode() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

// solidBlockPNG returns an image with no template resemblance: one solid
// ink rectangle that scores below every template.
func solidBlockPNG(t fixtureTB, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.White)
		}
	}
	for y := height / 4; y < 3*height/4; y++ {
		for x := width / 4; x < 3*width/4; x++ {
			img.Set(x, y, color.Black)
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("png.Encode() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

func TestExtractWithLocalEngineRedactsRecognizedPII(t *testing.T) {
	t.Parallel()

	body := renderTextPNG(t, "HI USER@EXAMPLE.COM")
	engine := NewLocalEngine(LocalOptions{})

	probe, err := engine.Recognize(context.Background(), Image{SourceName: "docs/pii.png", Format: picture.FormatPNG, Body: body})
	if err != nil {
		t.Fatalf("probe Recognize() error = %v, want nil", err)
	}
	joined := ""
	for _, region := range probe.Regions {
		joined += region.Text + " "
	}
	if !strings.Contains(joined, "@") {
		t.Fatalf("probe regions %q do not carry the PII marker, redaction proof is vacuous", joined)
	}

	result, err := Extract(context.Background(), testRequestWithOptions("docs/pii.png", body, engine, Options{}))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	sections, redacted := 0, 0
	for _, envelope := range result.Envelopes {
		if envelope.FactKind != docs.SectionFactKind {
			continue
		}
		sections++
		content, _ := envelope.Payload["content"].(string)
		if strings.Contains(content, "@") {
			t.Fatalf("section.content leaked recognized PII: %q", content)
		}
		metadata, _ := envelope.Payload["source_metadata"].(map[string]any)
		if metadata["redacted"] != "true" {
			continue
		}
		redacted++
		if metadata["redaction_class"] != string(picture.WarningSensitiveValueRedacted) {
			t.Fatalf("redaction_class = %v, want %q", metadata["redaction_class"], string(picture.WarningSensitiveValueRedacted))
		}
	}
	if sections == 0 {
		t.Fatalf("section fact count = 0, want > 0 for PII proof")
	}
	if redacted == 0 {
		t.Fatalf("redacted section count = 0, want the recognized email redacted before facts")
	}
}
