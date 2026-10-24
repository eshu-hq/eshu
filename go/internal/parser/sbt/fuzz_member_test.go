// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

// This file holds the member arm of the grammar fuzz. It interleaves resolved
// column-0 statements with dotted member calls on the key, sbt 0.13 and
// unknown assignment operators, and scalaVersion literals at the top level
// and nested where no grammar context applies. The generator knows from sbt's
// SettingKey API which statements supersede earlier rows, which emit partial
// rows, and which scalaVersion literals count, so the oracle checks the exact
// state of every row, the exact unresolved_declaration_count, and the exact
// scala_version, scala_version_count and `%%` hint. Rewriting members are
// placed at statement start and anywhere in an expression, `withRank(...)` is
// followed by every operator class, and scalaVersion statements that the
// scanner cannot read as a literal, or that assign outside a grammar context,
// poison the envelope in any order.

import (
	"fmt"
	"math/rand"
	"strings"
	"testing"
)

// fzMemberRow is one coordinate the member arm wrote.
type fzMemberRow struct {
	name     string
	binary   bool // %% coordinate
	partial  bool // written in a fallback statement
	resolved bool // written in a grammar statement; false once superseded
}

// fzMemberCase is one generated file and what the oracle expects of it.
type fzMemberCase struct {
	text       string
	rows       []*fzMemberRow
	unresolved int // counted statements that emit no row
	versions   []string
	poisoned   bool // an unreadable or out-of-context scalaVersion assignment
	stray      int  // out-of-context scalaVersion statements, each counted once
}

// fzMemberReads are members that return no Setting.
var fzMemberReads = []string{
	"map(_.withSources())", "filter(_.name != \"x\")", "flatMap(x => x)",
	"distinct", "previous", "zipWith(scalaVersion)((a, b) => a)", "foo(1)",
	"get(settings)", "or(Def.setting(Nil))",
}

// fzMemberRewrites are members that return a Setting replacing or removing
// what came before.
var fzMemberRewrites = []string{
	"transform(_.map(_.withSources()), pos)", "set(Def.setting(Nil), pos)",
	"set0(Def.setting(Nil), pos)", "remove1(Def.setting(x), pos)",
	"removeN(Def.setting(xs), pos)",
}

// fzStrayScala are scalaVersion assignments outside a grammar context, plus
// the dotted `.:=(...)` call; %s is the literal. Each is counted once and
// poisons the envelope.
var fzStrayScala = []string{
	"inConfig(Test)(scalaVersion := \"%s\")", "foo(scalaVersion := \"%s\")",
	"x.scalaVersion := \"%s\"", "lazy val common = Seq(scalaVersion := \"%s\")",
	"Test / scalaVersion := \"%s\"", "scalaVersion in Test := \"%s\"",
	"inThisBuild(List(scalaVersion := \"%s\"))", "foo(ThisBuild / scalaVersion := \"%s\")",
	"locally {\n  scalaVersion := \"%s\"\n}", "if isCI then\n  scalaVersion := \"%s\"",
	"foo(scalaVersion ~= (_ => \"%s\"))", "lazy val s = scalaVersion.set(Def.setting(\"%s\"), pos)",
	"Test / scalaVersion ~= (_ => \"%s\")", "x.scalaVersion <<= Def.setting(\"%s\")",
	"inConfig(Test)(scalaVersion.withRank(r) := \"%s\")",
	"scalaVersion.:=(\"%s\")", "lazy val s = scalaVersion.:=(\"%s\")",
	"scalaVersion in (Test, compile) := \"%s\"", "foo(scalaVersion.in(Test) := \"%s\")",
	"scalaVersion.in(Test).withRank(r) := \"%s\"", "def common = Seq(scalaVersion := \"%s\")",
	"lazy val s = Seq(\"%s\").map(v => scalaVersion := v)",
	"lazy val common: Seq[Setting[_]] = Seq(organization := \"x\", scalaVersion := \"%s\")",
}

// fzReadScala are reads of the scalaVersion key; %s is a literal that is no
// version. They are no statement: not counted, and they change nothing.
var fzReadScala = []string{
	"crossScalaVersions := Seq(scalaVersion.value, \"%s\")",
	"crossScalaVersions := Seq(scalaVersion.in(Test).value, \"%s\")",
	"check := ((scalaVersion in Test).value, \"%s\")",
}

// fzPoisonScala are scalaVersion statements in a grammar context that the
// scanner cannot read as a literal; each is counted and poisons the envelope.
var fzPoisonScala = []string{
	"scalaVersion := sys.env.getOrElse(\"SV\", \"%s\")", "scalaVersion := \"%s\".trim",
	"scalaVersion ~= (_ => \"%s\")", "scalaVersion <<= Def.setting(\"%s\")",
	"scalaVersion <+= Def.setting(\"%s\")", "scalaVersion.set(Def.setting(\"%s\"), pos)",
	"scalaVersion.transform(_ => \"%s\", pos)", "scalaVersion.withRank(KeyRanks.Invisible) := \"%s\"",
	"scalaVersion.in(ThisBuild) := \"%s\"", "ThisBuild / scalaVersion ~= (_ => \"%s\")",
	"scalaVersion in ThisBuild ~= (_ => \"%s\")", "ThisBuild / scalaVersion.set(Def.setting(\"%s\"), pos)",
	"lazy val core = project.settings(scalaVersion ~= (_ => \"%s\"))",
}

