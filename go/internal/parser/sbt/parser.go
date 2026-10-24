// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"path"
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

	// MaxConfigBytes is the longest configuration spelling read as one
	// dependency configuration. Real configurations are a few bytes
	// (`test`, `provided`) and the longest mapping in common use,
	// `compile->compile;test->test;runtime->runtime`, is under 50, so 256 leaves
	// wide margin. A longer string is not split or stored: the row is partial.
	MaxConfigBytes = 256

	// maxStringVals caps the top-level `val name = "literal"` table used to
	// resolve version references.
	maxStringVals = 1024

	// maxScalaVersions caps the distinct scalaVersion literals tracked; one
	// more poisons the envelope.
	maxScalaVersions = 64
)

// Parse reads one build.sbt and returns the parent parser payload with one
// content_entity-shaped "variables" row per literal libraryDependencies
// coordinate, plus an sbt_state envelope that carries the Scala version and
// the bounded counts of what the scanner could not resolve.
//
// Parse never evaluates sbt or Scala. It reads only file bytes. A row is
// resolved only when its whole statement matches the closed grammar in
// README.md; any other statement yields partial rows that are counted.
// repoRoot is the repository root path is read under; the meta-build flag is
// judged from path relative to it.
func Parse(repoRoot, path string, isDependency bool, options shared.Options) (map[string]any, error) {
	source, err := shared.ReadSource(path)
	if err != nil {
		return nil, err
	}
	text := string(source)

	payload := shared.BasePayload(path, LanguageName, isDependency)
	scan := newScan(text)
	scan.metaBuild = isMetaBuildPath(repoRoot, path)
	scan.run()
	rows, state := scan.finish()
	payload["variables"] = rows
	payload["sbt_state"] = state

	if options.IndexSource {
		payload["source"] = text
	}
	return payload, nil
}

// scan holds the single-pass state for one build.sbt.
type scan struct {
	lx      *lexer
	peeked  token
	hasPeek bool
	// consumed counts tokens handed out by next.
	consumed int
	// hist holds the last three consumed tokens, newest first. unread drops
	// the newest, so hist[2] may be empty right after an unread.
	hist [3]token

	deps []dependency
	vals map[string]string
	// scalaVersions holds the distinct scalaVersion literals recorded in a
	// grammar context, in source order.
	scalaVersions []string
	// scalaPoisoned is set by a scalaVersion statement in a grammar context
	// that the scanner cannot read as a literal; the envelope then reports
	// no version and no row gets a `%%` hint.
	scalaPoisoned bool

	// unresolved counts declarations the scanner recognised but could not
	// read as literals and that produced no row.
	unresolved int
	// overflow counts valid declarations dropped past MaxDependencyRows.
	overflow int
	// stmtRows counts the coordinates the current statement produced,
	// including any dropped past MaxDependencyRows.
	stmtRows int
	// window is the coordinate prefix the grammar consumed before an element
	// stopped matching; salvage continues from it.
	window coordinateWindow

	// projectCount numbers the project vals in source order; a project's
	// settings context is its number. The bare file is context 0.
	projectCount int32
	// openProject is the project whose `val x = project...` statement is
	// still open, or 0.
	openProject int32
	// settingsCtx is the project whose `.settings(` argument list is open,
	// or 0; settingsDepth is the bracket depth inside that list.
	settingsCtx   int32
	settingsDepth int
	// resetMark records, per context, the dependency count when its last
	// `:=`, `~=`, `-=` or `--=` ran; every earlier row of that context is
	// superseded. rootReset is set when the bare file reset the key.
	resetMark map[int32]int
	rootReset bool
	// globalReset is set by a reset outside a grammar context; it supersedes
	// every row of the file.
	globalReset bool
	// thisBuildAt is the consumed count of the last `ThisBuild` token that
	// starts a statement in a grammar context, or 0.
	thisBuildAt int
	// globalAt is the same for the last `Global` token, which may head a
	// `Global / excludeLintKeys` key-list statement.
	globalAt int
	// metaBuild is set for a build.sbt in a `project` directory: sbt's
	// meta-build, whose dependencies are the build definition's classpath.
	metaBuild bool
	// modifierAt is the consumed count of the last modifier (`lazy`,
	// `private`, ...) that heads a top-level statement, or 0.
	modifierAt int
	// watch follows key tokens consumed outside the main loop's claim
	// (keyWatch); watchBefore is its state before the newest token, so unread
	// restores it. claiming is set while the main loop consumes a token.
	watch, watchBefore keyWatch
	claiming           bool
	// cover is the key whose verdict the statement being consumed has
	// already applied: watchLib after a reset that supersedes every row,
	// watchScala after a poison. The main loop clears it per token.
	cover uint8
}

