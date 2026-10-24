// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// versionKind says how a dependency's revision operand was spelled.
type versionKind uint8

const (
	versionLiteral versionKind = iota
	// versionIdent is an identifier such as catsVersion or a selection such
	// as Versions.cats.value; only a lone identifier naming a top-level
	// string val resolves.
	versionIdent
	// versionOpaque is an interpolated or escaped string.
	versionOpaque
)

// Cross-version labels emitted as sbt_cross_version.
const (
	crossNone    = "none"
	crossBinary  = "binary"
	crossScalaJS = "scalajs"
	crossCustom  = "custom"
)

// dependency is one literal organization % name % revision coordinate before
// deferred version resolution.
type dependency struct {
	org     string
	name    string
	version string
	kind    versionKind
	// scopeKnown is false when the configuration operand could not be mapped
	// to a scope; the row is then partial and counted.
	scopeKnown bool
	// partial is true when the statement did not match the grammar or a later
	// assignment superseded it; the row is then partial and counted.
	partial bool
	// chained is true when the version identifier is the head of a longer
	// expression (`v.trim`, `v("x")`), so it never resolves through a val.
	chained bool
	// optional is true when the configuration names `optional`.
	optional bool
	cross    string
	config   string
	scope    string
	// ctx is the grammar context (0 the bare file, n a project's settings)
	// or noContext. ctx and line are int32 so the struct stays 112 bytes:
	// at 120 the dependency slice grows into larger size classes and a
	// 10,000-row file allocates 1.7 MB more per parse.
	ctx  int32
	line int32
}

// scopeUndetermined reports whether the row cannot be resolved because of
// its statement or configuration, so it is partial and counted.
func (d dependency) scopeUndetermined() bool {
	return !d.scopeKnown || d.partial
}

// setConfig records the `% config` operand and the scope it implies.
func (d *dependency) setConfig(spec configSpec) {
	d.config, d.scope, d.scopeKnown, d.optional = spec.text, spec.scope, spec.known, spec.optional
}

// grammarCollection reads `Seq(`, `List(` or `Vector(` followed by grammar
// elements separated by commas, with an optional trailing comma, and the
// closing ')'. It reports whether the collection matched and how many groups
// are still open when it did not, so the fallback can finish the statement.
func (s *scan) grammarCollection(ctx int32) (bool, int) {
	head := s.peek()
	if s.boundary(head) || head.kind != kindIdent || !isCollectionName(head.text) {
		return false, 0
	}
	s.next()
	open := s.peek()
	if s.boundary(open) || open.kind != kindOpen || open.text != "(" {
		return false, 0
	}
	s.next()
	for {
		tok := s.peek()
		if tok.kind == kindClose && tok.text == ")" {
			s.next()
			return true, 0
		}
		if !s.grammarElement(ctx) {
			return false, 1
		}
		tok = s.peek()
		switch {
		case isPunct(tok, ","):
			s.next()
		case tok.kind == kindClose && tok.text == ")":
			s.next()
			return true, 0
		default:
			return false, 1
		}
	}
}

func isCollectionName(name string) bool {
	switch name {
	case "Seq", "List", "Vector":
		return true
	}
	return false
}

// grammarElement reads one element:
//
//	string ("%" | "%%" | "%%%") string "%" version ["%" config] {call}
//
// It stores the coordinate as soon as its revision is read, so a statement
// that fails later still emits it (as partial). It reports whether the whole
// element matched; it never consumes the token that broke the match.
func (s *scan) grammarElement(ctx int32) bool {
	org, ok := s.expectField()
	if !ok {
		return false
	}
	// From here an early return hands the prefix read so far to the
	// fallback, so a coordinate split by a line break is still emitted.
	s.window = coordinateWindow{step: 1, org: org}
	op := s.peek()
	cross, ok := crossOf(op)
	if s.boundary(op) || !ok {
		return false
	}
	s.next()
	s.window.cross, s.window.step = cross, 2
	dep := dependency{org: org.text, line: org.line, ctx: ctx, scope: "compile", scopeKnown: true, cross: cross}
	name, ok := s.expectField()
	if !ok {
		return false
	}
	s.window.name, s.window.step = name.text, 3
	if !s.expectSymbol("%") {
		return false
	}
	s.window.step = 4
	dep.name = name.text
	if !s.readVersion(&dep) {
		return false
	}
	s.window = coordinateWindow{}
	matched := !dep.chained && s.readConfig(&dep) && s.readCalls(&dep)
	if !matched {
		dep.partial = true
	}
	s.addDep(dep)
	return matched
}

