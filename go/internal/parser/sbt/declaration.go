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
	// scopeKnown is false when the scope could not be read from the
	// configuration or tail; the row is then partial and counted. It sits
	// beside kind so the struct gains no padding.
	scopeKnown bool
	// axisScoped is true when the key was declared under a configuration axis
	// or wrapper; the row is then partial whatever the operands say.
	axisScoped bool
	// optional is true when the configuration names `optional`.
	optional bool
	cross    string
	config   string
	scope    string
	line     int
}

// parseCollections reads a collection literal and any `++ Seq(...)` operands
// that follow it, applying `.map(_ % config)` calls to the collection they
// follow. An operand that is not a literal collection is counted by
// parseCollection and ends the chain.
func (s *scan) parseCollections(scoped bool) {
	for {
		start := len(s.deps)
		if !s.parseCollection(scoped) {
			return
		}
		s.applyCollectionTail(start)
		op := s.peek()
		if op.kind != kindSymbol || op.text != "++" || op.newline {
			return
		}
		s.next()
	}
}

// parseCollection reads `Seq(...)`, `List(...)`, `Vector(...)`, `Set(...)` or
// `Array(...)` (with an optional type argument list) and parses each element.
func (s *scan) parseCollection(scoped bool) bool {
	head := s.next()
	if head.kind != kindIdent || !isCollectionName(head.text) {
		s.unread(head)
		if head.kind != kindEOF {
			s.unresolved++
		}
		return false
	}
	open := s.next()
	if open.kind == kindOpen && open.text == "[" {
		s.skipGroup()
		open = s.next()
	}
	if open.kind != kindOpen || open.text != "(" {
		s.unread(open)
		s.unresolved++
		return false
	}
	for {
		tok := s.peek()
		if tok.kind == kindEOF {
			return false
		}
		if tok.kind == kindClose {
			s.next()
			return true
		}
		before := s.consumed
		s.parseElement(scoped)
		tok = s.peek()
		switch {
		case tok.kind == kindPunct && tok.text == ",":
			s.next()
		case tok.kind == kindClose:
			s.next()
			return true
		case tok.kind == kindEOF:
			return false
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
func (s *scan) parseElement(scoped bool) {
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
	dep := dependency{org: first.text, line: first.line}
	dep.setAxis(scoped)

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
	effect, sawCross := s.scanTail(rel)
	effect.applyTo(&dep)
	if sawCross {
		dep.cross = crossCustom
	}

	if len(s.deps) >= MaxDependencyRows {
		s.overflow++
		return
	}
	s.deps = append(s.deps, dep)
}

// take returns the next token of an element already in progress. A token that
// starts a new line at group depth zero ends the statement (a leading '.' or a
// leading '%' operator continues it), so it is left unconsumed and ok is false.
func (s *scan) take(rel int) (token, bool) {
	tok := s.peek()
	if rel == 0 && tok.newline && !isPunct(tok, ".") && !isPercentOperator(tok) {
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
		chain, ok := s.readIdentChain(tok.text)
		if !ok {
			return false
		}
		dep.version, dep.kind = chain, versionIdent
	default:
		return false
	}
	return true
}

// readIdentChain extends an identifier with `.member` selections and skips
// call argument groups, so `Versions.cats.value` and `ver(1)` stay one operand.
// A chain longer than MaxFieldBytes is consumed without being buffered and
// reported as not ok, so a million-segment chain cannot inflate a row.
func (s *scan) readIdentChain(head string) (string, bool) {
	if next := s.peek(); len(head) <= MaxFieldBytes && !isChainContinuation(next) {
		// A lone identifier needs no builder: return the token's own text.
		return head, true
	}
	var chain strings.Builder
	over := len(head) > MaxFieldBytes
	if !over {
		chain.WriteString(head)
	}
	for {
		tok := s.peek()
		switch {
		case tok.kind == kindPunct && tok.text == "." && !tok.newline:
			s.next()
			member := s.peek()
			if member.kind != kindIdent {
				return chainResult(&chain, over)
			}
			s.next()
			if !over && chain.Len()+1+len(member.text) > MaxFieldBytes {
				over = true
			}
			if !over {
				chain.WriteByte('.')
				chain.WriteString(member.text)
			}
		case tok.kind == kindOpen && !tok.newline:
			s.next()
			s.skipGroup()
		default:
			return chainResult(&chain, over)
		}
	}
}

// isChainContinuation reports a token readIdentChain would consume: a `.member`
// selection or a call argument group on the same line.
func isChainContinuation(tok token) bool {
	if tok.newline {
		return false
	}
	return isPunct(tok, ".") || tok.kind == kindOpen
}

func chainResult(chain *strings.Builder, over bool) (string, bool) {
	if over {
		return "", false
	}
	return chain.String(), true
}

// failElement counts an unreadable element and resynchronises at its end.
// tok is the last consumed token and rel the group depth already opened.
func (s *scan) failElement(tok token, rel int) {
	switch {
	case tok.kind == kindEOF:
		return
	case tok.kind == kindClose && rel == 0,
		(isPunct(tok, ",") || isPunct(tok, ";")) && rel == 0:
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
