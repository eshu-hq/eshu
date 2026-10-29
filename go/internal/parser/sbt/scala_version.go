// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// scalaVersionContext reports whether the scalaVersion key just consumed sits
// where its assignment decides the build's version: the key itself in a
// grammar context (keyContext), or, behind a `ThisBuild /` axis, the
// `ThisBuild` token in one. These are the places libraryDependencies would
// resolve, so an assignment nested in a call, a wrapper, a block, a settings
// val, a member assignment or under another axis never reaches the envelope.
func (s *scan) scalaVersionContext(key token) bool {
	if isThisBuildAxis(s.hist[2], s.hist[1]) {
		return s.thisBuildAt != 0 && s.thisBuildAt == s.consumed-2
	}
	_, inContext := s.keyContext(key, s.hist[1])
	return inContext
}

// parseScalaVersion reads one statement that starts at a scalaVersion key.
// It is default-deny. Only `scalaVersion [in ThisBuild] := "literal"` (or a
// tracked top-level val) in a grammar context records a version, and only a
// read (`scalaVersion.value`, `scalaVersion.in(Test).value`,
// `(scalaVersion in Test).value`) leaves the envelope alone. Every other
// occurrence poisons the envelope and is counted once: a value that is not a
// plain literal, any operator but `:=`, any member but a read, chained or
// non-ThisBuild axes, a parenthesized key, an infix member (`withRank`,
// `set`), a bare key, and any assignment outside a grammar context, which
// may reach a project the scanner does not track.
func (s *scan) parseScalaVersion(key token, inContext bool) {
	if axes, thisBuild := s.skipLegacyAxes(); axes > 1 || (axes == 1 && !thisBuild) {
		inContext = false
	}
	op := s.peek()
	switch {
	case isPunct(s.hist[0], "."):
		if op.kind != kindIdent || !isKeyRead(op.text) {
			s.unknownScalaVersion(0)
		}
		return
	case op.kind == kindClose:
		if !s.readThroughClosers() {
			s.unknownScalaVersion(0)
		}
		return
	case op.kind != kindSymbol || op.text != ":=" || !inContext:
		s.unknownScalaVersion(0)
		return
	}
	s.next()
	value := s.next()
	switch {
	case value.kind == kindString && validField(value.text) && endsLiteral(s.peek()):
		s.recordScalaVersion(value.text, int(key.line))
	case value.kind == kindIdent && s.resolveIdentVersion(value):
		s.recordScalaVersion(s.vals[value.text], int(key.line))
	case value.kind == kindEOF:
		// `scalaVersion :=` cut off by end of input: counted and poisoned,
		// with nothing left to skip.
		s.unresolved++
		s.scalaPoisoned = true
	default:
		s.unknownScalaVersion(initialDepth(value))
	}
}

// unknownScalaVersion counts a scalaVersion statement the scanner cannot read
// and skips the rest of it from rel open groups deep. It poisons the envelope
// for good: sbt applies the last assignment of a scope, and the scanner
// neither decides the order of mixed forms across the bare file, ThisBuild
// and each project's settings nor tracks which projects a nested assignment
// reaches, so a later literal does not clear it.
func (s *scan) unknownScalaVersion(rel int) {
	s.unresolved++
	s.scalaPoisoned = true
	s.cover = watchScala
	s.salvage(rel, false)
}

// resolveIdentVersion reports whether value is a bare identifier naming a
// previously seen top-level string val and not the head of a longer expression.
func (s *scan) resolveIdentVersion(value token) bool {
	if _, ok := s.vals[value.text]; !ok {
		return false
	}
	return endsLiteral(s.peek())
}

// recordScalaVersion keeps value once among the distinct literals, up to
// maxScalaVersions. A literal past the cap still poisons the envelope, so the
// count stays a lower bound and never names a single version wrongly.
func (s *scan) recordScalaVersion(value string, line int) {
	for _, seen := range s.scalaVersions {
		if seen == value {
			return
		}
	}
	if len(s.scalaVersions) >= maxScalaVersions {
		s.scalaPoisoned = true
		return
	}
	if len(s.scalaVersions) == 0 {
		s.scalaVersionLine = line
	}
	s.scalaVersions = append(s.scalaVersions, value)
}

// scalaVersion is the single version the envelope reports: the one distinct
// literal when nothing poisoned the envelope, else empty. Two distinct
// literals leave it empty, because sbt's last-assignment-wins order across
// scopes is not decided by the scanner.
func (s *scan) scalaVersion() string {
	if s.scalaPoisoned || len(s.scalaVersions) != 1 {
		return ""
	}
	return s.scalaVersions[0]
}
