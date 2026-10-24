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
