// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// tokenKind classifies one lexed token. The lexer is a bounded scanner, not a
// Scala parser: it only separates the lexical shapes the declaration parser
// needs (identifiers, string literals, operator runs, brackets).
type tokenKind uint8

const (
	kindEOF tokenKind = iota
	// kindIdent is a Scala identifier or a backtick-quoted identifier.
	kindIdent
	// kindString is a plain "..." or """...""" literal with no escapes and no
	// interpolation prefix; text holds the interior verbatim.
	kindString
	// kindOpaque is a string the scanner refuses to read as a literal:
	// interpolated (s"..."), escaped, or unterminated. text holds a bounded
	// prefix of the raw spelling.
	kindOpaque
	// kindSymbol is a run of operator characters such as %, %%, %%%, :=, +=.
	kindSymbol
	// kindPunct is one of '.', ',' or ';'.
	kindPunct
	// kindOpen is '(', '[' or '{'.
	kindOpen
	// kindClose is ')', ']' or '}'.
	kindClose
	// kindOther is anything else, including numeric literals.
	kindOther
)

// maxOpaqueTextBytes bounds the raw spelling kept on a kindOpaque token so a
// 100 KB interpolated string cannot inflate report keys.
const maxOpaqueTextBytes = 96

// token is one lexed unit of build.sbt source.
type token struct {
	kind tokenKind
	text string
	// line is the 1-based source line the token starts on. It is an int32 so
	// a stored dependency stays within one allocation size class.
	line int32
	// newline reports that a line terminator separates this token from the
	// previous one.
	newline bool
	// depth is the bracket depth at the token: before an opener, after a
	// closer, and the current depth for every other token.
	depth int
	// inParen reports that the innermost enclosing bracket is '(' or '['.
	// Scala does not separate statements at a line break inside them, so a
	// newline there never ends an expression. For an opener it describes the
	// bracket around the opener; for a closer, the bracket left after it.
	inParen bool
	// closesCond marks the ')' that closes the condition of an if, while, or
	// for, so the statement after it on the next line is read as its body.
	closesCond bool
}

// Opener kinds kept on the lexer's bracket stack.
const (
	openParen   byte = 1
	openBracket byte = 2
	openBrace   byte = 3
	// openCond flags a '(' that follows if, while, or for.
	openCond byte = 8
)

// lexer scans build.sbt source one token at a time in a single forward pass.
// It never recurses and never re-reads bytes, so work stays linear in the
// input length for any nesting depth or line length.
type lexer struct {
	src   string
	pos   int
	line  int32
	depth int
	// stack holds one byte per open bracket. It grows with the nesting depth,
	// so its size is bounded by the input length.
	stack []byte
	// lastIdent is the text of the previous token when it was an identifier,
	// else empty; it marks the '(' after if, while, and for.
	lastIdent string
}

func newLexer(src string) *lexer {
	// A UTF-8 byte order mark would otherwise glue onto the first identifier.
	return &lexer{src: strings.TrimPrefix(src, "\ufeff"), line: 1}
}

// next returns the following token, or a kindEOF token at end of input.
func (l *lexer) next() token {
	newline := l.skipTrivia()
	start := l.pos
	tok := token{line: l.line, newline: newline, depth: l.depth, inParen: l.inParen()}
	if start >= len(l.src) {
		tok.kind = kindEOF
		return tok
	}
	l.scanToken(&tok, start)
	l.lastIdent = ""
	if tok.kind == kindIdent {
		l.lastIdent = tok.text
	}
	return tok
}

