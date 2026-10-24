// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// Key kinds the watch follows.
const (
	watchNone uint8 = iota
	watchScala
	watchLib
)

// keyWatch is the default-deny net under the statement handlers. The main
// loop claims each key token it dispatches to parseLibraryDependencies or
// parseScalaVersion, and those handlers classify the occurrence. Every other
// consumer (salvage, the grammar, a skipped group, parseVal) consumes a key
// token unclaimed; the watch then counts that occurrence as a rewrite unless
// the next two tokens are '.' and a read member (`k.value`). It runs inside
// scan.next, one token at a time, with no allocation and no lookahead.
type keyWatch struct {
	// step is 0 when idle, 1 after an unclaimed key, 2 after that key and '.'.
	step uint8
	// key is the kind of the unclaimed key in flight.
	key uint8
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
// the main loop consumed tok and will classify it itself.
func (w *keyWatch) observe(tok token, claimed bool) {
	switch w.step {
	case 1:
		if isPunct(tok, ".") {
			w.step = 2
			return
		}
		w.deny()
	case 2:
		if tok.kind == kindIdent && isKeyRead(tok.text) {
			w.step = 0
		} else {
			w.deny()
		}
	}
	if !claimed {
		if key := keyOf(tok); key != watchNone {
			w.step, w.key = 1, key
		}
	}
}

// deny counts the key in flight as a rewrite and returns the watch to idle.
func (w *keyWatch) deny() {
	if w.key == watchScala {
		w.scala++
	} else {
		w.lib++
	}
	w.step = 0
}

// applyKeyWatch folds the watch into the scan at end of input: an unclaimed
// key still in flight is a rewrite, an unclaimed scalaVersion rewrite poisons
// the envelope, and an unclaimed libraryDependencies rewrite supersedes every
// row of the file. Each occurrence is counted once.
func (s *scan) applyKeyWatch() {
	if s.watch.step != 0 {
		s.watch.deny()
	}
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
