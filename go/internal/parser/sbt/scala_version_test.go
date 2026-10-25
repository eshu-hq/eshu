// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// TestScalaVersionOnlyInAGrammarContext pins the scalaVersion context rule:
// a literal is recorded only where libraryDependencies would resolve (a
// column-0 statement or a project `.settings(` argument), from a legacy
// `scalaVersion in ThisBuild` there, or from `ThisBuild / scalaVersion` whose
// axis starts such a statement. A literal anywhere else is counted, never
// recorded, and poisons the envelope: the scanner does not know which
// projects it reaches, so a recorded literal elsewhere must not name the
// version or drive the `%%` hint.
func TestScalaVersionOnlyInAGrammarContext(t *testing.T) {
	t.Parallel()

	stray := map[string]string{
		"wrapper":       `inConfig(Test)(scalaVersion := "2.11.8")`,
		"call argument": `foo(scalaVersion := "2.11.8")`,
		"member":        `x.scalaVersion := "2.11.8"`,
		"settings val":  `lazy val common = Seq(scalaVersion := "2.11.8")`,
		"map value":     `val m = Map("a" -> (scalaVersion := "2.11.8"))`,
		"brace block":   "locally {\n  scalaVersion := \"2.11.8\"\n}",
		"indented":      "if isCI then\n  scalaVersion := \"2.11.8\"",
		"axis":          `Test / scalaVersion := "2.11.8"`,
		"legacy axis":   `scalaVersion in Test := "2.11.8"`,
		"inThisBuild":   `inThisBuild(List(scalaVersion := "2.11.8"))`,
		"def setting":   `val sv = Def.setting(scalaVersion := "2.11.8")`,
		"nested axis":   `foo(ThisBuild / scalaVersion := "2.11.8")`,
	}
	for name, source := range stray {
		alone := stateOf(t, parseSource(t, source+"\n"))
		if alone["scala_version"] != "" || alone["scala_version_count"] != 0 {
			t.Errorf("%s alone: scala_version = %v, count = %v; want empty and 0", name, alone["scala_version"], alone["scala_version_count"])
		}
		if got, _ := alone["unresolved_declaration_count"].(int); got != 1 {
			t.Errorf("%s alone: unresolved_declaration_count = %v, want 1", name, alone["unresolved_declaration_count"])
		}

		for _, order := range []string{
			"scalaVersion := \"2.13.12\"\n" + source + "\n",
			source + "\nscalaVersion := \"2.13.12\"\n",
		} {
			payload := parseSource(t, order+"libraryDependencies += \"o\" %% \"n\" % \"1\"\n")
			state := stateOf(t, payload)
			requireStateField(t, state, "scala_version", "")
			requireStateField(t, state, "scala_version_count", 1)
			requireStateField(t, state, "unresolved_declaration_count", 1)
			row := requireRow(t, payload, "o:n")
			requireField(t, row, "dependency_resolution_state", "resolved")
			if hint, ok := row["sbt_binary_artifact"]; ok {
				t.Errorf("%s order %q: sbt_binary_artifact = %v, want none", name, order, hint)
			}
		}
	}

	accepted := map[string]string{
		"bare":               `scalaVersion := "2.13.12"`,
		"after semicolon":    `organization := "x"; scalaVersion := "2.13.12"`,
		"ThisBuild axis":     `ThisBuild / scalaVersion := "2.13.12"`,
		"ThisBuild after ;":  `organization := "x"; ThisBuild / scalaVersion := "2.13.12"`,
		"legacy ThisBuild":   `scalaVersion in ThisBuild := "2.13.12"`,
		"project settings":   `lazy val core = project.settings(scalaVersion := "2.13.12")`,
		"settings ThisBuild": `lazy val core = project.settings(name := "c", ThisBuild / scalaVersion := "2.13.12")`,
	}
	for name, source := range accepted {
		state := stateOf(t, parseSource(t, source+"\n"))
		if state["scala_version"] != "2.13.12" {
			t.Errorf("%s: scala_version = %v, want 2.13.12", name, state["scala_version"])
		}
	}
}

