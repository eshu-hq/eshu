// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
	"github.com/eshu-hq/eshu/go/internal/facts/docs"
)

// TestExtractRedactsEngineTranscriptCanariesBeforeFacts runs the real engine
// adapter (against the fake binary) through the unchanged media.Extract path
// and proves sensitive transcript text never reaches a persisted envelope.
func TestExtractRedactsEngineTranscriptCanariesBeforeFacts(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "success", Limits{})
	body := encodeWAV(t, 8000, 1, 16, 8000*4)

	result, err := docmedia.Extract(context.Background(), docmedia.Request{
		ScopeID:      "doc-source:git:repo-media",
		GenerationID: "gen-1",
		ObservedAt:   time.Date(2026, time.November, 1, 0, 0, 0, 0, time.UTC),
		SourceSystem: "git",
		SourceURI:    "docs/standup.wav",
		SourceName:   "docs/standup.wav",
		RevisionID:   "rev-1",
		Body:         body,
		Engine:       engine,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(result.Sections) != 2 {
		t.Fatalf("sections = %d, want 2", len(result.Sections))
	}
	if got := result.Sections[0].Content; got != "Restart the payments worker." {
		t.Fatalf("benign section content = %q", got)
	}
	redacted := result.Sections[1]
	if redacted.Content != "[redacted]" || redacted.SourceMetadata["redacted"] != "true" {
		t.Fatalf("canary section not redacted: %+v", redacted)
	}
	for _, section := range result.Sections {
		meta := section.SourceMetadata
		if meta["transcript_engine"] != EngineName || meta["transcript_engine_version"] != "v1.9.5" {
			t.Fatalf("engine identity metadata = %q/%q", meta["transcript_engine"], meta["transcript_engine_version"])
		}
		if !strings.HasPrefix(meta["transcript_model_digest"], "sha256:") || meta["transcript_model"] != "ggml-fake.bin" {
			t.Fatalf("model identity metadata = %q/%q", meta["transcript_model"], meta["transcript_model_digest"])
		}
	}
	if status := result.Document.SourceMetadata["transcript_status"]; status != "completed" {
		t.Fatalf("transcript_status = %q, want completed", status)
	}
	encoded, err := json.Marshal(result.Envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, canary := range []string{canaryPassword, canaryEmail} {
		if strings.Contains(string(encoded), canary) {
			t.Fatalf("persisted envelopes leaked canary %q", canary)
		}
	}
}

func TestExtractRecordsEngineNoSpeechStatus(t *testing.T) {
	t.Parallel()
	engine := newFakeEngine(t, "nospeech", Limits{})
	result, err := docmedia.Extract(context.Background(), docmedia.Request{
		SourceName: "docs/quiet.wav",
		Body:       encodeWAV(t, 16000, 1, 16, 16000),
		Engine:     engine,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if status := result.Document.SourceMetadata["transcript_status"]; status != "no_speech" {
		t.Fatalf("transcript_status = %q, want no_speech", status)
	}
	if got := result.Document.SourceMetadata["warning"]; got != "transcript_no_speech" {
		t.Fatalf("warning = %q, want transcript_no_speech", got)
	}
	for _, envelope := range result.Envelopes {
		if envelope.FactKind == docs.SectionFactKind {
			t.Fatalf("no-speech run emitted a section fact")
		}
	}
}
