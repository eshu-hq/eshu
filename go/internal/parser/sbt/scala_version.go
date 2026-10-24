// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// scalaVersionContext reports whether the scalaVersion key just consumed has a
// `ThisBuild /` axis and whether it sits where a literal may be recorded: the
// key itself in a grammar context (keyContext), or, with the axis, the
// `ThisBuild` token in one. These are the places libraryDependencies would
// resolve, so a literal nested in a call, a wrapper, a block, a settings val,
// a member assignment or under another axis never reaches the envelope.
func (s *scan) scalaVersionContext(key token) (thisBuild, inContext bool) {
	if isThisBuildAxis(s.hist[2], s.hist[1]) {
		return true, s.thisBuildAt != 0 && s.thisBuildAt == s.consumed-2
	}
	_, inContext = s.keyContext(key, s.hist[1])
	return false, inContext
}

// parseScalaVersion reads `scalaVersion [in ThisBuild] := "literal"`. Anything
// else after the key (for example `scalaVersion.value`) is not an assignment
// and is left to the main loop. An assignment outside a grammar context, or
// under a legacy `in` axis other than ThisBuild, is counted once and never
// recorded.
func (s *scan) parseScalaVersion(thisBuild, inContext bool) {
	tok := s.next()
	if tok.kind == kindIdent && tok.text == "in" {
		scope := s.next()
		if scope.kind == kindIdent && scope.text == "ThisBuild" {
			thisBuild = true
		} else {
			inContext = false
		}
		tok = s.next()
	}
	if tok.kind != kindSymbol || tok.text != ":=" {
		s.unread(tok)
		return
	}
	if !inContext {
		s.unresolved++
		s.salvage(0, false)
		return
	}
	value := s.next()
	switch {
	case value.kind == kindString && validField(value.text) && endsLiteral(s.peek()):
		s.recordScalaVersion(value.text, thisBuild)
	case value.kind == kindIdent && s.resolveIdentVersion(value):
		s.recordScalaVersion(s.vals[value.text], thisBuild)
	case value.kind == kindEOF:
	default:
		s.unresolved++
		s.salvage(initialDepth(value), false)
	}
}

// resolveIdentVersion reports whether value is a bare identifier naming a
// previously seen top-level string val and not the head of a longer expression.
func (s *scan) resolveIdentVersion(value token) bool {
	if _, ok := s.vals[value.text]; !ok {
		return false
	}
	return endsLiteral(s.peek())
}

func (s *scan) recordScalaVersion(value string, thisBuild bool) {
	if len(s.scalaVersions) >= maxScalaVersions {
		return
	}
	s.scalaVersions = append(s.scalaVersions, scalaVersionDecl{value: value, thisBuild: thisBuild})
}

func (s *scan) distinctScalaVersions() []string {
	seen := map[string]struct{}{}
	versions := make([]string, 0, len(s.scalaVersions))
	for _, decl := range s.scalaVersions {
		if _, ok := seen[decl.value]; ok {
			continue
		}
		seen[decl.value] = struct{}{}
		versions = append(versions, decl.value)
	}
	return versions
}

// primaryScalaVersion prefers the first ThisBuild-scoped literal, else the
// first literal in source order, and is empty when none was a literal.
func (s *scan) primaryScalaVersion() string {
	for _, decl := range s.scalaVersions {
		if decl.thisBuild {
			return decl.value
		}
	}
	if len(s.scalaVersions) > 0 {
		return s.scalaVersions[0].value
	}
	return ""
}
