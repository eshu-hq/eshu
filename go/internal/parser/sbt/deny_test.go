// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"strings"
	"testing"
)

const denyCats = `libraryDependencies += "org.typelevel" %% "cats-core" % "2.9.0"`

// TestUnrecognisedLibraryDependenciesOccurrenceResetsEveryRow pins the
// default-deny rule for libraryDependencies: an occurrence of the key that is
// neither a recognised statement nor a read (`.value`, `.in(Test).value`,
// `(k in Test).value`) is a rewrite. It supersedes every row of the file and
// is counted. The shapes are the ones review rounds found the enumerated
// grammar missing: a parenthesized key at any depth, a key under a prefix
// axis inside parentheses, chained legacy axes, infix members, members that
// return no Setting, a bare key, and a key the fallback scanner consumed.
func TestUnrecognisedLibraryDependenciesOccurrenceResetsEveryRow(t *testing.T) {
	t.Parallel()

	shapes := []string{
		`lazy val core = project.settings(` + denyCats + `, (libraryDependencies) := Nil)`,
		denyCats + "\n(Compile / libraryDependencies) := Nil",
		denyCats + "\nlazy val s = Seq((libraryDependencies) := Nil)",
		denyCats + "\nfoo(((libraryDependencies)) := Nil)",
		denyCats + "\nlazy val s = Seq((libraryDependencies in Test) := Nil)",
		denyCats + "\n(libraryDependencies.in(Test)) := Nil",
		denyCats + "\nlibraryDependencies in Compile in Test := Nil",
		denyCats + "\nlibraryDependencies set (Def.setting(Nil), pos)",
		denyCats + "\nlazy val s = libraryDependencies withRank r := Nil",
		denyCats + "\nlibraryDependencies withRank KeyRanks.Invisible := Nil",
		denyCats + "\ndef f = if (x) (libraryDependencies) := Nil else Nil",
		denyCats + "\nval g = (x: Int) => (libraryDependencies) := Nil",
		denyCats + "\nlazy val s = libraryDependencies",
		denyCats + "\nlibraryDependencies.map(identity)",
		denyCats + "\nlazy val s = Seq(x).map(_ => libraryDependencies.map(identity))",
		denyCats + "\nlibraryDependencies ++= Seq(x) ++ { libraryDependencies := Nil; Nil }",
		denyCats + "\nlibraryDependencies ++= Seq(x) ++ Seq(libraryDependencies)",
	}
	for _, source := range shapes {
		payload := parseSource(t, source+"\n")
		row := requireRow(t, payload, "org.typelevel:cats-core")
		if got := row["dependency_resolution_state"]; got != "partial" {
			t.Errorf("%q: cats-core state = %v, want partial", source, got)
		}
		if got, _ := stateOf(t, payload)["unresolved_declaration_count"].(int); got < 2 {
			t.Errorf("%q: unresolved_declaration_count = %d, want at least 2", source, got)
		}
	}
}

