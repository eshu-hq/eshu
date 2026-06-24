// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package postgres

import (
	"bytes"
	"context"
	"encoding/binary"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/mediadoc"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestFactStoreUpsertFactsPersistsMediaTranscriptDocumentationFacts(t *testing.T) {
	t.Parallel()

	envelopes := mediaTranscriptDocumentationEnvelopes(t)
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
	payloadJSON, ok := db.execs[0].args[columnsPerFactRow+16].([]byte)
	if !ok {
		t.Fatalf("section payload arg type = %T, want []byte", db.execs[0].args[columnsPerFactRow+16])
	}
	payload := string(payloadJSON)
	for _, want := range []string{"media_transcript", "transcript_chunk", "start_millis", "speaker_label_hash", "Restore the service"} {
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
	if got, want := section.Payload["source_start_ref"], "time:00:00:01.000"; got != want {
		t.Fatalf("loaded section source_start_ref = %#v, want %#v", got, want)
	}
	if got, want := section.SourceRef.SourceURI, "docs/incident.wav"; got != want {
		t.Fatalf("loaded source URI = %q, want %q", got, want)
	}
	if sourceMetadata, ok := section.Payload["source_metadata"].(map[string]any); !ok ||
		sourceMetadata["format_family"] != "media_transcript" ||
		sourceMetadata["incident_media_source_class"] != "transcript_chunk" ||
		sourceMetadata["confidence_bucket"] != "high" {
		t.Fatalf("loaded section source_metadata = %#v, want media transcript metadata", section.Payload["source_metadata"])
	}
}

func mediaTranscriptDocumentationEnvelopes(t *testing.T) []facts.Envelope {
	t.Helper()

	result, err := mediadoc.Extract(context.Background(), mediadoc.Request{
		ScopeID:      "doc-source:git:repo-media",
		GenerationID: "gen-media-1",
		ObservedAt:   time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC),
		SourceSystem: "git",
		SourceURI:    "docs/incident.wav",
		SourceName:   "docs/incident.wav",
		SourceID:     "doc-source:git:repo-media",
		DocumentID:   "doc:git:repo-media:docs/incident.wav",
		ExternalID:   "docs/incident.wav",
		RevisionID:   "rev-1",
		CanonicalURI: "git://repo-media/docs/incident.wav",
		Title:        "Transcript fixture",
		Body:         encodeTestWAV(t, 3000),
		Engine: transcriptEngineFunc(func(context.Context, mediadoc.Media) (mediadoc.EngineResult, error) {
			return mediadoc.EngineResult{
				EngineName:    "synthetic-transcript",
				EngineVersion: "fixture",
				Language:      "en",
				Segments: []mediadoc.Segment{{
					SegmentID:    "restore",
					Text:         "Restore the service from the runbook.",
					StartMillis:  1000,
					EndMillis:    2500,
					Confidence:   0.92,
					SpeakerLabel: "speaker-a",
				}},
			}, nil
		}),
	})
	if err != nil {
		t.Fatalf("mediadoc.Extract() error = %v, want nil", err)
	}
	return result.Envelopes
}

type transcriptEngineFunc func(context.Context, mediadoc.Media) (mediadoc.EngineResult, error)

func (f transcriptEngineFunc) Transcribe(ctx context.Context, media mediadoc.Media) (mediadoc.EngineResult, error) {
	return f(ctx, media)
}

func encodeTestWAV(t *testing.T, durationMillis int) []byte {
	t.Helper()

	const (
		sampleRate    = 8000
		bitsPerSample = 16
		channels      = 1
	)
	bytesPerSample := bitsPerSample / 8
	byteRate := sampleRate * channels * bytesPerSample
	dataSize := byteRate * durationMillis / 1000
	var buffer bytes.Buffer
	writeString(t, &buffer, "RIFF")
	writeUint32(t, &buffer, uint32(36+dataSize))
	writeString(t, &buffer, "WAVE")
	writeString(t, &buffer, "fmt ")
	writeUint32(t, &buffer, 16)
	writeUint16(t, &buffer, 1)
	writeUint16(t, &buffer, channels)
	writeUint32(t, &buffer, sampleRate)
	writeUint32(t, &buffer, uint32(byteRate))
	writeUint16(t, &buffer, channels*bytesPerSample)
	writeUint16(t, &buffer, bitsPerSample)
	writeString(t, &buffer, "data")
	writeUint32(t, &buffer, uint32(dataSize))
	buffer.Write(make([]byte, dataSize))
	return buffer.Bytes()
}

func writeString(t *testing.T, buffer *bytes.Buffer, value string) {
	t.Helper()
	if _, err := buffer.WriteString(value); err != nil {
		t.Fatalf("WriteString(%q) error = %v, want nil", value, err)
	}
}

func writeUint16(t *testing.T, buffer *bytes.Buffer, value int) {
	t.Helper()
	if err := binary.Write(buffer, binary.LittleEndian, uint16(value)); err != nil {
		t.Fatalf("binary.Write(%d) error = %v, want nil", value, err)
	}
}

func writeUint32(t *testing.T, buffer *bytes.Buffer, value uint32) {
	t.Helper()
	if err := binary.Write(buffer, binary.LittleEndian, value); err != nil {
		t.Fatalf("binary.Write(%d) error = %v, want nil", value, err)
	}
}
