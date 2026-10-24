// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// noContext is the context of a key outside the grammar's two contexts. Its
// rows are always partial and never take part in superseding.
const noContext int32 = -1

// parseLibraryDependencies reads one statement that starts at a
// libraryDependencies key. A row is resolved only when the key sits in a
// grammar context (keyContext), the operator is `+=`, `++=` or `:=`, and the
// rest of the statement matches the grammar token for token (README.md,
// "Grammar"). Every other assignment falls back to salvage: each literal
// coordinate in the statement is emitted partial and counted. A key that is
// not followed by an assignment (`libraryDependencies.value`) is not a
// declaration and is left to the main loop.
func (s *scan) parseLibraryDependencies(key token) {
	ctx, inContext := s.keyContext(key, s.hist[1])
	if s.skipLegacyAxis() {
		inContext = false
	}
	op := s.peek()
	if op.kind != kindSymbol {
		return
	}
	switch op.text {
	case "+=", "++=", ":=":
		s.next()
	case "~=", "-=", "--=":
		// These transform or remove what earlier rows declared and add none.
		s.next()
		s.unresolved++
		s.markReset(ctx, inContext)
		s.salvage(0, false)
		return
	case "<+=", "<++=":
		s.next()
		s.unresolved++
		s.salvage(0, false)
		return
	default:
		return
	}

	start := len(s.deps)
	s.stmtRows = 0
	s.window = coordinateWindow{}
	if !inContext {
		if op.text == ":=" {
			s.markReset(ctx, false)
		}
		s.failStatement(start, 0)
		return
	}
	if op.text == ":=" {
		s.markReset(ctx, true)
	}
	matched, rel := false, 0
	if op.text == "+=" {
		matched = s.grammarElement(ctx)
	} else {
		matched, rel = s.grammarCollection(ctx)
	}
	if !matched || !s.statementEnds(ctx) {
		s.failStatement(start, rel)
	}
}

// keyContext reports whether a key starts a statement in one of the two
// grammar contexts and which one: the bare file (context 0), where the key
// starts a top-level statement, or directly inside the `.settings(` argument
// list of a project val (the project's context), where the key is the first
// token of an argument. Anything else, such as a key after an axis (`Test /`),
// inside a wrapper (`inConfig(Test)(...)`), a brace block, a val or def body,
// or an if body, is not a grammar context.
func (s *scan) keyContext(key, prev token) (int32, bool) {
	if key.depth == 0 && s.startsStatement(key, prev) {
		return 0, true
	}
	if s.settingsCtx != 0 && key.depth == s.settingsDepth && key.inParen {
		switch {
		case prev.kind == kindOpen && prev.text == "(" && prev.depth == s.settingsDepth-1,
			isPunct(prev, ",") && prev.depth == s.settingsDepth:
			return s.settingsCtx, true
		}
	}
	return noContext, false
}

// startsStatement reports whether key begins a top-level statement: it is the
// first token, follows ';', or starts a line after a token that can end a
// statement. A key after an operator, an opener, a keyword such as else or
// then, or the closed condition of an if, while or for continues an
// expression instead.
func (s *scan) startsStatement(key, prev token) bool {
	switch {
	case prev.kind == kindEOF:
		return s.consumed == 1
	case isPunct(prev, ";"):
		return true
	case !key.newline:
		return false
	}
	switch prev.kind {
	case kindString, kindOpaque, kindOther:
		return true
	case kindClose:
		return !prev.closesCond
	case kindIdent:
		return !isContinuationKeyword(prev.text)
	default:
		return false
	}
}

// isContinuationKeyword names the Scala keywords after which a new line
// continues the same expression or starts its body.
func isContinuationKeyword(word string) bool {
	switch word {
	case "if", "else", "then", "do", "while", "for", "yield", "match", "case",
		"return", "throw", "try", "catch", "finally", "new", "with", "extends":
		return true
	}
	return false
}

