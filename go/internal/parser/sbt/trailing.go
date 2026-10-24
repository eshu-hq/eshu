// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// skipElement consumes tokens to the end of the current element without
// reading any of it: a comma, ';' or closer at relative depth zero, or a token
// that starts a new line at depth zero (a leading '.' continues the expression).
// The terminator is not consumed. It resynchronises after an unreadable element;
// the scope of a readable one comes from scanTail.
func (s *scan) skipElement(rel int) {
	for {
		tok := s.peek()
		if tok.kind == kindEOF {
			return
		}
		if rel <= 0 {
			rel = 0
			if tok.kind == kindClose || isPunct(tok, ",") || isPunct(tok, ";") {
				return
			}
			if tok.newline && !isPunct(tok, ".") {
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
			// Strings, symbols, punctuation and numbers carry no structure.
		}
	}
}

// readConfigOperand reads the operand of a `% config`: a string literal or an
// identifier chain whose last segment names the configuration. It reports
// false, consuming nothing, for any other token.
func (s *scan) readConfigOperand() (string, bool) {
	operand := s.peek()
	switch operand.kind {
	case kindString:
		s.next()
		return operand.text, true
	case kindIdent:
		s.next()
		chain, ok := s.readIdentChain(operand.text)
		if !ok {
			return configOverlong, true
		}
		if dot := strings.LastIndexByte(chain, '.'); dot >= 0 {
			chain = chain[dot+1:]
		}
		return chain, true
	default:
		return "", false
	}
}

// isElementPreservingCall names the collection methods that keep every element
// as it is, so they change no scope. Any other call after a collection (a
// flatMap, a collect, a zip, a fold) may rebuild the dependencies and leaves
// their scope undetermined.
func isElementPreservingCall(name string) bool {
	switch name {
	case "toSeq", "toList", "toVector", "toSet", "toArray", "distinct", "filter", "filterNot":
		return true
	}
	return false
}

// applyCollectionTail reads the method calls that follow a collection literal
// (`Seq(...).map(_ % Test)`, `Seq(...) map (_ % Test)`) and applies their effect
// to the dependencies appended since start. Only `.map` bodies on the scope
// allowlist and the element-preserving calls keep the scope readable; any other
// call marks the scope undetermined. The calls are replayed into one effect and
// written once, however many are chained.
func (s *scan) applyCollectionTail(start int) {
	var effect scopeEffect
	defer func() {
		s.mapWrites += len(s.deps) - start
		effect.apply(s.deps[start:])
	}()
	for {
		tok := s.peek()
		switch {
		case isPunct(tok, "."):
			s.next()
			name := s.peek()
			if name.kind != kindIdent {
				effect.unknown = true
				return
			}
			s.next()
			s.collectionCall(name.text, &effect)
		case tok.kind == kindIdent && !tok.newline && tok.text != "else":
			s.next()
			s.collectionCall(tok.text, &effect)
		default:
			return
		}
	}
}

// collectionCall consumes one collection method call and records its effect.
func (s *scan) collectionCall(name string, effect *scopeEffect) {
	open := s.peek()
	if open.kind == kindOpen && open.text == "[" && !open.newline {
		s.next()
		s.skipGroup()
		open = s.peek()
	}
	hasArgs := open.kind == kindOpen && !open.newline
	if hasArgs {
		s.next()
	}
	switch {
	case name == "map" && hasArgs:
		s.readMap(effect)
		return
	case name == "map" || !isElementPreservingCall(name):
		effect.unknown = true
	}
	if hasArgs {
		s.skipGroup()
	} else {
		s.skipInfixOperand()
	}
}

// readMap reads the body of `.map(...)` up to its closer and merges its effect.
// The body must start with `_` and then be a run of the shapes scanTail accepts:
// `_ % config`, and allowlisted calls such as `_.withSources()`. Any other body
// (a lambda, a block, a call off the allowlist, a stray token) leaves the scope
// undetermined; there is no body that is skipped unread.
func (s *scan) readMap(effect *scopeEffect) {
	first := s.peek()
	if first.kind == kindIdent && first.text == "_" {
		s.next()
		body, _ := s.scanTail(0)
		if closer := s.peek(); closer.kind == kindClose {
			s.next()
			effect.merge(body)
			return
		}
	}
	effect.unknown = true
	s.skipGroup()
}
