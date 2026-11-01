// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/collector/preflight/media"
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
			name: "out-of-order speech after a keyword in the slice is redacted too",
			segments: []Segment{
				timedSegment("The password is", 3000),
				timedSegment("Restart the worker.", 1000),
			},
			wantRedacted: []bool{true, true},
		},
		{
			name: "keyword with max start and end, first in the slice",
			segments: []Segment{
				{Text: "The password is", StartMillis: math.MaxInt64, EndMillis: math.MaxInt64},
				timedSegment(value, 3000),
			},
			wantRedacted: []bool{true, true},
		},
		{
			name: "keyword with a wrong but valid time, first in the slice",
			segments: []Segment{
				{Text: "The password is", StartMillis: 9000, EndMillis: 9500},
				timedSegment(value, 3000),
			},
			wantRedacted: []bool{true, true},
		},
		{
			name: "same start, value ends first, keyword first in the slice",
			segments: []Segment{
				{Text: "The password is", StartMillis: 0, EndMillis: 2000},
				{Text: value, StartMillis: 0, EndMillis: 1000},
			},
			wantRedacted: []bool{true, true},
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
			name: "keyword with start after end, value earlier in time",
			segments: []Segment{
				{Text: "The password is", StartMillis: 5000, EndMillis: 1000},
				timedSegment(value, 3000),
			},
			wantRedacted: []bool{true},
		},
		{
			name: "keyword with max start, value earlier in time",
			segments: []Segment{
				{Text: "The password is", StartMillis: math.MaxInt64},
				timedSegment(value, 3000),
			},
			wantRedacted: []bool{true},
		},
		{
			name: "keyword with negative start sorts ahead of its value",
			segments: []Segment{
				timedSegment("Check one.", 1000),
				timedSegment("Check two.", 2000),
				{Text: "The password is", StartMillis: -1, EndMillis: 0},
				timedSegment(value, 3000),
			},
			wantRedacted: []bool{true, true, true},
		},
		{
			name: "stacked invalid-timing keywords each add a window",
			segments: []Segment{
				{Text: "The password is", StartMillis: 5000, EndMillis: 1000},
				{Text: "The api_key is", StartMillis: 6000, EndMillis: 1000},
				timedSegment(value, 1000),
				timedSegment("hunter2.", 2000),
				timedSegment("correcthorse.", 3000),
			},
			wantRedacted: []bool{true, true, true},
		},
		{
			name:         "two blank segments do not use up the window",
			segments:     sequentialSegments("The password is", "   ", "\t\n", "let me check", value),
			wantRedacted: []bool{true, true, true},
		},
		{
			name:         "punctuation segments do not use up the window",
			segments:     sequentialSegments("The password is", ".", "...", value),
			wantRedacted: []bool{true, true, true, true},
		},
		{
			name:         "zero-width segments do not use up the window",
			segments:     sequentialSegments("The password is", "\u200b", "\u200b", value),
			wantRedacted: []bool{true, true, true, true},
		},
		{
			name:         "digit-only segments use up the window",
			segments:     sequentialSegments("The password is", "1234", "5678", "Restart the worker."),
			wantRedacted: []bool{true, true, true, false},
		},
		{
			name:         "punctuation outside a window keeps its text",
			segments:     sequentialSegments("Restart the worker.", "...", "Read the logs."),
			wantRedacted: []bool{false, false, false},
		},
		{
			name:         "misheard words after the keyword, as the engine heard the fixture",
			segments:     sequentialSegments("The password in a column", "Swordfish"),
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
