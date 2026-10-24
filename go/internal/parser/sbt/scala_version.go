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
// Only `scalaVersion [in ThisBuild] := "literal"` (or a tracked top-level val)
// in a grammar context records a version. Every other assignment poisons the
// envelope and is counted once: in a grammar context a value that is not a
// plain literal, `~=`, `<<=`, `<+=`, or a member that returns a Setting such
// as `set`, `transform`, `withRank(...) :=`, `in(...) :=` or `.:=(...)`
// changes the version to something the scanner cannot read; outside one (a
// settings val or def, a wrapper, a lambda, an axis other than ThisBuild) any
// assignment may reach a project the scanner does not track. A key that is
// not assigned (`scalaVersion.value`, `scalaVersion.in(Test).value`, a
// comparison) is a read and is left to the main loop.
func (s *scan) parseScalaVersion(inContext bool) {
	tok := s.next()
	if tok.kind == kindIdent && tok.text == "in" {
		scope := s.next()
		switch {
		case scope.kind == kindOpen:
			s.skipGroup()
			inContext = false
		case scope.kind != kindIdent || scope.text != "ThisBuild":
			inContext = false
		}
		tok = s.next()
	}
	if isPunct(tok, ".") {
		// The '.' stays consumed for a read; the main loop skips the member.
		if s.scalaVersionMemberWrites() {
			s.unknownScalaVersion(0)
		}
		return
	}
	if tok.kind != kindSymbol || !isAssignmentOperator(tok.text) {
		s.unread(tok)
		return
	}
	if tok.text != ":=" || !inContext {
		s.unknownScalaVersion(0)
		return
	}
	value := s.next()
	switch {
	case value.kind == kindString && validField(value.text) && endsLiteral(s.peek()):
		s.recordScalaVersion(value.text)
	case value.kind == kindIdent && s.resolveIdentVersion(value):
		s.recordScalaVersion(s.vals[value.text])
	case value.kind == kindEOF:
		s.scalaPoisoned = true
	default:
		s.unknownScalaVersion(initialDepth(value))
	}
}

// scalaVersionMemberWrites reports whether the member after a consumed '.'
// can assign the key: a dotted assignment operator (`.:=(...)`), a member
// that returns a Setting or the key (isScalaVersionWriter), or, after a
// `.in(...)` axis, any of those or an assignment operator. A member that
// returns no Setting, alone or after the axis (`.in(Test).value`), is a read.
func (s *scan) scalaVersionMemberWrites() bool {
	member := s.peek()
	if member.kind == kindSymbol {
		return isAssignmentOperator(member.text)
	}
	if member.kind != kindIdent || !isScalaVersionWriter(member.text) {
		return false
	}
	if member.text != "in" {
		return true
	}
	s.next()
	if open := s.peek(); open.kind == kindOpen && open.text == "(" {
		s.next()
		s.skipGroup()
	}
	next := s.peek()
	if next.kind == kindSymbol {
		return isAssignmentOperator(next.text)
	}
	if !isPunct(next, ".") {
		return true
	}
	s.next()
	after := s.peek()
	if after.kind == kindSymbol {
		return isAssignmentOperator(after.text)
	}
	return after.kind == kindIdent && isScalaVersionWriter(after.text)
}

// isScalaVersionWriter reports a member of the scalaVersion key that returns
// a Setting or the key itself, so the statement can change the version: the
// rewriting and appending members of sbt's SettingKey API, `withRank`, and
// the legacy `in(...)` axis, which an assignment may follow.
func isScalaVersionWriter(member string) bool {
	return member == "in" || settingMemberOf(member) != memberValue
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
func (s *scan) recordScalaVersion(value string) {
	for _, seen := range s.scalaVersions {
		if seen == value {
			return
		}
	}
	if len(s.scalaVersions) >= maxScalaVersions {
		s.scalaPoisoned = true
		return
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