// skipLegacyAxis consumes a legacy `in <scope>` or `.in(<scope>)` after the
// key and reports whether one was there. Any axis takes the statement out of
// the grammar. A '.' followed by anything else is left in place.
func (s *scan) skipLegacyAxis() bool {
	tok := s.peek()
	switch {
	case tok.kind == kindIdent && tok.text == "in":
		s.next()
		operand := s.peek()
		switch operand.kind {
		case kindIdent:
			s.next()
			s.readQualID(operand.text)
		case kindOpen:
			s.next()
			s.skipGroup()
		default:
			// A missing operand: the operator check below decides.
		}
		return true
	case isPunct(tok, "."):
		s.next()
		name := s.peek()
		if name.kind != kindIdent || name.text != "in" {
			return false
		}
		s.next()
		if open := s.peek(); open.kind == kindOpen {
			s.next()
			s.skipGroup()
		}
		return true
	}
	return false
}

// statementEnds reports whether the next token closes a matched statement. In
// the bare file that is end of input, ';', or a line break before a token
// that starts a new statement (not '.', an operator, a bracket or ','). Inside
// a settings list it is the ',' or ')' that ends the argument.
func (s *scan) statementEnds(ctx int32) bool {
	next := s.peek()
	if ctx > 0 {
		return isPunct(next, ",") || (next.kind == kindClose && next.text == ")")
	}
	switch {
	case next.kind == kindEOF, isPunct(next, ";"):
		return true
	case !s.boundary(next):
		return false
	}
	switch next.kind {
	case kindIdent, kindString, kindOpaque, kindOther:
		return true
	default:
		return false
	}
}

// boundary reports whether a line break before tok ends the expression in
// progress: the break is outside '(' and '[' and the last consumed token is
// not an operator that carries the expression onto the next line.
func (s *scan) boundary(tok token) bool {
	return tok.newline && !tok.inParen && s.hist[0].kind != kindSymbol
}

// failStatement is the fail-closed fallback. It marks the rows the statement
// already produced partial, then salvages the rest of the statement from rel
// open groups deep, emitting each literal coordinate partial. A statement
// that yields no coordinate at all is counted once.
func (s *scan) failStatement(start, rel int) {
	for index := start; index < len(s.deps); index++ {
		s.deps[index].partial = true
	}
	s.salvage(rel, true)
	if s.stmtRows == 0 {
		s.unresolved++
	}
}

// markReset records that a `:=`, `~=`, `-=` or `--=` in ctx supersedes every
// row ctx declared so far. A reset outside a grammar context (in a settings
// val, a wrapper, a block, or under an axis) may be applied to any project at
// any point, so it supersedes every row of the file.
func (s *scan) markReset(ctx int32, inContext bool) {
	switch {
	case !inContext:
		s.globalReset = true
	case ctx == 0:
		s.resetMark[ctx] = len(s.deps)
		s.rootReset = true
	default:
		s.resetMark[ctx] = len(s.deps)
	}
}

// addDep stores one coordinate, or counts it past MaxDependencyRows.
func (s *scan) addDep(dep dependency) {
	s.stmtRows++
	if len(s.deps) >= MaxDependencyRows {
		s.overflow++
		return
	}
	s.deps = append(s.deps, dep)
}

// skipGroup consumes through the closer matching an opener already consumed.
func (s *scan) skipGroup() {
	depth := 1
	for depth > 0 {
		tok := s.next()
		switch tok.kind {
		case kindEOF:
			return
		case kindOpen:
			depth++
		case kindClose:
			depth--
		default:
			// Everything else is skipped without evaluation.
		}
	}
}

// initialDepth is the group depth a consumed token opens.
func initialDepth(tok token) int {
	if tok.kind == kindOpen {
		return 1
	}
	return 0
}

// isPunct reports whether tok is the punctuation token text.
func isPunct(tok token, text string) bool {
	return tok.kind == kindPunct && tok.text == text
}
