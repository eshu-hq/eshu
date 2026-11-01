// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package whispercpp

import (
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	docmedia "github.com/eshu-hq/eshu/go/internal/collector/document/media"
)

// nonSpeechPattern matches a segment that is only a bracketed, parenthesized,
// starred, or music-note annotation such as "[BLANK_AUDIO]", "(silence)",
// "*music*", or "♪". whisper.cpp emits these for non-speech audio; they are
// not lexical content and are dropped before facts.
var nonSpeechPattern = regexp.MustCompile(`^(\[[^\[\]]*\]|\([^()]*\)|\*[^*]*\*|♪[^♪]*♪?)[\s.!?♪]*$`)

// engineOutput mirrors the subset of `whisper-cli -ojf` JSON this engine
// reads. Offsets are milliseconds; token p is the token probability.
type engineOutput struct {
	Result struct {
		Language string `json:"language"`
	} `json:"result"`
	Transcription *[]struct {
		Offsets struct {
			From int64 `json:"from"`
			To   int64 `json:"to"`
		} `json:"offsets"`
		Text   string `json:"text"`
		Tokens []struct {
			Text string  `json:"text"`
			P    float64 `json:"p"`
		} `json:"tokens"`
	} `json:"transcription"`
}

type parsedOutput struct {
	language string
	segments []docmedia.Segment
}

// parseOutput converts engine JSON into speech segments. Raw control bytes
// are blanked first because whisper.cpp escapes only quotes and backslashes
// in JSON strings. Error details never quote the output.
func parseOutput(raw []byte, limits Limits, stats *runStats) (parsedOutput, error) {
	sanitized := make([]byte, len(raw))
	for i, b := range raw {
		if b < 0x20 {
			b = ' '
		}
		sanitized[i] = b
	}
	var decoded engineOutput
	if err := json.Unmarshal(sanitized, &decoded); err != nil {
		return parsedOutput{}, classified(ClassMalformedOutput, "engine stdout is not valid JSON")
	}
	if decoded.Transcription == nil {
		return parsedOutput{}, classified(ClassMalformedOutput, "engine JSON has no transcription array")
	}
	out := parsedOutput{language: decoded.Result.Language}
	for _, raw := range *decoded.Transcription {
		if raw.Offsets.From < 0 || raw.Offsets.To < raw.Offsets.From {
			return parsedOutput{}, classified(ClassMalformedOutput, "segment offsets are negative or inverted")
		}
		text := strings.TrimSpace(raw.Text)
		if text == "" {
			continue
		}
		if nonSpeechPattern.MatchString(text) {
			stats.segmentsDropped++
			continue
		}
		if utf8.RuneCountInString(text) > limits.MaxSegmentChars {
			return parsedOutput{}, classified(ClassResourceLimit, "segment text exceeds cap")
		}
		if len(out.segments) >= limits.MaxSegments {
			return parsedOutput{}, classified(ClassResourceLimit, "segment count exceeds cap")
		}
		var sum float64
		var count int
		for _, token := range raw.Tokens {
			if strings.HasPrefix(token.Text, "[_") && strings.HasSuffix(token.Text, "]") {
				continue
			}
			sum += token.P
			count++
		}
		confidence := 0.0
		if count > 0 {
			confidence = sum / float64(count)
		}
		out.segments = append(out.segments, docmedia.Segment{
			SegmentID:   fmt.Sprintf("seg-%04d", len(out.segments)+1),
			Text:        text,
			StartMillis: raw.Offsets.From,
			EndMillis:   raw.Offsets.To,
			Confidence:  confidence,
		})
	}
	stats.segmentsKept = int64(len(out.segments))
	if len(out.segments) == 0 {
		return parsedOutput{}, noSpeech()
	}
	return out, nil
}
