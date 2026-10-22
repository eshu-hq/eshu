// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package rank

import (
	"strings"
	"unicode"
)

// stopwords are function words that carry no tool-selection signal. They are
// dropped from queries, names, intents, and descriptions alike so a question
// phrased "where is the function defined" matches on "function" and "defined".
var stopwords = map[string]struct{}{}

func init() {
	for _, w := range strings.Fields(`a an and are as at be been by can could do does for from
		how i if in into is it its me my of on or our that the their then there these this
		those to us was we were what when where which who why will with would you your`) {
		stopwords[w] = struct{}{}
	}
}

// tokens lowercases s, splits it on every non-letter and non-digit rune,
// drops stopwords and one-rune tokens, and stems the rest. The same function
// tokenizes queries and every indexed field, so both sides agree.
func tokens(s string) []string {
	var out []string
	fields := strings.FieldsFunc(strings.ToLower(s), func(r rune) bool {
		return !unicode.IsLetter(r) && !unicode.IsDigit(r)
	})
	for _, f := range fields {
		if len([]rune(f)) < 2 {
			continue
		}
		if _, stop := stopwords[f]; stop {
			continue
		}
		out = append(out, stem(f))
	}
	return out
}

// stem applies a deliberately tiny plural fold: ies->y, sses->ss, and a
// trailing s (but not ss, us, or is). It is the same on both sides of a match,
// so it only needs to be consistent, not linguistically complete.
func stem(w string) string {
	switch {
	case len(w) > 4 && strings.HasSuffix(w, "ies"):
		return w[:len(w)-3] + "y"
	case len(w) > 4 && strings.HasSuffix(w, "sses"):
		return w[:len(w)-2]
	case len(w) > 3 && strings.HasSuffix(w, "s") &&
		!strings.HasSuffix(w, "ss") && !strings.HasSuffix(w, "us") && !strings.HasSuffix(w, "is"):
		return w[:len(w)-1]
	}
	return w
}

// sentences splits a description into sentences on newlines and on sentence
// punctuation followed by whitespace or the end of the text. Empty pieces are
// dropped and the original text of each sentence is kept for the Why line.
func sentences(s string) []string {
	var out []string
	start := 0
	runes := []rune(s)
	flush := func(end int) {
		if piece := strings.Join(strings.Fields(string(runes[start:end])), " "); piece != "" {
			out = append(out, piece)
		}
	}
	for i, r := range runes {
		switch r {
		case '\n', '\r':
			flush(i)
			start = i + 1
		case '.', '!', '?':
			if i+1 == len(runes) || unicode.IsSpace(runes[i+1]) {
				flush(i + 1)
				start = i + 1
			}
		}
	}
	flush(len(runes))
	return out
}

// sentenceKey normalizes a sentence for repeat counting: case, spacing, and
// trailing punctuation do not make two copies of the same boilerplate differ.
func sentenceKey(s string) string {
	return strings.ToLower(strings.TrimRight(strings.Join(strings.Fields(s), " "), ".!? "))
}

// oneLine collapses all whitespace to single spaces and truncates to max runes
// at a word boundary, appending an ellipsis when it cut.
func oneLine(s string, max int) string {
	s = strings.Join(strings.Fields(s), " ")
	r := []rune(s)
	if len(r) <= max {
		return s
	}
	cut := string(r[:max])
	if i := strings.LastIndex(cut, " "); i > max/2 {
		cut = cut[:i]
	}
	return cut + "…"
}