// expectField consumes a string literal that can be a coordinate field.
func (s *scan) expectField() (token, bool) {
	tok := s.peek()
	if s.boundary(tok) || tok.kind != kindString || !validField(tok.text) {
		return tok, false
	}
	return s.next(), true
}

// expectSymbol consumes the operator text.
func (s *scan) expectSymbol(text string) bool {
	tok := s.peek()
	if s.boundary(tok) || tok.kind != kindSymbol || tok.text != text {
		return false
	}
	s.next()
	return true
}

// readVersion reads the revision: a non-empty string literal, an
// interpolated or escaped string, or an identifier selection. An identifier
// followed by a call, a type argument, or a '.' on the next line is marked
// chained: the element no longer matches and the identifier never resolves.
func (s *scan) readVersion(dep *dependency) bool {
	tok := s.peek()
	if s.boundary(tok) {
		return false
	}
	switch tok.kind {
	case kindString:
		if tok.text == "" || len(tok.text) > MaxFieldBytes {
			return false
		}
		s.next()
		dep.version, dep.kind = tok.text, versionLiteral
	case kindOpaque:
		s.next()
		dep.version, dep.kind = tok.text, versionOpaque
	case kindIdent:
		s.next()
		chain, ok := s.readQualID(tok.text)
		if !ok {
			// The overlong selection is consumed; the prefix must not pair
			// with whatever follows it.
			s.window = coordinateWindow{}
			return false
		}
		dep.version, dep.kind = chain, versionIdent
		next := s.peek()
		if isPunct(next, ".") || (next.kind == kindOpen && next.text != "{" && !next.newline) {
			dep.chained = true
		}
	default:
		return false
	}
	return true
}

// configIdents are the sbt Configuration values the grammar reads by name.
func isConfigIdent(name string) bool {
	switch name {
	case "Compile", "Test", "Runtime", "Provided", "Optional", "IntegrationTest":
		return true
	}
	return false
}

// readConfig reads an optional `% config`, where config is a string literal
// or one of the Configuration names in isConfigIdent. A configuration the
// table cannot map leaves the scope unknown (the row is partial) but still
// matches; any other operand does not match.
func (s *scan) readConfig(dep *dependency) bool {
	op := s.peek()
	if s.boundary(op) || op.kind != kindSymbol || op.text != "%" {
		return true
	}
	s.next()
	operand := s.peek()
	if s.boundary(operand) {
		return false
	}
	switch {
	case operand.kind == kindString:
		s.next()
		dep.setConfig(resolveConfig(operand.text))
	case operand.kind == kindIdent && isConfigIdent(operand.text):
		s.next()
		dep.setConfig(resolveConfig(operand.text))
	default:
		return false
	}
	return true
}

// callKind classifies an infix method name after an element.
type callKind uint8

const (
	// callUnknown is every name off the allowlist; it ends the match.
	callUnknown callKind = iota
	// callFlag takes an empty argument list: `withSources()`.
	callFlag
	// callExclude takes two string literals: `exclude("org", "name")`.
	callExclude
	// callGroup takes one argument list: `excludeAll(ExclusionRule(...))`.
	callGroup
	// callClassifier takes a string literal, bare or in parentheses.
	callClassifier
	// callCross takes an argument list or a CrossVersion selection.
	callCross
)

// classifyCall is the allowlist of ModuleID methods the grammar accepts after
// an element. Each one was read in sbt librarymanagement
// (core/src/main/scala/sbt/librarymanagement/ModuleIDExtra.scala, branch
// 1.12.x at commit 36bd983463) and rewrites only a field other than the
// module's organization, name, revision and configurations:
//
//	exclude, excludeAll        withExclusions
//	classifier                 artifacts(Artifact(name, c))
//	intransitive               withIsTransitive(false)
//	withSources, withJavadoc   jarIfEmpty.sources() / .javadoc()
//	cross                      withCrossVersion
//	force, changing            withIsForce(true) / withIsChanging(true)
//
// Every other name (withConfigurations, withRevision, withName, configure,
// from, artifacts, an extension method) ends the match. Adding a name needs
// a test and a quote of its definition.
func classifyCall(name string) callKind {
	switch name {
	case "intransitive", "withSources", "withJavadoc", "force", "changing":
		return callFlag
	case "exclude":
		return callExclude
	case "excludeAll":
		return callGroup
	case "classifier":
		return callClassifier
	case "cross":
		return callCross
	}
	return callUnknown
}

