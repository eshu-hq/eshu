// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"math"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
	tree_sitter "github.com/tree-sitter/go-tree-sitter"
)

// ModuleExportName returns the name an import or export specifier spells at
// node. ES2022 lets a string literal stand wherever a module export name goes
// (`export { a as 'from' } from "m"`, `import { 'b c' as d } from "m"`), and the
// name is then the string's value, not its source text, so 'x' and "x" name the
// same symbol. An identifier node (or any other node) yields its trimmed text;
// a nil node yields the empty string.
func ModuleExportName(node *tree_sitter.Node, source []byte) string {
	if node == nil {
		return ""
	}
	text := strings.TrimSpace(shared.NodeText(node, source))
	if node.Kind() != "string" {
		return text
	}
	return stringLiteralValue(text)
}

// representableModuleName reports whether a decoded module export name can be
// handed to the reducer unchanged. The reducer's import and re-export
// resolution trims every name, so a name with leading or trailing whitespace
// (valid ECMAScript, vanishingly rare) would resolve as the trimmed symbol, a
// different one. The parser skips such a specifier instead, the same miss the
// quoted spelling caused before #7461 and not a wrong edge.
func representableModuleName(name string) bool {
	return name == strings.TrimSpace(name)
}

// unquoteModuleExportName trims text and, when it is spelled as a quoted string
// literal, returns the literal's value. The brace-text fallback reads names from
// raw text, so this is its counterpart of ModuleExportName.
func unquoteModuleExportName(text string) string {
	text = strings.TrimSpace(text)
	if text != "" && (text[0] == '"' || text[0] == '\'') {
		return stringLiteralValue(text)
	}
	return text
}

