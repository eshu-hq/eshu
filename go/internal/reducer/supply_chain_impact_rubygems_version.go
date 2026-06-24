// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

import (
	"strings"
	"unicode"
)

type rubyGemsVersionSegment struct {
	text    string
	numeric bool
}

func validRubyGemsVersion(raw string) bool {
	_, ok := parseRubyGemsVersion(raw)
	return ok
}

func compareRubyGemsVersion(left string, right string) (int, bool) {
	leftSegments, ok := parseRubyGemsVersion(left)
	if !ok {
		return 0, false
	}
	rightSegments, ok := parseRubyGemsVersion(right)
	if !ok {
		return 0, false
	}
	limit := len(leftSegments)
	if len(rightSegments) > limit {
		limit = len(rightSegments)
	}
	for index := range limit {
		leftSegment := rubyGemsSegmentAt(leftSegments, index)
		rightSegment := rubyGemsSegmentAt(rightSegments, index)
		if leftSegment == rightSegment {
			continue
		}
		switch {
		case leftSegment.numeric && !rightSegment.numeric:
			return 1, true
		case !leftSegment.numeric && rightSegment.numeric:
			return -1, true
		case leftSegment.numeric:
			return compareRubyGemsNumericSegment(leftSegment.text, rightSegment.text), true
		default:
			return strings.Compare(leftSegment.text, rightSegment.text), true
		}
	}
	return 0, true
}

func parseRubyGemsVersion(raw string) ([]rubyGemsVersionSegment, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, false
	}
	if strings.HasPrefix(raw, "-") || strings.HasSuffix(raw, "-") {
		return nil, false
	}
	raw = strings.ReplaceAll(raw, "-", ".pre.")
	if !unicode.IsDigit(rune(raw[0])) {
		return nil, false
	}
	segments := make([]rubyGemsVersionSegment, 0)
	for _, part := range strings.Split(raw, ".") {
		if part == "" {
			return nil, false
		}
		parsed, ok := parseRubyGemsVersionPart(part)
		if !ok {
			return nil, false
		}
		segments = append(segments, parsed...)
	}
	segments = trimRubyGemsPrereleaseZeroSegments(segments)
	segments = trimRubyGemsTrailingZeroSegments(segments)
	if len(segments) == 0 {
		return []rubyGemsVersionSegment{{text: "0", numeric: true}}, true
	}
	return segments, true
}

func parseRubyGemsVersionPart(part string) ([]rubyGemsVersionSegment, bool) {
	segments := make([]rubyGemsVersionSegment, 0)
	startsNumeric := unicode.IsDigit(rune(part[0]))
	for index := 0; index < len(part); {
		r := rune(part[index])
		if !rubyGemsVersionRuneAllowed(r) {
			return nil, false
		}
		if startsNumeric && !unicode.IsDigit(r) {
			return nil, false
		}
		start := index
		numeric := unicode.IsDigit(r)
		for index < len(part) {
			next := rune(part[index])
			if !rubyGemsVersionRuneAllowed(next) || unicode.IsDigit(next) != numeric {
				break
			}
			index++
		}
		text := part[start:index]
		if numeric {
			text = normalizeRubyGemsNumericSegment(text)
		}
		segments = append(segments, rubyGemsVersionSegment{text: text, numeric: numeric})
	}
	return segments, true
}

func rubyGemsVersionRuneAllowed(r rune) bool {
	return r >= '0' && r <= '9' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z'
}

func trimRubyGemsPrereleaseZeroSegments(segments []rubyGemsVersionSegment) []rubyGemsVersionSegment {
	firstText := -1
	for index, segment := range segments {
		if !segment.numeric {
			firstText = index
			break
		}
	}
	if firstText <= 0 {
		return segments
	}
	trimStart := firstText
	for trimStart > 0 {
		previous := segments[trimStart-1]
		if !previous.numeric || previous.text != "0" {
			break
		}
		trimStart--
	}
	if trimStart == firstText {
		return segments
	}
	out := make([]rubyGemsVersionSegment, 0, len(segments)-(firstText-trimStart))
	out = append(out, segments[:trimStart]...)
	out = append(out, segments[firstText:]...)
	return out
}

func trimRubyGemsTrailingZeroSegments(segments []rubyGemsVersionSegment) []rubyGemsVersionSegment {
	for len(segments) > 0 {
		last := segments[len(segments)-1]
		if !last.numeric || last.text != "0" {
			break
		}
		segments = segments[:len(segments)-1]
	}
	return segments
}

func rubyGemsSegmentAt(segments []rubyGemsVersionSegment, index int) rubyGemsVersionSegment {
	if index >= len(segments) {
		return rubyGemsVersionSegment{text: "0", numeric: true}
	}
	return segments[index]
}

func normalizeRubyGemsNumericSegment(raw string) string {
	trimmed := strings.TrimLeft(raw, "0")
	if trimmed == "" {
		return "0"
	}
	return trimmed
}

func compareRubyGemsNumericSegment(left string, right string) int {
	if len(left) != len(right) {
		if len(left) < len(right) {
			return -1
		}
		return 1
	}
	return strings.Compare(left, right)
}
