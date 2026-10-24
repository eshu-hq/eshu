// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// tailKind says what a method name after a dependency does to its scope.
type tailKind uint8

const (
	// tailUnknown is every name not on the allowlist. It may change the
	// configuration (`withConfigurations`, `configure`, an extension method),
	// so the scope is undetermined.
	tailUnknown tailKind = iota
	// tailFlag is an allowlisted method that takes no operand.
	tailFlag
	// tailOperand is an allowlisted method that takes an argument list or one
	// infix operand.
	tailOperand
)

// classifyTail is the scope-neutral allowlist. A name is on it only when its
// definition in sbt librarymanagement (ModuleIDExtra.scala, read at
// sbt/librarymanagement develop) rewrites something other than the module's
// `configurations` field; `withCrossVersion` is the generated ModuleID setter
// for the cross-version field:
//
//	exclude, excludeAll           add exclusion rules
//	classifier, artifacts, from   add or locate artifacts
//	intransitive, notTransitive   set the transitive flag
//	withSources, withJavadoc      add sources and javadoc artifacts
//	cross, withCrossVersion       set the cross-version suffix rule
//	force, changing               set the force and changing flags
//
// `withConfigurations`, `configure`, `%`-style operators other than the
// configuration operand, and any user-defined extension are not on the list:
// the scanner cannot see what they do, so they never keep a scope resolved.
// Adding a name here needs a test and a quote of the method from the ModuleID
// API; leaving it off is always safe.
func classifyTail(name string) tailKind {
	switch name {
	case "intransitive", "notTransitive", "withSources", "withJavadoc", "force", "changing":
		return tailFlag
	case "exclude", "excludeAll", "classifier", "cross", "withCrossVersion", "from", "artifacts":
		return tailOperand
	}
	return tailUnknown
}

// scopeEffect is what a run of tail operations does to the scope of a
// dependency, replayed in order: a later configuration replaces an earlier one
// and clears an earlier unknown, while an unknown after a configuration keeps
// that configuration but marks the scope undetermined.
type scopeEffect struct {
	spec    configSpec
	hasSpec bool
	unknown bool
}

// setConfig records an explicit `% config`.
func (e *scopeEffect) setConfig(spec configSpec) {
	e.spec, e.hasSpec, e.unknown = spec, true, false
}

// merge replays next after e, as the second of two chained calls.
func (e *scopeEffect) merge(next scopeEffect) {
	if next.hasSpec {
		e.setConfig(next.spec)
	}
	if next.unknown {
		e.unknown = true
	}
}

// applyTo writes the effect to one dependency.
func (e scopeEffect) applyTo(dep *dependency) {
	if e.hasSpec {
		dep.setConfig(e.spec)
	}
	if e.unknown {
		dep.scopeKnown = false
	}
}

// apply writes the effect to every dependency of a collection, once.
func (e scopeEffect) apply(deps []dependency) {
	for index := range deps {
		e.applyTo(&deps[index])
	}
}

// isPercentOperator reports a `%` coordinate or configuration operator.
func isPercentOperator(tok token) bool {
	return tok.kind == kindSymbol && tok.text == "%"
}

// startsStatement reports the prefix operators that begin a new statement at the
// start of a line, so they never continue the expression above.
func startsStatement(tok token) bool {
	if tok.kind != kindSymbol || len(tok.text) != 1 {
		return false
	}
	switch tok.text[0] {
	case '+', '-', '~', '!':
		return true
	}
	return false
}

// scanTail consumes the rest of one dependency expression and reports what it
// does to the scope. wrap is 1 when the expression sits inside its own
// parentheses (`("a" % "b" % "1").cross(...) % Test`), else 0.
//
// The default is "undetermined". Only these tokens keep the scope readable:
//
//   - `% config`, on the same line or leading the next line (sbt 2 leading
//     infix), where the last one wins;
//   - a method or infix name on the allowlist (classifyTail), with its argument
//     lists or one infix operand.
//
// Any other identifier, operator, literal, or bare group marks the scope
// unknown. The element ends at a comma, ';' or closer at group depth zero, or at
// a token that starts a new line, except a leading '.', a leading '%', and a
// leading infix operator (which is unknown but continues the expression). The
// terminator is not consumed. It reports whether a cross-version modifier was
// seen. Every iteration consumes a token and nothing recurses, so the work is
// linear in the tokens read and each configuration is resolved in work bounded
// by MaxConfigBytes.
func (s *scan) scanTail(wrap int) (effect scopeEffect, sawCross bool) {
	afterName := false
	for {
		tok := s.peek()
		if tok.kind == kindEOF {
			return effect, sawCross
		}
		if wrap == 0 {
			if tok.kind == kindClose || isPunct(tok, ",") || isPunct(tok, ";") {
				return effect, sawCross
			}
			if tok.newline && !isPunct(tok, ".") && !isPercentOperator(tok) &&
				(tok.kind != kindSymbol || startsStatement(tok)) {
				return effect, sawCross
			}
		}
		switch {
		case isPercentOperator(tok):
			s.next()
			if config, ok := s.readConfigOperand(); ok {
				effect.setConfig(resolveConfig(config))
			} else {
				effect.unknown = true
			}
			afterName = false
		case isPunct(tok, "."):
			s.next()
			name := s.peek()
			if name.kind != kindIdent {
				effect.unknown = true
				afterName = false
				continue
			}
			s.next()
			s.tailMethod(name.text, &effect, &sawCross)
			afterName = true
		case tok.kind == kindIdent:
			s.next()
			s.tailMethod(tok.text, &effect, &sawCross)
			afterName = true
		case tok.kind == kindOpen:
			// An argument list belongs to the method name before it. A group
			// with no name before it applies something the scanner cannot see.
			if !afterName {
				effect.unknown = true
			}
			s.next()
			s.skipGroup()
		case tok.kind == kindClose:
			// Only reachable inside the expression's own parentheses.
			s.next()
			wrap--
			afterName = false
		default:
			// An unknown operator, a literal, or a tuple comma.
			s.next()
			effect.unknown = true
			afterName = false
		}
	}
}

// tailMethod handles one method or infix name after a dependency. A name off the
// allowlist marks the scope unknown; an allowlisted one only records a cross
// modifier and consumes its infix operand, so the operand is not mistaken for a
// method name.
func (s *scan) tailMethod(name string, effect *scopeEffect, sawCross *bool) {
	kind := classifyTail(name)
	if kind == tailUnknown {
		effect.unknown = true
		return
	}
	if name == "cross" || name == "withCrossVersion" {
		*sawCross = true
	}
	if kind == tailOperand {
		s.skipInfixOperand()
	}
}

// skipInfixOperand consumes the single operand of an infix allowlisted call
// (`classifier "tests"`, `cross CrossVersion.full`) that is not an argument
// list. An argument list is left to the caller.
func (s *scan) skipInfixOperand() {
	operand := s.peek()
	if operand.newline {
		return
	}
	switch operand.kind {
	case kindString:
		s.next()
	case kindIdent:
		s.next()
		s.readIdentChain(operand.text)
	default:
		// Anything else is an argument list or the end of the tail.
	}
}
