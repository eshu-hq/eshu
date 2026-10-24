// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

const (
	// LanguageName is the parser payload language and package-manager label.
	LanguageName = "sbt"

	// MaxDependencyRows caps the dependency rows one build.sbt emits. Valid
	// declarations past the cap are counted in
	// sbt_state.dependency_overflow_count instead of stored, so a 100k-line
	// file cannot inflate the payload.
	MaxDependencyRows = 10_000

	// MaxFieldBytes is the longest organization, module name, or version
	// literal read as a coordinate field. A longer literal is not a coordinate.
	MaxFieldBytes = 512

	// maxStringVals caps the top-level `val name = "literal"` table used to
	// resolve version references.
	maxStringVals = 1024

	// maxScalaVersions caps the distinct scalaVersion literals tracked.
	maxScalaVersions = 64
)

// Parse reads one build.sbt and returns the parent parser payload with one
// content_entity-shaped "variables" row per literal libraryDependencies
// coordinate, plus an sbt_state envelope that carries the Scala version and
// the bounded counts of what the scanner could not resolve.
//
// Parse never evaluates sbt or Scala. It reads only file bytes, so a
// non-literal form (variable organization, `.value` setting, conditional
// block, interpolated string) is counted, never guessed.
func Parse(path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	text := string(source)

	payload := shared.BasePayload(path, LanguageName, isDependency)
	scan := newScan(text)
	scan.run()
	rows, state := scan.finish()
	payload["variables"] = rows
	payload["sbt_state"] = state

	if options.IndexSource {
		payload["source"] = text
	}
	return payload, nil
}

// scalaVersionDecl is one literal `scalaVersion := "x"` assignment.
type scalaVersionDecl struct {
	value     string
	thisBuild bool
}

// scan holds the single-pass state for one build.sbt.
type scan struct {
	lx      *lexer
	peeked  token
	hasPeek bool
	// consumed counts tokens handed out by next; collection parsing uses it to
	// prove every loop iteration made progress.
	consumed int

	deps          []dependency
	vals          map[string]string
	scalaVersions []scalaVersionDecl

	// unresolved counts declarations the scanner recognised but could not
	// read as literals.
	unresolved int
	// overflow counts valid declarations dropped past MaxDependencyRows.
	overflow int
}

func newScan(text string) *scan {
	return &scan{lx: newLexer(text), vals: map[string]string{}}
}

// next returns the next token, honouring a single pushed-back token.
func (s *scan) next() token {
	s.consumed++
	if s.hasPeek {
		s.hasPeek = false
		return s.peeked
	}
	return s.lx.next()
}

// peek returns the next token without consuming it.
func (s *scan) peek() token {
	if !s.hasPeek {
		s.peeked = s.lx.next()
		s.hasPeek = true
	}
	return s.peeked
}

// unread pushes one consumed token back. It must not be called while another
// token is already pushed back.
func (s *scan) unread(tok token) {
	s.consumed--
	s.peeked = tok
	s.hasPeek = true
}

// run walks the token stream once and dispatches on the three key names the
// parser reads.
func (s *scan) run() {
	var prev, prev2 token
	for {
		tok := s.next()
		if tok.kind == kindEOF {
			return
		}
		consumed := false
		if tok.kind == kindIdent {
			switch tok.text {
			case "libraryDependencies":
				s.parseLibraryDependencies(axisScope(prev2, prev))
				consumed = true
			case "scalaVersion":
				s.parseScalaVersion(isThisBuildAxis(prev2, prev))
				consumed = true
			case "val":
				if tok.depth == 0 {
					s.parseVal()
					consumed = true
				}
			}
		}
		if consumed {
			prev, prev2 = token{}, token{}
			continue
		}
		prev2, prev = prev, tok
	}
}

