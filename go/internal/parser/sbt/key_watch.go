// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// Key kinds the watch follows.
const (
	watchNone uint8 = iota
	watchScala
	watchLib
)

// Watch steps after an unclaimed key. They follow the read grammar of the
// statement handlers (skipLegacyAxes, readThroughClosers): a key is a read
// when, after any chain of legacy axes (`in X`, `in X.Y`, `in (...)`,
// `.in(...)`) and any ')' closers, a '.' and a read member follow. A prefix
// axis (`A / k`) comes before the key and changes nothing here.
const (
	stepIdle      uint8 = iota
	stepKey             // after the key or an axis group: '.', `in` or ')' may follow
	stepDot             // after '.' with no closer yet: a read member or `in`
	stepDotIn           // after `.in`: '(' must follow
	stepIn              // after `in`: a scope name or '(' must follow
	stepInName          // after `in X`: `.Y`, `in`, ')' may follow
	stepInNameDot       // after `in X.`: a name must follow
	stepGroup           // inside an axis group; depth counts its brackets
	stepClose           // after ')': ')' or '.' may follow
	stepCloseDot        // after ')' and '.': a read member must follow
)

// keyWatch is the default-deny net under the statement handlers. The main
// loop claims each key token it dispatches to parseLibraryDependencies or
// parseScalaVersion, and those handlers classify the occurrence. Every other
// consumer (salvage, the grammar, a skipped group, parseVal) consumes a key
// token unclaimed; the watch then counts that occurrence as a rewrite unless
// the tokens after it are a read in the handlers' read grammar (watchStep).
// It runs inside scan.next, one token at a time, with no allocation and no
// lookahead.
type keyWatch struct {
	// step is stepIdle or the read-grammar step of the unclaimed key in
	// flight.
	step uint8
	// key is the kind of the unclaimed key in flight.
	key uint8
	// depth counts open brackets inside an axis group (stepGroup).
	depth int32
	// covered is set when the key in flight started while its enclosing
	// statement had already applied the same verdict (scan.cover): its deny
	// changes nothing and is not counted again.
	covered bool
	// scala and lib count the unclaimed occurrences judged rewrites.
	scala int
	lib   int
}

// keyOf reports which watched key tok is, or watchNone. A backtick-quoted
// identifier lexes to the same text, so it is the same key.
func keyOf(tok token) uint8 {
	if tok.kind != kindIdent {
		return watchNone
	}
	switch tok.text {
	case "scalaVersion":
		return watchScala
	case "libraryDependencies":
		return watchLib
	}
	return watchNone
}

// observe advances the watch with one consumed token. claimed is true when
// the main loop consumed tok and will classify it itself; cover is the key
// whose verdict the statement being consumed has already applied. A watched key
// inside an axis group denies the key in flight, so a key nested in another
// key's axis is judged on its own and never hides a rewrite.
func (w *keyWatch) observe(tok token, claimed bool, cover uint8) {
	if w.step != stepIdle {
		if w.step == stepGroup && keyOf(tok) != watchNone {
			w.deny()
		} else {
			w.step = watchStep(w.step, tok, &w.depth)
			if w.step == stepDeny {
				w.deny()
			}
		}
	}
	if !claimed {
		if key := keyOf(tok); key != watchNone {
			w.step, w.key, w.depth, w.covered = stepKey, key, 0, key == cover
		}
	}
}

// stepDeny and stepRead are watchStep verdicts; neither is a watch state.
const (
	stepDeny uint8 = 0xfe
	stepRead uint8 = stepIdle
)

