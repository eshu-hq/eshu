// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"regexp"
	"strings"
)

var (
	defPattern         = regexp.MustCompile(`(?m)^\s*def\s+([A-Za-z_][A-Za-z0-9_]*)\s*=\s*['"]([^'"]+)['"]`)
	extDotPattern      = regexp.MustCompile(`(?m)^\s*(?:project\.)?ext\.([A-Za-z_][A-Za-z0-9_]*)\s*=\s*['"]([^'"]+)['"]`)
	extBlockHeader     = regexp.MustCompile(`(?m)^\s*ext\s*\{`)
	extBlockEntry      = regexp.MustCompile(`(?m)^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=\s*['"]([^'"]+)['"]`)
	valPattern         = regexp.MustCompile(`(?m)^\s*(?:val|var|const\s+val)\s+([A-Za-z_][A-Za-z0-9_]*)\s*(?::\s*[A-Za-z_][A-Za-z0-9_.]*\s*)?=\s*['"]([^'"]+)['"]`)
	interpolationCurly = regexp.MustCompile(`\$\{([^}]+)\}`)
	interpolationBare  = regexp.MustCompile(`\$([A-Za-z_][A-Za-z0-9_]*)`)
)

// stripCommentsAndStringInteriorsKept rewrites source to remove line and
// block comments while preserving string literal interiors verbatim. Unclosed
// single-line strings terminate at the newline so subsequent lines parse
// cleanly rather than getting absorbed into a runaway string.
func stripCommentsAndStringInteriorsKept(source string) string {
	var builder strings.Builder
	builder.Grow(len(source))
	index := 0
	for index < len(source) {
		current := source[index]
		next := byte(0)
		if index+1 < len(source) {
			next = source[index+1]
		}
		if current == '/' && next == '/' {
			for index < len(source) && source[index] != '\n' {
				index++
			}
			continue
		}
		if current == '/' && next == '*' {
			index += 2
			for index+1 < len(source) {
				if source[index] == '*' && source[index+1] == '/' {
					index += 2
					break
				}
				index++
			}
			continue
		}
		if current == '\'' || current == '"' {
			index = copyStringLiteral(source, index, current, &builder)
			continue
		}
		builder.WriteByte(current)
		index++
	}
	return builder.String()
}

func copyStringLiteral(source string, index int, quote byte, builder *strings.Builder) int {
	if index+2 < len(source) && source[index+1] == quote && source[index+2] == quote {
		builder.WriteByte(quote)
		builder.WriteByte(quote)
		builder.WriteByte(quote)
		index += 3
		for index < len(source) {
			if index+2 < len(source) && source[index] == quote && source[index+1] == quote && source[index+2] == quote {
				builder.WriteByte(quote)
				builder.WriteByte(quote)
				builder.WriteByte(quote)
				index += 3
				return index
			}
			builder.WriteByte(source[index])
			index++
		}
		return index
	}
	builder.WriteByte(quote)
	index++
	for index < len(source) {
		if source[index] == '\\' && index+1 < len(source) {
			builder.WriteByte(source[index])
			builder.WriteByte(source[index+1])
			index += 2
			continue
		}
		if source[index] == quote {
			builder.WriteByte(quote)
			index++
			return index
		}
		if source[index] == '\n' {
			return index
		}
		builder.WriteByte(source[index])
		index++
	}
	return index
}

// collectScalarProperties extracts string-valued Groovy `def` and Kotlin
// `val`/`var` declarations, plus `ext.x = '..'` and `ext { x = '..' }`
// blocks, so version interpolations declared in the same file can be
// resolved at parse time.
func collectScalarProperties(source string) map[string]string {
	properties := make(map[string]string)
	for _, match := range defPattern.FindAllStringSubmatch(source, -1) {
		properties[match[1]] = match[2]
	}
	for _, match := range valPattern.FindAllStringSubmatch(source, -1) {
		properties[match[1]] = match[2]
	}
	for _, match := range extDotPattern.FindAllStringSubmatch(source, -1) {
		properties[match[1]] = match[2]
	}
	for _, header := range extBlockHeader.FindAllStringIndex(source, -1) {
		body, ok := captureBraceBody(source, header[1]-1)
		if !ok {
			continue
		}
		for _, match := range extBlockEntry.FindAllStringSubmatch(body, -1) {
			properties[match[1]] = match[2]
		}
	}
	return properties
}

// captureBraceBody returns the substring between the brace at openBraceIndex
// and its matching close brace. Returns false when the index is not a `{`
// or no matching brace is found.
func captureBraceBody(source string, openBraceIndex int) (string, bool) {
	if openBraceIndex < 0 || openBraceIndex >= len(source) || source[openBraceIndex] != '{' {
		return "", false
	}
	depth := 1
	for index := openBraceIndex + 1; index < len(source); index++ {
		switch source[index] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return source[openBraceIndex+1 : index], true
			}
		}
	}
	return "", false
}
