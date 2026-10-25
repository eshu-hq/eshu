// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import "strings"

// lineEnd is the lexical state a line of HCL ends in. The skeleton redactor
// decides every cross-line state from it, so a heredoc and a block comment can
// never disagree about what the end of a line is.
type lineEnd uint8

const (
	// endCode: the line ends in code, so a trailing <<ID opens a heredoc.
	endCode lineEnd = iota
	// endString: the line ends inside a quoted string or a ${ } interpolation.
	// HCL strings do not span lines, so nothing opens from here.
	endString
	// endLineComment: a # or // comment runs to the end of the line, so a <<ID
	// or /* after it is comment text.
	endLineComment
	// endBlockComment: a /* is still open, so the next lines are comment text.
	endBlockComment
)

// maxScanDepth bounds the string and interpolation nesting scanLineEnd tracks.
// A line nested deeper fails closed as an open block comment, which redacts
// whole lines until a */ and never keeps one.
const maxScanDepth = 32

// scanLineEnd lexes one line in a single pass and returns the state it ends in.
// inBlock says the line begins inside a block comment. The lexer knows four
// things, and each decision reads only the state it ends in:
//
//   - a double-quoted string, with \ escapes and ${ } or %{ } interpolations
//     that nest strings and braces of their own; $${ and %%{ are plain text;
//   - a # or // line comment, which ends the scan;
//   - a /* block comment, closed by the first */, after which the line is
//     code again, so another comment or a heredoc can follow;
//   - everything else is code.
func scanLineEnd(line string, inBlock bool) lineEnd {
	// Each frame is 's' (inside a string), 'i' (inside an interpolation), or
	// 'b' (inside a brace within one). Code is the empty stack or an i/b top.
	var frames [maxScanDepth]byte
	depth := 0
	push := func(frame byte) bool {
		if depth == maxScanDepth {
			return false
		}
		frames[depth] = frame
		depth++
		return true
	}
	for i := 0; i < len(line); i++ {
		if inBlock {
			end := strings.Index(line[i:], "*/")
			if end < 0 {
				return endBlockComment
			}
			// Leave i on the closing slash; the loop step moves past it.
			i += end + 1
			inBlock = false
			continue
		}
		c := line[i]
		if depth > 0 && frames[depth-1] == 's' {
			switch {
			case c == '\\':
				i++
			case c == '"':
				depth--
			case (c == '$' || c == '%') && i+1 < len(line):
				switch {
				case line[i+1] == '{':
					if !push('i') {
						return endBlockComment
					}
					i++
				case line[i+1] == c && strings.HasPrefix(line[i+2:], "{"):
					// $${ and %%{ are literal text.
					i += 2
				}
			}
			continue
		}
		switch {
		case c == '"':
			if !push('s') {
				return endBlockComment
			}
		case c == '{' && depth > 0:
			if !push('b') {
				return endBlockComment
			}
		case c == '}' && depth > 0:
			depth--
		case c == '#', c == '/' && strings.HasPrefix(line[i:], "//"):
			return endLineComment
		case c == '/' && strings.HasPrefix(line[i:], "/*"):
			inBlock = true
			// Skip the star so /*/ is not a complete comment.
			i++
		}
	}
	switch {
	case inBlock:
		return endBlockComment
	case depth > 0:
		return endString
	}
	return endCode
}