// fzRewritePositions place a rewriting member call outside statement
// position, where it still changes the key; %s is the member call.
var fzRewritePositions = []string{
	"libraryDependencies.%s", "lazy val s = libraryDependencies.%s",
	"lazy val s: Setting[_] = libraryDependencies.%s", "commonSettings :+ libraryDependencies.%s",
	"if (ci) libraryDependencies.%s else Nil", "check := { deps => libraryDependencies.%s }",
	"lazy val common = Seq(foo(1), libraryDependencies.%s)", "lazy val all = base ++ libraryDependencies.%s",
	"libraryDependencies.in(Test).%s",
}

// fzRankedOps are the operators after `withRank(...)`; supersede marks the
// ones that replace or rewrite the key, and coord the ones whose operand is a
// coordinate emitted partial.
var fzRankedOps = []struct {
	format           string
	supersede, coord bool
}{
	{":= Nil", true, false},
	{"~= identity", true, false},
	{"-= otherDeps", true, false},
	{"--= otherDeps", true, false},
	{"<<= otherDeps", true, false},
	{".foo(1)", true, false},
	{"+= %s", false, true},
	{"++= Seq(%s)", false, true},
	{"<+= Def.setting(%s)", false, true},
	{"<++= Def.setting(Seq(%s))", false, true},
	{":+= %s", false, true},
}

// fzTopScala are scalaVersion assignments in a grammar context.
var fzTopScala = []string{
	"scalaVersion := \"%s\"", "ThisBuild / scalaVersion := \"%s\"",
	"scalaVersion in ThisBuild := \"%s\"", "lazy val core = project.settings(scalaVersion := \"%s\")",
}

// fzMemberBuild generates the member-arm file for one seed.
func fzMemberBuild(seed int64) fzMemberCase {
	r := rand.New(rand.NewSource(seed))
	var c fzMemberCase
	var out strings.Builder
	n := 0
	coord := func(partial bool) (string, *fzMemberRow) {
		n++
		row := &fzMemberRow{name: fmt.Sprintf("g%d.org:a%d", n, n), binary: r.Intn(2) == 0, partial: partial, resolved: !partial}
		op := "%"
		if row.binary {
			op = "%%"
		}
		c.rows = append(c.rows, row)
		return fmt.Sprintf("\"g%d.org\" %s \"a%d\" %% \"1.%d\"", n, op, n, n), row
	}
	supersede := func() {
		for _, row := range c.rows {
			row.resolved = false
		}
	}
	// global is set by a reset outside a grammar context, which may be
	// applied anywhere and so supersedes every row, later ones included.
	global := false
	// fallback writes a statement whose coordinate, if any, is partial and
	// counted once; a statement without one is counted once.
	fallback := func(format string, withCoord bool) {
		if !withCoord {
			fmt.Fprintf(&out, format+"\n", "otherDeps")
			c.unresolved++
			return
		}
		text, _ := coord(true)
		fmt.Fprintf(&out, format+"\n", text)
	}
	stmts := 2 + r.Intn(6)
	for i := 0; i < stmts; i++ {
		switch r.Intn(10) {
		case 0, 1, 2:
			text, _ := coord(false)
			out.WriteString("libraryDependencies += " + text + "\n")
		case 3:
			read := fzMemberReads[r.Intn(len(fzMemberReads))]
			forms := []string{"libraryDependencies.%s", "check := (libraryDependencies.%s).value", "lazy val common = Seq(libraryDependencies.%s)", "libraryDependencies.in(Test).%s"}
			if r.Intn(3) == 0 {
				// In the middle of an expression a read is not a statement.
				mid := []string{"check := libraryDependencies.%s", "lazy val s = base ++ libraryDependencies.%s"}
				fmt.Fprintf(&out, mid[r.Intn(len(mid))]+"\n", read)
				break
			}
			fmt.Fprintf(&out, forms[r.Intn(len(forms))]+"\n", read)
			c.unresolved++
		case 4:
			call := fzMemberRewrites[r.Intn(len(fzMemberRewrites))]
			supersede()
			if r.Intn(4) == 0 {
				// A coordinate later in the statement is emitted partial.
				text, _ := coord(true)
				fmt.Fprintf(&out, "if (ci) libraryDependencies.%s else libraryDependencies += %s\n", call, text)
				global = true
				break
			}
			position := r.Intn(len(fzRewritePositions))
			fmt.Fprintf(&out, fzRewritePositions[position]+"\n", call)
			global = global || position > 0
			c.unresolved++
		case 5:
			prefix := []string{"", "lazy val s = "}[r.Intn(2)]
			op := fzRankedOps[r.Intn(len(fzRankedOps))]
			if op.supersede {
				supersede()
				global = global || prefix != ""
			}
			format := prefix + "libraryDependencies.withRank(KeyRanks.Invisible) " + op.format
			if op.format == ".foo(1)" {
				format = prefix + "libraryDependencies.withRank(KeyRanks.Invisible).foo(1)"
			}
			if op.coord {
				fallback(format, true)
			} else {
				out.WriteString(format + "\n")
				c.unresolved++
			}
		case 6:
			forms := []string{"libraryDependencies.append1(Def.setting(%s), pos)", "libraryDependencies.appendN(Def.setting(Seq(%s)), pos)"}
			fallback(forms[r.Intn(len(forms))], true)
		case 7:
			ops := []string{"<<=", "<+=", "<++=", ":+=", "|="}
			op := ops[r.Intn(len(ops))]
			if op == "<<=" {
				supersede()
			}
			fallback("libraryDependencies "+op+" Def.setting(Seq(%s))", r.Intn(3) != 0)
		case 8:
			version := fmt.Sprintf("2.11.%d", r.Intn(4))
			if r.Intn(4) == 0 {
				fmt.Fprintf(&out, fzReadScala[r.Intn(len(fzReadScala))]+"\n", version)
				break
			}
			fmt.Fprintf(&out, fzStrayScala[r.Intn(len(fzStrayScala))]+"\n", version)
			c.stray++
			c.poisoned = true
		case 9:
			versions := []string{"2.13.12", "2.13.12", "3.3.1", "2.12.18"}
			version := versions[r.Intn(len(versions))]
			if r.Intn(3) == 0 {
				fmt.Fprintf(&out, fzPoisonScala[r.Intn(len(fzPoisonScala))]+"\n", version)
				c.poisoned = true
				c.unresolved++
				break
			}
			fmt.Fprintf(&out, fzTopScala[r.Intn(len(fzTopScala))]+"\n", version)
			c.versions = append(c.versions, version)
		}
	}
	if global {
		supersede()
	}
	c.text = out.String()
	return c
}