// parseScalaVersion reads `scalaVersion [in ThisBuild] := "literal"`. Anything
// else after the key (for example `scalaVersion.value`) is not an assignment
// and is left to the main loop.
func (s *scan) parseScalaVersion(thisBuild bool) {
	tok := s.next()
	if tok.kind == kindIdent && tok.text == "in" {
		scope := s.next()
		if scope.kind == kindIdent && scope.text == "ThisBuild" {
			thisBuild = true
		}
		tok = s.next()
	}
	if tok.kind != kindSymbol || tok.text != ":=" {
		s.unread(tok)
		return
	}
	value := s.next()
	switch {
	case value.kind == kindString && validField(value.text):
		s.recordScalaVersion(value.text, thisBuild)
	case value.kind == kindIdent && s.resolveIdentVersion(value):
		s.recordScalaVersion(s.vals[value.text], thisBuild)
	case value.kind == kindEOF:
	default:
		s.unresolved++
		s.skipElement(initialDepth(value))
	}
}

// resolveIdentVersion reports whether value is a bare identifier naming a
// previously seen top-level string val and not the head of a longer expression.
func (s *scan) resolveIdentVersion(value token) bool {
	if _, ok := s.vals[value.text]; !ok {
		return false
	}
	next := s.peek()
	return next.kind != kindPunct || next.text != "."
}

func (s *scan) recordScalaVersion(value string, thisBuild bool) {
	if len(s.scalaVersions) >= maxScalaVersions {
		return
	}
	s.scalaVersions = append(s.scalaVersions, scalaVersionDecl{value: value, thisBuild: thisBuild})
}

// parseVal records a depth-zero `val name[: String] = "literal"` so later
// version references can resolve. A val whose initializer is more than the
// literal (for example `"1" + suffix`) is ignored.
func (s *scan) parseVal() {
	name := s.next()
	if name.kind != kindIdent {
		s.unread(name)
		return
	}
	tok := s.next()
	if tok.kind == kindSymbol && tok.text == ":" {
		typ := s.next()
		if typ.kind != kindIdent {
			s.unread(typ)
			return
		}
		tok = s.next()
	}
	if tok.kind != kindSymbol || tok.text != "=" {
		s.unread(tok)
		return
	}
	value := s.next()
	if value.kind != kindString || !validField(value.text) {
		s.unread(value)
		return
	}
	end := s.peek()
	if end.kind != kindEOF && !end.newline && !isPunct(end, ";") {
		return
	}
	if _, exists := s.vals[name.text]; exists || len(s.vals) >= maxStringVals {
		return
	}
	s.vals[name.text] = value.text
}

// finish resolves deferred version references, builds the rows, and returns
// the sbt_state envelope.
func (s *scan) finish() ([]map[string]any, map[string]any) {
	versions := s.distinctScalaVersions()
	rows := make([]map[string]any, 0, len(s.deps))
	binary := ""
	if len(versions) == 1 {
		binary = scalaBinaryVersion(versions[0])
	}
	for _, dep := range s.deps {
		rows = append(rows, dep.row(s.vals, binary))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, _ := rows[i]["name"].(string)
		right, _ := rows[j]["name"].(string)
		return left < right
	})

	state := map[string]any{
		"state":                        "parsed",
		"scala_version":                s.primaryScalaVersion(),
		"scala_version_count":          len(versions),
		"dependency_count":             len(rows),
		"dependency_overflow_count":    s.overflow,
		"unresolved_declaration_count": s.unresolved + scopeUnknownCount(s.deps),
	}
	return rows, state
}

// scopeUnknownCount counts the rows whose scope could not be determined. They
// are emitted as partial and counted with the unresolved declarations.
func scopeUnknownCount(deps []dependency) int {
	count := 0
	for _, dep := range deps {
		if !dep.scopeKnown {
			count++
		}
	}
	return count
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

// validField reports whether text can be a coordinate field.
func validField(text string) bool {
	if text == "" || len(text) > MaxFieldBytes {
		return false
	}
	return !strings.ContainsAny(text, " \t\r\n:")
}
