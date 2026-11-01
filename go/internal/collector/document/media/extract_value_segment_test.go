// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"context"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/preflight/media"
	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts/docs"
)

// timedSegment builds a one-second Segment starting at startMillis. Tests that
// need other timing set the fields directly.
func timedSegment(text string, startMillis int64) Segment {
	return Segment{Text: text, StartMillis: startMillis, EndMillis: startMillis + 1000, Confidence: 0.9}
}

// sequentialSegments lays texts out one second apart, in the order given.
func sequentialSegments(texts ...string) []Segment {
	segments := make([]Segment, 0, len(texts))
	for i, text := range texts {
		segments = append(segments, timedSegment(text, int64(i)*1000))
	}
	return segments
}

// TestExtractRedactsValueSegmentsAfterMarker pins the #9786 rule: a segment
// that follows a keyword segment in time order, inside the value window, is
// treated as the keyword's value, and everything else keeps its per-segment
// behavior. wantRedacted lists one entry per emitted section, in emission order.
func TestExtractRedactsValueSegmentsAfterMarker(t *testing.T) {
	t.Parallel()

	const value = "swordfish."
	tests := []struct {
		name         string
		segments     []Segment
		wantRedacted []bool
	}{
		{
			name:         "marker ends segment, value in next",
			segments:     sequentialSegments("The password is", value),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "marker-only segment, then value",
			segments:     sequentialSegments("Password", value),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "label with a colon, then value",
			segments:     sequentialSegments("Password:", value),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "marker introduced by following words",
			segments:     sequentialSegments("The password is as follows.", value),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "words between marker and its verb",
			segments:     sequentialSegments("The password for the staging database is", value),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "whitespace-only segment between marker and value",
			segments:     sequentialSegments("The password is", "   ", value),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "long silence between marker and value",
			segments:     []Segment{timedSegment("The password is", 0), timedSegment(value, 600_000)},
			wantRedacted: []bool{true, true},
		},
		{
			name:         "filler segment, then value inside the window",
			segments:     sequentialSegments("The password is", "let me check", value),
			wantRedacted: []bool{true, true, true},
		},
		{
			name:         "speech past the window keeps its text",
			segments:     sequentialSegments("The password is", value, "let me check", "Restart the worker."),
			wantRedacted: []bool{true, true, true, false},
		},
		{
			name:         "two stacked markers, then two values",
			segments:     sequentialSegments("The password is", "The api_key is", value, "hunter2."),
			wantRedacted: []bool{true, true, true, true},
		},
		{
			name: "three stacked markers, then three values",
			segments: sequentialSegments(
				"The password is", "The api_key is", "The private_key is", value, "hunter2.", "correcthorse.",
			),
			wantRedacted: []bool{true, true, true, true, true, true},
		},
		{
			name:         "marker in the last segment",
			segments:     sequentialSegments("Restart the worker.", "The password is"),
			wantRedacted: []bool{false, true},
		},
		{
			name:         "value segment carries its own marker",
			segments:     sequentialSegments("The password is", "password swordfish.", "Restart the worker."),
			wantRedacted: []bool{true, true, true},
		},
		{
			name: "out-of-order engine output is judged in time order",
			segments: []Segment{
				timedSegment(value, 3000),
				timedSegment("The password is", 1000),
			},
			wantRedacted: []bool{true, true},
		},
		{
			name: "speech earlier in time stays, whatever the slice order",
			segments: []Segment{
				timedSegment("The password is", 3000),
				timedSegment("Restart the worker.", 1000),
			},
			wantRedacted: []bool{true, false},
		},
		{
			name: "same start orders by end",
			segments: []Segment{
				{Text: value, StartMillis: 0, EndMillis: 2000},
				{Text: "The password is", StartMillis: 0, EndMillis: 1000},
			},
			wantRedacted: []bool{true, true},
		},
		{
			name: "invalid-timing marker still arms, though it emits no section",
			segments: []Segment{
				{Text: "The password is", StartMillis: 2000, EndMillis: 1000},
				timedSegment(value, 3000),
			},
			wantRedacted: []bool{true},
		},
		{
			name: "invalid-timing segment does not use up the window",
			segments: []Segment{
				timedSegment("The password is", 0),
				{Text: "dropped one", StartMillis: 2000, EndMillis: 1000},
				{Text: "dropped two", StartMillis: 2500, EndMillis: 1000},
				timedSegment(value, 3000),
			},
			wantRedacted: []bool{true, true},
		},
		{
			name:         "misheard words after the keyword, as base.en heard the fixture",
			segments:     sequentialSegments("The password in El Falon", "Swordfish"),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "keyword early in a segment still opens the window",
			segments:     sequentialSegments("The password was rotated yesterday.", "swordfish.", "hunter2.", "Done."),
			wantRedacted: []bool{true, true, true, false},
		},
		{
			name:         "email does not arm",
			segments:     sequentialSegments("Ask ops@example.invalid", "Restart the worker."),
			wantRedacted: []bool{true, false},
		},
		{
			name:         "URL does not arm",
			segments:     sequentialSegments("See https://example.invalid/runbook", "Restart the worker."),
			wantRedacted: []bool{true, false},
		},
		{
			name:         "ticket does not arm",
			segments:     sequentialSegments("Track it in OPS-1234", "Restart the worker."),
			wantRedacted: []bool{true, false},
		},
		{
			name:         "path does not arm",
			segments:     sequentialSegments("The log is in /var/log/app", "Restart the worker."),
			wantRedacted: []bool{true, false},
		},
		{
			name:         "normal speech is not redacted",
			segments:     sequentialSegments("Restart the worker.", "The value is", "Then read the logs."),
			wantRedacted: []bool{false, false, false},
		},
		{
			name:         "DTMF digit segments are not redacted",
			segments:     sequentialSegments("415", "555#"),
			wantRedacted: []bool{false, false},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			engine := &fakeTranscriptEngine{result: EngineResult{Segments: tt.segments}}
			result, err := Extract(context.Background(), testRequest("docs/standup.wav", encodeTestWAV(t, 5000), engine))
			if err != nil {
				t.Fatalf("Extract() error = %v, want nil", err)
			}
			if got, want := len(result.Sections), len(tt.wantRedacted); got != want {
				t.Fatalf("sections = %d, want %d", got, want)
			}
			for i, section := range result.Sections {
				assertSectionRedaction(t, i, section, tt.wantRedacted[i])
			}
			if got := result.Document.SourceMetadata["transcript_status"]; got != "completed" {
				t.Fatalf("transcript_status = %q, want completed", got)
			}
			assertAbsentFromEnvelopes(t, result, "swordfish")
			assertAbsentFromEnvelopes(t, result, "hunter2")
		})
	}
}

