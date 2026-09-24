// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package statement

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// Placeholder replaces every numeric and string literal in a redacted
// statement (booleans and null are kept). It is the
// same spelling NornicDB's RedactLiterals uses, so an operator can line the two
// texts up.
const Placeholder = "<REDACTED>"

// Redact returns cypher with every numeric and string literal replaced by
// Placeholder (booleans and null are kept), comments dropped, and runs of
// whitespace collapsed to one space with the ends trimmed. See the package
// comment for the token classes it covers and what it keeps. A leading minus that touches a digit is folded into the number, the
// way NornicDB's lexer tokenizes it. Comments are dropped rather than kept
// because free text in a comment can carry the same values a literal would.
func Redact(cypher string) string {
	var out strings.Builder
	// Headroom: a short literal such as 'go' becomes the ten-byte Placeholder,
	// so the output can outgrow the input and a bare len() would regrow once.
	out.Grow(len(cypher) + len(cypher)/4)
	pendingSpace := false
	write := func(text string) {
		if pendingSpace && out.Len() > 0 {
			out.WriteByte(' ')
		}
		pendingSpace = false
		out.WriteString(text)
	}

	for i := 0; i < len(cypher); {
		c := cypher[i]
		if c == ' ' || c >= utf8.RuneSelf || (c >= '\t' && c <= '\r') || (c >= 0x1c && c <= 0x1f) {
			if n := spaceLen(cypher, i); n > 0 {
				pendingSpace = true
				i += n
				continue
			}
		}
		switch {
		case c == '/' && i+1 < len(cypher) && cypher[i+1] == '/':
			if end := strings.IndexByte(cypher[i:], '\n'); end >= 0 {
				i += end
			} else {
				i = len(cypher)
			}
			pendingSpace = true
		case c == '/' && i+1 < len(cypher) && cypher[i+1] == '*':
			if end := strings.Index(cypher[i+2:], "*/"); end >= 0 {
				i += 2 + end + 2
			} else {
				i = len(cypher)
			}
			pendingSpace = true
		case c == '\'' || c == '"':
			i = skipString(cypher, i)
			write(Placeholder)
		case c == '`':
			end, terminated := skipBacktick(cypher, i)
			if terminated {
				write(cypher[i:end])
			} else {
				write(Placeholder)
			}
			i = end
		case c == '$':
			end := skipIdentifier(cypher, i+1)
			write(cypher[i:end])
			i = end
		case c == '.' && i+1 < len(cypher) && cypher[i+1] == '.':
			write("..")
			i += 2
		case isIdentStart(c):
			end := skipIdentifier(cypher, i)
			write(cypher[i:end])
			i = end
		case startsNumber(cypher, i):
			i = skipNumber(cypher, i)
			write(Placeholder)
		default:
			write(cypher[i : i+1])
			i++
		}
	}
	return out.String()
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// asciiIdent marks the ASCII bytes that continue an identifier: letters,
// digits and underscore. A table lookup keeps the per-byte identifier loop
// branch-light on the hot path.
var asciiIdent = func() (table [utf8.RuneSelf]bool) {
	for c := 0; c < utf8.RuneSelf; c++ {
		b := byte(c)
		table[c] = isIdentStart(b) || isDigit(b)
	}
	return table
}()

// isIdentStart reports whether c can begin an identifier. Every byte of a
// multi-byte UTF-8 sequence is >= 0x80 and never equals an ASCII delimiter, so
// treating those bytes as identifier bytes keeps unicode identifiers whole
// without decoding runes. Callers test spaceLen first: a multi-byte space
// character is whitespace, not an identifier byte.
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

// spaceLen returns the byte length of the whitespace character at src[i], or 0
// when there is none. Whitespace is ASCII space, tab, newline, vertical tab,
// form feed, carriage return and the ASCII separators 0x1C-0x1F, plus every
// character unicode.IsSpace accepts. That union covers the Neo4j 5 lexer's
// SPACE rule (U+00A0, U+1680, U+2000-U+200A, U+2028, U+2029, U+202F, U+205F,
// U+3000). U+180E, U+200B and U+FEFF are whitespace in neither and stay
// identifier bytes, as in Neo4j's PART_LETTER.
func spaceLen(src string, i int) int {
	c := src[i]
	if c < utf8.RuneSelf {
		if c == ' ' || (c >= '\t' && c <= '\r') || (c >= 0x1c && c <= 0x1f) {
			return 1
		}
		return 0
	}
	r, size := utf8.DecodeRuneInString(src[i:])
	if r != utf8.RuneError && unicode.IsSpace(r) {
		return size
	}
	return 0
}

// identLen returns the byte length of the identifier character at src[i], or 0
// when src[i] is not one: an ASCII letter, digit or underscore, or any
// non-whitespace multi-byte character.
func identLen(src string, i int) int {
	c := src[i]
	if c < utf8.RuneSelf {
		if isIdentStart(c) || isDigit(c) {
			return 1
		}
		return 0
	}
	if spaceLen(src, i) > 0 {
		return 0
	}
	_, size := utf8.DecodeRuneInString(src[i:])
	return size
}

// numberTailLen returns the byte length of a character that continues a
// numeric token at src[i], or 0. Neo4j 5 lexes every PART_LETTER that follows a
// number's digits into the same token (digit separators such as 4111_1111,
// hex and octal digits, suffixes, and control characters). Taking all of them
// is a superset, so a value cannot survive as a kept identifier.
func numberTailLen(src string, i int) int {
	c := src[i]
	if c < utf8.RuneSelf && (c == '$' || c <= 0x08 || (c >= 0x0e && c <= 0x1b) || c == 0x7f) {
		return 1
	}
	return identLen(src, i)
}

// skipIdentifier returns the index just past the identifier characters that
// start at src[i]. ASCII is handled inline: this loop runs over most of a
// statement, and a call per byte showed up in BenchmarkGraphStatementFingerprint.
func skipIdentifier(src string, i int) int {
	for i < len(src) {
		if c := src[i]; c < utf8.RuneSelf {
			if !asciiIdent[c] {
				break
			}
			i++
			continue
		}
		n := identLen(src, i)
		if n == 0 {
			break
		}
		i += n
	}
	return i
}

// startsNumber reports whether a numeric literal begins at src[i]: a digit, a
// .digit, or a minus immediately followed by either.
func startsNumber(src string, i int) bool {
	if src[i] == '-' {
		i++
		if i >= len(src) {
			return false
		}
	}
	if isDigit(src[i]) {
		return true
	}
	return src[i] == '.' && i+1 < len(src) && isDigit(src[i+1])
}

// startsExponentDigits reports whether an exponent's digits begin at src[i]: a
// digit, or a digit separator followed by a digit (Neo4j 5 INTEGER_PART is
// `'_'? [0-9]`), as in 1e-_5.
func startsExponentDigits(src string, i int) bool {
	if i < len(src) && src[i] == '_' {
		i++
	}
	return i < len(src) && isDigit(src[i])
}

// skipNumber returns the index just past the numeric literal at src[i]. It
// consumes an optional sign and leading dot, then every character that
// continues a number token (numberTailLen), a fraction whose dot is followed by
// a digit, and an exponent sign that follows an e or E and precedes a digit or
// a separator and a digit.
// The `..` of a range such as 1..5 ends the number, so both bounds redact
// separately.
func skipNumber(src string, i int) int {
	if src[i] == '-' {
		i++
	}
	if src[i] == '.' {
		i++
	}
	for i < len(src) {
		if n := numberTailLen(src, i); n > 0 {
			i += n
			continue
		}
		next := i + 1
		switch c := src[i]; {
		case c == '.' && next < len(src) && isDigit(src[next]):
			i++
		case (c == '+' || c == '-') && (src[i-1] == 'e' || src[i-1] == 'E') && startsExponentDigits(src, next):
			i++
		default:
			return i
		}
	}
	return i
}

// skipString returns the index just past the quoted string opening at src[i].
// A backslash escapes the next byte and a doubled quote continues the string.
// An unterminated string runs to the end of src, which redacts everything
// after the opening quote.
func skipString(src string, i int) int {
	quote := src[i]
	i++
	for i < len(src) {
		switch src[i] {
		case '\\':
			i += 2
		case quote:
			if i+1 < len(src) && src[i+1] == quote {
				i += 2
				continue
			}
			return i + 1
		default:
			i++
		}
	}
	return len(src)
}

// skipBacktick returns the index just past the backtick-quoted identifier
// opening at src[i] and whether its closing backtick was found. A doubled
// backtick is an escaped backtick inside the identifier.
func skipBacktick(src string, i int) (int, bool) {
	i++
	for i < len(src) {
		if src[i] != '`' {
			i++
			continue
		}
		if i+1 < len(src) && src[i+1] == '`' {
			i += 2
			continue
		}
		return i + 1, true
	}
	return len(src), false
}