// memberReport tallies member-arm violations across seeds.
type memberReport struct {
	cases, rows                      int
	state, extra, count, scala, hint int
	shown                            int
}

// check parses one member-arm case and records its violations.
func (r *memberReport) check(t *testing.T, seed int64, c fzMemberCase) {
	t.Helper()
	payload := parseSource(t, c.text)
	rows := dependencyRows(t, payload)
	state := stateOf(t, payload)
	r.rows += len(rows)
	byName := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		byName[row["name"].(string)] = row
	}
	distinct := map[string]bool{}
	for _, version := range c.versions {
		distinct[version] = true
	}
	// The envelope names a version only when exactly one distinct literal
	// was recorded and nothing poisoned it.
	primary, binary := "", ""
	if len(distinct) == 1 && !c.poisoned {
		primary = c.versions[0]
		binary = scalaBinaryVersion(primary)
	}
	want := c.unresolved + c.stray
	for _, expect := range c.rows {
		row, ok := byName[expect.name]
		if !ok {
			r.state++
			r.log(t, "missing", seed, expect.name, c.text)
			continue
		}
		delete(byName, expect.name)
		wantState := "partial"
		if expect.resolved {
			wantState = "resolved"
		} else {
			want++
		}
		if row["dependency_resolution_state"] != wantState {
			r.state++
			r.log(t, fmt.Sprintf("state=%v want %s", row["dependency_resolution_state"], wantState), seed, expect.name, c.text)
		}
		hint, has := row["sbt_binary_artifact"]
		wantHint := expect.binary && binary != ""
		if has != wantHint || (has && hint != expect.name+"_"+binary) {
			r.hint++
			r.log(t, fmt.Sprintf("hint=%v", hint), seed, expect.name, c.text)
		}
	}
	r.extra += len(byName)
	if got := state["unresolved_declaration_count"]; got != want {
		r.count++
		r.log(t, fmt.Sprintf("unresolved=%v want %d", got, want), seed, "", c.text)
	}
	if state["scala_version_count"] != len(distinct) || state["scala_version"] != primary {
		r.scala++
		r.log(t, fmt.Sprintf("scala_version=%v count=%v want %d distinct", state["scala_version"], state["scala_version_count"], len(distinct)), seed, "", c.text)
	}
}

// failures is the total of every member-arm violation.
func (r *memberReport) failures() int {
	return r.state + r.extra + r.count + r.scala + r.hint
}

// log prints the first few violations with their source.
func (r *memberReport) log(t *testing.T, kind string, seed int64, name, text string) {
	t.Helper()
	if r.shown >= 5 {
		return
	}
	r.shown++
	t.Logf("member %s seed=%d dep=%s\n%s", kind, seed, name, text)
}
