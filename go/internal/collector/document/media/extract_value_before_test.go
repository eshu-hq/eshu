// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"context"
	"testing"
)

// TestExtractRedactsValueSegmentsBeforeKeyword pins the #9792 rule: a segment
// spoken just before a keyword segment is withheld, because a speech engine
// puts a value ahead of its label ("swordfish", then "is the password") as
// often as behind it. wantRedacted lists one entry per emitted section, in
// emission order. The canaries swordfish and hunter2 must never reach a fact.
func TestExtractRedactsValueSegmentsBeforeKeyword(t *testing.T) {
	t.Parallel()

	const value = "swordfish."
	tests := []struct {
		name         string
		segments     []Segment
		wantRedacted []bool
	}{
		{
			name:         "value, then a clause that ends in the keyword",
			segments:     sequentialSegments(value, "is the password"),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "value, then a clause that ends in a spoken API key",
			segments:     sequentialSegments("The value is swordfish", "and that is my API key"),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "value, then a PIN label",
			segments:     sequentialSegments("hunter2", "that is my P.I.N."),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "filler between the value and the keyword",
			segments:     sequentialSegments(value, "that is what I use", "for the password"),
			wantRedacted: []bool{true, true, true},
		},
		{
			name:         "value broken in two before the keyword",
			segments:     sequentialSegments("sword", "fish", "that is the password"),
			wantRedacted: []bool{true, true, true},
		},
		{
			name:         "value three segments back keeps its text",
			segments:     sequentialSegments("hunter2.", "so", "that is", "the password"),
			wantRedacted: []bool{false, true, true, true},
		},
		{
			name:         "blank segment between value and keyword does not use up the window",
			segments:     sequentialSegments("hunter2.", "so", "that is", "   ", "...", "the password"),
			wantRedacted: []bool{false, true, true, true, true},
		},
		{
			name:         "punctuation and zero-width segments hold nothing a value could be",
			segments:     sequentialSegments(value, "...", "\u200b", "is the password"),
			wantRedacted: []bool{true, true, true, true},
		},
		{
			name:         "value, then the first half of a split keyword, then the second",
			segments:     sequentialSegments(value, "The API", "key is"),
			wantRedacted: []bool{true, true, true},
		},
		{
			name:         "keyword in the first segment has nothing before it",
			segments:     sequentialSegments("The password is", value),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "ordinary speech before a keyword is withheld",
			segments:     sequentialSegments("Restart the worker.", "The password is rotated."),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "stacked keywords each cover their own value",
			segments:     sequentialSegments("hunter2.", value, "is the password", "and the api_key"),
			wantRedacted: []bool{true, true, true, true},
		},
		{
			name: "out-of-order output: keyword first in the slice, value earlier in time",
			segments: []Segment{
				timedSegment("is the password", 3000),
				timedSegment(value, 1000),
			},
			wantRedacted: []bool{true, true},
		},
		{
			name: "out-of-order output: value first in the slice, keyword earlier in time",
			segments: []Segment{
				timedSegment(value, 3000),
				timedSegment("is the password", 1000),
			},
			wantRedacted: []bool{true, true},
		},
		{
			name: "invalid-timing segment is not withheld and uses up no window",
			segments: []Segment{
				timedSegment(value, 0),
				{Text: "dropped one", StartMillis: 2000, EndMillis: 1000},
				{Text: "dropped two", StartMillis: 2500, EndMillis: 1000},
				timedSegment("is the password", 3000),
			},
			wantRedacted: []bool{true, true},
		},
		{
			name:         "email before the keyword is withheld by its own rule",
			segments:     sequentialSegments("Ask ops@example.invalid", "about the password"),
			wantRedacted: []bool{true, true},
		},
		{
			name:         "normal speech with no keyword keeps its text",
			segments:     sequentialSegments("Restart the worker.", "Then read the logs."),
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
			assertAbsentFromEnvelopes(t, result, "swordfish")
			if tt.wantRedacted[0] {
				assertAbsentFromEnvelopes(t, result, "hunter2")
			}
		})
	}
}

// TestValueSegmentsLongBlankRunsStayBounded runs valueSegments on 918 segments,
// close to the 1,000 the engine returns at most, with a 300-segment blank run
// before every keyword segment. That is the input that keeps the look-back
// window longest, because blanks stay in it until two lettered segments pass.
// Each block is five lettered segments, the blank run, and a keyword. The
// keyword withholds the blank run and the two lettered segments before it, the
// previous keyword's forward window withholds the first two lettered segments,
// and the third keeps its text. The window drops a segment once two more
// lettered segments arrive, so each segment is marked at most twice per walk
// and the walk stays linear in the number of segments.
func TestValueSegmentsLongBlankRunsStayBounded(t *testing.T) {
	t.Parallel()

	const blocks, blanks = 3, 300
	var texts []string
	for range blocks {
		texts = append(texts, "benign one", "benign two", "benign three", "benign four", "benign five")
		for range blanks {
			texts = append(texts, "...")
		}
		texts = append(texts, "the password")
	}
	if len(texts) > 1000 {
		t.Fatalf("%d segments exceed the engine cap of 1000", len(texts))
	}
	isValue, own := valueSegments(sequentialSegments(texts...))
	blockLen := 5 + blanks + 1
	for b := range blocks {
		base := b * blockLen
		for pos := range blockLen {
			i := base + pos
			withheld := isValue[i] || own[i]
			want := true
			switch {
			case pos < 2:
				want = b > 0
			case pos == 2:
				want = false
			}
			if withheld != want {
				t.Fatalf("block %d position %d withheld = %t, want %t", b, pos, withheld, want)
			}
		}
	}
}
