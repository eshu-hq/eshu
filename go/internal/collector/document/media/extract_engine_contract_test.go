// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"context"
	"fmt"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/facts/docs"
)

func TestExtractRecordsEngineNoSpeechAsDocumentWarning(t *testing.T) {
	t.Parallel()

	body := encodeTestWAV(t, 1000)
	engine := &fakeTranscriptEngine{err: fmt.Errorf("model heard nothing: %w", ErrNoSpeech)}

	result, err := Extract(context.Background(), testRequest("docs/silence.wav", body, engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil for engine no-speech", err)
	}
	if engine.calls != 1 {
		t.Fatalf("engine calls = %d, want 1", engine.calls)
	}
	document := payloadByKind(t, result.Envelopes, docs.DocumentFactKind)
	metadata := stringMapValue(t, document, "source_metadata")
	if got := metadata["transcript_status"]; got != "no_speech" {
		t.Fatalf("transcript_status = %q, want no_speech", got)
	}
	if got := metadata["warning"]; got != "transcript_no_speech" {
		t.Fatalf("warning = %q, want transcript_no_speech", got)
	}
	if got := countKind(result.Envelopes, docs.SectionFactKind); got != 0 {
		t.Fatalf("section fact count = %d, want 0", got)
	}
}

func TestExtractRecordsEngineModelIdentityOnSections(t *testing.T) {
	t.Parallel()

	body := encodeTestWAV(t, 2000)
	engine := &fakeTranscriptEngine{result: EngineResult{
		EngineName:    "whisper.cpp",
		EngineVersion: "v1.9.5",
		ModelName:     "ggml-base.en.bin",
		ModelDigest:   "sha256:a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002",
		Segments: []Segment{{
			Text:        "Restart the queue worker.",
			StartMillis: 0,
			EndMillis:   1500,
		}},
	}}

	result, err := Extract(context.Background(), testRequest("docs/standup.wav", body, engine))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	section := payloadByKind(t, result.Envelopes, docs.SectionFactKind)
	metadata := stringMapValue(t, section, "source_metadata")
	if got := metadata["transcript_model"]; got != "ggml-base.en.bin" {
		t.Fatalf("transcript_model = %q, want ggml-base.en.bin", got)
	}
	want := "sha256:a03779c86df3323075f5e796cb2ce5029f00ec8869eee3fdfb897afe36c6d002"
	if got := metadata["transcript_model_digest"]; got != want {
		t.Fatalf("transcript_model_digest = %q, want %q", got, want)
	}
}

func TestExtractOmitsModelIdentityWhenEngineHasNone(t *testing.T) {
	t.Parallel()

	body := encodeTestWAV(t, 2000)
	engine := &fakeTranscriptEngine{result: EngineResult{Segments: []Segment{{
		Text:        "Rotate the on-call schedule.",
		StartMillis: 0,
		EndMillis:   1000,
	}}}}

	result, err := Extract(context.Background(), testRequest("docs/oncall.wav", body, engine))
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	section := payloadByKind(t, result.Envelopes, docs.SectionFactKind)
	metadata := stringMapValue(t, section, "source_metadata")
	for _, key := range []string{"transcript_model", "transcript_model_digest"} {
		if value, ok := metadata[key]; ok {
			t.Fatalf("%s = %q, want absent", key, value)
		}
	}
}
