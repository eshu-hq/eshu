// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package statement

import "strings"

// Placeholder replaces every literal token in a redacted statement. It is the
// same spelling NornicDB's RedactLiterals uses, so an operator can line the two
// texts up.
const Placeholder = "<REDACTED>"

// Redact returns cypher with every literal replaced by Placeholder, comments
// dropped, and runs of whitespace collapsed to one space with the ends
// trimmed. See the package comment for the token classes it covers and what it
// keeps. A leading minus that touches a digit is folded into the number, the
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
		switch {
		case isSpace(c):
			pendingSpace = true
			i++
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
			end := i + 1
			for end < len(cypher) && isIdentChar(cypher[end]) {
				end++
			}
			write(cypher[i:end])
			i = end
		case c == '.' && i+1 < len(cypher) && cypher[i+1] == '.':
			write("..")
			i += 2
		case startsNumber(cypher, i):
			i = skipNumber(cypher, i)
			write(Placeholder)
		case isIdentStart(c):
			end := i + 1
			for end < len(cypher) && isIdentChar(cypher[end]) {
				end++
			}
			write(cypher[i:end])
			i = end
		default:
			write(cypher[i : i+1])
			i++
		}
	}
	return out.String()
}

func isSpace(c byte) bool {
	return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r'
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

func isHexDigit(c byte) bool {
	return isDigit(c) || (c >= 'a' && c <= 'f') || (c >= 'A' && c <= 'F')
}

// isIdentStart reports whether c can begin an identifier. Every byte of a
// multi-byte UTF-8 sequence is >= 0x80 and never equals an ASCII delimiter, so
// treating those bytes as identifier characters keeps unicode identifiers whole
// without decoding runes.
func isIdentStart(c byte) bool {
	return c == '_' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentChar(c byte) bool { return isIdentStart(c) || isDigit(c) }

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

// skipNumber returns the index just past the numeric literal at src[i]. It
// consumes an optional sign, hex or octal digits, or decimal digits with an
// optional fraction, exponent and f/d suffix.
func skipNumber(src string, i int) int {
	if src[i] == '-' {
		i++
	}
	if src[i] == '0' && i+2 < len(src) {
		switch {
		case (src[i+1] == 'x' || src[i+1] == 'X') && isHexDigit(src[i+2]):
			i += 2
			for i < len(src) && isHexDigit(src[i]) {
				i++
			}
			return i
		case (src[i+1] == 'o' || src[i+1] == 'O') && src[i+2] >= '0' && src[i+2] <= '7':
			i += 2
			for i < len(src) && src[i] >= '0' && src[i] <= '7' {
				i++
			}
			return i
		}
	}
	for i < len(src) && isDigit(src[i]) {
		i++
	}
	if i+1 < len(src) && src[i] == '.' && isDigit(src[i+1]) {
		i++
		for i < len(src) && isDigit(src[i]) {
			i++
		}
	}
	if i < len(src) && (src[i] == 'e' || src[i] == 'E') {
		j := i + 1
		if j < len(src) && (src[j] == '+' || src[j] == '-') {
			j++
		}
		if j < len(src) && isDigit(src[j]) {
			for j < len(src) && isDigit(src[j]) {
				j++
			}
			i = j
		}
	}
	if i < len(src) && (src[i] == 'f' || src[i] == 'F' || src[i] == 'd' || src[i] == 'D') &&
		(i+1 == len(src) || !isIdentChar(src[i+1])) {
		i++
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
