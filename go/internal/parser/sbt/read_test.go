// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// keyReadExprs are reads of either key in every shape the statement handlers
// treat as a read: a dotted read member, closers, a prefix axis, legacy `in`
// chains, `in (...)` groups and `.in(...)` calls before the member.
var keyReadExprs = []string{
	"scalaVersion.value",
	"(ThisBuild / scalaVersion).value",
	"(Compile / scalaVersion).value",
	"((Compile / scalaVersion)).value",
	"(scalaVersion in Test).value",
	"(scalaVersion in Compile in Test).value",
	"(scalaVersion in (Test, compile)).value",
	"scalaVersion.in(Test).value",
	"scalaVersion.in(Test).in(Compile).value",
	"(scalaVersion.in(Test)).value",
	"scalaVersion in (Test).value",
	"(libraryDependencies in Test).value",
	"(Test / libraryDependencies).value",
	"libraryDependencies.in(Test).value",
	"libraryDependencies.taskValue",
}

// keyWriteExprs are axis and closer shapes of either key that do not end in a
// read; inside a consumed statement each is a rewrite. lib and scala say
// which key the shape writes.
var keyWriteExprs = []struct {
	text       string
	lib, scala int
}{
	{"(Compile / scalaVersion) := \"2.12.18\"", 0, 1},
	{"(scalaVersion in Test) := \"2.12.18\"", 0, 1},
	{"scalaVersion in Test := \"2.12.18\"", 0, 1},
	{"scalaVersion.in(Test) := \"2.12.18\"", 0, 1},
	{"(scalaVersion in Test).in(Compile).value", 0, 1},
	{"scalaVersion.in(Test).map(identity)", 0, 1},
	{"scalaVersion.in Test).value", 0, 1},
	{"scalaVersion.in(scalaVersion).value", 0, 1},
	{"(Compile / libraryDependencies) := Nil", 1, 0},
	{"(libraryDependencies in Test) := Nil", 1, 0},
	{"libraryDependencies.in(Test) := Nil", 1, 0},
	{"(libraryDependencies in Test).map(identity)", 1, 0},
}

// fzConsumedPositions place a key expression (%s) inside a fallback
// statement with no literal coordinate, which is counted once on its own.
var fzConsumedPositions = []string{
	"libraryDependencies <+= Def.setting(other %% %s)",
	"libraryDependencies <+= Def.setting(Seq(%s))",
	"libraryDependencies <+= Def.setting(if (%s.isEmpty) Nil else Nil)",
	"libraryDependencies <+= Def.setting(xs.map(_ => %s))",
	"libraryDependencies <+= Def.setting { val v = %s; v }",
}

// fzConsumedKey draws one read or write of a key inside a consumed
// statement for the member arm: the text and the key occurrences it writes.
func fzConsumedKey(pick func(int) int) (text string, lib, scala int) {
	position := fzConsumedPositions[pick(len(fzConsumedPositions))]
	if pick(3) == 0 {
		write := keyWriteExprs[pick(len(keyWriteExprs))]
		return sprintfOne(position, write.text), write.lib, write.scala
	}
	return sprintfOne(position, keyReadExprs[pick(len(keyReadExprs))]), 0, 0
}

// TestKeyReadInsideConsumedStatementChangesNothing pins that a read of either
// key inside a statement the grammar, salvage or a skipped group consumes is
// a read in the watch too: it keeps the literal scalaVersion, the `%%` hint
// and every resolved row, wherever the statement sits.
func TestKeyReadInsideConsumedStatementChangesNothing(t *testing.T) {
	t.Parallel()

	wrappers := []string{
		`libraryDependencies += "org.scala-lang" % "scala-reflect" % %s`,
		`libraryDependencies ++= Seq("org.scala-lang" % "scala-reflect" % %s)`,
		`lazy val core = project.settings(libraryDependencies += "org.scala-lang" % "scala-reflect" % %s)`,
		`libraryDependencies ++= (if (%s.toString.startsWith("2.12")) Seq("a" % "b" % "1") else Nil)`,
		`libraryDependencies ++= Seq(1).map(_ => "a" % "b" % %s.toString)`,
		`libraryDependencies <+= Def.setting(other %% %s)`,
		`check := { val x = %s; x }`,
	}
	for _, wrapper := range wrappers {
		for _, read := range keyReadExprs {
			line := sprintfOne(wrapper, read)
			for _, source := range []string{
				"scalaVersion := \"2.13.12\"\n" + denyCats + "\n" + line + "\n",
				line + "\nscalaVersion := \"2.13.12\"\n" + denyCats + "\n",
			} {
				payload := parseSource(t, source)
				state := stateOf(t, payload)
				if got := state["scala_version"]; got != "2.13.12" {
					t.Errorf("%q: scala_version = %v, want 2.13.12", source, got)
				}
				row := requireRow(t, payload, "org.typelevel:cats-core")
				if got := row["dependency_resolution_state"]; got != "resolved" {
					t.Errorf("%q: cats-core state = %v, want resolved", source, got)
				}
				if got := row["sbt_binary_artifact"]; got != "org.typelevel:cats-core_2.13" {
					t.Errorf("%q: sbt_binary_artifact = %v, want org.typelevel:cats-core_2.13", source, got)
				}
			}
		}
	}
}

