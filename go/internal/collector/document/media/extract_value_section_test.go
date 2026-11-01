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

	"github.com/eshu-hq/eshu/go/internal/doctruth"
	"github.com/eshu-hq/eshu/go/internal/facts/docs"
)

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
// The last section is earlier in time than the keyword but follows it in the
// slice, so the slice-order pass redacts it: over-redaction on out-of-order
// input is accepted policy. The first section follows the keyword in neither
// window and keeps its text.
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
		"transcript:5=[redacted]",
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
