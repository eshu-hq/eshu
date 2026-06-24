// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mcp

import (
	"bytes"
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/mediadoc"
	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/query"
)

func TestDispatchToolListDocumentationFactsPreservesMediaTranscriptReadback(t *testing.T) {
	t.Parallel()

	db := openMCPDocumentationMediaTranscriptTestDB(t, mcpDocumentationMediaTranscriptFactRow(t))
	mux := http.NewServeMux()
	handler := &query.DocumentationHandler{
		Content: query.NewContentReader(db),
		Profile: query.ProfileProduction,
	}
	handler.Mount(mux)

	result, err := dispatchTool(
		context.Background(),
		mux,
		"list_documentation_facts",
		map[string]any{
			"fact_kind":   "documentation_section",
			"source_id":   "doc-source:git:repo-media",
			"document_id": "doc:git:repo-media:docs/incident.wav",
			"section_id":  "transcript:restore",
			"q":           "Restore",
			"limit":       float64(1),
		},
		"",
		slog.New(slog.NewTextHandler(io.Discard, nil)),
	)
	if err != nil {
		t.Fatalf("dispatchTool() error = %v, want nil", err)
	}
	if result.Envelope == nil {
		t.Fatal("dispatchTool() envelope is nil, want documentation facts envelope")
	}
	data := mcpEnvelopeData(t, result)
	rows := mcpMapSliceValue(data, "facts")
	if got, want := len(rows), 1; got != want {
		t.Fatalf("len(facts) = %d, want %d", got, want)
	}
	payload := mcpMapValue(rows[0], "payload")
	metadata := mcpMapValue(payload, "source_metadata")
	if got, want := query.StringVal(metadata, "format_family"), "media_transcript"; got != want {
		t.Fatalf("format_family = %q, want %q", got, want)
	}
	if got, want := query.StringVal(metadata, "incident_media_source_class"), "transcript_chunk"; got != want {
		t.Fatalf("incident_media_source_class = %q, want %q", got, want)
	}
	if got, want := query.StringVal(metadata, "speaker_label_present"), "true"; got != want {
		t.Fatalf("speaker_label_present = %q, want %q", got, want)
	}
}

func openMCPDocumentationMediaTranscriptTestDB(t *testing.T, row []byte) *sql.DB {
	t.Helper()

	name := fmt.Sprintf("mcp-doc-media-transcript-test-%d", atomic.AddUint64(&mcpDocumentationMediaTranscriptDriverSeq, 1))
	sql.Register(name, mcpDocumentationMediaTranscriptDriver{row: row})
	db, err := sql.Open(name, "")
	if err != nil {
		t.Fatalf("sql.Open() error = %v, want nil", err)
	}
	db.SetMaxOpenConns(1)
	t.Cleanup(func() {
		_ = db.Close()
	})
	return db
}

var mcpDocumentationMediaTranscriptDriverSeq uint64

type mcpDocumentationMediaTranscriptDriver struct {
	row []byte
}

func (d mcpDocumentationMediaTranscriptDriver) Open(string) (driver.Conn, error) {
	return &mcpDocumentationMediaTranscriptConn{row: d.row}, nil
}

type mcpDocumentationMediaTranscriptConn struct {
	row []byte
}

func (c *mcpDocumentationMediaTranscriptConn) Prepare(string) (driver.Stmt, error) {
	return nil, fmt.Errorf("Prepare not implemented")
}

func (c *mcpDocumentationMediaTranscriptConn) Close() error {
	return nil
}

func (c *mcpDocumentationMediaTranscriptConn) Begin() (driver.Tx, error) {
	return nil, fmt.Errorf("Begin not implemented")
}

func (c *mcpDocumentationMediaTranscriptConn) QueryContext(_ context.Context, queryText string, _ []driver.NamedValue) (driver.Rows, error) {
	for _, want := range []string{
		"FROM fact_records",
		"fact_records.payload->>'source_id'",
		"fact_records.payload->>'document_id'",
		"fact_records.payload->>'section_id'",
		"LOWER(",
	} {
		if !strings.Contains(queryText, want) {
			return nil, fmt.Errorf("documentation media transcript query missing %q: %s", want, queryText)
		}
	}
	return &mcpDocumentationMediaTranscriptRows{row: c.row}, nil
}