// watchStep is the read grammar after a key, one token at a time: it returns
// the next step, stepRead when tok completes a read, or stepDeny when tok
// cannot continue one.
func watchStep(step uint8, tok token, depth *int32) uint8 {
	isIn := tok.kind == kindIdent && tok.text == "in"
	isRead := tok.kind == kindIdent && isKeyRead(tok.text)
	isParenClose := tok.kind == kindClose && tok.text == ")"
	switch step {
	case stepKey, stepInName:
		switch {
		case isPunct(tok, "."):
			if step == stepInName {
				return stepInNameDot
			}
			return stepDot
		case isIn:
			return stepIn
		case isParenClose:
			return stepClose
		}
	case stepDot:
		switch {
		case isRead:
			return stepRead
		case isIn:
			return stepDotIn
		}
	case stepDotIn:
		if tok.kind == kindOpen {
			*depth = 1
			return stepGroup
		}
	case stepIn:
		if tok.kind == kindIdent {
			return stepInName
		}
		if tok.kind == kindOpen {
			*depth = 1
			return stepGroup
		}
	case stepInNameDot:
		if tok.kind == kindIdent {
			return stepInName
		}
	case stepGroup:
		switch tok.kind {
		case kindOpen:
			*depth++
		case kindClose:
			*depth--
			if *depth == 0 {
				return stepKey
			}
		case kindEOF:
			return stepDeny
		default:
			// Any other token inside the group carries no structure.
		}
		return stepGroup
	case stepClose:
		switch {
		case isParenClose:
			return stepClose
		case isPunct(tok, "."):
			return stepCloseDot
		}
	case stepCloseDot:
		if isRead {
			return stepRead
		}
	}
	return stepDeny
}

// deny counts the key in flight as a rewrite and returns the watch to idle.
// A covered key is not counted: its statement already superseded every row
// or poisoned the envelope, and is counted once on its own.
func (w *keyWatch) deny() {
	switch {
	case w.covered:
	case w.key == watchScala:
		w.scala++
	default:
		w.lib++
	}
	w.step = stepIdle
}

// applyKeyWatch folds the watch into the scan at end of input: an unclaimed
// scalaVersion rewrite poisons the envelope, and an unclaimed
// libraryDependencies rewrite supersedes every row of the file. Each
// occurrence is counted once. No key is still in flight here: the main loop
// consumes the EOF token through scan.next, and observe denies a key that EOF
// follows.
func (s *scan) applyKeyWatch() {
	if s.watch.scala > 0 {
		s.scalaPoisoned = true
		s.unresolved += s.watch.scala
	}
	if s.watch.lib > 0 {
		s.globalReset = true
		s.unresolved += s.watch.lib
	}
}

// readThroughClosers consumes the ')' closers after a key and reports whether
// a read member follows them (`(scalaVersion in Test).value`). The '.' stays
// consumed for a read; the main loop skips the member.
func (s *scan) readThroughClosers() bool {
	for {
		tok := s.peek()
		if tok.kind != kindClose || tok.text != ")" {
			break
		}
		s.next()
	}
	if !isPunct(s.peek(), ".") {
		return false
	}
	s.next()
	member := s.peek()
	return member.kind == kindIdent && isKeyRead(member.text)
}

// skipLegacyAxes consumes every legacy `in <scope>`, `in (<scope>)` or
// `.in(<scope>)` axis after a key, chained or not, and reports how many there
// were and whether the last was `in ThisBuild`. A '.' followed by anything
// but `in` stays consumed, so the caller reads the member after it.
func (s *scan) skipLegacyAxes() (axes int, thisBuild bool) {
	for {
		tok := s.peek()
		switch {
		case tok.kind == kindIdent && tok.text == "in":
			s.next()
			axes++
			thisBuild = false
			operand := s.peek()
			switch operand.kind {
			case kindIdent:
				s.next()
				scope, _ := s.readQualID(operand.text)
				thisBuild = scope == "ThisBuild"
			case kindOpen:
				s.next()
				s.skipGroup()
			default:
				// A missing operand: the operator check decides.
			}
		case isPunct(tok, "."):
			s.next()
			name := s.peek()
			if name.kind != kindIdent || name.text != "in" {
				return axes, thisBuild
			}
			s.next()
			axes++
			thisBuild = false
			if open := s.peek(); open.kind == kindOpen {
				s.next()
				s.skipGroup()
			}
		default:
			return axes, thisBuild
		}
	}
}
