// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gitcontent

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// maxScanDepth bounds the lexical nesting scanState tracks. A push past it
// fails the scan closed (see scanState.failed); it never errors.
const maxScanDepth = 32

type frameKind uint8

const (
	// frameString is a double-quoted template: "..." with \ escapes.
	frameString frameKind = iota + 1
	// frameHeredoc is a heredoc template: text up to its marker line.
	frameHeredoc
	// frameInterp is the code of a ${ } interpolation or a %{ } directive.
	frameInterp
	// frameBrace is a { } inside an interpolation.
	frameBrace
)

// frame is one open lexical context. marker, startOfLine, and closerGoes are
// set on a frameHeredoc only.
type frame struct {
	kind frameKind
	// startOfLine says the next literal run begins a line, so it may be the
	// closing marker. An interpolation clears it for the rest of its line, as
	// hclsyntax does (scan_tokens.rl, heredocLiteralEOL).
	startOfLine bool
	// closerGoes says the closing line is redacted. It is false only for a
	// top-level key = <<ID whose opener line was kept, so the closer stays.
	closerGoes bool
	marker     string
}

// scanState is the lexical state that crosses a line boundary. It follows
// HashiCorp's lexer (hclsyntax scan_tokens.rl): a stack of quoted templates,
// heredoc templates, interpolations, and braces, plus an open block comment.
// It is a value in skeletonState and allocates nothing.
//
// A line that begins with anything open is redacted whole. The redactor never
// parses an interpolation; it only needs to know where the text of a template
// or a comment ends so that a later line is read as code or as text correctly.
type scanState struct {
	frames  [maxScanDepth]frame
	depth   int
	inBlock bool
	// failed is sticky. It is set when nesting passes maxScanDepth or a line
	// ends inside a quoted template (HCL's TokenQuotedNewline, invalid HCL).
	// Every later line is then redacted whole; nothing clears it. The body is
	// still returned, with its line count and its findings.
	failed bool
}

// inside reports whether the next line begins inside a template, an
// interpolation, a block comment, or a failed scan, so it is redacted whole.
func (st *scanState) inside() bool { return st.failed || st.inBlock || st.depth > 0 }

func (st *scanState) top() *frame { return &st.frames[st.depth-1] }

func (st *scanState) push(f frame) bool {
	if st.depth == maxScanDepth {
		st.failed = true
		return false
	}
	st.frames[st.depth] = f
	st.depth++
	return true
}

// line scans one line (its \r already trimmed) and updates the state.
// keepOpener says a heredoc this line opens at the top level has its opener
// line kept, so its closer is kept too. It returns true when the line closed
// such a heredoc.
func (st *scanState) line(line string, keepOpener bool) (closerKept bool) {
	if st.failed {
		return false
	}
	// The text a closing marker may be is the part of the line after the last
	// interpolation that ended on it.
	segStart := 0
	lineComment := false
	for i := 0; i < len(line); {
		switch {
		case st.inBlock:
			end := strings.Index(line[i:], "*/")
			if end < 0 {
				return false
			}
			i += end + 2
			st.inBlock = false
		case st.depth > 0 && st.top().kind == frameString:
			i = st.scanString(line, i)
		case st.depth > 0 && st.top().kind == frameHeredoc:
			i = st.scanHeredoc(line, i)
		default:
			i, lineComment = st.scanCode(line, i, &segStart)
		}
		if st.failed {
			return false
		}
	}
	switch {
	case st.inBlock:
		return false
	case st.depth == 0 || st.top().kind == frameInterp || st.top().kind == frameBrace:
		if !lineComment {
			st.openHeredoc(line, keepOpener)
		}
		return false
	case st.top().kind == frameString:
		// HCL strings do not span lines; the lexer reports TokenQuotedNewline.
		st.failed = true
		return false
	}
	return st.endHeredocLine(line, segStart)
}

// endHeredocLine ends a line that finished in heredoc text. The line closes the
// heredoc when it starts a line and its last run of text is the marker. It
// returns true when it closed a heredoc whose closer line is kept.
func (st *scanState) endHeredocLine(line string, segStart int) bool {
	top := st.top()
	if top.startOfLine && strings.TrimSpace(line[segStart:]) == top.marker {
		kept := !top.closerGoes
		st.depth--
		return kept
	}
	top.startOfLine = true
	return false
}