// TestScalaVersionRewriteOrNonLiteralPoisonsEnvelope pins the fail-closed
// envelope. A scalaVersion statement in a grammar context that is not a plain
// literal assignment (a non-literal value, `~=`, `<<=`, `<+=`, `.set`,
// `.transform`, `.withRank(...) :=`, `.in(...) :=`, with or without a
// `ThisBuild` axis) changes the version to something the scanner cannot read,
// so the envelope carries no scala_version and no `%%` hint, and the
// statement is counted once. The poison stays when a literal follows.
func TestScalaVersionRewriteOrNonLiteralPoisonsEnvelope(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"sys env":            `scalaVersion := sys.env("SV")`,
		"unknown ident":      `scalaVersion := scala3`,
		"literal method":     `scalaVersion := "3.3.1".trim`,
		"transform op":       `scalaVersion ~= (_ => "3.3.1")`,
		"legacy replace":     `scalaVersion <<= Def.setting("3.3.1")`,
		"legacy append":      `scalaVersion <+= Def.setting("3.3.1")`,
		"legacy replace val": "val sv = \"3.3.1\"\nscalaVersion <<= sv",
		"transform val":      "val sv = \"3.3.1\"\nscalaVersion ~= sv",
		"set member":         `scalaVersion.set(Def.setting("3.3.1"), pos)`,
		"transform member":   `scalaVersion.transform(_ => "3.3.1", pos)`,
		"withRank assign":    `scalaVersion.withRank(KeyRanks.Invisible) := "3.3.1"`,
		"in member assign":   `scalaVersion.in(ThisBuild) := "3.3.1"`,
		"ThisBuild axis":     `ThisBuild / scalaVersion ~= (_ => "3.3.1")`,
		"legacy ThisBuild":   `scalaVersion in ThisBuild ~= (_ => "3.3.1")`,
		"project settings":   `lazy val core = project.settings(scalaVersion ~= (_ => "3.3.1"))`,
		"ThisBuild settings": `lazy val core = project.settings(ThisBuild / scalaVersion.set(Def.setting("3.3.1"), pos))`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, order := range []string{
				"scalaVersion := \"2.13.12\"\n" + source + "\n",
				source + "\nscalaVersion := \"2.13.12\"\n",
			} {
				payload := parseSource(t, order+"libraryDependencies += \"a\" %% \"b\" % \"1\"\n")
				state := stateOf(t, payload)
				requireStateField(t, state, "scala_version", "")
				requireStateField(t, state, "unresolved_declaration_count", 1)
				if hint, ok := requireRow(t, payload, "a:b")["sbt_binary_artifact"]; ok {
					t.Errorf("order %q: sbt_binary_artifact = %v, want none", order, hint)
				}
			}
		})
	}
}

// TestScalaVersionStrayAssignmentPoisonsEnvelope pins every assignment form
// outside a grammar context (an operator, a rewriting member, `.:=(...)`, a
// legacy `in` axis) and the dotted `.:=(...)` call in one: each could set the
// version somewhere the scanner does not track, so it is counted once and
// poisons the envelope even when a recorded literal follows it. A read of
// the key (`.value`, `.in(Test).value`) is not a statement and changes nothing.
func TestScalaVersionStrayAssignmentPoisonsEnvelope(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"call argument":   `foo(scalaVersion ~= (_ => "2.11.8"))`,
		"val rhs":         `lazy val s = scalaVersion.set(Def.setting("2.11.8"), pos)`,
		"axis":            `Test / scalaVersion ~= (_ => "2.11.8")`,
		"member":          `x.scalaVersion <<= Def.setting("2.11.8")`,
		"transform rhs":   `lazy val s = scalaVersion.transform(_ => "2.11.8", pos)`,
		"in member":       `foo(scalaVersion.in(Test) := "2.11.8")`,
		"in legacy":       `foo(scalaVersion in Test := "2.11.8")`,
		"in group":        `scalaVersion in (Test, compile) := "2.11.8"`,
		"dot assign call": `scalaVersion.:=("2.11.8")`,
		"dot assign nest": `lazy val s = scalaVersion.:=("2.11.8")`,
		"in then member":  `scalaVersion.in(Test).withRank(r) := "2.11.8"`,
		"def setting":     "def common = Seq(scalaVersion := \"2.11.8\")",
		"lambda":          `lazy val s = Seq("2.11.8").map(v => scalaVersion := v)`,
	} {
		t.Run(name, func(t *testing.T) {
			for _, order := range []string{
				"scalaVersion := \"2.13.12\"\n" + source + "\n",
				source + "\nscalaVersion := \"2.13.12\"\n",
			} {
				payload := parseSource(t, order+"libraryDependencies += \"o\" %% \"n\" % \"1\"\n")
				state := stateOf(t, payload)
				requireStateField(t, state, "scala_version", "")
				requireStateField(t, state, "unresolved_declaration_count", 1)
				if hint, ok := requireRow(t, payload, "o:n")["sbt_binary_artifact"]; ok {
					t.Errorf("order %q: sbt_binary_artifact = %v, want none", order, hint)
				}
			}
		})
	}

	// A read is not a statement: not counted, nothing changes.
	for _, read := range []string{
		"crossScalaVersions := Seq(scalaVersion.value)",
		"crossScalaVersions := Seq(scalaVersion.in(Test).value)",
		"check := (scalaVersion in Test).value",
	} {
		state := stateOf(t, parseSource(t, "scalaVersion := \"2.13.12\"\n"+read+"\n"))
		requireStateField(t, state, "scala_version", "2.13.12")
		requireStateField(t, state, "unresolved_declaration_count", 0)
	}
}

