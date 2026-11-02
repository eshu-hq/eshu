// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"context"
	"encoding/json"
	"strconv"
	"strings"
	"testing"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

func annotationOutput(texts ...string) []byte {
	var rows []string
	for i, text := range texts {
		encoded, _ := json.Marshal(text)
		rows = append(rows, `{"offsets": {"from": `+strconv.Itoa(i*1000)+`, "to": `+strconv.Itoa(i*1000+1000)+`}, "text": `+string(encoded)+`}`)
	}
	return []byte(`{"result": {"language": "en"}, "transcription": [` + strings.Join(rows, ",") + `]}`)
}

// TestParseOutputKeepsAnnotationNamingASecret pins #9788: an annotation that
// holds a sensitive keyword is real speech whisper wrapped in brackets, so the
// engine must not drop it before media.Extract can open a value window. Plain
// non-speech annotations are still dropped.
func TestParseOutputKeepsAnnotationNamingASecret(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		text string
		keep bool
	}{
		{"parenthesized sentence", " (The password is)", true},
		{"bracketed sentence", " [The password is]", true},
		{"starred label", " *the passcode*", true},
		{"single word", " (password)", true},
		{"spoken variant", " (the pass word is)", true},
		{"pin", " [PIN]", true},
		{"token", " (token)", true},
		{"blank audio", " [BLANK_AUDIO]", false},
		{"silence", " (silence)", false},
		{"music", " *music*", false},
		{"note", " ♪", false},
		{"wind", " (wind blowing)", false},
		{"pinned is not a keyword", " (pinned)", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			var stats runStats
			out, err := parseOutput(annotationOutput(tt.text, " swordfish."), Limits{MaxSegmentChars: 100, MaxSegments: 10}, &stats)
			if err != nil {
				t.Fatalf("parseOutput() error = %v", err)
			}
			wantKept := 1
			if tt.keep {
				wantKept = 2
			}
			if len(out.segments) != wantKept {
				t.Fatalf("kept %d segments, want %d: %+v", len(out.segments), wantKept, out.segments)
			}
			if tt.keep != (stats.segmentsDropped == 0) {
				t.Fatalf("segmentsDropped = %d, keep = %v", stats.segmentsDropped, tt.keep)
			}
		})
	}
}

// TestExtractRedactsValueBehindAnnotatedKeyword proves the fix end to end:
// "(The password is)" then "swordfish." must persist neither.
func TestExtractRedactsValueBehindAnnotatedKeyword(t *testing.T) {
	t.Parallel()

	var stats runStats
	out, err := parseOutput(annotationOutput(" (The password is)", " swordfish.", " Restart the worker."),
		Limits{MaxSegmentChars: 100, MaxSegments: 10}, &stats)
	if err != nil {
		t.Fatalf("parseOutput() error = %v", err)
	}
	engine := stubEngine{result: docmedia.EngineResult{Segments: out.segments}}
	result, err := docmedia.Extract(context.Background(), docmedia.Request{
		SourceName: "docs/standup.wav",
		Body:       encodeWAV(t, 16000, 1, 16, 16000),
		Engine:     engine,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	encoded, err := json.Marshal(result.Envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	for _, leaked := range []string{"swordfish", "The password is"} {
		if strings.Contains(string(encoded), leaked) {
			t.Fatalf("persisted envelopes leaked %q", leaked)
		}
	}
}

type stubEngine struct{ result docmedia.EngineResult }

func (s stubEngine) Transcribe(context.Context, docmedia.Media) (docmedia.EngineResult, error) {
	return s.result, nil
}

// TestParseOutputLoneKeywordAnnotationIsOneSegment pins the changed no-speech
// edge: a run whose only content is "(password)" used to be dropped to
// ClassNoSpeech and now yields one segment for media.Extract to withhold.
func TestParseOutputLoneKeywordAnnotationIsOneSegment(t *testing.T) {
	t.Parallel()

	var stats runStats
	out, err := parseOutput(annotationOutput(" (password)"), Limits{MaxSegmentChars: 100, MaxSegments: 10}, &stats)
	if err != nil {
		t.Fatalf("parseOutput() error = %v, want one kept segment", err)
	}
	if len(out.segments) != 1 || stats.segmentsDropped != 0 {
		t.Fatalf("segments = %d dropped = %d, want 1 and 0", len(out.segments), stats.segmentsDropped)
	}
	if _, err := parseOutput(annotationOutput(" [BLANK_AUDIO]"), Limits{MaxSegmentChars: 100, MaxSegments: 10}, &runStats{}); ClassOf(err) != ClassNoSpeech {
		t.Fatalf("a lone [BLANK_AUDIO] ClassOf = %q, want no_speech", ClassOf(err))
	}
}
