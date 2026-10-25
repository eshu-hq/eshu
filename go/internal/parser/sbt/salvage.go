// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// salvage consumes the rest of a statement that did not match the grammar.
// rel is the number of groups already open inside the statement. The
// statement ends, unconsumed, at a ',', ';' or closer with no group of its own
// open, or at a line break that ends the expression (outside '(' and '[', not
// after an operator, and not before '.', an operator, or a '{' block).
//
// When emit is true, every literal coordinate `"org" (%|%%|%%%) "name" %
// revision` met on the way is stored as a partial row with an unknown scope:
// the scanner keeps what it read literally and claims nothing else about it.
// Every iteration consumes one token and nothing recurses, so the work is
// linear in the statement.
func (s *scan) salvage(rel int, emit bool) {
	// The window starts from any coordinate prefix the grammar consumed
	// before it stopped matching.
	window := s.window
	s.window = coordinateWindow{}
	for {
		tok := s.peek()
		if tok.kind == kindEOF {
			return
		}
		if rel == 0 {
			if tok.kind == kindClose || isPunct(tok, ",") || isPunct(tok, ";") {
				return
			}
			if s.boundary(tok) && !continuesExpression(tok) {
				return
			}
		}
		s.next()
		switch tok.kind {
		case kindOpen:
			rel++
		case kindClose:
			rel--
		default:
			// Other tokens carry no structure.
		}
		if emit {
			window.push(s, tok)
		}
	}
}

// continuesExpression reports a token that carries an expression across a
// line break: a leading '.', a leading infix operator, or a '{' block
// argument. Salvage keeps reading through it so the continuation is never
// read as a new statement.
func continuesExpression(tok token) bool {
	return isPunct(tok, ".") || tok.kind == kindSymbol || (tok.kind == kindOpen && tok.text == "{")
}

// coordinateWindow recognises `"org" op "name" % revision` in a token stream
// one token at a time. It holds only the fields read so far.
type coordinateWindow struct {
	step  int
	org   token
	cross string
	name  string
}

// push advances the window with one consumed token (already s.hist[0]) and
// stores a partial row when a coordinate completes. A token that breaks the
// pattern restarts it, and a string that breaks it starts a new one.
func (w *coordinateWindow) push(s *scan, tok token) {
	switch w.step {
	case 1:
		if cross, ok := crossOf(tok); ok {
			w.cross, w.step = cross, 2
			return
		}
	case 2:
		if tok.kind == kindString && validField(tok.text) {
			w.name, w.step = tok.text, 3
			return
		}
	case 3:
		if tok.kind == kindSymbol && tok.text == "%" {
			w.step = 4
			return
		}
	case 4:
		if w.emit(s, tok) {
			w.step = 0
			return
		}
	}
	w.step = 0
	// An organization never follows a coordinate operator: `% "test" %
	// "test" % "test"` is a run of configuration operands, not a coordinate.
	// s.hist[1] is the token consumed before tok.
	if _, afterOperator := crossOf(s.hist[1]); tok.kind == kindString && validField(tok.text) && !afterOperator {
		w.org, w.step = tok, 1
	}
}

// emit stores the coordinate whose revision is tok, if tok can be one.
func (w *coordinateWindow) emit(s *scan, tok token) bool {
	dep := dependency{
		org: w.org.text, name: w.name, cross: w.cross, line: w.org.line,
		scope: "compile", partial: true, ctx: noContext, version: tok.text,
	}
	switch tok.kind {
	case kindString:
		if len(tok.text) > MaxFieldBytes {
			return false
		}
		dep.kind = versionLiteral
	case kindOpaque:
		dep.kind = versionOpaque
	case kindIdent:
		if len(tok.text) > MaxFieldBytes {
			return false
		}
		dep.kind = versionIdent
		next := s.peek()
		dep.chained = isPunct(next, ".") || (next.kind == kindOpen && !next.newline)
	default:
		return false
	}
	s.addDep(dep)
	return true
}

// crossOf maps a coordinate operator to its cross-version label.
func crossOf(tok token) (string, bool) {
	if tok.kind != kindSymbol {
		return "", false
	}
	switch tok.text {
	case "%":
		return crossNone, true
	case "%%":
		return crossBinary, true
	case "%%%":
		return crossScalaJS, true
	}
	return "", false
}
