// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package json

import "strings"

// stripJSONCCommentsWithOffsets removes `//` and `/* */` comments from a
// JSONC source string, leaving string literal contents (including any `//`
// or `/*` text inside a quoted string) untouched. It also returns a parallel
// offset map: offsets[j] is the byte index in source that produced result[j],
// for every j in [0, len(result)]; offsets[len(result)] is the sentinel
// len(source) (issue #5358). A `/* ... */` block comment writes nothing to
// result for its full span -- including any '\n' bytes inside it -- so a
// caller that needs real on-disk line numbers for offsets into result MUST
// translate through this map before consulting a newlineIndex built over the
// original source; counting '\n' bytes in result directly undercounts every
// line after a multi-line block comment by however many newlines the comment
// swallowed.
func stripJSONCCommentsWithOffsets(source string) (string, []int64) {
	var builder strings.Builder
	offsets := make([]int64, 0, len(source)+1)
	write := func(b byte, at int) {
		builder.WriteByte(b)
		offsets = append(offsets, int64(at))
	}
	inString := false
	escapeNext := false
	for index := 0; index < len(source); index++ {
		current := source[index]
		if inString {
			write(current, index)
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
			write(current, index)
			continue
		}
		if current == '/' && index+1 < len(source) {
			next := source[index+1]
			if next == '/' {
				for index < len(source) && source[index] != '\n' {
					index++
				}
				if index < len(source) {
					write(source[index], index)
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
		write(current, index)
	}
	offsets = append(offsets, int64(len(source)))
	return builder.String(), offsets
}

// stripTrailingCommas removes a trailing comma immediately before a closing
// `}` or `]` (ignoring whitespace), leaving string literal contents
// untouched. JSONC tooling commonly allows trailing commas; encoding/json
// does not, so normalizeJSONSource applies this before decoding.
func stripTrailingCommas(source string) string {
	result, _ := stripTrailingCommasWithOffsets(source)
	return result
}

// stripTrailingCommasWithOffsets is stripTrailingCommas plus a parallel
// offset map with the same contract as stripJSONCCommentsWithOffsets:
// offsets[j] is the byte index in source that produced result[j], for every j
// in [0, len(result)], with offsets[len(result)] == len(source) (issue
// #5358).
func stripTrailingCommasWithOffsets(source string) (string, []int64) {
	var builder strings.Builder
	offsets := make([]int64, 0, len(source)+1)
	write := func(b byte, at int) {
		builder.WriteByte(b)
		offsets = append(offsets, int64(at))
	}
	inString := false
	escapeNext := false
	for index := 0; index < len(source); index++ {
		current := source[index]
		if inString {
			write(current, index)
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
			write(current, index)
			continue
		}
		if current == ',' {
			next := nextNonWhitespaceIndex(source, index+1)
			if next < len(source) && (source[next] == '}' || source[next] == ']') {
				continue
			}
		}
		write(current, index)
	}
	offsets = append(offsets, int64(len(source)))
	return builder.String(), offsets
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
