// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package mediadoc

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/eshu-hq/eshu/go/internal/collector/mediapreflight"
	"github.com/eshu-hq/eshu/go/internal/facts"
)

func TestExtractEmitsTranscriptDocumentAndTimestampSections(t *testing.T) {
	t.Parallel()

	body := encodeTestWAV(t, 5000)
	engine := &fakeTranscriptEngine{result: EngineResult{
		EngineName:    "synthetic-transcript",
		EngineVersion: "fixture",
		Language:      "en",
		Segments: []Segment{{
			SegmentID:    "restore",
			Text:         "Restore the service from the runbook.",
			StartMillis:  1200,
			EndMillis:    3450,
			Confidence:   0.91,
			SpeakerLabel: "speaker-a",
		}},
	}}

	result, err := Extract(context.Background(), testRequest("docs/incident.wav", body, engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	if engine.calls != 1 {
		t.Fatalf("Transcribe() calls = %d, want 1", engine.calls)
	}
	if got, want := engine.lastMedia.Format, mediapreflight.FormatWAV; got != want {
		t.Fatalf("Media.Format = %q, want %q", got, want)
	}
	if engine.lastMedia.DurationMillis <= 0 {
		t.Fatalf("Media.DurationMillis = %d, want positive duration", engine.lastMedia.DurationMillis)
	}

	document := payloadByKind(t, result.Envelopes, facts.DocumentationDocumentFactKind)
	if got, want := document["format"], "media_transcript"; got != want {
		t.Fatalf("document.format = %#v, want %#v", got, want)
	}
	if got, want := document["document_type"], "media"; got != want {
		t.Fatalf("document.document_type = %#v, want %#v", got, want)
	}
	metadata := stringMapValue(t, document, "source_metadata")
	for key, want := range map[string]string{
		"format_family":               "media_transcript",
		"incident_media_source_class": "transcript_chunk",
		"transcript_status":           "completed",
		"transcript_segment_count":    "1",
		"media_format":                mediapreflight.FormatWAV,
	} {
		if got := metadata[key]; got != want {
			t.Fatalf("document source_metadata[%q] = %q, want %q", key, got, want)
		}
	}
	if got := document["content_hash"]; got == "" {
		t.Fatalf("document.content_hash = %#v, want source hash", got)
	}

	section := payloadByKind(t, result.Envelopes, facts.DocumentationSectionFactKind)
	if got, want := section["content"], "Restore the service from the runbook."; got != want {
		t.Fatalf("section.content = %#v, want %#v", got, want)
	}
	if got, want := section["source_start_ref"], "time:00:00:01.200"; got != want {
		t.Fatalf("section.source_start_ref = %#v, want %#v", got, want)
	}
	if got, want := section["source_end_ref"], "time:00:00:03.450"; got != want {
		t.Fatalf("section.source_end_ref = %#v, want %#v", got, want)
	}
	sectionMetadata := stringMapValue(t, section, "source_metadata")
	for key, want := range map[string]string{
		"incident_media_source_class": "transcript_chunk",
		"transcript_engine":           "synthetic-transcript",
		"transcript_engine_version":   "fixture",
		"confidence_bucket":           "high",
		"start_millis":                "1200",
		"end_millis":                  "3450",
		"speaker_label_present":       "true",
		"source_hash":                 document["content_hash"].(string),
	} {
		if got := sectionMetadata[key]; got != want {
			t.Fatalf("section source_metadata[%q] = %q, want %q", key, got, want)
		}
	}
	if sectionMetadata["speaker_label_hash"] == "" {
		t.Fatal("speaker_label_hash = empty, want opaque speaker label hash")
	}

	for _, envelope := range result.Envelopes {
		switch envelope.FactKind {
		case facts.DocumentationEntityMentionFactKind, facts.DocumentationClaimCandidateFactKind:
			t.Fatalf("unexpected truth fact kind from transcript text: %s", envelope.FactKind)
		}
	}
}

func TestExtractRecordsSkippedMediaAsDocumentWarnings(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		sourceName string
		body       []byte
		options    Options
		wantClass  string
	}{
		{
			name:       "unsupported_codec",
			sourceName: "docs/incident.mp3",
			body:       []byte("ID3 unsupported local transcript codec"),
			wantClass:  string(mediapreflight.WarningUnsupportedCodec),
		},
		{
			name:       "malformed_wav",
			sourceName: "docs/broken.wav",
			body:       []byte("not media"),
			wantClass:  string(mediapreflight.WarningMalformedMedia),
		},
		{
			name:       "resource_limit",
			sourceName: "docs/huge.wav",
			body:       encodeTestWAV(t, 5000),
			options:    Options{Preflight: mediapreflight.Options{MaxSourceBytes: 4}},
			wantClass:  string(mediapreflight.WarningResourceLimitExceeded),
		},
		{
			name:       "duration_limit",
			sourceName: "docs/long.wav",
			body:       encodeTestWAV(t, 5000),
			options:    Options{Preflight: mediapreflight.Options{MaxDurationMillis: 100}},
			wantClass:  string(mediapreflight.WarningResourceLimitExceeded),
		},
	}
	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakeTranscriptEngine{}
			result, err := Extract(context.Background(), testRequestWithOptions(tt.sourceName, tt.body, engine, tt.options))
			if err != nil {
				t.Fatalf("Extract() error = %v, want nil", err)
			}
			document := payloadByKind(t, result.Envelopes, facts.DocumentationDocumentFactKind)
			metadata := stringMapValue(t, document, "source_metadata")
			if got := metadata["transcript_status"]; got != "skipped" {
				t.Fatalf("transcript_status = %q, want skipped", got)
			}
			if !strings.Contains(metadata["warning"], tt.wantClass) {
				t.Fatalf("warning = %q, want class %q", metadata["warning"], tt.wantClass)
			}
			if engine.calls != 0 {
				t.Fatalf("Transcribe() calls = %d, want 0 for skipped preflight", engine.calls)
			}
			if got := countKind(result.Envelopes, facts.DocumentationSectionFactKind); got != 0 {
				t.Fatalf("section fact count = %d, want 0", got)
			}
		})
	}
}

