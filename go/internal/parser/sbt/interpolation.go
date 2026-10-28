// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// This file scans interpolated string literals (`s"..."`, `f"..."`,
// `s"""..."""` and any other identifier glued to a quote). An interpolation
// hole `${...}` holds Scala code, so its braces belong to the bracket depth
// only when they really open and close, and the strings nested inside it may
// hold unbalanced brackets (issue #8980). The scanner consumes each hole with
// brace depth and skips the strings, character literals, and comments inside
// it, so no bracket that is string content ever reaches the lexer's bracket
// stack. A hole the scanner cannot close ends the string at the line (or at
// end of input): the token stays opaque and the depth stays in sync, which
// fails the statement closed instead of desyncing the rest of the file.

// interpFrame is one suspended context of an interpolated string: either a
// string body or a `${...}` hole. Frames nest strictly (a body holds holes,
// a hole holds strings), so a slice used as a stack keeps the scan linear.
type interpFrame struct {
	// inHole reports a `${...}` hole; depth counts its unmatched `{`.
	inHole bool
	depth  int
	// triple and interp describe a string body: triple-quoted or single
	// line, interpolated (holes open) or plain (every byte is content).
	triple, interp bool
}

// scanInterp consumes an interpolated string starting at the opening quote
// (l.pos) and stops past its closing quote, at a line terminator a
// single-line string cannot cross, or at end of input. It never touches the
// lexer's bracket stack or depth: holes track their own braces. Every loop
// iteration consumes at least one byte or one line terminator, and every
// pushed frame consumed at least two bytes to open, so the scan is linear in
// the input with no recursion.
func (l *lexer) scanInterp() {
	triple := l.hasPrefix(`"""`)
	if triple {
		l.pos += 3
	} else {
		l.pos++
	}
	frames := []interpFrame{{triple: triple, interp: true}}
	for len(frames) > 0 {
		if l.pos >= len(l.src) {
			return
		}
		top := &frames[len(frames)-1]
		if !top.inHole {
			if l.scanInterpBody(&frames) {
				return
			}
			continue
		}
		l.scanInterpHole(&frames)
	}
}

// scanInterpBody scans one step of the string body on top of frames. It
// reports true when the whole interpolated string is done.
func (l *lexer) scanInterpBody(frames *[]interpFrame) bool {
	top := &(*frames)[len(*frames)-1]
	c := l.src[l.pos]
	switch {
	case c == '\n' || c == '\r':
		if !top.triple {
			// A single-line string cannot cross the line: it ends
			// unterminated here, like scanString.
			*frames = (*frames)[:len(*frames)-1]
			return len(*frames) == 0
		}
		l.consumeTerminator()
	case !top.triple && c == '\\':
		l.pos++
		if l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
			l.pos++
		}
	case c == '"' && top.triple && l.hasPrefix(`"""`):
		l.pos += 3
		for l.pos < len(l.src) && l.src[l.pos] == '"' {
			l.pos++
		}
		*frames = (*frames)[:len(*frames)-1]
		return len(*frames) == 0
	case c == '"' && !top.triple:
		l.pos++
		*frames = (*frames)[:len(*frames)-1]
		return len(*frames) == 0
	case c == '$' && top.interp:
		l.scanInterpDollar(frames)
	default:
		l.pos++
	}
	return false
}

// scanInterpDollar consumes the `$` escape, splice, or hole opener at l.pos
// in an interpolated string body: `$$` is a literal dollar, `$"` a literal
// quote that never closes the string, `$ident` a splice with no braces, and
// `${` opens a hole. A splice is `$` plus a letter-start identifier without
// `$`, so `$x${y}` reads as a splice followed by a hole.
func (l *lexer) scanInterpDollar(frames *[]interpFrame) {
	if l.pos+1 < len(l.src) {
		switch next := l.src[l.pos+1]; {
		case next == '$' || next == '"':
			l.pos += 2
			return
		case next == '{':
			l.pos += 2
			*frames = append(*frames, interpFrame{inHole: true, depth: 1})
			return
		case isSpliceStart(next):
			l.pos += 2
			for l.pos < len(l.src) && isSplicePart(l.src[l.pos]) {
				l.pos++
			}
			return
		}
	}
	l.pos++
}

// isSpliceStart reports whether c can start the identifier of a `$ident`
// splice: a letter, like the Scala lexical syntax for interpolated strings.
func isSpliceStart(c byte) bool {
	return c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// isSplicePart reports whether c continues a splice identifier: a letter, a
// digit, or an underscore. `$` is excluded so a hole opener after a splice
// still opens.
func isSplicePart(c byte) bool {
	return isSpliceStart(c) || c >= '0' && c <= '9' || c == '_'
}

// scanInterpHole scans one step of the `${...}` hole on top of frames: braces
// adjust the hole depth, and nested strings, character literals, and comments
// are skipped whole so their brackets never count.
func (l *lexer) scanInterpHole(frames *[]interpFrame) {
	top := &(*frames)[len(*frames)-1]
	c := l.src[l.pos]
	switch {
	case c == '\n' || c == '\r':
		// A hole may span lines; the terminator keeps later line numbers
		// and column-0 flags exact.
		l.consumeTerminator()
	case c == '{':
		top.depth++
		l.pos++
	case c == '}':
		top.depth--
		l.pos++
		if top.depth == 0 {
			*frames = (*frames)[:len(*frames)-1]
		}
	case c == '"':
		triple := l.hasPrefix(`"""`)
		if triple {
			l.pos += 3
		} else {
			l.pos++
		}
		*frames = append(*frames, interpFrame{triple: triple})
	case c == '\'':
		l.pos += charLiteralLen(l.src, l.pos)
	case c == '`':
		l.scanInterpBacktick()
	case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/':
		for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
			l.pos++
		}
	case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*':
		l.skipInterpComment()
	case isIdentStart(c):
		for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
			l.pos++
		}
		// An identifier glued to a quote is a nested interpolator
		// (`s"..."`, `f"..."`); anything else is code with no brackets.
		if l.pos < len(l.src) && l.src[l.pos] == '"' {
			triple := l.hasPrefix(`"""`)
			if triple {
				l.pos += 3
			} else {
				l.pos++
			}
			*frames = append(*frames, interpFrame{triple: triple, interp: true})
		}
	default:
		l.pos++
	}
}

// scanInterpBacktick skips a `quoted identifier` in a hole, stopping at the
// line terminator like scanBacktick.
func (l *lexer) scanInterpBacktick() {
	l.pos++
	for l.pos < len(l.src) && l.src[l.pos] != '`' && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
		l.pos++
	}
	if l.pos < len(l.src) && l.src[l.pos] == '`' {
		l.pos++
	}
}

// skipInterpComment consumes a (possibly nested) block comment in a hole,
// counting line terminators so later lines stay exact.
func (l *lexer) skipInterpComment() {
	nesting := 0
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*':
			nesting++
			l.pos += 2
		case c == '*' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/':
			nesting--
			l.pos += 2
			if nesting == 0 {
				return
			}
		case c == '\n' || c == '\r':
			l.consumeTerminator()
		default:
			l.pos++
		}
	}
}