// readCalls reads the infix allowlisted calls after an element. It stops at
// the first token that is not an identifier on the element's line (or inside
// its parentheses) and reports false for an identifier off the allowlist or a
// malformed argument list.
func (s *scan) readCalls(dep *dependency) bool {
	for {
		tok := s.peek()
		if s.boundary(tok) || tok.kind != kindIdent {
			return true
		}
		kind := classifyCall(tok.text)
		if kind == callUnknown {
			return false
		}
		s.next()
		if !s.readCallArgs(kind) {
			return false
		}
		if kind == callCross {
			dep.cross = crossCustom
		}
	}
}

// readCallArgs reads the operand of one allowlisted call.
func (s *scan) readCallArgs(kind callKind) bool {
	switch kind {
	case callFlag:
		return s.expectOpen() && s.expectClose()
	case callExclude:
		return s.expectOpen() && s.expectAnyString() && s.expectComma() && s.expectAnyString() && s.expectClose()
	case callGroup:
		if !s.expectOpen() {
			return false
		}
		s.skipGroup()
		return true
	case callClassifier:
		if s.expectAnyString() {
			return true
		}
		return s.expectOpen() && s.expectAnyString() && s.expectClose()
	case callCross:
		return s.readCrossOperand()
	case callUnknown:
		return false
	}
	return false
}

// readCrossOperand reads `(args)` or a selection such as CrossVersion.full,
// optionally applied to one argument list.
func (s *scan) readCrossOperand() bool {
	if s.expectOpen() {
		s.skipGroup()
		return true
	}
	tok := s.peek()
	if s.boundary(tok) || tok.kind != kindIdent {
		return false
	}
	s.next()
	if _, ok := s.readQualID(tok.text); !ok {
		return false
	}
	if next := s.peek(); next.kind == kindOpen && next.text == "(" && !next.newline {
		s.next()
		s.skipGroup()
	}
	return true
}

func (s *scan) expectOpen() bool {
	tok := s.peek()
	if s.boundary(tok) || tok.kind != kindOpen || tok.text != "(" {
		return false
	}
	s.next()
	return true
}

func (s *scan) expectClose() bool {
	tok := s.peek()
	if tok.kind != kindClose || tok.text != ")" {
		return false
	}
	s.next()
	return true
}

func (s *scan) expectComma() bool {
	if !isPunct(s.peek(), ",") {
		return false
	}
	s.next()
	return true
}

func (s *scan) expectAnyString() bool {
	tok := s.peek()
	if s.boundary(tok) || tok.kind != kindString {
		return false
	}
	s.next()
	return true
}

// readQualID extends an identifier with `.member` selections on the same line.
// A selection longer than MaxFieldBytes is consumed without being buffered
// and reported as not ok, so a million-segment chain cannot inflate a row.
func (s *scan) readQualID(head string) (string, bool) {
	if next := s.peek(); len(head) <= MaxFieldBytes && (!isPunct(next, ".") || next.newline) {
		// A lone identifier needs no builder: return the token's own text.
		return head, true
	}
	var chain strings.Builder
	over := len(head) > MaxFieldBytes
	if !over {
		chain.WriteString(head)
	}
	for {
		dot := s.peek()
		if !isPunct(dot, ".") || dot.newline {
			break
		}
		s.next()
		member := s.peek()
		if member.kind != kindIdent {
			// `v.` followed by a non-name: the '.' was consumed, so the
			// selection is malformed.
			return "", false
		}
		s.next()
		if !over && chain.Len()+1+len(member.text) > MaxFieldBytes {
			over = true
		}
		if !over {
			chain.WriteByte('.')
			chain.WriteString(member.text)
		}
	}
	if over {
		return "", false
	}
	return chain.String(), true
}
