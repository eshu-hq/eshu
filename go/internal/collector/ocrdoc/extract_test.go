// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ocrdoc

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/imagepreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractEmitsOCRDocumentAndRegionSections(t *testing.T) {
	t.Parallel()

	body := encodePNG(t, 4, 2)
	engine := &fakeEngine{result: EngineResult{
		EngineName:    "synthetic-ocr",
		EngineVersion: "fixture",
		Language:      "en",
		Regions: []Region{{
			RegionID:   "title",
			Text:       "Architecture dashboard",
			Bounds:     Bounds{X: 0.10, Y: 0.20, Width: 0.70, Height: 0.15},
			Confidence: 0.92,
		}},
	}}

	result, err := Extract(context.Background(), testRequest("docs/architecture.png", body, engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	assertEngineImage(t, engine.lastImage, imagepreflight.FormatPNG, 4, 2, 0)

	document := payloadByKind(t, result.Envelopes, facts.DocumentationDocumentFactKind)
	if got, want := document["format"], "image_ocr"; got != want {
		t.Fatalf("document.format = %#v, want %#v", got, want)
	}
	if got, want := document["document_type"], "image"; got != want {
		t.Fatalf("document.document_type = %#v, want %#v", got, want)
	}
	metadata := stringMapValue(t, document, "source_metadata")
	for key, want := range map[string]string{
		"format_family":               "image_ocr",
		"incident_media_source_class": "ocr_region",
		"ocr_status":                  "completed",
		"ocr_region_count":            "1",
		"image_format":                imagepreflight.FormatPNG,
	} {
		if got := metadata[key]; got != want {
			t.Fatalf("document source_metadata[%q] = %q, want %q", key, got, want)
		}
	}
	if got := document["content_hash"]; got == "" {
		t.Fatalf("document.content_hash = %#v, want source hash", got)
	}

	section := payloadByKind(t, result.Envelopes, facts.DocumentationSectionFactKind)
	if got, want := section["content"], "Architecture dashboard"; got != want {
		t.Fatalf("section.content = %#v, want %#v", got, want)
	}
	if got, want := section["content_format"], "text/plain"; got != want {
		t.Fatalf("section.content_format = %#v, want %#v", got, want)
	}
	sectionMetadata := stringMapValue(t, section, "source_metadata")
	for key, want := range map[string]string{
		"incident_media_source_class": "ocr_region",
		"ocr_engine":                  "synthetic-ocr",
		"ocr_engine_version":          "fixture",
		"confidence_bucket":           "high",
		"bounds_x":                    "0.1000",
		"bounds_y":                    "0.2000",
		"bounds_width":                "0.7000",
		"bounds_height":               "0.1500",
		"source_hash":                 document["content_hash"].(string),
	} {
		if got := sectionMetadata[key]; got != want {
			t.Fatalf("section source_metadata[%q] = %q, want %q", key, got, want)
		}
	}

	for _, envelope := range result.Envelopes {
		switch envelope.FactKind {
		case facts.DocumentationEntityMentionFactKind, facts.DocumentationClaimCandidateFactKind:
			t.Fatalf("unexpected truth fact kind from OCR text: %s", envelope.FactKind)
		}
	}
}

func TestExtractRecordsSkippedImagesAsDocumentWarnings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		body       []byte
		options    Options
		wantStatus string
		wantClass  string
	}{
		{
			name:       "unsupported_webp",
			sourceName: "docs/screenshot.webp",
			body:       []byte("RIFF\x10\x00\x00\x00WEBPVP8 \x00\x00\x00\x00"),
			wantStatus: "skipped",
			wantClass:  string(imagepreflight.WarningUnsupportedCodec),
		},
		{
			name:       "malformed_media",
			sourceName: "docs/broken.png",
			body:       []byte("not an image"),
			wantStatus: "skipped",
			wantClass:  string(imagepreflight.WarningMalformedMedia),
		},
		{
			name:       "resource_limit",
			sourceName: "docs/huge.png",
			body:       encodePNG(t, 2, 2),
			options:    Options{Preflight: imagepreflight.Options{MaxSourceBytes: 4}},
			wantStatus: "skipped",
			wantClass:  string(imagepreflight.WarningResourceLimitExceeded),
		},
		{
			name:       "no_text",
			sourceName: "docs/empty.png",
			body:       encodePNG(t, 1, 1),
			wantStatus: "no_text",
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakeEngine{}
			result, err := Extract(context.Background(), testRequestWithOptions(tt.sourceName, tt.body, engine, tt.options))
			if err != nil {
				t.Fatalf("Extract() error = %v, want nil", err)
			}
			document := payloadByKind(t, result.Envelopes, facts.DocumentationDocumentFactKind)
			metadata := stringMapValue(t, document, "source_metadata")
			if got := metadata["ocr_status"]; got != tt.wantStatus {
				t.Fatalf("ocr_status = %q, want %q", got, tt.wantStatus)
			}
			if tt.wantClass != "" && !strings.Contains(metadata["warning"], tt.wantClass) {
				t.Fatalf("warning = %q, want class %q", metadata["warning"], tt.wantClass)
			}
			if tt.wantStatus == "skipped" && engine.calls != 0 {
				t.Fatalf("OCR engine calls = %d, want 0 for skipped preflight", engine.calls)
			}
			if got := countKind(result.Envelopes, facts.DocumentationSectionFactKind); got != 0 {
				t.Fatalf("section fact count = %d, want 0", got)
			}
		})
	}
}

