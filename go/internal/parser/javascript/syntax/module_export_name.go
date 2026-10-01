// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package syntax

import (
	"strconv"
	"strings"
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
// string literal: single-character escapes, \xHH, \uHHHH, \u{H...}, \0 and line
// continuations. It reports false for a sequence it cannot decode (a legacy
// octal escape or a truncated hex escape).
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
			value, ok := parseHexDigits(body, i, 2)
			if !ok {
				return "", false
			}
			out.WriteRune(rune(value))
			i += 2
		case 'u':
			value, width, ok := parseUnicodeEscape(body, i)
			if !ok {
				return "", false
			}
			out.WriteRune(value)
			i += width
		default:
			if next >= '1' && next <= '9' {
				return "", false
			}
			// \\, \', \" and every other escaped character stand for themselves.
			_, size := utf8.DecodeRuneInString(body[i-1:])
			out.WriteString(body[i-1 : i-1+size])
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
	value, ok := parseHexDigits(body, start, 4)
	if !ok {
		return 0, 0, false
	}
	return rune(value), 4, true
}

func parseHexDigits(body string, start int, count int) (uint64, bool) {
	if start+count > len(body) {
		return 0, false
	}
	value, err := strconv.ParseUint(body[start:start+count], 16, 32)
	if err != nil {
		return 0, false
	}
	return value, true
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
