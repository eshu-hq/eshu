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
	"unicode/utf8"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

// TestSandboxValueBeforeKeywordIsRedacted feeds the real binary a canary spoken
// before its keyword, with a silence between them (#9792), and proves that
// whisper.cpp puts the value in an earlier segment than the keyword and that
// media.Extract still keeps it out of every envelope. Positions and the value
// text come from the transcript media.Extract consumed (recorded by a wrapper
// around the engine), so a mishearing cannot make the check pass. Failure
// messages carry positions and counts only: transcript text must stay out of the
// proof log.
func TestSandboxValueBeforeKeywordIsRedacted(t *testing.T) {
	engine := &recordingEngine{inner: sandboxEngine(t, Limits{Timeout: 2 * time.Minute, MaxAddressSpaceBytes: 6 << 30})}
	body, err := os.ReadFile(os.Getenv("ESHU_WHISPERCPP_VALUE_BEFORE_FIXTURE"))
	if err != nil {
		t.Fatalf("read value-before fixture: %v", err)
	}
	extracted, err := docmedia.Extract(context.Background(), docmedia.Request{
		SourceName: "fixture/speech-before.wav",
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
	if keyword < 1 || value < 0 || value >= keyword {
		t.Fatalf("fixture did not put the value before the keyword: segments=%d keyword=%d value=%d",
			len(segments), keyword, value)
	}
	valueText := strings.Trim(strings.ToLower(segments[value].Text), " .,!?")
	t.Logf("EVIDENCE value-before segments=%d value_segment=%d keyword_segment=%d value_runes=%d",
		len(segments), value, keyword, utf8.RuneCountInString(valueText))

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