// stringLiteralValue decodes a quoted JavaScript string literal into its value.
// A literal that is not a well-formed single- or double-quoted string, or that
// holds an escape this decoder does not understand, falls back to the text
// between its quotes so the name is still unquoted rather than dropped.
func stringLiteralValue(literal string) string {
	if len(literal) < 2 {
		return literal
	}
	quote := literal[0]
	if (quote != '"' && quote != '\'') || literal[len(literal)-1] != quote {
		return literal
	}
	body := literal[1 : len(literal)-1]
	if !strings.Contains(body, `\`) {
		return body
	}
	if decoded, ok := decodeStringEscapes(body); ok {
		return decoded
	}
	return body
}

// decodeStringEscapes resolves the ECMAScript escape sequences in the body of a
// string literal: single-character escapes, \xHH, \uHHHH (a surrogate pair is
// spelled as two of them), \u{H...}, \0 and line continuations. It reports
// false for a sequence it cannot decode: a legacy octal escape, a truncated hex
// escape, or an unpaired surrogate half, none of which names a well-formed
// module export name.
func decodeStringEscapes(body string) (string, bool) {
	var out strings.Builder
	out.Grow(len(body))
	for i := 0; i < len(body); {
		if body[i] != '\\' {
			out.WriteByte(body[i])
			i++
			continue
		}
		if i+1 >= len(body) {
			return "", false
		}
		next := body[i+1]
		i += 2
		switch next {
		case 'n':
			out.WriteByte('\n')
		case 'r':
			out.WriteByte('\r')
		case 't':
			out.WriteByte('\t')
		case 'b':
			out.WriteByte('\b')
		case 'f':
			out.WriteByte('\f')
		case 'v':
			out.WriteByte('\v')
		case '0':
			if i < len(body) && body[i] >= '0' && body[i] <= '9' {
				return "", false
			}
			out.WriteByte(0)
		case '\n':
			// Line continuation: the backslash and newline contribute nothing.
		case '\r':
			if i < len(body) && body[i] == '\n' {
				i++
			}
		case 'x':
			value, ok := parseHexRune(body, i, 2)
			if !ok {
				return "", false
			}
			out.WriteRune(value)
			i += 2
		case 'u':
			value, width, ok := parseUnicodeEscape(body, i)
			if !ok {
				return "", false
			}
			i += width
			if utf16.IsSurrogate(value) {
				low, lowWidth, ok := pairedLowSurrogate(body, i, value)
				if !ok {
					return "", false
				}
				value = utf16.DecodeRune(value, low)
				i += lowWidth
			}
			out.WriteRune(value)
		default:
			if next >= '1' && next <= '9' {
				return "", false
			}
			// An escaped character stands for itself, except that a backslash
			// before U+2028 or U+2029 is a line continuation like backslash-newline.
			r, size := utf8.DecodeRuneInString(body[i-1:])
			if r != '\u2028' && r != '\u2029' {
				out.WriteString(body[i-1 : i-1+size])
			}
			i += size - 1
		}
	}
	return out.String(), true
}

// parseUnicodeEscape reads the digits after `\u`: four hex digits, or a braced
// code point. It returns the rune and how many bytes of body it consumed.
func parseUnicodeEscape(body string, start int) (rune, int, bool) {
	if start < len(body) && body[start] == '{' {
		end := strings.IndexByte(body[start:], '}')
		if end < 0 {
			return 0, 0, false
		}
		value, err := strconv.ParseUint(body[start+1:start+end], 16, 32)
		if err != nil || value > utf8.MaxRune {
			return 0, 0, false
		}
		return rune(value), end + 1, true
	}
	value, ok := parseHexRune(body, start, 4)
	if !ok {
		return 0, 0, false
	}
	return value, 4, true
}

// pairedLowSurrogate reads the second half of a UTF-16 surrogate pair at
// body[start:], after the caller decoded high: a backslash-u escape in either
// spelling (four digits or braced) whose value is a low surrogate. It reports
// false when high is not a high surrogate or no low-surrogate escape follows, and
// returns the bytes the second escape took.
func pairedLowSurrogate(body string, start int, high rune) (rune, int, bool) {
	if high >= 0xDC00 || !strings.HasPrefix(body[start:], `\u`) {
		return 0, 0, false
	}
	value, width, ok := parseUnicodeEscape(body, start+2)
	if !ok || value < 0xDC00 || value > 0xDFFF {
		return 0, 0, false
	}
	return value, 2 + width, true
}

// parseHexRune reads count hex digits at body[start:] as a rune. The explicit
// bound keeps the uint64 to int32 conversion provably in range (gosec G115); a
// sequence above it is no code point and no code unit either.
func parseHexRune(body string, start int, count int) (rune, bool) {
	if start+count > len(body) {
		return 0, false
	}
	value, err := strconv.ParseUint(body[start:start+count], 16, 32)
	if err != nil || value > math.MaxInt32 {
		return 0, false
	}
	return rune(value), true
}

// braceClauseSpecifiers returns the comma-separated specifiers between the first
// `{` of an export statement's text and its matching `}`, reading outside quoted
// strings and comments so a name holding a comma, a brace, or the word `as` stays
// whole and an apostrophe in a comment does not open a string. It
// reports false when no balanced brace clause exists.
func braceClauseSpecifiers(text string) ([]string, bool) {
	start := strings.IndexByte(text, '{')
	if start < 0 {
		return nil, false
	}
	var parts []string
	partStart := start + 1
	for i := start + 1; i < len(text); {
		switch text[i] {
		case '\'', '"':
			i = skipQuoted(text, i)
		case '/':
			// A quote inside a comment is not a string: skip comments whole.
			i = skipComment(text, i)
		case ',':
			parts = append(parts, text[partStart:i])
			partStart = i + 1
			i++
		case '}':
			return append(parts, text[partStart:i]), true
		default:
			i++
		}
	}
	return nil, false
}

// tokensOutsideQuotes splits text on whitespace, keeping each quoted string
// (quotes included) inside one token.
func tokensOutsideQuotes(text string) []string {
	var tokens []string
	for i := 0; i < len(text); {
		if text[i] == ' ' || text[i] == '\t' || text[i] == '\n' || text[i] == '\r' {
			i++
			continue
		}
		start := i
		for i < len(text) && text[i] != ' ' && text[i] != '\t' && text[i] != '\n' && text[i] != '\r' {
			if text[i] == '\'' || text[i] == '"' {
				i = skipQuoted(text, i)
				continue
			}
			i++
		}
		tokens = append(tokens, text[start:i])
	}
	return tokens
}

// skipQuoted returns the index just past the quoted string that opens at
// text[open], honouring backslash escapes. An unterminated string runs to the
// end of text.
func skipQuoted(text string, open int) int {
	quote := text[open]
	for i := open + 1; i < len(text); i++ {
		switch text[i] {
		case '\\':
			i++
		case quote:
			return i + 1
		}
	}
	return len(text)
}

// skipComment returns the index just past the comment that opens at text[open]
// (`//` to the end of the line, `/*` to its closing `*/`), or open+1 when the
// slash does not start a comment. An unterminated block comment runs to the end
// of text.
func skipComment(text string, open int) int {
	if open+1 >= len(text) {
		return open + 1
	}
	switch text[open+1] {
	case '/':
		if end := strings.IndexByte(text[open:], '\n'); end >= 0 {
			return open + end + 1
		}
		return len(text)
	case '*':
		if end := strings.Index(text[open+2:], "*/"); end >= 0 {
			return open + 2 + end + 2
		}
		return len(text)
	}
	return open + 1
}