func TestExtractUsesFirstFrameForAnimatedGIFWithWarning(t *testing.T) {
	t.Parallel()

	body := encodeGIF(t, 2)
	engine := &fakeEngine{result: EngineResult{Regions: []Region{{
		RegionID:   "first-frame",
		Text:       "Release checklist",
		Bounds:     Bounds{X: 0.05, Y: 0.05, Width: 0.90, Height: 0.30},
		Confidence: 0.80,
	}}}}

	result, err := Extract(context.Background(), testRequest("docs/flow.gif", body, engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	assertEngineImage(t, engine.lastImage, imagepreflight.FormatGIF, 2, 1, 0)
	document := payloadByKind(t, result.Envelopes, facts.DocumentationDocumentFactKind)
	metadata := stringMapValue(t, document, "source_metadata")
	if got, want := metadata["gif_frame_policy"], "first_frame"; got != want {
		t.Fatalf("gif_frame_policy = %q, want %q", got, want)
	}
	if !strings.Contains(metadata["warning"], string(imagepreflight.WarningPartialExtraction)) {
		t.Fatalf("warning = %q, want partial extraction", metadata["warning"])
	}
	if got := countKind(result.Envelopes, facts.DocumentationSectionFactKind); got != 1 {
		t.Fatalf("section fact count = %d, want 1", got)
	}
}

func TestExtractRedactsSensitiveOCRText(t *testing.T) {
	t.Parallel()

	body := encodeJPEG(t, 3, 1)
	engine := &fakeEngine{result: EngineResult{Regions: []Region{{
		RegionID:   "sensitive",
		Text:       "credential_marker visible label",
		Bounds:     Bounds{X: 0, Y: 0, Width: 1, Height: 1},
		Confidence: 0.99,
	}}}}

	result, err := Extract(context.Background(), testRequest("docs/secret-looking.jpg", body, engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	section := payloadByKind(t, result.Envelopes, facts.DocumentationSectionFactKind)
	if content := section["content"].(string); strings.Contains(content, "credential_marker") {
		t.Fatalf("section.content leaked sensitive OCR text: %q", content)
	}
	metadata := stringMapValue(t, section, "source_metadata")
	for key, want := range map[string]string{
		"redacted":        "true",
		"redaction_class": string(imagepreflight.WarningSensitiveValueRedacted),
	} {
		if got := metadata[key]; got != want {
			t.Fatalf("section source_metadata[%q] = %q, want %q", key, got, want)
		}
	}
	if !strings.Contains(metadata["warning"], string(imagepreflight.WarningSensitiveValueRedacted)) {
		t.Fatalf("warning = %q, want sensitive redaction", metadata["warning"])
	}
	if section["text_hash"] == "" || section["excerpt_hash"] == "" {
		t.Fatalf("expected hashes on redacted section: %#v", section)
	}
}

type fakeEngine struct {
	result    EngineResult
	err       error
	calls     int
	lastImage Image
}

func (f *fakeEngine) Recognize(ctx context.Context, image Image) (EngineResult, error) {
	f.calls++
	f.lastImage = image
	if f.result.EngineName == "" {
		f.result.EngineName = "synthetic-ocr"
	}
	if f.result.EngineVersion == "" {
		f.result.EngineVersion = "fixture"
	}
	return f.result, f.err
}

func testRequest(sourceName string, body []byte, engine Engine) Request {
	return testRequestWithOptions(sourceName, body, engine, Options{})
}

func testRequestWithOptions(sourceName string, body []byte, engine Engine, options Options) Request {
	return Request{
		ScopeID:      "doc-source:git:repo-ocr",
		GenerationID: "gen-ocr-1",
		ObservedAt:   time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC),
		SourceSystem: "git",
		SourceURI:    sourceName,
		SourceName:   sourceName,
		SourceID:     "doc-source:git:repo-ocr",
		DocumentID:   "doc:git:repo-ocr:" + sourceName,
		ExternalID:   sourceName,
		RevisionID:   "rev-1",
		CanonicalURI: "git://repo-ocr/" + sourceName,
		Title:        "OCR fixture",
		Body:         body,
		Engine:       engine,
		Options:      options,
	}
}

func payloadByKind(t *testing.T, envelopes []facts.Envelope, kind string) map[string]any {
	t.Helper()

	var matches []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			matches = append(matches, envelope.Payload)
		}
	}
	if len(matches) != 1 {
		t.Fatalf("fact kind %q count = %d, want 1", kind, len(matches))
	}
	return matches[0]
}

func countKind(envelopes []facts.Envelope, kind string) int {
	count := 0
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			count++
		}
	}
	return count
}

func stringMapValue(t *testing.T, row map[string]any, key string) map[string]string {
	t.Helper()

	raw, ok := row[key]
	if !ok {
		t.Fatalf("row missing %q: %#v", key, row)
	}
	values, ok := raw.(map[string]any)
	if !ok {
		t.Fatalf("row[%q] type = %T, want map[string]any", key, raw)
	}
	out := make(map[string]string, len(values))
	for k, v := range values {
		value, ok := v.(string)
		if !ok {
			t.Fatalf("row[%q][%q] type = %T, want string", key, k, v)
		}
		out[k] = value
	}
	return out
}

func assertEngineImage(t *testing.T, got Image, wantFormat string, wantWidth int, wantHeight int, wantFrame int) {
	t.Helper()

	if got.Format != wantFormat || got.Width != wantWidth || got.Height != wantHeight || got.FrameIndex != wantFrame {
		t.Fatalf(
			"engine image = {format:%q width:%d height:%d frame:%d}, want {%q %d %d %d}",
			got.Format,
			got.Width,
			got.Height,
			got.FrameIndex,
			wantFormat,
			wantWidth,
			wantHeight,
			wantFrame,
		)
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