// TestScalaVersionReviewShapesReportNoHint pins the minimized review shapes:
// a ThisBuild literal plus a settings val, def, lambda or inThisBuild
// assignment that a project applies. sbt builds the project with the nested
// version, so the envelope reports none and no `%%` hint is written.
func TestScalaVersionReviewShapesReportNoHint(t *testing.T) {
	t.Parallel()

	const project = "lazy val core = project.settings(commonSettings, libraryDependencies += \"org.typelevel\" %% \"cats-core\" % \"2.9.0\")\n"
	for name, source := range map[string]string{
		"settings val": "ThisBuild / scalaVersion := \"2.13.12\"\nlazy val commonSettings = Seq(scalaVersion := \"2.12.18\")\n" + project,
		"settings def": "ThisBuild / scalaVersion := \"2.13.12\"\ndef commonSettings = Seq(scalaVersion := \"2.12.18\")\n" + project,
		"inThisBuild":  "inThisBuild(List(scalaVersion := \"2.12.18\"))\nscalaVersion := \"2.13.12\"\n" + project,
		"val settings": "scalaVersion := \"2.13.12\"\nval commonSettings: Seq[Setting[_]] = Seq(organization := \"x\", scalaVersion := \"2.12.18\")\n" + project,
		"lambda":       "scalaVersion := \"2.13.12\"\nlazy val commonSettings = Seq(\"2.12.18\").map(v => scalaVersion := v)\n" + project,
	} {
		t.Run(name, func(t *testing.T) {
			payload := parseSource(t, source)
			requireStateField(t, stateOf(t, payload), "scala_version", "")
			row := requireRow(t, payload, "org.typelevel:cats-core")
			requireField(t, row, "dependency_resolution_state", "resolved")
			if hint, ok := row["sbt_binary_artifact"]; ok {
				t.Errorf("sbt_binary_artifact = %v, want none", hint)
			}
		})
	}
}

// TestScalaVersionDistinctLiteralsFailClosed pins the envelope for more than
// one distinct literal. sbt applies the last assignment in a scope, but the
// order across the bare file, ThisBuild and each project's settings is not
// something the scanner can decide, so two distinct literals leave
// scala_version empty with scala_version_count 2 and no `%%` hint. A repeated
// identical literal is one version.
func TestScalaVersionDistinctLiteralsFailClosed(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"same scope":       "scalaVersion := \"2.12.18\"\nscalaVersion := \"2.13.12\"\n",
		"ThisBuild first":  "ThisBuild / scalaVersion := \"2.13.12\"\nscalaVersion := \"3.3.1\"\n",
		"project override": "ThisBuild / scalaVersion := \"2.13.12\"\nlazy val b = project.settings(scalaVersion := \"3.3.1\")\n",
	} {
		t.Run(name, func(t *testing.T) {
			payload := parseSource(t, source+"libraryDependencies += \"a\" %% \"b\" % \"1\"\n")
			state := stateOf(t, payload)
			requireStateField(t, state, "scala_version", "")
			requireStateField(t, state, "scala_version_count", 2)
			if hint, ok := requireRow(t, payload, "a:b")["sbt_binary_artifact"]; ok {
				t.Errorf("sbt_binary_artifact = %v, want none", hint)
			}
		})
	}

	same := stateOf(t, parseSource(t, "scalaVersion := \"2.13.12\"\nThisBuild / scalaVersion := \"2.13.12\"\n"))
	requireStateField(t, same, "scala_version", "2.13.12")
	requireStateField(t, same, "scala_version_count", 1)
}
