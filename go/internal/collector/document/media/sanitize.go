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

// legacyKeywords are the substring keywords the rule started with (#9786).
// They stay the whole rule for source identities and mention hints, which are
// labels such as "token-service", not speech. A transcript segment also gets the
// spoken forms in spoken_keyword.go.
var legacyKeywords = []string{
	"credential_marker", "secret_marker", "token_marker", "password", "api_key", "private_key",
}

var (
	emailPattern       = regexp.MustCompile(`(?i)\b[a-z0-9._%+\-]+@[a-z0-9.\-]+\.[a-z]{2,}\b`)
	schemePattern      = regexp.MustCompile(`(?i)\b[a-z][a-z0-9+\-.]*://\S+`)
	ticketPattern      = regexp.MustCompile(`\b[A-Z][A-Z0-9]+-[0-9]+\b`)
	unixPathPattern    = regexp.MustCompile(`(?i)(^|[^a-z0-9])/(users|home|var|etc|private|volumes|tmp)/\S+`)
	windowsPathPattern = regexp.MustCompile(`(?i)\b[a-z]:\\[^\s]+`)
)

// containsSensitiveMarker is the label rule for source identities and mention
// hints: a legacy keyword or a value pattern. It does not fold spoken forms, so
// a name such as "token-service" or "pin-assignments.wav" stays readable.
func containsSensitiveMarker(text string) bool {
	return containsLegacyKeyword(text) || containsSensitiveValuePattern(text)
}

func containsSensitiveValuePattern(text string) bool {
	return emailPattern.MatchString(text) ||
		schemePattern.MatchString(text) ||
		ticketPattern.MatchString(text) ||
		unixPathPattern.MatchString(text) ||
		windowsPathPattern.MatchString(text)
}

func containsLegacyKeyword(text string) bool {
	lower := strings.ToLower(text)
	for _, keyword := range legacyKeywords {
		if strings.Contains(lower, keyword) {
			return true
		}
	}
	return false
}

// containsSensitiveKeyword reports whether a transcript segment holds a word
// that names a secret, in any spoken form (#9788). Only a keyword opens a value
// window; a value pattern does not.
func containsSensitiveKeyword(text string) bool {
	return containsSpokenKeyword(text)
}

// valueSegments reports, by index into segments, which segments fall in the
// value window of a keyword segment (isValue) and which hold a keyword of their
// own (ownKeyword), folding each segment once. Each keyword segment adds a full window,
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
func valueSegments(segments []Segment) (isValue []bool, ownKeyword []bool) {
	sliceOrder := make([]int, len(segments))
	folded := make([]string, len(segments))
	ownKeyword = make([]bool, len(segments))
	for i, segment := range segments {
		sliceOrder[i] = i
		folded[i] = foldSpoken(segment.Text)
		ownKeyword[i] = foldedHasKeyword(folded[i])
	}
	timeOrder := slices.Clone(sliceOrder)
	slices.SortStableFunc(timeOrder, func(a, b int) int {
		return cmp.Or(
			cmp.Compare(segments[a].StartMillis, segments[b].StartMillis),
			cmp.Compare(segments[a].EndMillis, segments[b].EndMillis),
		)
	})
	isValue = make([]bool, len(segments))
	markValueWindows(segments, timeOrder, folded, ownKeyword, isValue)
	markValueWindows(segments, sliceOrder, folded, ownKeyword, isValue)
	return isValue, ownKeyword
}

// markValueWindows walks segments in the given order and sets isValue for each
// segment inside a window opened by an earlier keyword segment. Only a segment
// with a letter or digit uses up the window. Blank, punctuation-only, and
// zero-width segments hold nothing a value could be, so a value behind them
// stays covered. A segment with invalid timing never reaches a section and uses
// up nothing, though its keyword still opens a window: its text was spoken.
//
// A keyword that the engine split across two segments ("The API", then "key
// is") opens a window at the second one, and that segment is withheld too. The
// pair is the previous segment with a letter or digit in this walk's order and
// the current one, and it counts only when neither holds a keyword alone, so
// one keyword never opens two windows.
func markValueWindows(segments []Segment, order []int, folded []string, ownKeyword []bool, isValue []bool) {
	remaining := 0
	prev := -1
	for _, i := range order {
		opens := ownKeyword[i]
		straddles := !opens && prev >= 0 && !ownKeyword[prev] && joinedHasKeyword(folded[prev], folded[i])
		if (remaining > 0 || straddles) && hasValidTiming(segments[i]) {
			isValue[i] = true
		}
		if remaining > 0 && hasValidTiming(segments[i]) && hasLexicalContent(segments[i].Text) {
			remaining--
		}
		if opens || straddles {
			remaining += valueWindowSegments
		}
		if hasLexicalContent(segments[i].Text) {
			prev = i
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
