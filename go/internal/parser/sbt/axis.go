// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "strings"

// maxProjects caps the `lazy val x = project` names remembered for axis checks.
const maxProjects = 1024

// parseLibraryDependencies reads the operator after a libraryDependencies key.
// `+=` takes one element; `++=` and `:=` take a Seq/List/Vector literal. Any
// other shape (for example `libraryDependencies.value`) is not a declaration
// and is left to the main loop; a recognised assignment whose right-hand side
// is not a literal collection is counted as unresolved. scoped is true when the
// key sits under a configuration axis or an `inConfig(...)` style wrapper.
func (s *scan) parseLibraryDependencies(scoped bool) {
	op := s.next()
	switch {
	case op.kind == kindIdent && op.text == "in":
		scoped = s.readInAxis(scoped)
		op = s.next()
	case isPunct(op, "."):
		name := s.next()
		if name.kind != kindIdent || name.text != "in" {
			s.unread(name)
			return
		}
		scoped = s.readDotInAxis(scoped)
		op = s.next()
	}
	if op.kind != kindSymbol {
		s.unread(op)
		return
	}
	switch op.text {
	case "+=":
		s.parseElement(scoped)
	case "++=", ":=":
		s.parseCollections(scoped)
	case "~=", "-=", "--=", "<+=", "<++=":
		s.unresolved++
	default:
		s.unread(op)
	}
}

// readInAxis reads the scope after a legacy `libraryDependencies in <scope>`. A
// ThisBuild, Global or declared project reference keeps the axis it had. Any
// other scope (a configuration, a task, a parenthesised list, a name the file
// does not declare as a project) cannot be proven to leave the project-level key
// alone, so the declaration is scoped.
func (s *scan) readInAxis(scoped bool) bool {
	scope := s.next()
	switch scope.kind {
	case kindIdent:
		return scoped || !s.axisNeutral(scope.text)
	case kindOpen:
		s.skipGroup()
		return true
	case kindEOF:
		return scoped
	default:
		s.unread(scope)
		return scoped
	}
}

// readDotInAxis reads the `(<scope>)` of `libraryDependencies.in(<scope>)`
// after the `in`. Only a single neutral axis name keeps the key unscoped.
func (s *scan) readDotInAxis(scoped bool) bool {
	open := s.next()
	if open.kind != kindOpen || open.text != "(" {
		s.unread(open)
		return true
	}
	scope := s.next()
	if scope.kind == kindIdent && s.axisNeutral(scope.text) && s.peek().kind == kindClose {
		s.next()
		return scoped
	}
	switch scope.kind {
	case kindClose, kindEOF:
		return true
	case kindOpen:
		s.skipGroup()
	default:
		// A plain operand: the rest of the list is skipped below.
	}
	s.skipGroup()
	return true
}

// axisNeutral reports whether name, used as `name / key` or `key in name`,
// leaves libraryDependencies a project-level key: the build-wide axes, or a
// name this file declared as a project. Everything else is a configuration,
// a task, or a name the scanner cannot place, and scopes the declaration.
func (s *scan) axisNeutral(name string) bool {
	switch name {
	case "ThisBuild", "Global", "ThisProject", "LocalRootProject":
		return true
	}
	_, ok := s.projects[name]
	return ok
}

// recordProject remembers a val declared as an sbt project, so
// `core / libraryDependencies` is read as a project-level key.
func (s *scan) recordProject(name string) {
	if len(s.projects) >= maxProjects {
		return
	}
	s.projects[name] = struct{}{}
}

// isProjectHead reports the identifiers that start an sbt project definition.
func isProjectHead(tok token) bool {
	if tok.kind != kindIdent {
		return false
	}
	switch tok.text {
	case "project", "crossProject", "Project", "projectMatrix":
		return true
	}
	return false
}

// isScopeWrapper reports `inConfig`, `inTask`, `inScope`, `inAll` and the other
// `in<Name>(scope)(settings)` wrappers that move the settings inside to another
// scope. `inThisBuild` is the build-wide axis and is not one.
func isScopeWrapper(name string) bool {
	if len(name) < 3 || !strings.HasPrefix(name, "in") || name == "inThisBuild" {
		return false
	}
	return name[2] >= 'A' && name[2] <= 'Z'
}

// axisScoped reports whether the libraryDependencies key just read sits under a
// configuration axis (`Test / libraryDependencies`, `ThisBuild / Test / ...`) or
// inside an `inConfig(...)`-style wrapper. prev is the token before the key.
func (s *scan) axisScoped(prev token) bool {
	if s.wrapDepth > 0 {
		return true
	}
	return prev.kind == kindSymbol && prev.text == "/" && s.chainScoped
}

// trackAxis updates the axis chain and wrapper state for one main-loop token
// before it is dispatched. prev is the token before tok.
//
// chainScoped says the `a / b / ...` chain that ends at tok holds a name that
// is not a neutral axis. The wrapper state covers the argument lists after an
// `in<Name>` wrapper, including the curried second list.
//
// It returns whether the token, if it is a libraryDependencies key, is scoped;
// the answer is read before the token joins the chain.
func (s *scan) trackAxis(prev, tok token) bool {
	s.trackWrapper(tok)
	scoped := s.axisScoped(prev)
	prevSlash := prev.kind == kindSymbol && prev.text == "/"
	switch {
	case tok.kind == kindIdent:
		if !prevSlash {
			s.chainScoped = false
		}
		if !s.axisNeutral(tok.text) {
			s.chainScoped = true
		}
	case tok.kind == kindSymbol && tok.text == "/":
		// The separator keeps the chain going.
	default:
		s.chainScoped = false
	}
	return scoped
}

// trackWrapper follows the region covered by an `in<Name>(...)(...)` wrapper by
// bracket depth. Tokens inside have depth wrapDepth or deeper; the closer of an
// argument list leaves wrapClosed set until the next token shows whether a
// curried list follows. A wrapper met inside an active region changes nothing,
// because the outer wrapper already scopes everything inside it.
func (s *scan) trackWrapper(tok token) {
	if s.wrapPending > 0 {
		if tok.kind == kindOpen && tok.text == "(" && s.wrapDepth == 0 {
			s.wrapDepth, s.wrapClosed = s.wrapPending, false
		}
		s.wrapPending = 0
	}
	if s.wrapDepth > 0 {
		switch {
		case s.wrapClosed && tok.kind == kindOpen && tok.text == "(":
			s.wrapClosed = false
		case s.wrapClosed, tok.depth < s.wrapDepth-1:
			s.wrapDepth, s.wrapClosed = 0, false
		case tok.kind == kindClose && tok.depth == s.wrapDepth-1:
			s.wrapClosed = true
		}
	}
	if s.wrapDepth == 0 && tok.kind == kindIdent && isScopeWrapper(tok.text) {
		s.wrapPending = tok.depth + 1
	}
}