func newScan(text string) *scan {
	return &scan{lx: newLexer(text), vals: map[string]string{}, resetMark: map[int32]int{}}
}

// next returns the next token, honouring a single pushed-back token.
func (s *scan) next() token {
	s.consumed++
	var tok token
	if s.hasPeek {
		s.hasPeek = false
		tok = s.peeked
	} else {
		tok = s.lx.next()
	}
	s.hist[2], s.hist[1], s.hist[0] = s.hist[1], s.hist[0], tok
	s.watchBefore = s.watch
	s.watch.observe(tok, s.claiming, s.cover)
	return tok
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
	s.hist[0], s.hist[1], s.hist[2] = s.hist[1], s.hist[2], token{}
	s.watch = s.watchBefore
}

// run walks the token stream once and dispatches on the three key names the
// parser reads. It claims every token it consumes, so a key token reaches
// keyWatch unclaimed only when a statement handler consumed it.
func (s *scan) run() {
	for {
		s.cover = watchNone
		s.claiming = true
		tok := s.next()
		s.claiming = false
		if tok.kind == kindEOF {
			return
		}
		s.trackProject(tok)
		if tok.kind != kindIdent {
			continue
		}
		head := s.headsStatement(tok)
		switch tok.text {
		case "libraryDependencies":
			s.parseLibraryDependencies(tok)
		case "scalaVersion":
			s.parseScalaVersion(s.scalaVersionContext(tok))
		case "ThisBuild":
			s.thisBuildAt = 0
			if _, ok := s.keyContext(tok, s.hist[1]); ok {
				s.thisBuildAt = s.consumed
			}
		case "Global":
			s.globalAt = 0
			if _, ok := s.keyContext(tok, s.hist[1]); ok {
				s.globalAt = s.consumed
			}
		case "val":
			if head {
				s.parseVal()
			}
		default:
			switch {
			case isKeyListSetting(tok.text):
				if s.keyListHead(tok) {
					s.parseKeyList()
				}
			case head && isModifier(tok.text):
				s.modifierAt = s.consumed
			}
		}
	}
}

// headsStatement reports whether tok heads a top-level statement: it sits at
// depth zero on a line that starts at column 0, and it is the first token of
// the line or the file, follows ';', or follows a modifier that heads one. An
// indented line at depth zero is a Scala 3 braceless block (a val or def body,
// an if, match, for or while body), so nothing on it is top-level.
func (s *scan) headsStatement(tok token) bool {
	if tok.depth != 0 || !tok.lineAtCol0 {
		return false
	}
	switch {
	case tok.newline, s.consumed == 1, isPunct(s.hist[1], ";"):
		return true
	}
	return s.modifierAt != 0 && s.modifierAt == s.consumed-1
}

// isModifier names the definition modifiers that may precede a top-level val.
func isModifier(word string) bool {
	switch word {
	case "lazy", "private", "protected", "implicit", "final", "override", "inline":
		return true
	}
	return false
}

// trackProject follows the open project statement and its `.settings(`
// argument list. A project statement ends at a depth-zero line break that is
// not a leading '.', or at ';'. The settings list is the '(' after
// `.settings` at depth zero of an open project statement, and it ends when a
// token falls back below its depth.
func (s *scan) trackProject(tok token) {
	if s.settingsCtx != 0 && tok.depth < s.settingsDepth {
		s.settingsCtx = 0
	}
	if s.openProject != 0 && tok.depth == 0 && tok.kind != kindClose &&
		((tok.newline && !isPunct(tok, ".")) || isPunct(tok, ";")) {
		s.openProject = 0
	}
	if s.openProject != 0 && tok.kind == kindOpen && tok.text == "(" && tok.depth == 0 &&
		s.hist[1].kind == kindIdent && s.hist[1].text == "settings" && isPunct(s.hist[2], ".") {
		s.settingsCtx, s.settingsDepth = s.openProject, 1
	}
}

