// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// TestScalaVersionOnlyInAGrammarContext pins the scalaVersion context rule:
// a literal is recorded only where libraryDependencies would resolve (a
// column-0 statement or a project `.settings(` argument), from a legacy
// `scalaVersion in ThisBuild` there, or from `ThisBuild / scalaVersion` whose
// axis starts such a statement. A literal anywhere else is counted, never
// recorded, so it cannot add a phantom version or drive the `%%` hint.
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

		payload := parseSource(t, "scalaVersion := \"2.13.12\"\n"+source+"\nlibraryDependencies += \"o\" %% \"n\" % \"1\"\n")
		state := stateOf(t, payload)
		requireStateField(t, state, "scala_version", "2.13.12")
		requireStateField(t, state, "scala_version_count", 1)
		requireField(t, requireRow(t, payload, "o:n"), "sbt_binary_artifact", "o:n_2.13")
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

// TestScalaVersionStrayRewriteIsCountedOnly pins a rewriting scalaVersion
// form outside a grammar context: like a stray literal it is counted once and
// changes nothing in the envelope.
func TestScalaVersionStrayRewriteIsCountedOnly(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"call argument": `foo(scalaVersion ~= (_ => "2.11.8"))`,
		"val rhs":       `lazy val s = scalaVersion.set(Def.setting("2.11.8"), pos)`,
		"axis":          `Test / scalaVersion ~= (_ => "2.11.8")`,
		"member":        `x.scalaVersion <<= Def.setting("2.11.8")`,
	} {
		t.Run(name, func(t *testing.T) {
			payload := parseSource(t, "scalaVersion := \"2.13.12\"\n"+source+"\nlibraryDependencies += \"o\" %% \"n\" % \"1\"\n")
			state := stateOf(t, payload)
			requireStateField(t, state, "scala_version", "2.13.12")
			requireStateField(t, state, "unresolved_declaration_count", 1)
			requireField(t, requireRow(t, payload, "o:n"), "sbt_binary_artifact", "o:n_2.13")
		})
	}

	// A read is not a statement: not counted, nothing changes.
	read := stateOf(t, parseSource(t, "scalaVersion := \"2.13.12\"\ncrossScalaVersions := Seq(scalaVersion.value)\n"))
	requireStateField(t, read, "scala_version", "2.13.12")
	requireStateField(t, read, "unresolved_declaration_count", 0)
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
