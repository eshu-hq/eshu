// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// versionKind says how a dependency's revision operand was spelled.
type versionKind uint8

const (
	versionLiteral versionKind = iota
	// versionIdent is an identifier chain such as catsVersion or
	// Versions.cats.value; a bare identifier may resolve through a
	// top-level string val.
	versionIdent
	// versionOpaque is an interpolated or escaped string.
	versionOpaque
)

// Cross-version labels emitted as sbt_cross_version.
const (
	crossNone    = "none"
	crossBinary  = "binary"
	crossScalaJS = "scalajs"
	crossCustom  = "custom"
)

// dependency is one literal organization % name % revision coordinate before
// deferred version resolution.
type dependency struct {
	org     string
	name    string
	version string
	kind    versionKind
	cross   string
	config  string
	scope   string
	line    int
}

// parseLibraryDependencies reads the operator after a libraryDependencies key.
// `+=` takes one element; `++=` and `:=` take a Seq/List/Vector literal. Any
// other shape (for example `libraryDependencies.value`) is not a declaration
// and is left to the main loop; a recognised assignment whose right-hand side
// is not a literal collection is counted as unresolved.
func (s *scan) parseLibraryDependencies(axis string) {
	op := s.next()
	if op.kind != kindSymbol {
		s.unread(op)
		return
	}
	switch op.text {
	case "+=":
		s.parseElement(axis)
	case "++=", ":=":
		s.parseCollection(axis)
	case "~=", "-=", "--=", "<+=", "<++=":
		s.unresolved++
	default:
		s.unread(op)
	}
}

// parseCollection reads `Seq(...)`, `List(...)`, `Vector(...)`, `Set(...)` or
// `Array(...)` (with an optional type argument list) and parses each element.
func (s *scan) parseCollection(axis string) {
	head := s.next()
	if head.kind != kindIdent || !isCollectionName(head.text) {
		s.unread(head)
		s.unresolved++
		return
	}
	open := s.next()
	if open.kind == kindOpen && open.text == "[" {
		s.skipGroup()
		open = s.next()
	}
	if open.kind != kindOpen || open.text != "(" {
		s.unread(open)
		s.unresolved++
		return
	}
	for {
		tok := s.peek()
		if tok.kind == kindEOF {
			return
		}
		if tok.kind == kindClose {
			s.next()
			return
		}
		before := s.consumed
		s.parseElement(axis)
		tok = s.peek()
		switch {
		case tok.kind == kindPunct && tok.text == ",":
			s.next()
		case tok.kind == kindClose:
			s.next()
			return
		case tok.kind == kindEOF:
			return
		case s.consumed == before:
			s.next()
		}
	}
}

func isCollectionName(name string) bool {
	switch name {
	case "Seq", "List", "Vector", "Set", "Array":
		return true
	}
	return false
}

// parseElement reads one `org (%|%%|%%%) name % revision [% config] ...`
// expression. Tokens after the recognised prefix (exclude(...), cross, and
// method chains) are skipped without evaluation.
func (s *scan) parseElement(axis string) {
	first := s.next()
	rel := 0
	if first.kind == kindOpen && first.text == "(" {
		rel = 1
		first = s.next()
	}
	if first.kind != kindString || !validField(first.text) {
		s.failElement(first, rel)
		return
	}
	dep := dependency{org: first.text, line: first.line, scope: scopeForConfig(axis)}

	op, ok := s.take(rel)
	if !ok {
		s.unresolved++
		return
	}
	if op.kind != kindSymbol {
		s.failElement(op, rel)
		return
	}
	switch op.text {
	case "%":
		dep.cross = crossNone
	case "%%":
		dep.cross = crossBinary
	case "%%%":
		dep.cross = crossScalaJS
	default:
		s.failElement(op, rel)
		return
	}

	name, ok := s.take(rel)
	if !ok {
		s.unresolved++
		return
	}
	if name.kind != kindString || !validField(name.text) {
		s.failElement(name, rel)
		return
	}
	dep.name = name.text

	sep, ok := s.take(rel)
	if !ok {
		s.unresolved++
		return
	}
	if sep.kind != kindSymbol || sep.text != "%" {
		s.failElement(sep, rel)
		return
	}
	version, ok := s.take(rel)
	if !ok {
		s.unresolved++
		return
	}
	if !s.readVersion(&dep, version) {
		s.failElement(version, rel)
		return
	}
	s.readConfig(&dep)
	if s.skipElement(rel) {
		dep.cross = crossCustom
	}

	if len(s.deps) >= MaxDependencyRows {
		s.overflow++
		return
	}
	s.deps = append(s.deps, dep)
}

