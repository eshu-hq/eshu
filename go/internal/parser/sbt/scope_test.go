// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// TestParseTestScopeSurvivesRealisticSbtForms pins the dependency_scope for
// the forms sbt builds use to declare test-only dependencies. Each case must
// come out test/resolved; compile here would put a test library on the
// production dependency list.
func TestParseTestScopeSurvivesRealisticSbtForms(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"map with Test config": `libraryDependencies ++= Seq("a" % "b" % "1", "c" % "d" % "2").map(_ % Test)`,
		"map with string":      `libraryDependencies ++= Seq("a" % "b" % "1", "c" % "d" % "2").map(_ % "test")`,
		"infix map":            `libraryDependencies ++= Seq("a" % "b" % "1", "c" % "d" % "2") map (_ % Test)`,
		"map on next line": `libraryDependencies ++= Seq("a" % "b" % "1", "c" % "d" % "2")
  .map(_ % Test)`,
		"multi-line seq map": `libraryDependencies ++= Seq(
  "a" % "b" % "1",
  "c" % "d" % "2"
).map(_ % Test)`,
		"map after chained call": `libraryDependencies ++= Seq("a" % "b" % "1", "c" % "d" % "2").map(_.withSources()).map(_ % Test)`,
	}
	for name, source := range cases {
		payload := parseSource(t, source+"\n")
		for _, dep := range []string{"a:b", "c:d"} {
			row := requireRow(t, payload, dep)
			if row["dependency_scope"] != "test" || row["dependency_resolution_state"] != "resolved" {
				t.Errorf("%s: %s scope/state = %v/%v, want test/resolved", name, dep, row["dependency_scope"], row["dependency_resolution_state"])
			}
		}
		requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 0)
	}
}

func TestParseTrailingConfigAfterParenthesisedCoordinate(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"cross then Test": `libraryDependencies += ("a" %% "b" % "1").cross(CrossVersion.for3Use2_13) % Test`,
		"plain paren":     `libraryDependencies += ("a" %% "b" % "1") % Test`,
		"in seq":          `libraryDependencies ++= Seq(("a" %% "b" % "1").cross(CrossVersion.for3Use2_13) % Test)`,
		"string config":   `libraryDependencies += ("a" %% "b" % "1") % "test"`,
		"after exclude":   `libraryDependencies += "a" %% "b" % "1" exclude("x", "y") % Test`,
	}
	for name, source := range cases {
		payload := parseSource(t, source+"\n")
		row := requireRow(t, payload, "a:b")
		if row["dependency_scope"] != "test" || row["dependency_resolution_state"] != "resolved" {
			t.Errorf("%s: scope/state = %v/%v, want test/resolved", name, row["dependency_scope"], row["dependency_resolution_state"])
		}
		requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 0)
	}

	// The munit form from real builds keeps both the cross modifier and scope.
	payload := parseSource(t, `libraryDependencies += ("org.scalameta" %% "munit" % "0.7.29").cross(CrossVersion.for3Use2_13) % Test`+"\n")
	row := requireRow(t, payload, "org.scalameta:munit")
	requireField(t, row, "sbt_cross_version", "custom")
	requireField(t, row, "sbt_configuration", "Test")
}

func TestParseConfigurationListsSplitOnCommaAndSemicolon(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		`"it,test"`:                     "test",
		`"test,it"`:                     "test",
		`"test;it"`:                     "test",
		`"test, it"`:                    "test",
		`"compile;test"`:                "compile",
		`"test,compile"`:                "compile",
		`"compile->compile;test->test"`: "compile",
		`"test->default(compile)"`:      "test",
		`"runtime,test"`:                "compile",
		`"provided,test"`:               "provided",
		`"runtime"`:                     "runtime",
	}
	for config, want := range cases {
		payload := parseSource(t, `libraryDependencies += "a" % "b" % "1" % `+config+"\n")
		row := requireRow(t, payload, "a:b")
		if row["dependency_scope"] != want || row["dependency_resolution_state"] != "resolved" {
			t.Errorf("%s: scope/state = %v/%v, want %s/resolved", config, row["dependency_scope"], row["dependency_resolution_state"], want)
		}
		requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 0)
	}
}

// TestParseUndeterminedScopeIsPartialAndCounted covers the shapes the scanner
// cannot map to a scope: they must never read as compile/resolved.
func TestParseUndeterminedScopeIsPartialAndCounted(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"custom config":          `libraryDependencies += "a" % "b" % "1" % Custom`,
		"variable config":        `libraryDependencies += "a" % "b" % "1" % testConfig`,
		"unknown string":         `libraryDependencies += "a" % "b" % "1" % "e2e"`,
		"list with unknown":      `libraryDependencies += "a" % "b" % "1" % "test,e2e"`,
		"lambda map":             `libraryDependencies ++= Seq("a" % "b" % "1").map(dep => dep % Test)`,
		"map with block":         `libraryDependencies ++= Seq("a" % "b" % "1").map { _ % scopes.value }`,
		"trailing unknown ident": `libraryDependencies += ("a" % "b" % "1") % Custom`,
		"in group axis":          `libraryDependencies in (core, Test) += "a" % "b" % "1"`,
	}
	for name, source := range cases {
		payload := parseSource(t, source+"\n")
		row := requireRow(t, payload, "a:b")
		if row["dependency_resolution_state"] != "partial" {
			t.Errorf("%s: state = %v, want partial", name, row["dependency_resolution_state"])
		}
		if got := stateOf(t, payload)["unresolved_declaration_count"]; got != 1 {
			t.Errorf("%s: unresolved_declaration_count = %v, want 1", name, got)
		}
	}
}

func TestParseScopePreservingMapKeepsScopeResolved(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies ++= Seq("a" % "b" % "1").map(_.withSources()).toSeq
libraryDependencies ++= Seq("c" % "d" % "2" % Provided).map(_.exclude("x", "y"))
`)
	requireField(t, requireRow(t, payload, "a:b"), "dependency_scope", "compile")
	requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "resolved")
	requireField(t, requireRow(t, payload, "c:d"), "dependency_scope", "provided")
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 0)
}

func TestParseLegacyInConfigAxis(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies in Test += "org.scalatest" %% "scalatest" % "3.2.17"
libraryDependencies in ThisBuild += "a" % "b" % "1"
libraryDependencies in Test ++= Seq("c" % "d" % "2")
libraryDependencies in IntegrationTest := Seq("e" % "f" % "3")
`)
	requireField(t, requireRow(t, payload, "org.scalatest:scalatest"), "dependency_scope", "test")
	requireField(t, requireRow(t, payload, "a:b"), "dependency_scope", "compile")
	requireField(t, requireRow(t, payload, "c:d"), "dependency_scope", "test")
	requireField(t, requireRow(t, payload, "e:f"), "dependency_scope", "test")
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 0)
}
