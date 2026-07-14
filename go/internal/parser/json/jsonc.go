// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import "strings"

// stripJSONCComments removes `//` and `/* */` comments from a JSONC source
// string, leaving string literal contents (including any `//` or `/*` text
// inside a quoted string) untouched.
func stripJSONCComments(source string) string {
	var builder strings.Builder
	inString := false
	escapeNext := false
	for index := 0; index < len(source); index++ {
		current := source[index]
		if inString {
			builder.WriteByte(current)
			if escapeNext {
				escapeNext = false
				continue
			}
			if current == '\\' {
				escapeNext = true
				continue
			}
			if current == '"' {
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			builder.WriteByte(current)
			continue
		}
		if current == '/' && index+1 < len(source) {
			next := source[index+1]
			if next == '/' {
				for index < len(source) && source[index] != '\n' {
					index++
				}
				if index < len(source) {
					builder.WriteByte(source[index])
				}
				continue
			}
			if next == '*' {
				index += 2
				for index+1 < len(source) && (source[index] != '*' || source[index+1] != '/') {
					index++
				}
				index++
				continue
			}
		}
		builder.WriteByte(current)
	}
	return builder.String()
}

// stripTrailingCommas removes a trailing comma immediately before a closing
// `}` or `]` (ignoring whitespace), leaving string literal contents
// untouched. JSONC tooling commonly allows trailing commas; encoding/json
// does not, so normalizeJSONSource applies this before decoding.
func stripTrailingCommas(source string) string {
	var builder strings.Builder
	inString := false
	escapeNext := false
	for index := 0; index < len(source); index++ {
		current := source[index]
		if inString {
			builder.WriteByte(current)
			if escapeNext {
				escapeNext = false
				continue
			}
			if current == '\\' {
				escapeNext = true
				continue
			}
			if current == '"' {
				inString = false
			}
			continue
		}
		if current == '"' {
			inString = true
			builder.WriteByte(current)
			continue
		}
		if current == ',' {
			next := nextNonWhitespaceIndex(source, index+1)
			if next < len(source) && (source[next] == '}' || source[next] == ']') {
				continue
			}
		}
		builder.WriteByte(current)
	}
	return builder.String()
}

func nextNonWhitespaceIndex(source string, start int) int {
	for start < len(source) {
		switch source[start] {
		case ' ', '\t', '\r', '\n':
			start++
		default:
			return start
		}
	}
	return start
}
