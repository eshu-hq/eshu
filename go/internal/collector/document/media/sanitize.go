// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package media

import (
	"cmp"
	"path"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"unicode"
)

// valueWindowSegments is how many spoken segments after a keyword segment are
// treated as the keyword's value. One covers the immediate value. The second
// covers a value the engine broke in two or a filler spoken before it. It stays
// small because every segment in the window loses its text.
const valueWindowSegments = 2

// sensitiveKeywords are the words that mark a transcript segment as sensitive.
// Unlike the patterns below, a keyword names a value rather than being one, so
// only a keyword opens a value window.
var sensitiveKeywords = []string{
	"credential_marker", "secret_marker", "token_marker", "password", "api_key", "private_key",
}

var (
	emailPattern       = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	schemePattern      = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+\-.]*://\S+`)
	ticketPattern      = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-[0-9]+\b`)
	unixPathPattern    = regexp.MustCompile(`(?i)(^|[^a-z0-9])/(users|home|var|etc|private|volumes|tmp)/\S+`)
	windowsPathPattern = regexp.MustCompile(`(?i)\b[a-z]:\\[^\s]+`)
)

func containsSensitiveMarker(text string) bool {
	return containsSensitiveKeyword(text) ||
		emailPattern.MatchString(text) ||
		schemePattern.MatchString(text) ||
		ticketPattern.MatchString(text) ||
		unixPathPattern.MatchString(text) ||
		windowsPathPattern.MatchString(text)
}

func containsSensitiveKeyword(text string) bool {
	lower := strings.ToLower(text)
	for _, keyword := range sensitiveKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// valueSegments reports, by index into segments, which segments fall in the
// value window of a keyword segment. Each keyword segment adds a full window,
// so stacked markers each cover their own value. Callers keep their slice order.
//
// The walk runs twice, in time order and in slice order, and every keyword
// segment opens a window in both. Time order is needed because the Engine
// contract does not promise sorted output. Slice order is needed because
// timestamps cannot be trusted to place a keyword: a wrong one can still look
// valid and sort away from its value, so a validity check or a duration bound
// does not close the gap. On time-sorted input the passes agree. On out-of-order
// input the second pass only adds redaction, and that over-redaction is accepted
// policy.
//
// Any keyword segment opens a window, not only one that ends on the keyword.
// The words around a keyword are the least reliable part of a transcript:
// through this engine path base.en hears the sandbox fixture's "The password is
// as follows." as "The password in a column", so no reading of the tail tells a
// value still to come from one already spoken. Losing a segment of speech is
// visible in section metadata; a persisted secret is not recoverable.
func valueSegments(segments []Segment) []bool {
	sliceOrder := make([]int, len(segments))
	hasKeyword := make([]bool, len(segments))
	for i, segment := range segments {
		sliceOrder[i] = i
		hasKeyword[i] = containsSensitiveKeyword(segment.Text)
	}
	timeOrder := slices.Clone(sliceOrder)
	slices.SortStableFunc(timeOrder, func(a, b int) int {
		return cmp.Or(
			cmp.Compare(segments[a].StartMillis, segments[b].StartMillis),
			cmp.Compare(segments[a].EndMillis, segments[b].EndMillis),
		)
	})
	isValue := make([]bool, len(segments))
	markValueWindows(segments, timeOrder, hasKeyword, isValue)
	markValueWindows(segments, sliceOrder, hasKeyword, isValue)
	return isValue
}

// markValueWindows walks segments in the given order and sets isValue for each
// segment inside a window opened by an earlier keyword segment. Only a segment
// with a letter or digit uses up the window. Blank, punctuation-only, and
// zero-width segments hold nothing a value could be, so a value behind them
// stays covered. A segment with invalid timing never reaches a section and uses
// up nothing, though its keyword still opens a window: its text was spoken.
func markValueWindows(segments []Segment, order []int, hasKeyword []bool, isValue []bool) {
	remaining := 0
	for _, i := range order {
		if remaining > 0 && hasValidTiming(segments[i]) {
			isValue[i] = true
			if hasLexicalContent(segments[i].Text) {
				remaining--
			}
		}
		if hasKeyword[i] {
			remaining += valueWindowSegments
		}
	}
}

func hasLexicalContent(text string) bool {
	return strings.IndexFunc(text, func(r rune) bool {
		return unicode.IsLetter(r) || unicode.IsDigit(r)
	}) >= 0
}

func safeIdentity(value string, prefix string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if containsSensitiveMarker(value) || isUnsafeSourcePath(value) {
		return prefix + ":" + hashText(value), true
	}
	return value, false
}

func safeCanonicalURI(value string) (string, bool) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false
	}
	if isSafeRelativeSource(value) {
		return value, false
	}
	return "", true
}

func safeSourceURI(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return ""
	}
	if isSafeRelativeSource(value) {
		return path.Clean(filepath.ToSlash(value))
	}
	return "redacted:" + hashText(value)
}

func isSafeRelativeSource(value string) bool {
	raw := strings.TrimSpace(value)
	if strings.Contains(raw, "://") || schemePattern.MatchString(raw) {
		return false
	}
	cleaned := path.Clean(filepath.ToSlash(raw))
	if cleaned == "." || cleaned == ".." || strings.HasPrefix(cleaned, "../") || path.IsAbs(cleaned) {
		return false
	}
	if containsSensitiveMarker(cleaned) {
		return false
	}
	return true
}

func isUnsafeSourcePath(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return false
	}
	if strings.Contains(value, "://") {
		return true
	}
	if strings.HasPrefix(value, "/") || strings.HasPrefix(value, "~/") {
		return true
	}
	if len(value) > 2 && value[1] == ':' && (value[2] == '\\' || value[2] == '/') {
		return true
	}
	return false
}
