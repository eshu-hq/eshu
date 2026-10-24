// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import "strings"

// keepsVerbatim reports whether the whole remainder of the line, after the
// separator, is exactly an environment reference or an earlier marker,
// optionally in one matching pair of quotes, with nothing after it. Anything
// else, a trailing comment included, is redacted from the separator on.
func keepsVerbatim(value string) bool {
	inner := value
	if n := len(value); n >= 2 && (value[0] == '"' || value[0] == '\'') && value[n-1] == value[0] {
		inner = value[1 : n-1]
	}
	return envReference.MatchString(inner) || existingMarker.MatchString(inner)
}

// stripCommentTagAnchor removes a YAML comment and leading !tag and &anchor
// tokens from a value, returning what is left of the node itself. A tag or
// anchor token is only stripped when more value follows it: a lone "!abc" or
// "&abc" is returned unchanged, so it is redacted as a value, not mistaken for
// an empty one. A comment needs whitespace before "#": spacedAfterColon says
// the separator supplied it for a "#" at the start of the value.
func stripCommentTagAnchor(value string, spacedAfterColon bool) string {
	if strings.HasPrefix(value, "#") && spacedAfterColon {
		return ""
	}
	if cut := strings.Index(value, " #"); cut >= 0 && !startsQuoted(value) {
		value = strings.TrimSpace(value[:cut])
	}
	for value != "" && (value[0] == '!' || value[0] == '&') {
		space := strings.IndexAny(value, " \t")
		if space < 0 {
			return value
		}
		rest := strings.TrimSpace(value[space:])
		if rest == "" || strings.HasPrefix(rest, "#") {
			return value
		}
		value = rest
	}
	return value
}

func startsQuoted(value string) bool {
	return value != "" && (value[0] == '"' || value[0] == '\'')
}

// splitQuotedValue splits a value into its quote character (zero when
// unquoted), the unquoted text, any text after the closing quote, and whether
// the quote closed on this line. An unquoted value is one token; an unterminated
// quote takes the rest of the line as the value, which fails closed.
func splitQuotedValue(value string) (quote byte, inner, rest string, closed bool) {
	if !startsQuoted(value) {
		return 0, value, "", true
	}
	quote = value[0]
	end := quoteEnd(value[1:], quote)
	if end < 0 {
		return quote, value[1:], "", false
	}
	return quote, value[1 : 1+end], value[2+end:], true
}

// quoteEnd returns the index of the first unescaped closing quote in text, or
// -1. Double quotes escape with a backslash; single quotes escape by doubling.
func quoteEnd(text string, quote byte) int {
	for i := 0; i < len(text); i++ {
		switch {
		case quote == '"' && text[i] == '\\':
			i++
		case text[i] == quote && quote == '\'' && i+1 < len(text) && text[i+1] == '\'':
			i++
		case text[i] == quote:
			return i
		}
	}
	return -1
}

func closesQuote(line string, quote byte) bool {
	return quoteEnd(line, quote) >= 0
}