// parseVal records a top-level `val name[: Type] = "literal"` so later
// version references can resolve. A val whose literal is followed by more of
// the expression (`"1" + suffix`, `"1".trim`, or a next line that starts with
// '.' or an operator) is not that literal and is ignored. A val initialised
// with `project`, `crossProject`, `Project`, `projectMatrix` or
// `(project in ...)` opens a project statement for the settings context.
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
	switch {
	case isProjectHead(value):
		s.openProjectStatement()
		return
	case value.kind == kindOpen && value.text == "(":
		if isProjectHead(s.peek()) {
			s.openProjectStatement()
		}
		return
	case value.kind != kindString || !validField(value.text):
		s.unread(value)
		return
	}
	if !endsLiteral(s.peek()) {
		return
	}
	if _, exists := s.vals[name.text]; exists || len(s.vals) >= maxStringVals {
		return
	}
	s.vals[name.text] = value.text
}

// endsLiteral reports whether next ends the expression of a literal before
// it: end of input, ';', ',', a closer, or a line break followed by a token
// that cannot continue the expression (not '.', an operator, or an opener).
func endsLiteral(next token) bool {
	switch {
	case next.kind == kindEOF, next.kind == kindClose, isPunct(next, ";"), isPunct(next, ","):
		return true
	case !next.newline:
		return false
	}
	return !isPunct(next, ".") && next.kind != kindSymbol && next.kind != kindOpen
}

// openProjectStatement numbers a new project val and marks its statement open.
func (s *scan) openProjectStatement() {
	s.projectCount++
	s.openProject = s.projectCount
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

// finish resolves deferred version references, applies the superseding
// assignments, builds the rows, and returns the sbt_state envelope. Every row
// of a meta-build file is partial: its scope is the build definition's
// classpath, not the project's.
func (s *scan) finish() ([]map[string]any, map[string]any) {
	s.applyKeyWatch()
	s.applyResets()
	if s.metaBuild {
		for index := range s.deps {
			s.deps[index].partial = true
		}
	}
	version := s.scalaVersion()
	rows := make([]map[string]any, 0, len(s.deps))
	binary := ""
	if version != "" {
		binary = scalaBinaryVersion(version)
	}
	partial := 0
	for _, dep := range s.deps {
		if dep.scopeUndetermined() {
			partial++
		}
		rows = append(rows, dep.row(s.vals, binary))
	}
	sort.SliceStable(rows, func(i, j int) bool {
		left, _ := rows[i]["name"].(string)
		right, _ := rows[j]["name"].(string)
		return left < right
	})

	state := map[string]any{
		"state":                        "parsed",
		"scala_version":                version,
		"scala_version_count":          len(s.scalaVersions),
		"dependency_count":             len(rows),
		"dependency_overflow_count":    s.overflow,
		"unresolved_declaration_count": s.unresolved + partial,
		"meta_build":                   s.metaBuild,
	}
	return rows, state
}

// applyResets marks every row a later `:=`, `~=`, `-=` or `--=` of the same
// context superseded. A reset in the bare file applies to the root project,
// which may be any project the file declares, so it supersedes every project
// row too, and a reset outside a grammar context supersedes every row. One
// pass, whatever the number of resets.
func (s *scan) applyResets() {
	for index := range s.deps {
		dep := &s.deps[index]
		if dep.ctx < 0 {
			continue
		}
		if mark, ok := s.resetMark[dep.ctx]; ok && index < mark {
			dep.partial = true
		}
		if (s.rootReset && dep.ctx > 0) || s.globalReset {
			dep.partial = true
		}
	}
}

// isMetaBuildPath reports whether file is a build.sbt directly inside a
// directory named `project`, judged on its path relative to repoRoot so the
// directory a repository is cloned into never counts (#8997). sbt reads
// `<root>/project/*.sbt` as the meta-build that configures the build itself;
// a nested `project/build.sbt` is flagged too, fail-closed. Both paths are
// read with '/' and '\' as separators. A file outside repoRoot, or an empty
// repoRoot, is judged on its full path, which can only flag more.
func isMetaBuildPath(repoRoot, file string) bool {
	rel := strings.ReplaceAll(file, `\`, "/")
	if root := strings.TrimRight(strings.ReplaceAll(repoRoot, `\`, "/"), "/"); root != "" {
		if inside, ok := strings.CutPrefix(rel, root+"/"); ok {
			rel = inside
		}
	}
	dir := path.Dir(rel)
	return dir != "." && dir != "/" && path.Base(dir) == "project"
}

// validField reports whether text can be a coordinate field.
func validField(text string) bool {
	if text == "" || len(text) > MaxFieldBytes {
		return false
	}
	return !strings.ContainsAny(text, " \t\r\n:")
}
