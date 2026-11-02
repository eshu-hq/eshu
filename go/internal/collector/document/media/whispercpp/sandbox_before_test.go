// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

//go:build whispercpp_sandbox

package whispercpp

import (
	"context"
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"
	"unicode"
	"unicode/utf8"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

// TestSandboxValueJustBeforeKeywordIsRedacted feeds the real binary a canary
// spoken right before its keyword, after a silence (#9792).
func TestSandboxValueJustBeforeKeywordIsRedacted(t *testing.T) {
	checkValueBeforeKeyword(t, "ESHU_WHISPERCPP_VALUE_BEFORE_FIXTURE", "fixture/speech-before.wav", 0)
}

// TestSandboxValueBeforeKeywordIsRedacted feeds the real binary a canary spoken
// two segments before its keyword, with a sentence between them (#9792). The
// pinned base.en really returns that shape for "Swordfish.", "Write it down.",
// "That is the password.", so a look-back of one segment would leak it.
func TestSandboxValueBeforeKeywordIsRedacted(t *testing.T) {
	checkValueBeforeKeyword(t, "ESHU_WHISPERCPP_VALUE_BEFORE_SPLIT_FIXTURE", "fixture/speech-before-split.wav", 1)
}

// checkValueBeforeKeyword runs media.Extract on the fixture named by env and
// proves, from the one transcript Extract consumed (recorded by a wrapper around
// the engine), that whisper.cpp put the canary in an earlier segment than the
// keyword with at least minBetween segments holding a letter or digit between
// them, and that every section is redacted and the canary text is in no
// envelope. The canary is compared as the engine heard it, so a mishearing
// cannot make the check pass. Failure messages carry positions and counts only:
// transcript text must stay out of the proof log.
func checkValueBeforeKeyword(t *testing.T, env, sourceName string, minBetween int) {
	t.Helper()
	engine := &recordingEngine{inner: sandboxEngine(t, Limits{Timeout: 2 * time.Minute, MaxAddressSpaceBytes: 6 << 30})}
	body, err := os.ReadFile(os.Getenv(env))
	if err != nil {
		t.Fatalf("read fixture from %s: %v", env, err)
	}
	extracted, err := docmedia.Extract(context.Background(), docmedia.Request{
		SourceName: sourceName,
		Body:       body,
		Engine:     engine,
	})
	if err != nil {
		t.Fatalf("Extract() error = %v", err)
	}
	if len(engine.results) != 1 {
		t.Fatalf("Extract made %d successful Transcribe calls, want exactly 1", len(engine.results))
	}
	segments := engine.results[0].Segments
	keyword, value := -1, -1
	for i, segment := range segments {
		switch text := strings.ToLower(segment.Text); {
		case docmedia.ContainsSensitiveKeyword(text):
			if keyword < 0 {
				keyword = i
			}
		case keyword < 0 && strings.Contains(text, "swordfish"):
			value = i
		}
	}
	between := 0
	for i := value + 1; value >= 0 && i < keyword; i++ {
		if strings.IndexFunc(segments[i].Text, func(r rune) bool { return unicode.IsLetter(r) || unicode.IsDigit(r) }) >= 0 {
			between++
		}
	}
	if keyword < 1 || value < 0 || value >= keyword || between < minBetween {
		t.Fatalf("fixture did not put the value before the keyword: segments=%d value=%d keyword=%d between=%d want>=%d",
			len(segments), value, keyword, between, minBetween)
	}
	valueText := strings.Trim(strings.ToLower(segments[value].Text), " .,!?")
	t.Logf("EVIDENCE value-before segments=%d value_segment=%d keyword_segment=%d lettered_between=%d value_runes=%d",
		len(segments), value, keyword, between, utf8.RuneCountInString(valueText))

	redacted := 0
	for _, section := range extracted.Sections {
		if section.SourceMetadata["redacted"] == "true" {
			redacted++
		}
	}
	encoded, err := json.Marshal(extracted.Envelopes)
	if err != nil {
		t.Fatalf("marshal envelopes: %v", err)
	}
	if strings.Contains(strings.ToLower(string(encoded)), valueText) {
		t.Fatalf("value segment reached the facts (sections %d, redacted %d)", len(extracted.Sections), redacted)
	}
	if len(extracted.Sections) != len(segments) || redacted != len(segments) {
		t.Fatalf("want %d sections all redacted, got sections %d redacted %d",
			len(segments), len(extracted.Sections), redacted)
	}
	t.Logf("EVIDENCE value-before-redaction sections=%d redacted=%d canary_in_facts=false",
		len(extracted.Sections), redacted)
}