// scanToken fills tok with the token that starts at start.
func (l *lexer) scanToken(tok *token, start int) {
	c := l.src[start]
	switch {
	case c == '\'':
		l.pos += charLiteralLen(l.src, start)
		tok.kind, tok.text = kindOther, l.src[start:l.pos]
	case c == '"':
		tok.kind, tok.text = l.scanString(false)
	case c == '`':
		tok.kind, tok.text = l.scanBacktick()
	case isIdentStart(c):
		l.scanIdentInto(tok)
	case c >= '0' && c <= '9':
		l.pos++
		for l.pos < len(l.src) && isNumberPart(l.src[l.pos]) {
			l.pos++
		}
		tok.kind, tok.text = kindOther, l.src[start:l.pos]
	case c == '(' || c == '[' || c == '{':
		l.pos++
		l.depth++
		l.push(c)
		tok.kind, tok.text = kindOpen, l.src[start:l.pos]
	case c == ')' || c == ']' || c == '}':
		l.pos++
		if l.depth > 0 {
			l.depth--
		}
		tok.closesCond = l.pop()&openCond != 0
		tok.kind, tok.text, tok.depth, tok.inParen = kindClose, l.src[start:l.pos], l.depth, l.inParen()
	case c == '.' || c == ',' || c == ';':
		l.pos++
		tok.kind, tok.text = kindPunct, l.src[start:l.pos]
	case isSymbolChar(c):
		l.pos++
		for l.pos < len(l.src) && isSymbolChar(l.src[l.pos]) && !l.atCommentStart() {
			l.pos++
		}
		tok.kind, tok.text = kindSymbol, l.src[start:l.pos]
	default:
		l.pos++
		tok.kind, tok.text = kindOther, l.src[start:l.pos]
	}
}

// push records an opener. A '(' right after if, while, or for is flagged as a
// condition.
func (l *lexer) push(c byte) {
	kind := openBrace
	switch c {
	case '(':
		kind = openParen
		switch l.lastIdent {
		case "if", "while", "for":
			kind |= openCond
		}
	case '[':
		kind = openBracket
	}
	l.stack = append(l.stack, kind)
}

// pop removes the innermost opener and returns it, or 0 for an unmatched
// closer.
func (l *lexer) pop() byte {
	if len(l.stack) == 0 {
		return 0
	}
	top := l.stack[len(l.stack)-1]
	l.stack = l.stack[:len(l.stack)-1]
	return top
}

// inParen reports whether the innermost open bracket is '(' or '['.
func (l *lexer) inParen() bool {
	if len(l.stack) == 0 {
		return false
	}
	kind := l.stack[len(l.stack)-1] &^ openCond
	return kind == openParen || kind == openBracket
}

// charLiteralLen returns the length of the Scala character literal at start
// ('a', '"', '\n', '\u0041', or a multi-byte UTF-8 character), or 1 for a
// lone quote such as a symbol literal. Reading '"' as a quote would desync the
// string scanner to the end of the line.
func charLiteralLen(src string, start int) int {
	from, limit := start+2, start+2
	switch {
	case start+1 >= len(src):
		return 1
	case src[start+1] == '\\':
		// The escaped character itself may be a quote ('\'').
		from, limit = start+3, start+8
	case src[start+1] >= 0x80:
		limit = start + 6
	}
	for end := from; end < len(src) && end <= limit; end++ {
		switch src[end] {
		case '\'':
			return end - start + 1
		case '\n', '\r':
			return 1
		}
	}
	return 1
}

// skipTrivia consumes whitespace and comments and reports whether a line
// terminator was crossed. A bare '\r' counts as a terminator so classic-Mac
// sources keep their line structure.
func (l *lexer) skipTrivia() bool {
	crossed := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		switch {
		case c == '\n' || c == '\r':
			l.consumeTerminator()
			crossed = true
		case c == ' ' || c == '\t' || c == '\f' || c == '\v':
			l.pos++
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '/':
			for l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
				l.pos++
			}
		case c == '/' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '*':
			if l.skipBlockComment() {
				crossed = true
			}
		default:
			return crossed
		}
	}
	return crossed
}

// consumeTerminator advances past one "\r\n", "\n" or bare "\r".
func (l *lexer) consumeTerminator() {
	if l.src[l.pos] == '\r' && l.pos+1 < len(l.src) && l.src[l.pos+1] == '\n' {
		l.pos++
	}
	l.pos++
	l.line++
}

// skipBlockComment consumes a (possibly nested) block comment and reports
// whether it contained a line terminator. An unterminated comment runs to EOF.
func (l *lexer) skipBlockComment() bool {
	crossed := false
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
				return crossed
			}
		case c == '\n' || c == '\r':
			l.consumeTerminator()
			crossed = true
		default:
			l.pos++
		}
	}
	return crossed
}

