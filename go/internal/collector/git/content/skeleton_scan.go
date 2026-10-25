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
	// endTemplate: the line ends in the code of a ${ } interpolation that is
	// still open. Like endCode, a trailing <<ID opens a heredoc (the safe
	// direction), and the interpolation carries to the next line.
	endTemplate
	// endString: the line ends inside a quoted string. HCL strings do not span
	// lines, so the string ends here and nothing opens.
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

// scanStack is the interpolation nesting that crosses a line boundary. Each
// frame is 's' (inside a string), 'i' (inside a ${ } or %{ }), or 'b' (inside a
// brace within one). Code is an empty stack or an i/b top. A string never
// outlives its line, so a stack carried to the next line ends in 'i' or 'b'.
type scanStack struct {
	frames [maxScanDepth]byte
	depth  int
}

func (st *scanStack) push(frame byte) bool {
	if st.depth == maxScanDepth {
		return false
	}
	st.frames[st.depth] = frame
	st.depth++
	return true
}

func (st *scanStack) inString() bool { return st.depth > 0 && st.frames[st.depth-1] == 's' }

// scanLineEnd lexes one line in a single pass and returns the state it ends in.
// inBlock says the line begins inside a block comment, and st holds the
// interpolation the line begins inside; both carry from the line before, and st
// is left holding what the next line begins inside. The lexer knows four
// things, and each decision reads only the state it ends in:
//
//   - a double-quoted string, with \ escapes and ${ } or %{ } interpolations
//     that nest strings and braces of their own; $${ and %%{ are plain text;
//   - a # or // line comment, which ends the scan;
//   - a /* block comment, closed by the first */, after which the line is
//     code again, so another comment or a heredoc can follow;
//   - everything else is code.
func scanLineEnd(line string, inBlock bool, st *scanStack) lineEnd {
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
		if st.inString() {
			switch {
			case c == '\\':
				i++
			case c == '"':
				st.depth--
			case (c == '$' || c == '%') && i+1 < len(line):
				switch {
				case line[i+1] == '{':
					if !st.push('i') {
						return failScan(st)
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
			if !st.push('s') {
				return failScan(st)
			}
		case c == '{' && st.depth > 0:
			if !st.push('b') {
				return failScan(st)
			}
		case c == '}' && st.depth > 0:
			st.depth--
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
	case st.inString():
		// The string ends with the line; an interpolation below it stays open.
		st.depth--
		return endString
	case st.depth > 0:
		return endTemplate
	}
	return endCode
}

// failScan is the answer for nesting past maxScanDepth: forget the stack and
// fail closed as an open block comment.
func failScan(st *scanStack) lineEnd {
	st.depth = 0
	return endBlockComment
}