// TestUnrecognisedScalaVersionOccurrencePoisonsEnvelope pins the default-deny
// rule for scalaVersion: an occurrence that is neither the in-context literal
// statement nor a read poisons the envelope for good, so no `%%` hint names a
// binary version the build may not use.
func TestUnrecognisedScalaVersionOccurrencePoisonsEnvelope(t *testing.T) {
	t.Parallel()

	shapes := []string{
		`lazy val core = project.settings((scalaVersion) := "2.12.18", ` + denyCats + `)`,
		`lazy val core = project.settings((scalaVersion) := "2.12.18")`,
		`lazy val s = Seq((Compile / scalaVersion) := "2.12.18")`,
		`lazy val s = Seq(name := "x", (ThisBuild / scalaVersion) := "2.12.18")`,
		`foo(((scalaVersion)) := "2.12.18")`,
		`(scalaVersion in Test) := "2.12.18"`,
		`scalaVersion in Compile in Test := "2.12.18"`,
		`scalaVersion in ThisBuild in Test := "2.12.18"`,
		`scalaVersion withRank KeyRanks.Invisible := "2.12.18"`,
		`scalaVersion set (x, pos)`,
		`lazy val s = scalaVersion withRank r := "2.12.18"`,
		`def f = if (x) (scalaVersion) := "2.12.18" else Nil`,
		`val g = (x: Int) => (scalaVersion) := "2.12.18"`,
		`scalaVersion.map(identity)`,
		`lazy val s = scalaVersion`,
		`libraryDependencies ++= Seq(x) ++ { scalaVersion := "2.12.18"; Nil }`,
	}
	for _, shape := range shapes {
		for _, source := range []string{
			"ThisBuild / scalaVersion := \"2.13.12\"\n" + shape + "\n" + denyCats + "\n",
			shape + "\nscalaVersion := \"2.13.12\"\n" + denyCats + "\n",
		} {
			payload := parseSource(t, source)
			state := stateOf(t, payload)
			if got := state["scala_version"]; got != "" {
				t.Errorf("%q: scala_version = %v, want empty", source, got)
			}
			for _, row := range dependencyRows(t, payload) {
				if hint, ok := row["sbt_binary_artifact"]; ok {
					t.Errorf("%q: %v sbt_binary_artifact = %v, want none", source, row["name"], hint)
				}
			}
			if got, _ := state["unresolved_declaration_count"].(int); got < 1 {
				t.Errorf("%q: unresolved_declaration_count = %d, want at least 1", source, got)
			}
		}
	}
}

// TestRecognisedKeyOccurrencesStayResolved pins what default-deny must not
// lose: reads of either key (`.value`, `.in(Test).value`, `(k in Test).value`,
// `(ThisBuild / k).value`), reads inside another statement or inside a
// statement the fallback scanner reads, longer identifiers, and keys inside
// comments, strings and interpolations leave the literal version and the
// resolved row alone.
func TestRecognisedKeyOccurrencesStayResolved(t *testing.T) {
	t.Parallel()

	legal := []string{
		`name := scalaVersion.value`,
		`crossScalaVersions := Seq(scalaVersion.value, "2.12.18")`,
		`version := (scalaVersion in Test).value`,
		`version := scalaVersion.in(Test).value`,
		`version := (ThisBuild / scalaVersion).value`,
		`version := ((Compile / scalaVersion)).value.trim`,
		`version := libraryDependencies.in(Test).value.toString`,
		`version := (libraryDependencies in Test).value.toString`,
		`myScalaVersion := "2.12.18"`,
		`lazy val scalaVersionX = "2.12.18"`,
		`myLibraryDependencies := Nil`,
		`// scalaVersion := "2.12.18"; libraryDependencies := Nil`,
		`/* (scalaVersion) := "2.12.18" */`,
		`description := "scalaVersion := 2.12, (libraryDependencies) := Nil"`,
		`description := s"${scalaVersion.value} ${libraryDependencies.value}"`,
		`lazy val core = project.settings(libraryDependencies ++= libraryDependencies.value.filter(f))`,
		`libraryDependencies += "a" %% "b" % scalaVersion.value`,
		`Test / libraryDependencies += "a" % "b" % "1"`,
		`libraryDependencies ++= (if (scalaVersion.value.startsWith("2.12")) Seq("a" % "b" % "1") else Nil)`,
	}
	for _, line := range legal {
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
	if !strings.Contains(denyCats, "%%") {
		t.Fatal("denyCats must be a cross-built coordinate")
	}
}

// TestUnclaimedKeyAtEndOfInputIsDenied pins the watch at end of input: a key
// the fallback consumes as the last token, or followed only by '.', is still
// a rewrite.
func TestUnclaimedKeyAtEndOfInputIsDenied(t *testing.T) {
	t.Parallel()

	for _, tail := range []string{"libraryDependencies", "libraryDependencies.", "scalaVersion"} {
		payload := parseSource(t, "scalaVersion := \"2.13.12\"\n"+denyCats+"\nlibraryDependencies <+= Def.setting("+tail)
		row := requireRow(t, payload, "org.typelevel:cats-core")
		state := stateOf(t, payload)
		if tail == "scalaVersion" {
			requireStateField(t, state, "scala_version", "")
			continue
		}
		requireField(t, row, "dependency_resolution_state", "partial")
	}
}
