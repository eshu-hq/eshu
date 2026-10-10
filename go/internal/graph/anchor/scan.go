// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package anchor

import "strings"

// Text-scanning helpers for the Cypher scan in parse.go: bracket depth, literal
// and comment blanking, top-level comma splits, and pattern masking.

// maskedSpan returns text[from:to] with the recognized node patterns and the
// relationship brackets blanked out.
func maskedSpan(text string, from, to int, nodeRanges [][2]int) string {
	masked := []byte(text[from:to])
	blank := func(a, b int) {
		for k := max(a, from); k < min(b, to); k++ {
			masked[k-from] = ' '
		}
	}
	for _, r := range nodeRanges {
		blank(r[0], r[1])
	}
	for _, r := range relationshipBrackets(text) {
		blank(r[0], r[1])
	}
	return string(masked)
}

// relationshipBrackets returns the [start, end) ranges of every bracket
// group opened by an arrow dash: the -[r:TYPE {props}]- of a pattern.
func relationshipBrackets(text string) [][2]int {
	var out [][2]int
	for i := 0; i < len(text); i++ {
		if text[i] != '-' {
			continue
		}
		j := i + 1
		for j < len(text) && (text[j] == ' ' || text[j] == '\n' || text[j] == '\t') {
			j++
		}
		if j >= len(text) || text[j] != '[' {
			continue
		}
		depth := 0
		for k := j; k < len(text); k++ {
			switch text[k] {
			case '[':
				depth++
			case ']':
				depth--
				if depth == 0 {
					out = append(out, [2]int{j, k + 1})
					i = k
					k = len(text)
				}
			}
		}
	}
	return out
}

// bracketDepths returns the bracket depth, over (), [], and {}, at every byte.
func bracketDepths(text string) []int {
	depths := make([]int, len(text)+1)
	depth := 0
	for i := 0; i < len(text); i++ {
		depths[i] = depth
		switch text[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		}
	}
	depths[len(text)] = depth
	return depths
}

// isPatternStart reports whether the '(' at pos opens a node pattern rather
// than a function call: a '(' glued to a preceding identifier is a call
// unless that identifier is a clause keyword.
func isPatternStart(text string, pos int) bool {
	if pos == 0 || !isWordByte(text[pos-1]) {
		return true
	}
	end := pos
	start := end
	for start > 0 && isWordByte(text[start-1]) {
		start--
	}
	switch strings.ToUpper(text[start:end]) {
	case "MERGE", "MATCH", "CREATE":
		return true
	}
	return false
}

func isWordByte(b byte) bool {
	return b == '_' || b >= '0' && b <= '9' || b >= 'A' && b <= 'Z' || b >= 'a' && b <= 'z'
}

func previousNonSpace(text string, pos int) byte {
	for i := pos - 1; i >= 0; i-- {
		if text[i] != ' ' && text[i] != '\n' && text[i] != '\t' {
			return text[i]
		}
	}
	return 0
}

func slice(text string, from, to int) string {
	if from < 0 || to < 0 {
		return ""
	}
	return text[from:to]
}

// splitTopLevel splits text on commas that sit outside (), [], and {}.
func splitTopLevel(text string) []string {
	var out []string
	depth, last := 0, 0
	for i := 0; i < len(text); i++ {
		switch text[i] {
		case '(', '[', '{':
			depth++
		case ')', ']', '}':
			depth--
		case ',':
			if depth == 0 {
				out = append(out, text[last:i])
				last = i + 1
			}
		}
	}
	return append(out, text[last:])
}

// blankLiteralsAndComments replaces string-literal contents and comments with
// nothing, so text inside them never reads as Cypher. Quote characters stay.
// Backtick identifiers pass through untouched.
func blankLiteralsAndComments(text string) string {
	var b strings.Builder
	for i := 0; i < len(text); {
		c := text[i]
		switch {
		case c == '/' && i+1 < len(text) && text[i+1] == '/':
			for i < len(text) && text[i] != '\n' {
				i++
			}
		case c == '/' && i+1 < len(text) && text[i+1] == '*':
			end := strings.Index(text[i+2:], "*/")
			if end < 0 {
				return b.String()
			}
			i += end + 4
		case c == '`':
			// A backtick identifier is copied through whole: quotes and comment
			// markers inside it are part of the name, not Cypher.
			b.WriteByte(c)
			i++
			for i < len(text) {
				if text[i] == '`' {
					if i+1 < len(text) && text[i+1] == '`' {
						b.WriteString("``")
						i += 2
						continue
					}
					break
				}
				b.WriteByte(text[i])
				i++
			}
			b.WriteByte('`')
			i++
		case c == '\'' || c == '"':
			b.WriteByte(c)
			i++
			for i < len(text) {
				if text[i] == '\\' {
					i += 2
					continue
				}
				if text[i] == c {
					break
				}
				i++
			}
			b.WriteByte(c)
			i++
		default:
			b.WriteByte(c)
			i++
		}
	}
	return b.String()
}