// TestKeyWriteInsideConsumedStatementIsDenied pins the other side: an axis or
// closer shape that ends in an assignment or a non-read member inside a
// consumed statement is still a rewrite.
func TestKeyWriteInsideConsumedStatementIsDenied(t *testing.T) {
	t.Parallel()

	for _, write := range []string{
		"(Compile / scalaVersion) := \"2.12.18\"",
		"(scalaVersion in Test) := \"2.12.18\"",
		"scalaVersion in Test := \"2.12.18\"",
		"scalaVersion.in(Test) := \"2.12.18\"",
		"(scalaVersion in Test).in(Compile).value",
		"(scalaVersion in Test).map(identity)",
		"scalaVersion in (Test) := \"2.12.18\"",
		"scalaVersion.in(Test).map(identity)",
		"(scalaVersion in Test)",
		"scalaVersion.in Test).value",
		"scalaVersion.in(scalaVersion).value",
	} {
		source := "scalaVersion := \"2.13.12\"\n" + denyCats + "\nlibraryDependencies <+= Def.setting(" + write + ")\n"
		requireStateField(t, stateOf(t, parseSource(t, source)), "scala_version", "")
	}
	for _, write := range []string{
		"(Compile / libraryDependencies) := Nil",
		"(libraryDependencies in Test) := Nil",
		"libraryDependencies.in(Test) := Nil",
		"(libraryDependencies in Test).map(identity)",
		"libraryDependencies in Test",
	} {
		source := denyCats + "\nlibraryDependencies <+= Def.setting(" + write + ")\n"
		requireField(t, requireRow(t, parseSource(t, source), "org.typelevel:cats-core"), "dependency_resolution_state", "partial")
	}
}

// TestScalaVersionStatementCountsOnce pins the count of one unreadable
// scalaVersion statement: an inner read is no second occurrence, and an
// assignment cut off by end of input is counted.
func TestScalaVersionStatementCountsOnce(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"scalaVersion := (ThisBuild / scalaVersion).value\n",
		"scalaVersion := (scalaVersion in Test).value\n",
		"scalaVersion :=",
		"scalaVersion := foo(scalaVersion)\n",
		"scalaVersion ~= (_ => scalaVersion)\n",
	} {
		state := stateOf(t, parseSource(t, source))
		requireStateField(t, state, "scala_version", "")
		if got, _ := state["unresolved_declaration_count"].(int); got != 1 {
			t.Errorf("%q: unresolved_declaration_count = %d, want 1", source, got)
		}
	}
}

// TestLibraryDependenciesRewriteCountsOnce pins that a key occurrence inside
// a statement that already superseded every row adds no second count: the
// statement is counted once, here by its one partial row.
func TestLibraryDependenciesRewriteCountsOnce(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		"if (ci) libraryDependencies.set(Def.setting(Nil), pos) else libraryDependencies += \"a\" % \"b\" % \"1\"\n",
		"lazy val s = Seq((libraryDependencies) := Seq(\"a\" % \"b\" % \"1\", libraryDependencies.map(f)))\n",
	} {
		payload := parseSource(t, source)
		requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "partial")
		if got, _ := stateOf(t, payload)["unresolved_declaration_count"].(int); got != 1 {
			t.Errorf("%q: unresolved_declaration_count = %d, want 1", source, got)
		}
	}

	// A reset of its own context only covers nothing: the inner rewrite is a
	// global reset and is counted next to the `~=` statement and the partial
	// project row.
	scoped := parseSource(t, "libraryDependencies ~= (_ => libraryDependencies.map(f))\n"+
		"lazy val core = project.settings(libraryDependencies += \"c\" % \"d\" % \"2\")\n")
	requireField(t, requireRow(t, scoped, "c:d"), "dependency_resolution_state", "partial")
	requireStateField(t, stateOf(t, scoped), "unresolved_declaration_count", 3)
}

// TestBareKeyReferenceStaysDenied pins a documented known limit (#9007): a
// bare key passed as a value, such as sbt-buildinfo's `buildInfoKeys` or a
// lint exclusion, is denied because the scanner cannot tell it from a
// rewrite.
func TestBareKeyReferenceStaysDenied(t *testing.T) {
	t.Parallel()

	buildInfo := "scalaVersion := \"2.13.12\"\n" + denyCats +
		"\nbuildInfoKeys := Seq[BuildInfoKey](name, version, scalaVersion, sbtVersion)\n"
	requireStateField(t, stateOf(t, parseSource(t, buildInfo)), "scala_version", "")

	lint := denyCats + "\nGlobal / excludeLintKeys += libraryDependencies\n"
	requireField(t, requireRow(t, parseSource(t, lint), "org.typelevel:cats-core"), "dependency_resolution_state", "partial")
}

// sprintfOne substitutes the single %s in format without reading any other
// verb, so a `%%` coordinate operator stays as written.
func sprintfOne(format, value string) string {
	for i := 0; i+1 < len(format); i++ {
		if format[i] == '%' && format[i+1] == 's' {
			return format[:i] + value + format[i+2:]
		}
	}
	return format
}
