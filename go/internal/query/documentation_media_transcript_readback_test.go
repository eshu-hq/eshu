// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"bytes"
	"context"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/mediadoc"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestDocumentationHandlerListsMediaTranscriptSectionFactsWithMetadata(t *testing.T) {
	t.Parallel()

	row := queryMediaTranscriptFactRow(t)
	db := openContentReaderTestDB(t, []contentReaderQueryResult{{
		columns: []string{"payload"},
		rows:    [][]driver.Value{{row}},
		queryContains: []string{
			"fact_records.payload->>'source_id'",
			"fact_records.payload->>'document_id'",
			"LOWER(",
		},
	}})
	handler := &DocumentationHandler{
		Content: NewContentReader(db),
		Profile: ProfileProduction,
	}
	mux := http.NewServeMux()
	handler.Mount(mux)

	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v0/documentation/facts?source_id=doc-source:git:repo-media&document_id=doc:git:repo-media:docs/incident.wav&fact_kind=section&q=Restore&limit=1",
		nil,
	)
	req.Header.Set("Accept", EnvelopeMIMEType)
	w := httptest.NewRecorder()
	mux.ServeHTTP(w, req)

	if got, want := w.Code, http.StatusOK; got != want {
		t.Fatalf("status = %d, want %d; body = %s", got, want, w.Body.String())
	}

	var resp ResponseEnvelope
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("json.Unmarshal() error = %v, want nil", err)
	}
	data := resp.Data.(map[string]any)
	rows := data["facts"].([]any)
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	payload := rows[0].(map[string]any)["payload"].(map[string]any)
	if got, want := payload["source_start_ref"], "time:00:00:01.000"; got != want {
		t.Fatalf("source_start_ref = %#v, want %#v", got, want)
	}
	metadata := payload["source_metadata"].(map[string]any)
	if got, want := metadata["format_family"], "media_transcript"; got != want {
		t.Fatalf("format_family = %#v, want %#v", got, want)
	}
	if got, want := metadata["incident_media_source_class"], "transcript_chunk"; got != want {
		t.Fatalf("incident_media_source_class = %#v, want %#v", got, want)
	}
	if got, want := metadata["speaker_label_present"], "true"; got != want {
		t.Fatalf("speaker_label_present = %#v, want %#v", got, want)
	}
}

func queryMediaTranscriptFactRow(t *testing.T) []byte {
	t.Helper()

	envelopes := queryMediaTranscriptEnvelopes(t)
	for _, envelope := range envelopes {
		if envelope.FactKind == facts.DocumentationSectionFactKind {
			row := map[string]any{
				"fact_id":       envelope.FactID,
				"fact_kind":     envelope.FactKind,
				"scope_id":      envelope.ScopeID,
				"generation_id": envelope.GenerationID,
				"payload":       envelope.Payload,
			}
			encoded, err := json.Marshal(row)
			if err != nil {
				t.Fatalf("json.Marshal(media transcript fact row) error = %v, want nil", err)
			}
			return encoded
		}
	}
	t.Fatal("media transcript fixture did not emit a documentation section fact")
	return nil
}

func queryMediaTranscriptEnvelopes(t *testing.T) []facts.Envelope {
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
		CanonicalURI: "docs/incident.wav",
		Title:        "Transcript fixture",
		Body:         queryEncodeTestWAV(t, 3000),
		Engine: queryTranscriptEngineFunc(func(context.Context, mediadoc.Media) (mediadoc.EngineResult, error) {
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

type queryTranscriptEngineFunc func(context.Context, mediadoc.Media) (mediadoc.EngineResult, error)

func (f queryTranscriptEngineFunc) Transcribe(ctx context.Context, media mediadoc.Media) (mediadoc.EngineResult, error) {
	return f(ctx, media)
}

func queryEncodeTestWAV(t *testing.T, durationMillis int) []byte {
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
	queryWriteString(t, &buffer, "RIFF")
	queryWriteUint32(t, &buffer, uint32(36+dataSize))
	queryWriteString(t, &buffer, "WAVE")
	queryWriteString(t, &buffer, "fmt ")
	queryWriteUint32(t, &buffer, 16)
	queryWriteUint16(t, &buffer, 1)
	queryWriteUint16(t, &buffer, channels)
	queryWriteUint32(t, &buffer, sampleRate)
	queryWriteUint32(t, &buffer, uint32(byteRate))
	queryWriteUint16(t, &buffer, channels*bytesPerSample)
	queryWriteUint16(t, &buffer, bitsPerSample)
	queryWriteString(t, &buffer, "data")
	queryWriteUint32(t, &buffer, uint32(dataSize))
	buffer.Write(make([]byte, dataSize))
	return buffer.Bytes()
}

func queryWriteString(t *testing.T, buffer *bytes.Buffer, value string) {
	t.Helper()
	if _, err := buffer.WriteString(value); err != nil {
		t.Fatalf("WriteString(%q) error = %v, want nil", value, err)
	}
}

func queryWriteUint16(t *testing.T, buffer *bytes.Buffer, value int) {
	t.Helper()
	if err := binary.Write(buffer, binary.LittleEndian, uint16(value)); err != nil {
		t.Fatalf("binary.Write(%d) error = %v, want nil", value, err)
	}
}

func queryWriteUint32(t *testing.T, buffer *bytes.Buffer, value uint32) {
	t.Helper()
	if err := binary.Write(buffer, binary.LittleEndian, value); err != nil {
		t.Fatalf("binary.Write(%d) error = %v, want nil", value, err)
	}
}