// scanString reads quoted-template text from i. It stops when the template
// closes or an interpolation opens, and returns the next index.
func (st *scanState) scanString(line string, i int) int {
	for ; i < len(line); i++ {
		switch c := line[i]; c {
		case '\\':
			i++
		case '"':
			st.depth--
			return i + 1
		case '$', '%':
			switch {
			case i+1 < len(line) && line[i+1] == '{':
				st.push(frame{kind: frameInterp})
				return i + 2
			case i+2 < len(line) && line[i+1] == c && line[i+2] == '{':
				// $${ and %%{ are literal text.
				i += 2
			}
		}
	}
	return len(line)
}

// scanHeredoc reads heredoc text from i. Heredoc text has no escapes. It stops
// when an interpolation opens, and returns the next index.
func (st *scanState) scanHeredoc(line string, i int) int {
	// Most heredoc text holds neither opener byte; two vectorized searches skip
	// it without a byte loop.
	if strings.IndexByte(line[i:], '$') < 0 && strings.IndexByte(line[i:], '%') < 0 {
		return len(line)
	}
	for ; i < len(line); i++ {
		if c := line[i]; c == '$' || c == '%' {
			switch {
			case i+1 < len(line) && line[i+1] == '{':
				st.top().startOfLine = false
				st.push(frame{kind: frameInterp})
				return i + 2
			case i+2 < len(line) && line[i+1] == c && line[i+2] == '{':
				i += 2
			}
		}
	}
	return len(line)
}

// scanCode reads code from i: the top level or the inside of an interpolation.
// It stops when a template opens, a template resumes, a block comment opens,
// or the line ends in a line comment, and returns the next index. segStart
// moves to just after the } that returns to heredoc text.
func (st *scanState) scanCode(line string, i int, segStart *int) (next int, lineComment bool) {
	for ; i < len(line); i++ {
		switch c := line[i]; {
		case c == '"':
			st.push(frame{kind: frameString})
			return i + 1, false
		case c == '{' && st.depth > 0:
			if !st.push(frame{kind: frameBrace}) {
				return i, false
			}
		case c == '}' && st.depth > 0:
			st.depth--
			if st.depth > 0 && st.top().kind == frameHeredoc {
				*segStart = i + 1
				return i + 1, false
			}
			if st.depth > 0 && st.top().kind == frameString {
				return i + 1, false
			}
		case c == '#':
			return len(line), true
		case c == '/' && i+1 < len(line) && line[i+1] == '/':
			return len(line), true
		case c == '/' && i+1 < len(line) && line[i+1] == '*':
			// Step over the star so /*/ is not a complete comment.
			st.inBlock = true
			return i + 2, false
		}
	}
	return len(line), false
}

// openHeredoc pushes a heredoc when the line, still in code, ends in <<ID or
// <<-ID. HCL needs the marker to end the line, so a trailing space opens
// nothing.
func (st *scanState) openHeredoc(line string, keepOpener bool) {
	marker, ok := heredocMarker(line)
	if !ok {
		return
	}
	st.push(frame{
		kind: frameHeredoc, marker: marker, startOfLine: true,
		closerGoes: !keepOpener || st.depth != 0,
	})
}

// heredocMarker returns the delimiter when line ends in <<ID or <<-ID.
func heredocMarker(line string) (string, bool) {
	at := strings.LastIndex(line, "<<")
	if at < 0 {
		return "", false
	}
	id := strings.TrimPrefix(line[at+2:], "-")
	if id == "" || !isIdent(id) {
		return "", false
	}
	return id, true
}

// isIdent reports whether text is an HCL Ident: a letter or _, then letters,
// digits, marks, connectors, _, and -. It follows hclsyntax's ID_Start and
// ID_Continue closely enough that the oracle test finds no difference.
func isIdent(text string) bool {
	for i, r := range text {
		switch {
		case r == '_', r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z':
		case i > 0 && (r == '-' || r >= '0' && r <= '9'):
		case r < utf8.RuneSelf:
			return false
		case unicode.In(r, unicode.L, unicode.Nl):
		case i > 0 && unicode.In(r, unicode.Mn, unicode.Mc, unicode.Nd, unicode.Pc):
		default:
			return false
		}
	}
	return text != ""
}
