// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package facet

import (
	"strings"
	"unicode"
)

// containsPhrase reports whether s contains the exact phrase (with word
// boundaries on both sides or at the string edges).
func containsPhrase(s, phrase string) bool {
	idx := strings.Index(s, phrase)
	if idx < 0 {
		return false
	}
	end := idx + len(phrase)
	// Check that the phrase is not part of a longer word.
	before := idx == 0 || !isWordChar(rune(s[idx-1]))
	after := end >= len(s) || !isWordChar(rune(s[end]))
	return before && after
}

// containsWholeWord reports whether word appears as a whole word in s.
func containsWholeWord(s, word string) bool {
	start := 0
	for {
		idx := strings.Index(s[start:], word)
		if idx < 0 {
			return false
		}
		abs := start + idx
		end := abs + len(word)
		before := abs == 0 || !isWordChar(rune(s[abs-1]))
		after := end >= len(s) || !isWordChar(rune(s[end]))
		if before && after {
			return true
		}
		start = abs + 1
	}
}

// extractFirstWord returns the first contiguous sequence of word characters
// from s (letters, digits, hyphens within a word). Hyphens are treated as
// word characters only when surrounded by letters (e.g. "my-tool").
func extractFirstWord(s string) string {
	s = strings.TrimLeftFunc(s, func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
	end := 0
	for end < len(s) {
		r := rune(s[end])
		if unicode.IsLetter(r) || unicode.IsDigit(r) || r == '-' || r == '_' {
			end++
		} else {
			break
		}
	}
	return strings.TrimRight(s[:end], "-_")
}

// isWordChar reports whether r is a word character (letter, digit, underscore,
// or hyphen).
func isWordChar(r rune) bool {
	return unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-'
}

// isCommonWord rejects short articles, prepositions, and pronouns that are
// not plausible tool names.
func isCommonWord(w string) bool {
	switch w {
	case "a", "an", "the", "it", "its", "my", "our", "their",
		"this", "that", "some", "all", "any", "no",
		"in", "on", "at", "to", "by", "of", "or", "and",
		"we", "they", "i", "me", "you", "he", "she":
		return true
	}
	return false
}

// tokenize splits a lowercased question string into word tokens, stripping
// punctuation. Only sequences of letter/digit characters are returned; hyphens
// and underscores that join two letter sequences are preserved within a token.
func tokenize(lower string) []string {
	var words []string
	start := -1
	for i, r := range lower {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			if start < 0 {
				start = i
			}
		} else {
			if start >= 0 {
				words = append(words, strings.TrimRight(lower[start:i], "-_"))
				start = -1
			}
		}
	}
	if start >= 0 {
		words = append(words, strings.TrimRight(lower[start:], "-_"))
	}
	return words
}