// take returns the next token of an element already in progress. A token that
// starts a new line at group depth zero ends the statement (a leading '.'
// continues it), so it is left unconsumed and ok is false.
func (s *scan) take(rel int) (token, bool) {
	tok := s.peek()
	if rel == 0 && tok.newline && !isPunct(tok, ".") {
		return tok, false
	}
	return s.next(), true
}

// readVersion fills the revision fields from the operand token. It reports
// false when the token cannot start a revision operand.
func (s *scan) readVersion(dep *dependency, tok token) bool {
	switch tok.kind {
	case kindString:
		if len(tok.text) > MaxFieldBytes {
			return false
		}
		dep.version, dep.kind = tok.text, versionLiteral
	case kindOpaque:
		dep.version, dep.kind = tok.text, versionOpaque
	case kindIdent:
		dep.version, dep.kind = s.readIdentChain(tok.text), versionIdent
	default:
		return false
	}
	return true
}

// readIdentChain extends an identifier with `.member` selections and skips
// call argument groups, so `Versions.cats.value` and `ver(1)` stay one operand.
func (s *scan) readIdentChain(head string) string {
	var chain strings.Builder
	chain.WriteString(head)
	for {
		tok := s.peek()
		switch {
		case tok.kind == kindPunct && tok.text == "." && !tok.newline:
			s.next()
			member := s.peek()
			if member.kind != kindIdent {
				return chain.String()
			}
			s.next()
			chain.WriteByte('.')
			chain.WriteString(member.text)
		case tok.kind == kindOpen && !tok.newline:
			s.next()
			s.skipGroup()
		default:
			return chain.String()
		}
	}
}

// readConfig reads an optional `% config` operand on the same line.
func (s *scan) readConfig(dep *dependency) {
	sep := s.peek()
	if sep.kind != kindSymbol || sep.text != "%" || sep.newline {
		return
	}
	s.next()
	operand := s.next()
	switch operand.kind {
	case kindString:
		dep.config = operand.text
	case kindIdent:
		dep.config = s.readIdentChain(operand.text)
		if dot := strings.LastIndexByte(dep.config, '.'); dot >= 0 {
			dep.config = dep.config[dot+1:]
		}
	default:
		s.unread(operand)
		return
	}
	dep.scope = scopeForConfig(dep.config)
}

// failElement counts an unreadable element and resynchronises at its end.
// tok is the last consumed token and rel the group depth already opened.
func (s *scan) failElement(tok token, rel int) {
	switch {
	case tok.kind == kindEOF:
		return
	case tok.kind == kindClose && rel == 0,
		tok.kind == kindPunct && tok.text == "," && rel == 0:
		s.unresolved++
		s.unread(tok)
		return
	}
	s.unresolved++
	s.skipElement(rel + initialDepth(tok))
}

// initialDepth is the group depth a consumed token opens.
func initialDepth(tok token) int {
	if tok.kind == kindOpen {
		return 1
	}
	return 0
}

// skipElement consumes tokens to the end of the current element: a comma or
// closer at relative depth zero, or a token that starts a new line at depth
// zero (a leading '.' continues the expression). The terminator is not
// consumed. It reports whether a `cross` modifier was seen.
func (s *scan) skipElement(rel int) bool {
	sawCross := false
	for {
		tok := s.peek()
		if tok.kind == kindEOF {
			return sawCross
		}
		if rel <= 0 {
			rel = 0
			if tok.kind == kindClose || (tok.kind == kindPunct && tok.text == ",") {
				return sawCross
			}
			if tok.newline && !isPunct(tok, ".") {
				return sawCross
			}
		}
		s.next()
		switch tok.kind {
		case kindOpen:
			rel++
		case kindClose:
			rel--
		case kindIdent:
			if tok.text == "cross" {
				sawCross = true
			}
		default:
			// Strings, symbols, punctuation and numbers carry no structure.
		}
	}
}

// skipGroup consumes through the closer matching an opener already consumed.
func (s *scan) skipGroup() {
	depth := 1
	for depth > 0 {
		tok := s.next()
		switch tok.kind {
		case kindEOF:
			return
		case kindOpen:
			depth++
		case kindClose:
			depth--
		default:
			// Everything else is skipped without evaluation.
		}
	}
}

// isPunct reports whether tok is the punctuation token text.
func isPunct(tok token, text string) bool {
	return tok.kind == kindPunct && tok.text == text
}
