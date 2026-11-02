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

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

// TestSandboxSpokenKeywordCanariesAreRedacted feeds the real binary a spoken
// "API key" (value in the same segment, and in its own segment after a silence)
// and a spoken "PIN", the keyword forms #9788 added. Each fixture's transcript
// is whatever whisper.cpp heard; the test requires that at least one segment
// holds a keyword by the same check media.Extract uses, that every section is
// redacted, and that no segment text reaches an envelope. It logs counts only:
// transcript text must stay out of the proof log.
func TestSandboxSpokenKeywordCanariesAreRedacted(t *testing.T) {
	cases := []struct{ name, env string }{
		{"api-key", "ESHU_WHISPERCPP_APIKEY_FIXTURE"},
		{"api-key-split", "ESHU_WHISPERCPP_APIKEY_SPLIT_FIXTURE"},
		{"pin", "ESHU_WHISPERCPP_PIN_FIXTURE"},
	}
	for _, tc := range cases {
		engine := &recordingEngine{inner: sandboxEngine(t, Limits{Timeout: 2 * time.Minute, MaxAddressSpaceBytes: 6 << 30})}
		body, err := os.ReadFile(os.Getenv(tc.env))
		if err != nil {
			t.Fatalf("%s: read fixture from %s: %v", tc.name, tc.env, err)
		}
		extracted, err := docmedia.Extract(context.Background(), docmedia.Request{
			SourceName: "fixture/speech-" + tc.name + ".wav",
			Body:       body,
			Engine:     engine,
		})
		if err != nil {
			t.Fatalf("%s: Extract() error = %v", tc.name, err)
		}
		if len(engine.results) != 1 {
			t.Fatalf("%s: Extract made %d successful Transcribe calls, want 1", tc.name, len(engine.results))
		}
		segments := engine.results[0].Segments
		keywordSegments := 0
		for _, segment := range segments {
			if docmedia.ContainsSensitiveKeyword(segment.Text) {
				keywordSegments++
			}
		}
		if keywordSegments == 0 {
			t.Fatalf("%s: no segment of %d holds a keyword, so this run proves nothing about the rule", tc.name, len(segments))
		}
		redacted := 0
		for _, section := range extracted.Sections {
			if section.SourceMetadata["redacted"] == "true" {
				redacted++
			}
		}
		if len(extracted.Sections) != len(segments) || redacted != len(segments) {
			t.Fatalf("%s: want %d sections all redacted, got sections %d redacted %d",
				tc.name, len(segments), len(extracted.Sections), redacted)
		}
		encoded, err := json.Marshal(extracted.Envelopes)
		if err != nil {
			t.Fatalf("%s: marshal envelopes: %v", tc.name, err)
		}
		lowered := strings.ToLower(string(encoded))
		for i, segment := range segments {
			spoken := strings.Trim(strings.ToLower(segment.Text), " .,!?\"")
			if len(spoken) >= 3 && strings.Contains(lowered, spoken) {
				t.Fatalf("%s: segment %d text reached the facts", tc.name, i)
			}
		}
		t.Logf("EVIDENCE spoken-keyword fixture=%s segments=%d keyword_segments=%d sections=%d redacted=%d canary_in_facts=false",
			tc.name, len(segments), keywordSegments, len(extracted.Sections), redacted)
	}
}
