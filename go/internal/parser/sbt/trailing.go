// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// maxMapShapeTokens bounds the tokens kept to recognise `_ % config` inside a
// collection `.map(...)` call. Longer bodies are never the plain shape.
const maxMapShapeTokens = 8

// skipElement consumes tokens to the end of the current element. See skipTail.
func (s *scan) skipElement(rel int) bool {
	return s.skipTail(rel, nil)
}

// skipTail consumes tokens to the end of the current element: a comma, ';' or
// closer at relative depth zero, or a token that starts a new line at depth
// zero (a leading '.' continues the expression). The terminator is not
// consumed. It reports whether a `cross` modifier was seen.
//
// When dep is non-nil, every `% config` at depth zero is applied to it and the
// last one wins, as in sbt where each `moduleID % config` replaces the
// configuration: that is the scope of `("a" %% "b" % "1").cross(...) % Test`,
// where the configuration follows the closing parenthesis. An operand that is
// not a string or identifier leaves the scope undetermined. Each operand is
// resolved in work bounded by MaxConfigBytes, however long the string is.
func (s *scan) skipTail(rel int, dep *dependency) bool {
	sawCross := false
	for {
		tok := s.peek()
		if tok.kind == kindEOF {
			return sawCross
		}
		if rel <= 0 {
			rel = 0
			if tok.kind == kindClose || isPunct(tok, ",") || isPunct(tok, ";") {
				return sawCross
			}
			if tok.newline && !isPunct(tok, ".") {
				return sawCross
			}
			if dep != nil && tok.kind == kindSymbol && tok.text == "%" {
				s.next()
				if config, ok := s.readConfigOperand(); ok {
					dep.setConfig(resolveConfig(config))
				} else {
					dep.scopeKnown = false
				}
				continue
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

// readConfig reads an optional `% config` operand on the same line.
func (s *scan) readConfig(dep *dependency) {
	sep := s.peek()
	if sep.kind != kindSymbol || sep.text != "%" || sep.newline {
		return
	}
	s.next()
	config, ok := s.readConfigOperand()
	if !ok {
		dep.scopeKnown = false
		return
	}
	dep.setConfig(resolveConfig(config))
}

// mapEffect accumulates what the `.map(...)` calls after one collection do to
// the scope of its dependencies. It replays the calls in order without touching
// a dependency: a later configuration replaces an earlier one and clears an
// earlier unknown, while an unknown after a configuration keeps that
// configuration but marks the scope undetermined.
type mapEffect struct {
	spec    configSpec
	hasSpec bool
	unknown bool
}

// apply writes the accumulated effect to the dependencies from start on, once,
// so the work is one pass over the collection however many calls were chained.
func (e mapEffect) apply(deps []dependency) {
	for index := range deps {
		if e.hasSpec {
			deps[index].setConfig(e.spec)
		}
		if e.unknown {
			deps[index].scopeKnown = false
		}
	}
}

// applyCollectionTail reads the method calls that follow a collection literal
// (`Seq(...).map(_ % Test)`, `Seq(...) map (_ % Test)`) and applies a
// configuration to the dependencies appended since start. Calls that do not
// touch the configuration (`.map(_.withSources())`, `.toSeq`) are skipped.
func (s *scan) applyCollectionTail(start int) {
	var effect mapEffect
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
				return
			}
			s.next()
			s.collectionCall(name.text, &effect)
		case tok.kind == kindIdent && tok.text == "map" && !tok.newline:
			s.next()
			s.collectionCall(tok.text, &effect)
		default:
			return
		}
	}
}

// collectionCall consumes the argument list of one collection method call.
func (s *scan) collectionCall(name string, effect *mapEffect) {
	open := s.peek()
	if open.kind == kindOpen && open.text == "[" && !open.newline {
		s.next()
		s.skipGroup()
		open = s.peek()
	}
	if open.kind != kindOpen || open.newline {
		return
	}
	s.next()
	if name != "map" {
		s.skipGroup()
		return
	}
	s.readMap(effect)
}

// readMap reads the body of `.map(...)` up to its closer and records its effect.
// The plain shape `_ % config` sets config on every dependency of the
// collection; the configuration is resolved here once. Any other body that uses
// `%` (a lambda, a computed configuration) leaves their scope undetermined. A
// body without `%` (`_.withSources()`) changes no scope.
func (s *scan) readMap(effect *mapEffect) {
	var shape [maxMapShapeTokens]token
	total, depth, sawPercent := 0, 1, false
	for depth > 0 {
		tok := s.next()
		if tok.kind == kindEOF {
			break
		}
		switch tok.kind {
		case kindOpen:
			depth++
		case kindClose:
			depth--
			if depth == 0 {
				continue
			}
		default:
			// Every other token only feeds the shape check below.
		}
		if tok.kind == kindSymbol && strings.Contains(tok.text, "%") {
			sawPercent = true
		}
		if total < maxMapShapeTokens {
			shape[total] = tok
		}
		total++
	}
	if config, ok := mapShapeConfig(shape[:min(total, maxMapShapeTokens)], total); ok {
		effect.spec, effect.hasSpec, effect.unknown = resolveConfig(config), true, false
		return
	}
	if sawPercent {
		effect.unknown = true
	}
}

// mapShapeConfig recognises `_ % "cfg"` and `_ % Cfg` (including a dotted
// `Configurations.Cfg`) and returns the configuration. total is the number of
// body tokens; a body longer than the buffer is never the plain shape.
func mapShapeConfig(toks []token, total int) (string, bool) {
	if total != len(toks) || len(toks) < 3 {
		return "", false
	}
	if toks[0].kind != kindIdent || toks[0].text != "_" || toks[1].kind != kindSymbol || toks[1].text != "%" {
		return "", false
	}
	operand := toks[2:]
	if len(operand) == 1 && operand[0].kind == kindString {
		return operand[0].text, true
	}
	if len(operand)%2 == 0 || operand[0].kind != kindIdent {
		return "", false
	}
	for index := 1; index < len(operand); index += 2 {
		if !isPunct(operand[index], ".") || operand[index+1].kind != kindIdent {
			return "", false
		}
	}
	return operand[len(operand)-1].text, true
}