func assertSectionRedaction(t *testing.T, index int, section docs.SectionPayload, want bool) {
	t.Helper()

	metadata := section.SourceMetadata
	redacted := metadata["redacted"] == "true"
	if redacted != want {
		t.Fatalf("section %d redacted = %t, want %t", index, redacted, want)
	}
	if !want {
		if section.Content == "[redacted]" {
			t.Fatalf("section %d content redacted with no redaction metadata", index)
		}
		return
	}
	if section.Content != "[redacted]" {
		t.Fatalf("section %d content is not the placeholder", index)
	}
	if got, want := metadata["redaction_class"], string(media.WarningSensitiveValueRedacted); got != want {
		t.Fatalf("section %d redaction_class = %q, want %q", index, got, want)
	}
	if !strings.Contains(metadata["warning"], string(media.WarningSensitiveValueRedacted)) {
		t.Fatalf("section %d warning = %q, want sensitive redaction", index, metadata["warning"])
	}
	if !section.ContainsWarnings {
		t.Fatalf("section %d ContainsWarnings = false, want true", index)
	}
}

func assertAbsentFromEnvelopes(t *testing.T, result Result, canary string) {
	t.Helper()

	encoded, err := json.Marshal(result.Envelopes)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	if strings.Contains(string(encoded), canary) {
		t.Fatalf("envelopes leaked canary %q", canary)
	}
}

