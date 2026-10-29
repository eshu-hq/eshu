// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// isKeyListSetting names the settings whose value is a list of keys, so a key
// named as a plain element is a reference, not a rewrite:
//
//   - buildInfoKeys (sbt-buildinfo): `settingKey[Seq[BuildInfoKey.Entry[_]]]`;
//     a key converts to an entry by `BuildInfoKey.setting`.
//   - excludeLintKeys and includeLintKeys (sbt 1.4+ Keys.scala):
//     `settingKey[Set[Def.KeyedInitialize[_]]]`; a key converts implicitly.
//
// Any other head keeps default-deny, so the rewrite surface stays closed.
func isKeyListSetting(name string) bool {
	switch name {
	case "buildInfoKeys", "excludeLintKeys", "includeLintKeys":
		return true
	}
	return false
}

// isKeyListCollection names the collection constructors a key list may be
// written with.
func isKeyListCollection(name string) bool {
	switch name {
	case "Seq", "Set", "List", "Vector":
		return true
	}
	return false
}

// keyListHead reports whether head, an allow-listed key-list setting the main
// loop just claimed, starts a statement in a grammar context, either on its
// own or behind a `Global /` or `ThisBuild /` axis that starts one. Any other
// axis (`core /`) or position (a val body, `inThisBuild(...)`) is no head.
func (s *scan) keyListHead(head token) bool {
	if s.hist[1].kind == kindSymbol && s.hist[1].text == "/" {
		axis := s.hist[2]
		switch {
		case axis.kind != kindIdent:
			return false
		case axis.text == "ThisBuild":
			return s.thisBuildAt != 0 && s.thisBuildAt == s.consumed-2
		case axis.text == "Global":
			return s.globalAt != 0 && s.globalAt == s.consumed-2
		}
		return false
	}
	_, inContext := s.keyContext(head, s.hist[1])
	return inContext
}

// parseKeyList reads the value of an allow-listed key-list setting. Only
// `+= k` and `:=` or `++=` followed by `Seq|Set|List|Vector[T](...)` are
// read. A key named as a plain element (an identifier chain such as
// `scalaVersion` or `Compile / scalaVersion` that ends at ',' or ')') is a
// pending read; it becomes a read only when the list ends the statement.
// Anything after the list (`.map(...)`, `++ ...`, `:=`) denies the pending
// keys. A key in any other element is judged as everywhere else. A form not
// read here leaves its tokens to the main loop, which denies the keys in it.
func (s *scan) parseKeyList() {
	op := s.peek()
	if op.kind != kindSymbol {
		return
	}
	switch op.text {
	case "+=":
		s.next()
		key := s.peek()
		if keyOf(key) == watchNone {
			return
		}
		s.claimNext()
		if !s.endsKeyList(s.peek()) {
			s.judgeKey(key)
		}
	case ":=", "++=":
		s.next()
		s.keyListCollection()
	default:
		// Any other operator: the main loop judges the keys after it.
	}
}

// keyListCollection reads `Seq[T](e1, e2, ...)` and settles its pending reads.
func (s *scan) keyListCollection() {
	name := s.peek()
	if name.kind != kindIdent || !isKeyListCollection(name.text) {
		return
	}
	s.next()
	if typ := s.peek(); typ.kind == kindOpen && typ.text == "[" && !typ.newline {
		s.next()
		s.skipGroup()
	}
	open := s.peek()
	if open.kind != kindOpen || open.text != "(" || open.newline {
		return
	}
	s.next()
	var lib, scala int
	for {
		key, state := s.keyListElement(open.depth)
		switch key {
		case watchLib:
			lib++
		case watchScala:
			scala++
		}
		switch state {
		case listBroken:
			s.denyKeyListReads(lib, scala)
			return
		case listClosed:
			if !s.endsKeyList(s.peek()) {
				s.denyKeyListReads(lib, scala)
			}
			return
		}
	}
}

