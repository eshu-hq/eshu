// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package declaration

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// eachToken calls fn with the index and text of every whitespace-separated token
// of one pin-file line, in order, until fn returns false. A placeholder span
// stays inside one token even when it holds spaces: "${{ inputs.node-version
// }}", "{{ v }}" and "${ NODE_VERSION }". A span that is never closed keeps the
// rest of the line as one token, so a broken template is reported as one
// unresolved declaration instead of being cut into fragments that classify as
// aliases.
//
// The scan is a single pass over the line with no backtracking, so a hostile
// line of repeated openers costs O(len(line)). It allocates nothing: tokens are
// substrings of line, so a file over the token cap is counted without a slice
// per line.
func eachToken(line string, fn func(index int, token string) bool) {
	scanner := tokenScanner{line: line}
	for index := 0; ; index++ {
		start, end, ok := scanner.next()
		if !ok || !fn(index, line[start:end]) {
			return
		}
	}
}

// tokenScanner walks the tokens of one line by byte offset, with the same
// splitting rule as eachToken. It lets a caller look at a token's neighbours
// and slice a span of several tokens out of the line without allocating.
type tokenScanner struct {
	line string
	pos  int
}

// next returns the byte offsets of the next token, or ok false at the end of
// the line.
func (s *tokenScanner) next() (start, end int, ok bool) {
	i := skipSpace(s.line, s.pos)
	if i >= len(s.line) {
		s.pos = i
		return 0, 0, false
	}
	start = i
	for i < len(s.line) {
		if closer, isPlaceholder := placeholderEnd(s.line, i); isPlaceholder {
			i = closer
			continue
		}
		r, width := utf8.DecodeRuneInString(s.line[i:])
		if unicode.IsSpace(r) {
			break
		}
		i += width
	}
	s.pos = i
	return start, i, true
}

// skipSpace returns the index of the first non-space rune at or after i.
func skipSpace(line string, i int) int {
	for i < len(line) {
		r, width := utf8.DecodeRuneInString(line[i:])
		if !unicode.IsSpace(r) {
			return i
		}
		i += width
	}
	return i
}

// placeholderEnd reports whether a placeholder opener starts at line[i] and,
// if so, the index just past its closer. "${{" and "{{" close on the matching
// "}}" with nesting; "${" closes on the next "}". An opener with no closer
// consumes the rest of the line.
func placeholderEnd(line string, i int) (int, bool) {
	rest := line[i:]
	switch {
	case strings.HasPrefix(rest, "${{"):
		return doubleBraceEnd(line, i+1), true
	case strings.HasPrefix(rest, "{{"):
		return doubleBraceEnd(line, i), true
	case strings.HasPrefix(rest, "${"):
		if closer := strings.IndexByte(rest, '}'); closer >= 0 {
			return i + closer + 1, true
		}
		return len(line), true
	}
	return i, false
}

// doubleBraceEnd scans from the "{{" at line[i:] and returns the index past the
// "}}" that balances it, or len(line) when the span is never closed.
func doubleBraceEnd(line string, i int) int {
	depth := 0
	for i < len(line) {
		switch {
		case strings.HasPrefix(line[i:], "{{"):
			depth++
			i += 2
		case strings.HasPrefix(line[i:], "}}"):
			depth--
			i += 2
			if depth == 0 {
				return i
			}
		default:
			i++
		}
	}
	return len(line)
}