var _ driver.QueryerContext = (*mcpDocumentationMediaTranscriptConn)(nil)

type mcpDocumentationMediaTranscriptRows struct {
	row  []byte
	done bool
}

func (r *mcpDocumentationMediaTranscriptRows) Columns() []string {
	return []string{"payload"}
}

func (r *mcpDocumentationMediaTranscriptRows) Close() error {
	return nil
}

func (r *mcpDocumentationMediaTranscriptRows) Next(dest []driver.Value) error {
	if r.done {
		return io.EOF
	}
	dest[0] = r.row
	r.done = true
	return nil
}

func mcpDocumentationMediaTranscriptFactRow(t *testing.T) []byte {
	t.Helper()

	envelopes := mcpDocumentationMediaTranscriptEnvelopes(t)
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

func mcpDocumentationMediaTranscriptEnvelopes(t *testing.T) []facts.Envelope {
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
		Body:         mcpDocumentationMediaTranscriptWAV(t, 3000),
		Engine: mcpDocumentationMediaTranscriptEngineFunc(func(context.Context, mediadoc.Media) (mediadoc.EngineResult, error) {
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

type mcpDocumentationMediaTranscriptEngineFunc func(context.Context, mediadoc.Media) (mediadoc.EngineResult, error)

func (f mcpDocumentationMediaTranscriptEngineFunc) Transcribe(ctx context.Context, media mediadoc.Media) (mediadoc.EngineResult, error) {
	return f(ctx, media)
}

func mcpDocumentationMediaTranscriptWAV(t *testing.T, durationMillis int) []byte {
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
	mcpDocumentationMediaTranscriptWriteString(t, &buffer, "RIFF")
	mcpDocumentationMediaTranscriptWriteUint32(t, &buffer, uint32(36+dataSize))
	mcpDocumentationMediaTranscriptWriteString(t, &buffer, "WAVE")
	mcpDocumentationMediaTranscriptWriteString(t, &buffer, "fmt ")
	mcpDocumentationMediaTranscriptWriteUint32(t, &buffer, 16)
	mcpDocumentationMediaTranscriptWriteUint16(t, &buffer, 1)
	mcpDocumentationMediaTranscriptWriteUint16(t, &buffer, channels)
	mcpDocumentationMediaTranscriptWriteUint32(t, &buffer, sampleRate)
	mcpDocumentationMediaTranscriptWriteUint32(t, &buffer, uint32(byteRate))
	mcpDocumentationMediaTranscriptWriteUint16(t, &buffer, channels*bytesPerSample)
	mcpDocumentationMediaTranscriptWriteUint16(t, &buffer, bitsPerSample)
	mcpDocumentationMediaTranscriptWriteString(t, &buffer, "data")
	mcpDocumentationMediaTranscriptWriteUint32(t, &buffer, uint32(dataSize))
	buffer.Write(make([]byte, dataSize))
	return buffer.Bytes()
}

func mcpDocumentationMediaTranscriptWriteString(t *testing.T, buffer *bytes.Buffer, value string) {
	t.Helper()
	if _, err := buffer.WriteString(value); err != nil {
		t.Fatalf("WriteString(%q) error = %v, want nil", value, err)
	}
}

func mcpDocumentationMediaTranscriptWriteUint16(t *testing.T, buffer *bytes.Buffer, value int) {
	t.Helper()
	if err := binary.Write(buffer, binary.LittleEndian, uint16(value)); err != nil {
		t.Fatalf("binary.Write(%d) error = %v, want nil", value, err)
	}
}

func mcpDocumentationMediaTranscriptWriteUint32(t *testing.T, buffer *bytes.Buffer, value uint32) {
	t.Helper()
	if err := binary.Write(buffer, binary.LittleEndian, value); err != nil {
		t.Fatalf("binary.Write(%d) error = %v, want nil", value, err)
	}
}