// Key-list element states.
const (
	listMore   = iota // the element ended at ','
	listClosed        // the element ended at the list's ')'
	listBroken        // end of input, or a handler read past the list
)

// keyListElement consumes one list element and its ',' or the list's ')'. It
// reports the key of a plain key reference (watchNone for any other element)
// and the list state. A key followed by anything but the element's end goes
// to its handler; a key consumed later in the element is judged by the
// watch.
func (s *scan) keyListElement(listDepth int) (uint8, int) {
	var plain uint8
	for chain := true; chain; {
		tok := s.peek()
		switch {
		case keyOf(tok) != watchNone:
			s.claimNext()
			chain = false
			if s.endsElement(s.peek(), listDepth) {
				plain = keyOf(tok)
				break
			}
			s.judgeKey(tok)
			if s.hist[0].depth <= listDepth {
				// The handler read to the list's end or past it; which
				// ')' was the list's own is unknown, so fail closed.
				return watchNone, listBroken
			}
		case tok.kind == kindIdent:
			s.next()
			if slash := s.peek(); slash.kind == kindSymbol && slash.text == "/" {
				s.next()
			} else {
				chain = false
			}
		default:
			chain = false
		}
	}
	for {
		if state, done := s.listPosition(listDepth); done {
			return watchNone, state
		}
		tok := s.peek()
		if s.endsElement(tok, listDepth) {
			s.next()
			if tok.kind == kindClose {
				return plain, listClosed
			}
			return plain, listMore
		}
		// A token of an element that is no plain key; the watch judges any
		// key consumed here.
		s.next()
	}
}

// listPosition reports, from the last consumed token, whether the scan has
// left the list. Inside it every token sits deeper than listDepth, except
// the list's own '(' (an opener carries the depth before it). The list's ')'
// consumed by a handler closes it; end of input or any other token at or
// above listDepth means a handler read past it (broken).
func (s *scan) listPosition(listDepth int) (int, bool) {
	last := s.hist[0]
	switch {
	case last.kind == kindEOF, last.depth < listDepth:
		return listBroken, true
	case last.depth > listDepth, last.kind == kindOpen:
		return listMore, false
	case last.kind == kindClose && last.text == ")":
		return listClosed, true
	}
	return listBroken, true
}

// endsElement reports whether tok is the ',' or ')' that ends a list element
// at listDepth.
func (s *scan) endsElement(tok token, listDepth int) bool {
	switch {
	case isPunct(tok, ","):
		return tok.depth == listDepth+1
	case tok.kind == kindClose && tok.text == ")":
		return tok.depth == listDepth
	}
	return false
}

// endsKeyList reports whether tok ends the key-list statement: end of input,
// ';', the ',' or ')' of an enclosing settings list, or a new line that does
// not continue the expression.
func (s *scan) endsKeyList(tok token) bool {
	switch {
	case tok.kind == kindEOF, isPunct(tok, ";"), isPunct(tok, ","), tok.kind == kindClose:
		return true
	}
	return s.boundary(tok) && !continuesExpression(tok)
}

// claimNext consumes one token the way the main loop does, so the key watch
// does not judge it; the caller classifies it.
func (s *scan) claimNext() token {
	s.claiming = true
	tok := s.next()
	s.claiming = false
	return tok
}

// judgeKey hands a claimed key that is no plain reference to its handler, as
// the main loop would.
func (s *scan) judgeKey(key token) {
	if keyOf(key) == watchLib {
		s.parseLibraryDependencies(key)
		return
	}
	s.parseScalaVersion(key, s.scalaVersionContext(key))
}

// denyKeyListReads denies the pending references of a key list that does not
// end its statement: scalaVersion poisons the envelope, libraryDependencies
// supersedes every row, each counted once for the statement.
func (s *scan) denyKeyListReads(lib, scala int) {
	if scala > 0 {
		s.unresolved++
		s.scalaPoisoned = true
	}
	if lib > 0 {
		s.unresolved++
		s.globalReset = true
	}
}
