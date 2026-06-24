// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"regexp"
	"strings"
)

type dependencyBlock struct {
	section string
	body    string
}

var blockHeaderAtCursor = regexp.MustCompile(`^[ \t]*([A-Za-z_][A-Za-z0-9_.]*)[ \t]*\{`)

func extractDependencyBlocks(source string) []dependencyBlock {
	blocks := make([]dependencyBlock, 0)
	collectBlocks(source, "", &blocks)
	return blocks
}

// collectBlocks walks source line by line, attempting to match a block
// header at the start of each line. When it finds a "dependencies" block it
// captures the body for statement parsing; when it finds a wrapper block
// ("buildscript", "subprojects", "allprojects") it recurses with the wrapper
// name as parent. After handling a block, the cursor jumps to the position
// after the block's closing brace so nested headers are not double-processed
// at this depth.
func collectBlocks(source string, parent string, blocks *[]dependencyBlock) {
	index := 0
	for index < len(source) {
		lineStart := index
		match := blockHeaderAtCursor.FindStringSubmatchIndex(source[lineStart:])
		if match != nil {
			blockName := source[lineStart+match[2] : lineStart+match[3]]
			braceIndex := lineStart + match[1] - 1
			body, ok := captureBraceBody(source, braceIndex)
			if ok {
				switch blockName {
				case "dependencies":
					*blocks = append(*blocks, dependencyBlock{section: parent, body: body})
				case "buildscript", "subprojects", "allprojects":
					collectBlocks(body, blockName, blocks)
				}
				index = braceIndex + 1 + len(body) + 1
				continue
			}
		}
		newline := strings.IndexByte(source[index:], '\n')
		if newline < 0 {
			return
		}
		index += newline + 1
	}
}

// splitDependencyStatements splits a dependencies-block body into individual
// declaration statements on newlines or semicolons at top-level brace and
// paren depth. Unclosed single-line strings are treated as terminated at the
// newline so a malformed declaration does not swallow the rest of the block.
func splitDependencyStatements(body string) []string {
	statements := make([]string, 0)
	depth := 0
	parenDepth := 0
	start := 0
	inString := false
	var stringQuote byte
	flush := func(end int) {
		segment := strings.TrimSpace(body[start:end])
		if segment != "" {
			statements = append(statements, segment)
		}
		start = end + 1
	}
	for index := 0; index < len(body); index++ {
		current := body[index]
		if inString {
			if current == '\\' && index+1 < len(body) {
				index++
				continue
			}
			if current == '\n' {
				inString = false
				if depth == 0 && parenDepth == 0 {
					flush(index)
				}
				continue
			}
			if current == stringQuote {
				inString = false
			}
			continue
		}
		switch current {
		case '\'', '"':
			inString = true
			stringQuote = current
		case '(':
			parenDepth++
		case ')':
			if parenDepth > 0 {
				parenDepth--
			}
		case '{':
			depth++
		case '}':
			if depth > 0 {
				depth--
			}
		case '\n':
			if depth == 0 && parenDepth == 0 {
				flush(index)
			}
		case ';':
			if depth == 0 && parenDepth == 0 {
				flush(index)
			}
		}
	}
	flush(len(body))
	return statements
}
