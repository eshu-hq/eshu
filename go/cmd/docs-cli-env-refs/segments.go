// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import "strings"

// commandSegments returns the command segments of one logical shell line that
// the flag scanner is willing to attribute flags to, or nil when the line stays
// outside the gate's scope.
//
// The supported grammar is deliberately narrow (#6108):
//
//	list    := segment ( SEP segment )*
//	SEP     := "|" | "&&" | ";"     (unquoted, unescaped, outside a comment)
//	segment := one literal command with no list operator of its own
//
// A line with no unquoted list operator is returned unchanged, so the
// pre-existing single-command behaviour is untouched. A line that carries any
// unquoted list operator is admitted only when every one of them is exactly a
// pipe, an AND list, or a semicolon list, every segment is non-empty, and the
// line contains no unquoted "(", ")", or backtick. Everything else -- "||",
// a background "&", "|&", ";;", a subshell, and command substitution -- keeps
// the pre-#6108 skip, because a segment of those forms is not reliably one
// literal command and guessing would attribute a flag to the wrong command.
func commandSegments(line string) []string {
	segments, separators, grouping, supported := scanShellSegments(line)
	if !supported {
		return nil
	}
	if separators == 0 {
		return []string{line}
	}
	if grouping {
		return nil
	}
	for _, segment := range segments {
		if segment == "" {
			return nil
		}
	}
	return segments
}

// scanShellSegments walks line once with the same quoting, escaping, and
// trailing-comment rules as splitShellFields and reports the trimmed segment
// text between top-level list separators, how many separators it cut on,
// whether it saw an unquoted grouping or substitution character, and whether
// every separator it met is one this scanner supports.
func scanShellSegments(line string) (segments []string, separators int, grouping bool, supported bool) {
	segments = []string{}
	start := 0
	var quote byte
	escaped := false
	started := false
	cut := func(end int) {
		segments = append(segments, strings.TrimSpace(line[start:end]))
	}
	for index := 0; index < len(line); index++ {
		char := line[index]
		if escaped {
			escaped = false
			started = true
			continue
		}
		if quote != 0 {
			if char == quote {
				quote = 0
			}
			started = true
			continue
		}
		switch char {
		case '\\':
			escaped = true
			started = true
		case '\'', '"':
			quote = char
			started = true
		case '#':
			if !started {
				cut(index)
				return segments, separators, grouping, true
			}
			started = true
		case ' ', '\t':
			started = false
		case '(', ')', '`':
			grouping = true
			started = true
		case '|', '&', ';':
			width := shellSeparatorWidth(line, index)
			if width == 0 {
				return nil, separators, grouping, false
			}
			cut(index)
			separators++
			index += width - 1
			start = index + 1
			started = false
		default:
			started = true
		}
	}
	cut(len(line))
	return segments, separators, grouping, true
}

// shellSeparatorWidth returns the byte width of the supported list separator
// starting at index, or 0 when the operator there is one this scanner refuses
// to interpret.
func shellSeparatorWidth(line string, index int) int {
	next := byte(0)
	if index+1 < len(line) {
		next = line[index+1]
	}
	switch line[index] {
	case '|':
		if next == '|' || next == '&' {
			return 0
		}
		return 1
	case '&':
		if next != '&' {
			return 0
		}
		if index+2 < len(line) && line[index+2] == '&' {
			return 0
		}
		return 2
	case ';':
		if next == ';' || next == '&' {
			return 0
		}
		return 1
	}
	return 0
}

// mentionsEshuCommand reports whether a logical line carries a bare `eshu`
// word. It only feeds the skipped-line count, so a quoted literal `eshu` counts
// too: over-reporting a diagnostic is safer than under-reporting one.
func mentionsEshuCommand(line string) bool {
	for _, field := range splitShellFields(line) {
		if field == "eshu" {
			return true
		}
	}
	return false
}

// eshuCommandFields returns the arguments of the eshu invocation in one command
// segment. The second result reports whether the segment invokes the eshu CLI
// at all, which is what separates an attributed segment from a neighbouring
// non-Eshu stage such as `cat` in `cat story.json | eshu service-report`. A
// leading console prompt (`$` or `>`) is stripped.
func eshuCommandFields(segment string) ([]string, bool) {
	if containsUnquotedShellListOperator(segment) {
		return nil, false
	}
	fields := splitShellFields(strings.TrimSpace(segment))
	if len(fields) > 0 && (fields[0] == "$" || fields[0] == ">") {
		fields = fields[1:]
	}
	if len(fields) == 0 || fields[0] != "eshu" {
		return nil, false
	}
	return fields[1:], true
}