func (l *lexer) atCommentStart() bool {
	if l.src[l.pos] != '/' || l.pos+1 >= len(l.src) {
		return false
	}
	next := l.src[l.pos+1]
	return next == '/' || next == '*'
}

// scanIdentInto scans an identifier. An identifier glued to a quote is a
// Scala string interpolator (s"...", f"...", raw"...") and becomes an opaque
// string token because its value is computed at build time.
func (l *lexer) scanIdentInto(tok *token) {
	start := l.pos
	for l.pos < len(l.src) && isIdentPart(l.src[l.pos]) {
		l.pos++
	}
	if l.pos < len(l.src) && l.src[l.pos] == '"' {
		l.scanString(true)
		tok.kind, tok.text = kindOpaque, boundOpaque(l.src[start:l.pos])
		return
	}
	tok.kind, tok.text = kindIdent, l.src[start:l.pos]
}

// scanBacktick scans a `quoted identifier`. An unterminated one ends at the
// line terminator.
func (l *lexer) scanBacktick() (tokenKind, string) {
	start := l.pos + 1
	l.pos++
	for l.pos < len(l.src) && l.src[l.pos] != '`' && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
		l.pos++
	}
	text := l.src[start:l.pos]
	if l.pos < len(l.src) && l.src[l.pos] == '`' {
		l.pos++
	}
	return kindIdent, text
}

// scanString scans a string starting at the opening quote. interpolated forces
// an opaque result. Plain strings without a backslash are literals; anything
// escaped, unterminated or interpolated is opaque.
func (l *lexer) scanString(interpolated bool) (tokenKind, string) {
	start := l.pos
	if l.hasPrefix(`"""`) {
		l.pos += 3
		bodyStart := l.pos
		for l.pos < len(l.src) {
			if l.hasPrefix(`"""`) {
				bodyEnd := l.pos
				l.pos += 3
				for l.pos < len(l.src) && l.src[l.pos] == '"' {
					bodyEnd++
					l.pos++
				}
				if interpolated {
					return kindOpaque, l.src[start:l.pos]
				}
				return kindString, l.src[bodyStart:bodyEnd]
			}
			if c := l.src[l.pos]; c == '\n' || c == '\r' {
				l.consumeTerminator()
				continue
			}
			l.pos++
		}
		return kindOpaque, boundOpaque(l.src[start:])
	}
	l.pos++
	bodyStart := l.pos
	escaped := false
	for l.pos < len(l.src) {
		c := l.src[l.pos]
		if c == '\n' || c == '\r' {
			return kindOpaque, boundOpaque(l.src[start:l.pos])
		}
		if c == '\\' {
			escaped = true
			l.pos++
			if l.pos < len(l.src) && l.src[l.pos] != '\n' && l.src[l.pos] != '\r' {
				l.pos++
			}
			continue
		}
		if c == '"' {
			bodyEnd := l.pos
			l.pos++
			if escaped || interpolated {
				return kindOpaque, boundOpaque(l.src[start:l.pos])
			}
			return kindString, l.src[bodyStart:bodyEnd]
		}
		l.pos++
	}
	return kindOpaque, boundOpaque(l.src[start:])
}

func (l *lexer) hasPrefix(prefix string) bool {
	return len(l.src)-l.pos >= len(prefix) && l.src[l.pos:l.pos+len(prefix)] == prefix
}

func boundOpaque(raw string) string {
	if len(raw) > maxOpaqueTextBytes {
		return raw[:maxOpaqueTextBytes]
	}
	return raw
}

func isIdentStart(c byte) bool {
	return c == '_' || c == '$' || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c >= 0x80
}

func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}

func isNumberPart(c byte) bool {
	return isIdentPart(c) || c == '.'
}

func isSymbolChar(c byte) bool {
	switch c {
	case '!', '#', '%', '&', '*', '+', '-', '/', ':', '<', '=', '>', '?', '@', '\\', '^', '|', '~':
		return true
	}
	return false
}
