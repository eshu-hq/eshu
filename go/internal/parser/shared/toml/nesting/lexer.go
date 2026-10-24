// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package nesting

import "bytes"

// skipComment returns the index of the newline that ends the comment starting
// at from, or len(source) when the comment ends the file. The newline is left
// for the caller, because it also ends a top-level expression.
func skipComment(source []byte, from int) int {
	if end := bytes.IndexByte(source[from:], '\n'); end >= 0 {
		return from + end
	}
	return len(source)
}

// skipString returns the index just after the string that opens at from,
// where source[from] is a double or a single quote. ok is false when the
// string never closes.
//
// It follows the go-toml v2.3.1 scanner: a basic string honours backslash
// escapes and ends at the next unescaped quote; a literal string has no
// escapes; neither may cross a newline. Three quotes open a multi-line
// string, which may hold newlines and ends at the first run of three quotes,
// taking up to two more quotes from that run as content.
func skipString(source []byte, from int) (end int, ok bool) {
	quote := source[from]
	escapes := quote == '"'
	if hasTripleQuote(source, from, quote) {
		return skipMultiline(source, from+3, quote, escapes)
	}
	for i := from + 1; i < len(source); i++ {
		switch source[i] {
		case quote:
			return i + 1, true
		case '\n':
			return i, false
		case '\\':
			if escapes {
				i++
			}
		}
	}
	return len(source), false
}

// skipMultiline returns the index just after the closing delimiter of a
// multi-line string whose body starts at from.
func skipMultiline(source []byte, from int, quote byte, escapes bool) (int, bool) {
	for i := from; i < len(source); i++ {
		switch {
		case escapes && source[i] == '\\':
			i++
		case hasTripleQuote(source, i, quote):
			// One or two quotes of content may sit right before the closing
			// delimiter, so `""""` is a quote then the close and `"""""` is
			// two quotes then the close. Take the whole run, up to five.
			end := i + 3
			for extra := 0; extra < 2 && end < len(source) && source[end] == quote; extra++ {
				end++
			}
			return end, true
		}
	}
	return len(source), false
}

// hasTripleQuote reports whether source holds three quote bytes at at.
func hasTripleQuote(source []byte, at int, quote byte) bool {
	return at+2 < len(source) && source[at] == quote && source[at+1] == quote && source[at+2] == quote
}