// TestExtractWithholdsRedactedSectionTextHash proves a redacted section's
// text_hash cannot confirm a guess at the withheld text. A bare value such as
// "swordfish." would otherwise sit in the fact as an unsalted sha256 of itself.
func TestExtractWithholdsRedactedSectionTextHash(t *testing.T) {
	t.Parallel()

	const value = "swordfish."
	inline := "The password is swordfish."
	engine := &fakeTranscriptEngine{result: EngineResult{Segments: sequentialSegments(
		"The password is", value, "Restart the worker.", "Restart the queue.", inline,
	)}}
	result, err := Extract(context.Background(), testRequest("docs/standup.wav", encodeTestWAV(t, 5000), engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	encoded, err := json.Marshal(result.Envelopes)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v, want nil", err)
	}
	for _, raw := range []string{value, inline} {
		if strings.Contains(string(encoded), hashText(raw)) {
			t.Fatalf("envelopes carry the sha256 of withheld text %q", raw)
		}
	}
	for _, section := range result.Sections {
		redacted := section.SourceMetadata["redacted"] == "true"
		if redacted && section.TextHash != hashText("[redacted]") {
			t.Fatalf("redacted section %s text_hash derives from withheld text", section.SectionID)
		}
		if !redacted && section.TextHash != hashText(section.Content) {
			t.Fatalf("kept section %s text_hash changed", section.SectionID)
		}
	}
}

// TestExtractKeepsValueSegmentOutOfMentions proves a carried redaction reaches
// the mention path: a value segment emits no mention fact from its text or from
// a caller-supplied hint.
func TestExtractKeepsValueSegmentOutOfMentions(t *testing.T) {
	t.Parallel()

	segments := sequentialSegments("The password is", "payments-api")
	segments[1].MentionHints = []doctruth.MentionHint{{
		Text: "payments-api",
		Kind: "service",
		From: doctruth.MentionHintStructuredSection,
	}}
	req := testRequest("docs/standup.wav", encodeTestWAV(t, 5000), &fakeTranscriptEngine{
		result: EngineResult{Segments: segments},
	})
	req.Entities = []doctruth.Entity{{
		ID:      "service:payments-api",
		Kind:    "service",
		Aliases: []string{"payments-api"},
	}}

	result, err := Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	if got := countKind(result.Envelopes, docs.EntityMentionFactKind); got != 0 {
		t.Fatalf("mention fact count = %d, want 0 from a redacted value segment", got)
	}
	assertAbsentFromEnvelopes(t, result, "payments-api")
}

// TestExtractValueSegmentBeatsSectionTruncation proves the section character
// cap cannot let the head of a carried value through as a partial extraction.
func TestExtractValueSegmentBeatsSectionTruncation(t *testing.T) {
	t.Parallel()

	req := testRequestWithOptions("docs/standup.wav", encodeTestWAV(t, 5000), &fakeTranscriptEngine{
		result: EngineResult{Segments: sequentialSegments("The password is", "swordfishswordfish")},
	}, Options{MaxSectionChars: 4})

	result, err := Extract(context.Background(), req)
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	section := result.Sections[1]
	if section.Content != "[redacted]" {
		t.Fatalf("section content is not the placeholder")
	}
	if strings.Contains(section.SourceMetadata["warning"], "partial_extraction") {
		t.Fatalf("warning = %q, want no partial_extraction on a redacted value", section.SourceMetadata["warning"])
	}
}

// TestExtractValueSegmentKeepsEmissionOrder proves judging segments in time
// order does not reorder sections or change the ordinal fallback segment IDs.
func TestExtractValueSegmentKeepsEmissionOrder(t *testing.T) {
	t.Parallel()

	engine := &fakeTranscriptEngine{result: EngineResult{Segments: []Segment{
		timedSegment("Read the logs.", 7000),
		timedSegment("Check the queue.", 5000),
		timedSegment("swordfish.", 3000),
		timedSegment("The password is", 1000),
		timedSegment("Restart the worker.", 0),
	}}}
	result, err := Extract(context.Background(), testRequest("docs/standup.wav", encodeTestWAV(t, 5000), engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	got := make([]string, 0, len(result.Sections))
	for _, section := range result.Sections {
		got = append(got, fmt.Sprintf("%s=%s", section.SectionID, section.Content))
	}
	want := []string{
		"transcript:1=Read the logs.",
		"transcript:2=[redacted]",
		"transcript:3=[redacted]",
		"transcript:4=[redacted]",
		"transcript:5=Restart the worker.",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("sections = %q, want %q", got, want)
	}
}

// TestExtractNoSpeechIgnoresValueRule proves the ErrNoSpeech path stays ahead
// of redaction: any segments an engine returned with it are not judged.
func TestExtractNoSpeechIgnoresValueRule(t *testing.T) {
	t.Parallel()

	engine := &fakeTranscriptEngine{
		result: EngineResult{Segments: sequentialSegments("The password is", "swordfish.")},
		err:    fmt.Errorf("heard nothing: %w", ErrNoSpeech),
	}
	result, err := Extract(context.Background(), testRequest("docs/standup.wav", encodeTestWAV(t, 5000), engine))
	if err != nil {
		t.Fatalf("Extract() error = %v, want nil", err)
	}
	if got := result.Document.SourceMetadata["transcript_status"]; got != "no_speech" {
		t.Fatalf("transcript_status = %q, want no_speech", got)
	}
	if got := countKind(result.Envelopes, docs.SectionFactKind); got != 0 {
		t.Fatalf("section fact count = %d, want 0", got)
	}
}
