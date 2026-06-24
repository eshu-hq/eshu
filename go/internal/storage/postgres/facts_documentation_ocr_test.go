// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/ocrdoc"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestFactStoreUpsertFactsPersistsOCRDocumentationFacts(t *testing.T) {
	t.Parallel()

	envelopes := ocrDocumentationEnvelopes(t)
	db := &fakeExecQueryer{}
	store := NewFactStore(db)

	if err := store.UpsertFacts(context.Background(), envelopes); err != nil {
		t.Fatalf("UpsertFacts() error = %v, want nil", err)
	}

	if got, want := len(db.execs), 1; got != want {
		t.Fatalf("exec count = %d, want %d", got, want)
	}
	if got, want := len(db.execs[0].args), columnsPerFactRow*len(envelopes); got != want {
		t.Fatalf("arg count = %d, want %d", got, want)
	}
	if got, want := db.execs[0].args[3], facts.DocumentationDocumentFactKind; got != want {
		t.Fatalf("first fact_kind arg = %q, want %q", got, want)
	}
	if got, want := db.execs[0].args[columnsPerFactRow+3], facts.DocumentationSectionFactKind; got != want {
		t.Fatalf("second fact_kind arg = %q, want %q", got, want)
	}
	if got, want := db.execs[0].args[columnsPerFactRow+5], facts.DocumentationSectionFactSchemaVersion; got != want {
		t.Fatalf("section schema_version arg = %q, want %q", got, want)
	}
	payloadJSON, ok := db.execs[0].args[columnsPerFactRow+16].([]byte)
	if !ok {
		t.Fatalf("section payload arg type = %T, want []byte", db.execs[0].args[columnsPerFactRow+16])
	}
	payload := string(payloadJSON)
	for _, want := range []string{"image_ocr", "bounds_x", "confidence_bucket", "Architecture dashboard"} {
		if !strings.Contains(payload, want) {
			t.Fatalf("section payload missing %q: %s", want, payload)
		}
	}

	db.queryResponses = []queueFakeRows{{rows: factRowsFromEnvelopes(t, envelopes)}}
	loaded, err := store.ListFactsByKind(context.Background(), envelopes[0].ScopeID, envelopes[0].GenerationID, []string{
		facts.DocumentationDocumentFactKind,
		facts.DocumentationSectionFactKind,
	})
	if err != nil {
		t.Fatalf("ListFactsByKind() error = %v, want nil", err)
	}
	if got, want := len(loaded), len(envelopes); got != want {
		t.Fatalf("ListFactsByKind() len = %d, want %d", got, want)
	}
	section := loaded[1]
	if got, want := section.FactKind, facts.DocumentationSectionFactKind; got != want {
		t.Fatalf("loaded section FactKind = %q, want %q", got, want)
	}
	if got, want := section.Payload["content"], "Architecture dashboard"; got != want {
		t.Fatalf("loaded section content = %#v, want %#v", got, want)
	}
	if got, want := section.SourceRef.SourceURI, "docs/architecture.png"; got != want {
		t.Fatalf("loaded source URI = %q, want %q", got, want)
	}
	if sourceMetadata, ok := section.Payload["source_metadata"].(map[string]any); !ok || sourceMetadata["confidence_bucket"] != "high" {
		t.Fatalf("loaded section source_metadata = %#v, want confidence_bucket=high", section.Payload["source_metadata"])
	}
}

func ocrDocumentationEnvelopes(t *testing.T) []facts.Envelope {
	t.Helper()

	body := encodeOCRPNG(t)
	result, err := ocrdoc.Extract(context.Background(), ocrdoc.Request{
		ScopeID:      "doc-source:git:repo-ocr",
		GenerationID: "gen-ocr-1",
		ObservedAt:   time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC),
		SourceSystem: "git",
		SourceURI:    "docs/architecture.png",
		SourceName:   "docs/architecture.png",
		SourceID:     "doc-source:git:repo-ocr",
		DocumentID:   "doc:git:repo-ocr:docs/architecture.png",
		ExternalID:   "docs/architecture.png",
		RevisionID:   "rev-1",
		CanonicalURI: "git://repo-ocr/docs/architecture.png",
		Title:        "OCR fixture",
		Body:         body,
		Engine: ocrEngineFunc(func(context.Context, ocrdoc.Image) (ocrdoc.EngineResult, error) {
			return ocrdoc.EngineResult{
				EngineName:    "synthetic-ocr",
				EngineVersion: "fixture",
				Language:      "en",
				Regions: []ocrdoc.Region{{
					RegionID:   "title",
					Text:       "Architecture dashboard",
					Bounds:     ocrdoc.Bounds{X: 0.10, Y: 0.20, Width: 0.70, Height: 0.15},
					Confidence: 0.92,
				}},
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("ocrdoc.Extract() error = %v, want nil", err)
	}
	return result.Envelopes
}

type ocrEngineFunc func(context.Context, ocrdoc.Image) (ocrdoc.EngineResult, error)

func (f ocrEngineFunc) Recognize(ctx context.Context, image ocrdoc.Image) (ocrdoc.EngineResult, error) {
	return f(ctx, image)
}

func encodeOCRPNG(t *testing.T) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	for y := 0; y < 2; y++ {
		for x := 0; x < 4; x++ {
			img.Set(x, y, color.RGBA{R: 0x22, G: 0x44, B: 0x66, A: 0xff})
		}
	}
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("png.Encode() error = %v, want nil", err)
	}
	return buffer.Bytes()
}

func factRowsFromEnvelopes(t *testing.T, envelopes []facts.Envelope) [][]any {
	t.Helper()

	rows := make([][]any, 0, len(envelopes))
	for _, envelope := range envelopes {
		payload, err := marshalPayload(envelope.Payload)
		if err != nil {
			t.Fatalf("marshalPayload() error = %v, want nil", err)
		}
		rows = append(rows, []any{
			envelope.FactID,
			envelope.ScopeID,
			envelope.GenerationID,
			envelope.FactKind,
			envelope.StableFactKey,
			envelope.SchemaVersion,
			envelope.CollectorKind,
			envelope.FencingToken,
			envelope.SourceConfidence,
			envelope.SourceRef.SourceSystem,
			envelope.SourceRef.FactKey,
			envelope.SourceRef.SourceURI,
			envelope.SourceRef.SourceRecordID,
			envelope.ObservedAt.UTC(),
			envelope.IsTombstone,
			payload,
		})
	}
	return rows
}
