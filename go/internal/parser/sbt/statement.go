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
// declaration and is left to the main loop. A member called on the key is
// judged by settingMemberOf: one that returns a Setting is applied from any
// position, because the Setting changes the key wherever it is used; one that
// returns no Setting is counted only at the start of an expression and is
// otherwise a read.
func (s *scan) parseLibraryDependencies(key token) {
	prev := s.hist[1]
	ctx, inContext := s.keyContext(key, prev)
	if s.skipLegacyAxis() {
		inContext = false
	}
	op := s.peek()
	if op.kind == kindIdent && isPunct(s.hist[0], ".") && !isKeyRead(op.text) &&
		(settingMemberOf(op.text) != memberValue || startsExpression(key, prev, inContext)) {
		s.applyMember(op.text, ctx, inContext)
		return
	}
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
	case "<<=":
		// sbt 0.13's `<<=` replaces the key with an Initialize.
		s.next()
		s.markReset(ctx, inContext)
		s.fallback()
		return
	default:
		// `<+=` and `<++=` append an Initialize; any other assignment
		// operator is no sbt setting operator. Both take the fallback. A
		// comparison or other operator is not a setting at all.
		if !isAssignmentOperator(op.text) {
			return
		}
		s.next()
		s.fallback()
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
// starts a top-level statement on a line that starts at column 0, or directly
// inside the `.settings(` argument
// list of a project val (the project's context), where the key is the first
// token of an argument. Anything else, such as a key after an axis (`Test /`),
// inside a wrapper (`inConfig(Test)(...)`), a brace block, a val or def body,
// an if body, or an indented line at depth zero (a Scala 3 braceless block),
// is not a grammar context.
func (s *scan) keyContext(key, prev token) (int32, bool) {
	if key.depth == 0 && key.lineAtCol0 && s.startsStatement(key, prev) {
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

// isKeyRead names the members of a setting key that read it rather than
// change it: `libraryDependencies.value` inside a task or setting body, and the
// key objects themselves.
func isKeyRead(member string) bool {
	switch member {
	case "value", "key", "scopedKey", "taskValue":
		return true
	}
	return false
}

// settingMember classifies a member called on the key by what sbt's
// SettingKey API (main-settings Structure.scala) says it returns.
type settingMember int

const (
	// memberValue returns an Initialize or a plain value, never a Setting,
	// so it cannot change the key (`map`, `flatMap`, `get`, `or`, and any
	// member sbt does not define).
	memberValue settingMember = iota
	// memberRewrite returns a Setting that replaces or removes what came
	// before (`set`, `set0`, `transform`, `remove1`, `removeN`).
	memberRewrite
	// memberAppend returns a Setting that adds like `+=` or `++=`
	// (`append1`, `appendN`).
	memberAppend
	// memberKey returns the key itself (`withRank`), so an assignment the
	// scanner does not read may follow.
	memberKey
)

// settingMemberOf classifies member. sbt 1's SettingKey defines append1,
// appendN, remove1, removeN and transform, DefinableSetting defines set, sbt 2
// adds set0, and withRank returns a SettingKey; every other member returns no
// Setting.
func settingMemberOf(member string) settingMember {
	switch member {
	case "set", "set0", "transform", "remove1", "removeN":
		return memberRewrite
	case "append1", "appendN":
		return memberAppend
	case "withRank":
		return memberKey
	}
	return memberValue
}

// applyMember reads a statement whose key is followed by a member call. A
// member that rewrites the key supersedes earlier rows and takes the fallback,
// so a literal coordinate later in the statement (`if (ci) k.set(...) else k
// += c`) is still emitted partial. A member that appends takes the fallback.
// A member that returns the key (`withRank`) is judged by the operator after
// it (applyRankedOperator). A member that returns no Setting cannot change
// the key, so the statement is counted once and supersedes nothing. Out of a
// grammar context every reset is global (markReset).
func (s *scan) applyMember(member string, ctx int32, inContext bool) {
	switch settingMemberOf(member) {
	case memberRewrite:
		s.markReset(ctx, inContext)
		s.fallback()
	case memberAppend:
		s.fallback()
	case memberKey:
		s.next()
		if open := s.peek(); open.kind == kindOpen && open.text == "(" && !open.newline {
			s.next()
			s.skipGroup()
		}
		s.applyRankedOperator(ctx, inContext)
	default:
		s.unresolved++
		s.salvage(0, false)
	}
}

// applyRankedOperator reads the operator after `withRank(...)`, which returns
// the key itself. `:=` and `<<=` replace the key; `~=`, `-=` and `--=`
// rewrite it and their operands are no dependencies, as on the bare key;
// `+=`, `++=`, `<+=`, `<++=` and any other assignment operator append. Every
// form takes the fallback, so no row after `withRank` is resolved. A
// continuation the scanner cannot read supersedes, since any assignment may
// follow it.
func (s *scan) applyRankedOperator(ctx int32, inContext bool) {
	op := s.peek()
	if op.kind != kindSymbol || !isAssignmentOperator(op.text) {
		s.markReset(ctx, inContext)
		s.fallback()
		return
	}
	s.next()
	switch op.text {
	case "~=", "-=", "--=":
		s.unresolved++
		s.markReset(ctx, inContext)
		s.salvage(0, false)
	case ":=", "<<=":
		s.markReset(ctx, inContext)
		s.fallback()
	default:
		s.fallback()
	}
}

// fallback salvages the rest of a statement that adds outside the grammar:
// each literal coordinate is emitted partial, and a statement with none is
// counted once.
func (s *scan) fallback() {
	s.stmtRows = 0
	s.window = coordinateWindow{}
	s.failStatement(len(s.deps), 0)
}

// isAssignmentOperator reports a Scala assignment operator: a symbol that ends
// in '=' and is not '=', a comparison (`==`, `<=`, `>=`, `!=`), or another
// '='-led symbol (`=>`, `===`).
func isAssignmentOperator(op string) bool {
	if len(op) < 2 || op[len(op)-1] != '=' || op[0] == '=' {
		return false
	}
	switch op {
	case "<=", ">=", "!=":
		return false
	}
	return true
}

// startsExpression reports whether key sits where a member that returns no
// Setting is counted: in a grammar context, at the start of a line, or right
// after an opener, ',' or ';'. Such a member in the middle of an expression
// (`x := libraryDependencies.map(f)`) is a read. A member that returns a
// Setting is applied from any position (parseLibraryDependencies).
func startsExpression(key, prev token, inContext bool) bool {
	switch {
	case inContext, key.newline, prev.kind == kindEOF, prev.kind == kindOpen:
		return true
	}
	return isPunct(prev, ",") || isPunct(prev, ";")
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

// markReset records that a `:=`, `~=`, `-=`, `--=`, `<<=`, a rewriting member
// or `withRank` in ctx supersedes every row ctx declared so far. A reset
// outside a grammar context (in a settings val, a wrapper, a block, or under
// an axis) may be applied to any project at any point, so it supersedes every
// row of the file.
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