func TestExtractRecordsNoTranscriptText(t *testing.T) {
	t.Parallel()

	body := encodeTestWAV(t, 1000)
	engine := &fakeTranscriptEngine{result: EngineResult{
		EngineName: "synthetic-transcript",
		Segments: []Segment{{
			SegmentID:   "empty",
			Text:        "   ",
			StartMillis: 0,
			EndMillis:   500,
		}},
	}}

	result, err := Extract(context.Background(), testRequest("docs/silence.wav", body, engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	document := payloadByKind(t, result.Envelopes, facts.DocumentationDocumentFactKind)
	metadata := stringMapValue(t, document, "source_metadata")
	if got := metadata["transcript_status"]; got != "no_text" {
		t.Fatalf("transcript_status = %q, want no_text", got)
	}
	if got := countKind(result.Envelopes, facts.DocumentationSectionFactKind); got != 0 {
		t.Fatalf("section fact count = %d, want 0", got)
	}
}

func TestExtractRedactsSensitiveTranscriptText(t *testing.T) {
	t.Parallel()

	body := encodeTestWAV(t, 1000)
	engine := &fakeTranscriptEngine{result: EngineResult{Segments: []Segment{{
		SegmentID:   "sensitive",
		Text:        "credential_marker was spoken aloud",
		StartMillis: 0,
		EndMillis:   900,
		Confidence:  0.98,
	}}}}

	result, err := Extract(context.Background(), testRequest("docs/secret-looking.wav", body, engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	section := payloadByKind(t, result.Envelopes, facts.DocumentationSectionFactKind)
	if content := section["content"].(string); strings.Contains(content, "credential_marker") {
		t.Fatalf("section.content leaked sensitive transcript text: %q", content)
	}
	metadata := stringMapValue(t, section, "source_metadata")
	for key, want := range map[string]string{
		"redacted":        "true",
		"redaction_class": string(mediapreflight.WarningSensitiveValueRedacted),
	} {
		if got := metadata[key]; got != want {
			t.Fatalf("section source_metadata[%q] = %q, want %q", key, got, want)
		}
	}
	if !strings.Contains(metadata["warning"], string(mediapreflight.WarningSensitiveValueRedacted)) {
		t.Fatalf("warning = %q, want sensitive redaction", metadata["warning"])
	}
	if section["text_hash"] == "" || section["excerpt_hash"] == "" {
		t.Fatalf("expected hashes on redacted section: %#v", section)
	}
}

func TestExtractRedactsUnsafeMediaSourceIdentities(t *testing.T) {
	t.Parallel()

	body := encodeTestWAV(t, 1000)
	engine := &fakeTranscriptEngine{result: EngineResult{Segments: []Segment{{
		SegmentID:   "safe",
		Text:        "Visible transcript text",
		StartMillis: 0,
		EndMillis:   800,
		Confidence:  0.95,
	}}}}
	req := testRequest("/Users/example/private.wav", body, engine)
	req.SourceURI = "file:///Users/example/private.wav"
	req.SourceName = "file:///Users/example/private.wav"
	req.CanonicalURI = "git://private.example.invalid/Users/example/private.wav"
	req.DocumentID = "doc:git:/Users/example/private.wav"
	req.ExternalID = "media:/Users/example/private.wav"
	req.SourceRecordID = "record:/Users/example/private.wav"

	result, err := Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	document := payloadByKind(t, result.Envelopes, facts.DocumentationDocumentFactKind)
	encoded, err := json.Marshal(document)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	documentJSON := string(encoded)
	for _, disallowed := range []string{"private.example.invalid", "/Users/example/private.wav", "file:///Users"} {
		if strings.Contains(documentJSON, disallowed) {
			t.Fatalf("document payload leaked %q: %s", disallowed, documentJSON)
		}
	}
	metadata := stringMapValue(t, document, "source_metadata")
	for _, key := range []string{"canonical_uri_redacted", "document_id_redacted", "external_id_redacted"} {
		if got, want := metadata[key], "true"; got != want {
			t.Fatalf("source_metadata[%q] = %q, want %q", key, got, want)
		}
	}
	sourceRef := result.Envelopes[0].SourceRef
	for _, disallowed := range []string{"private.example.invalid", "/Users/example/private.wav", "file:///Users"} {
		if strings.Contains(sourceRef.SourceURI, disallowed) || strings.Contains(sourceRef.SourceRecordID, disallowed) {
			t.Fatalf("SourceRef leaked %q: %#v", disallowed, sourceRef)
		}
	}
	if !strings.HasPrefix(sourceRef.SourceURI, "redacted:sha256:") {
		t.Fatalf("SourceRef.SourceURI = %q, want redacted fingerprint", sourceRef.SourceURI)
	}
	if !strings.HasPrefix(sourceRef.SourceRecordID, "redacted:sha256:") {
		t.Fatalf("SourceRef.SourceRecordID = %q, want redacted source fingerprint", sourceRef.SourceRecordID)
	}
	if strings.Contains(engine.lastMedia.SourceName, "file:///Users") ||
		strings.Contains(engine.lastMedia.SourceName, "private.example.invalid") {
		t.Fatalf("engine SourceName leaked unsafe source location: %q", engine.lastMedia.SourceName)
	}
}

type fakeTranscriptEngine struct {
	result    EngineResult
	err       error
	calls     int
	lastMedia Media
}

func (f *fakeTranscriptEngine) Transcribe(ctx context.Context, media Media) (EngineResult, error) {
	f.calls++
	f.lastMedia = media
	if f.result.EngineName == "" {
		f.result.EngineName = "synthetic-transcript"
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
		ScopeID:      "doc-source:git:repo-media",
		GenerationID: "gen-media-1",
		ObservedAt:   time.Date(2026, time.June, 9, 12, 0, 0, 0, time.UTC),
		SourceSystem: "git",
		SourceURI:    sourceName,
		SourceName:   sourceName,
		SourceID:     "doc-source:git:repo-media",
		DocumentID:   "doc:git:repo-media:" + sourceName,
		ExternalID:   sourceName,
		RevisionID:   "rev-1",
		CanonicalURI: "git://repo-media/" + sourceName,
		Title:        "Transcript fixture",
		Body:         body,
		Engine:       engine,
		Options:      options,
	}
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

func payloadByKind(t *testing.T, envelopes []facts.Envelope, kind string) map[string]any {
	t.Helper()

	matches := payloadsByKind(envelopes, kind)
	if len(matches) != 1 {
		t.Fatalf("fact kind %q count = %d, want 1", kind, len(matches))
	}
	return matches[0]
}

func payloadsByKind(envelopes []facts.Envelope, kind string) []map[string]any {
	var matches []map[string]any
	for _, envelope := range envelopes {
		if envelope.FactKind == kind {
			matches = append(matches, envelope.Payload)
		}
	}
	return matches
}

func stringMapValue(t *testing.T, payload map[string]any, key string) map[string]string {
	t.Helper()

	raw, ok := payload[key].(map[string]any)
	if !ok {
		t.Fatalf("payload[%q] = %#v, want map[string]any", key, payload[key])
	}
	out := map[string]string{}
	for k, v := range raw {
		if text, ok := v.(string); ok {
			out[k] = text
		}
	}
	return out
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
