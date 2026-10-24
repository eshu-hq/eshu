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
// scala_version, scala_version_count and `%%` hint.

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
	primary    string // first ThisBuild literal, else first literal
	stray      int    // nested scalaVersion literals, each counted once
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

// fzStrayScala are scalaVersion assignments outside a grammar context; %s is
// the literal.
var fzStrayScala = []string{
	"inConfig(Test)(scalaVersion := \"%s\")", "foo(scalaVersion := \"%s\")",
	"x.scalaVersion := \"%s\"", "lazy val common = Seq(scalaVersion := \"%s\")",
	"Test / scalaVersion := \"%s\"", "scalaVersion in Test := \"%s\"",
	"inThisBuild(List(scalaVersion := \"%s\"))", "foo(ThisBuild / scalaVersion := \"%s\")",
	"locally {\n  scalaVersion := \"%s\"\n}", "if isCI then\n  scalaVersion := \"%s\"",
}

// fzTopScala are scalaVersion assignments in a grammar context.
var fzTopScala = []string{
	"scalaVersion := \"%s\"", "ThisBuild / scalaVersion := \"%s\"",
	"scalaVersion in ThisBuild := \"%s\"",
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
	thisBuild := false
	stmts := 2 + r.Intn(6)
	for i := 0; i < stmts; i++ {
		switch r.Intn(10) {
		case 0, 1, 2:
			text, _ := coord(false)
			out.WriteString("libraryDependencies += " + text + "\n")
		case 3:
			read := fzMemberReads[r.Intn(len(fzMemberReads))]
			forms := []string{"libraryDependencies.%s", "check := (libraryDependencies.%s).value", "lazy val common = Seq(libraryDependencies.%s)"}
			fmt.Fprintf(&out, forms[r.Intn(len(forms))]+"\n", read)
			c.unresolved++
		case 4:
			fmt.Fprintf(&out, "libraryDependencies.%s\n", fzMemberRewrites[r.Intn(len(fzMemberRewrites))])
			c.unresolved++
			supersede()
		case 5:
			supersede()
			if r.Intn(2) == 0 {
				out.WriteString("libraryDependencies.withRank(KeyRanks.Invisible) := Nil\n")
				c.unresolved++
			} else {
				fallback("libraryDependencies.withRank(KeyRanks.Invisible) ++= Seq(%s)", true)
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
			fmt.Fprintf(&out, fzStrayScala[r.Intn(len(fzStrayScala))]+"\n", version)
			c.stray++
		case 9:
			versions := []string{"2.13.12", "2.13.12", "3.3.1", "2.12.18"}
			version := versions[r.Intn(len(versions))]
			form := r.Intn(len(fzTopScala))
			fmt.Fprintf(&out, fzTopScala[form]+"\n", version)
			if (form > 0 && !thisBuild) || len(c.versions) == 0 {
				c.primary = version
			}
			thisBuild = thisBuild || form > 0
			c.versions = append(c.versions, version)
		}
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
	binary := ""
	if len(distinct) == 1 {
		binary = scalaBinaryVersion(c.versions[0])
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
	if state["scala_version_count"] != len(distinct) || state["scala_version"] != c.primary {
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
