// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package catalog

import "bytes"

// nestingExceeds reports whether the document nests arrays or inline tables
// deeper than limit. The TOML parser recurses once per level and has no depth
// bound of its own: a measured one-million-level input overflows the goroutine
// stack with a fatal, unrecoverable error, and a 256 KiB input of the same
// shape allocates about 77 MB. This check runs first so neither can happen.
//
// It is a single linear pass with a small lexer that skips comments and the
// four TOML string forms, so brackets inside a string never count. When the
// lexer and the real parser disagree about a string boundary the lexer can
// only over-report depth on the rest of that line, which fails closed.
func nestingExceeds(source []byte, limit int) bool {
	depth := 0
	for i := 0; i < len(source); {
		switch source[i] {
		case '#':
			i = skipLine(source, i)
		case '"':
			if bytes.HasPrefix(source[i:], []byte(`"""`)) {
				i = skipMultiline(source, i+3, '"', true)
			} else {
				i = skipSingleLine(source, i+1, '"', true)
			}
		case '\'':
			if bytes.HasPrefix(source[i:], []byte(`'''`)) {
				i = skipMultiline(source, i+3, '\'', false)
			} else {
				i = skipSingleLine(source, i+1, '\'', false)
			}
		case '[', '{':
			depth++
			if depth > limit {
				return true
			}
			i++
		case ']', '}':
			if depth > 0 {
				depth--
			}
			i++
		default:
			i++
		}
	}
	return false
}

// skipLine returns the index of the newline that ends the line holding from,
// or len(source) when the line is the last one.
func skipLine(source []byte, from int) int {
	if end := bytes.IndexByte(source[from:], '\n'); end >= 0 {
		return from + end
	}
	return len(source)
}

// skipSingleLine returns the index after the closing quote of a one-line
// string that starts at from, or the index of the newline that ends an
// unterminated one. A basic string honours backslash escapes.
func skipSingleLine(source []byte, from int, quote byte, escapes bool) int {
	for i := from; i < len(source); i++ {
		switch {
		case source[i] == '\n':
			return i
		case escapes && source[i] == '\\':
			i++
		case source[i] == quote:
			return i + 1
		}
	}
	return len(source)
}

// skipMultiline returns the index after the closing triple quote of a
// multi-line string, or len(source) when it never closes.
func skipMultiline(source []byte, from int, quote byte, escapes bool) int {
	for i := from; i < len(source); i++ {
		switch {
		case escapes && source[i] == '\\':
			i++
		case source[i] == quote && i+2 < len(source) && source[i+1] == quote && source[i+2] == quote:
			return i + 3
		}
	}
	return len(source)
}
