// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package main

import (
	"regexp"
	"strings"
)

// shellAssignmentPattern matches the `NAME=` prefix of a shell variable
// assignment that runs ahead of a command on the same line.
var shellAssignmentPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

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
// A line that carries any unquoted "(", ")", or backtick is out of scope
// whether or not it also carries a list operator, because a subshell or a
// command substitution is not one literal command and its inner words are not
// arguments of the outer one. A line with no unquoted list operator and no such
// grouping is returned unchanged, so the pre-existing single-command behaviour
// is untouched. A line that carries a list operator is admitted only when every
// one of them is exactly a pipe, an AND list, or a semicolon list, and every
// segment is non-empty. Everything else -- "||", a background "&", "|&", ";;",
// a subshell, and command substitution -- keeps the pre-#6108 skip, because a
// segment of those forms is not reliably one literal command and guessing would
// attribute a flag to the wrong command.
func commandSegments(line string) []string {
	segments, separators, grouping, supported := scanShellSegments(line)
	if !supported {
		return nil
	}
	if grouping {
		return nil
	}
	if separators == 0 {
		return []string{line}
	}
	for _, segment := range segments {
		if segment == "" {
			return nil
		}
	}
	return segments
}

// unquotedShellByte describes one byte of a shell line as the shell sees it
// after quoting and escaping are applied.
type unquotedShellByte struct {
	// index is the byte offset of char in the line.
	index int
	// char is the byte itself.
	char byte
	// literal is true when quoting or a backslash escape stripped the byte of
	// any operator, grouping, or comment meaning.
	literal bool
	// wordStart is true when no word byte has been seen since the last blank or
	// shell metacharacter, which is the only position where "#" opens a
	// trailing comment.
	wordStart bool
}

// walkShellLine visits every byte of line with the same quoting and escaping
// rules as splitShellFields: a backslash escapes the next byte everywhere
// except inside single quotes, which process no escapes at all, so a `\"` does
// not close a double-quoted word. The visitor returns false to stop the walk.
// Callers decide what a non-literal byte means; the walk itself only tracks
// quote state and word starts, and it breaks a word on a shell metacharacter as
// well as on a blank so a "#" directly after an operator still opens a comment.
//
// Every scanner in this package that has to tell a real operator from a quoted
// one shares this walk. Two hand-copied versions of these rules drifted apart
// once already (#6108 review): the copy that checked quote state before the
// backslash let `eshu docs verify "a\"|b" --stale` split at the pipe, and every
// flag on the tail escaped validation.
func walkShellLine(line string, visit func(unquotedShellByte) bool) {
	var quote byte
	escaped := false
	started := false
	for index := 0; index < len(line); index++ {
		char := line[index]
		literal := true
		wordStart := !started
		switch {
		case escaped:
			escaped = false
			started = true
		case quote == '\'':
			if char == quote {
				quote = 0
			}
			started = true
		case char == '\\':
			escaped = true
			started = true
		case quote == '"':
			if char == quote {
				quote = 0
			}
			started = true
		case char == '\'' || char == '"':
			quote = char
			started = true
		default:
			literal = false
			switch char {
			case ' ', '\t', '|', '&', ';', '(', ')', '`':
				started = false
			default:
				started = true
			}
		}
		if !visit(unquotedShellByte{index: index, char: char, literal: literal, wordStart: wordStart}) {
			return
		}
	}
}

// scanShellSegments walks line once and reports the trimmed segment text
// between top-level list separators, how many separators it cut on, whether it
// saw an unquoted grouping or substitution character, and whether every
// separator it met is one this scanner supports.
func scanShellSegments(line string) (segments []string, separators int, grouping bool, supported bool) {
	segments = []string{}
	start := 0
	skipUntil := 0
	cutAtComment := false
	supported = true
	cut := func(end int) {
		segments = append(segments, strings.TrimSpace(line[start:end]))
	}
	walkShellLine(line, func(shellByte unquotedShellByte) bool {
		if shellByte.literal || shellByte.index < skipUntil {
			return true
		}
		switch shellByte.char {
		case '#':
			if shellByte.wordStart {
				cut(shellByte.index)
				cutAtComment = true
				return false
			}
		case '(', ')', '`':
			grouping = true
		case '|', '&', ';':
			width := shellSeparatorWidth(line, shellByte.index)
			if width == 0 {
				supported = false
				return false
			}
			cut(shellByte.index)
			separators++
			skipUntil = shellByte.index + width
			start = skipUntil
		}
		return true
	})
	if !supported {
		return nil, separators, grouping, false
	}
	if !cutAtComment {
		cut(len(line))
	}
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

// commandPositionChunks splits line at every unquoted byte that ends one
// command and opens another -- a list operator, a redirection's "&", a subshell
// parenthesis, or a backtick -- and returns the text between them. A trailing
// comment ends the last chunk. The chunks are command positions, not segments:
// unlike commandSegments this never refuses a line, because its callers ask
// what a line contains rather than what may be attributed to it.
func commandPositionChunks(line string) []string {
	chunks := []string{}
	start := 0
	cutAtComment := false
	walkShellLine(line, func(shellByte unquotedShellByte) bool {
		if shellByte.literal {
			return true
		}
		switch shellByte.char {
		case '#':
			if shellByte.wordStart {
				chunks = append(chunks, line[start:shellByte.index])
				cutAtComment = true
				return false
			}
		case '|', '&', ';', '(', ')', '`':
			chunks = append(chunks, line[start:shellByte.index])
			start = shellByte.index + 1
		}
		return true
	})
	if !cutAtComment {
		chunks = append(chunks, line[start:])
	}
	return chunks
}

// mentionsEshuCommand reports whether any command position on a logical line
// invokes the eshu CLI. It feeds the skipped-line count, which #6108 turned
// into an exact pin asserted in both directions, so it matches the same leading
// `eshu` word that eshuCommandFields requires rather than any field equal to
// `eshu`: `docker compose logs eshu 2>&1` names a container, not a command, and
// counting it would fail the gate on an unrelated docs edit.
//
// It differs from eshuCommandFields in one deliberate way: it steps over
// leading `NAME=value` assignments, because a command behind an environment
// prefix is still an eshu command line the scanner declined to parse. This
// count measures the gate's blind spot, and hiding a real blind spot is the
// failure it exists to prevent.
func mentionsEshuCommand(line string) bool {
	for _, chunk := range commandPositionChunks(line) {
		fields := stripConsolePrompt(splitShellFields(strings.TrimSpace(chunk)))
		for len(fields) > 0 && shellAssignmentPattern.MatchString(fields[0]) {
			fields = fields[1:]
		}
		if len(fields) > 0 && fields[0] == "eshu" {
			return true
		}
	}
	return false
}

// stripConsolePrompt drops a leading console prompt (`$` or `>`) from the
// fields of a command, so a copy-pasted console transcript reads the same as a
// bare command line.
func stripConsolePrompt(fields []string) []string {
	if len(fields) > 0 && (fields[0] == "$" || fields[0] == ">") {
		return fields[1:]
	}
	return fields
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
	fields := stripConsolePrompt(splitShellFields(strings.TrimSpace(segment)))
	if len(fields) == 0 || fields[0] != "eshu" {
		return nil, false
	}
	return fields[1:], true
}
