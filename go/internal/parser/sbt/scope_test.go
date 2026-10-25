// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"strings"
	"testing"
)

// TestParseCollectionCallsArePartial covers the collection calls sbt builds use
// to scope a list of dependencies. A call after a collection is outside the
// grammar: the scanner cannot see what a `.map` body or a chain does to each
// element, so every row is partial and counted, never resolved.
func TestParseCollectionCallsArePartial(t *testing.T) {
	t.Parallel()

	const seq = `libraryDependencies ++= Seq("a" % "b" % "1", "c" % "d" % "2")`
	cases := map[string]string{
		"map with Test config":   seq + `.map(_ % Test)`,
		"map with string":        seq + `.map(_ % "test")`,
		"infix map":              seq + ` map (_ % Test)`,
		"map on next line":       seq + "\n  .map(_ % Test)",
		"multi-line seq map":     "libraryDependencies ++= Seq(\n  \"a\" % \"b\" % \"1\",\n  \"c\" % \"d\" % \"2\"\n).map(_ % Test)",
		"map after chained call": seq + `.map(_.withSources()).map(_ % Test)`,
		"last map wins":          seq + `.map(_ % Test).map(_ % Provided)`,
		"lambda then map":        seq + `.map(d => d % X).map(_ % Test)`,
		"neutral calls":          seq + `.map(_.withSources()).toSeq`,
		"exclude in map":         seq + `.map(_.exclude("x", "y"))`,
	}
	for name, source := range cases {
		payload := parseSource(t, source+"\n")
		for _, dep := range []string{"a:b", "c:d"} {
			if state := requireRow(t, payload, dep)["dependency_resolution_state"]; state != "partial" {
				t.Errorf("%s: %s state = %v, want partial", name, dep, state)
			}
		}
		requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 2)
	}
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
		`"runtime,test"`:                "runtime",
		`"runtime->runtime;test->test"`: "runtime",
		`"test,runtime"`:                "runtime",
		`"compile,runtime"`:             "compile",
		`"provided,test"`:               "provided",
		`"provided,compile"`:            "compile",
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
		"compiler plugin":        `libraryDependencies += "a" % "b" % "1" % "plugin->default(compile)"`,
		"scala tool":             `libraryDependencies += "a" % "b" % "1" % "scala-tool"`,
		"scala doc tool":         `libraryDependencies += "a" % "b" % "1" % "scala-doc-tool"`,
		"runtime with provided":  `libraryDependencies += "a" % "b" % "1" % "runtime,provided"`,
		"overlong string":        `libraryDependencies += "a" % "b" % "1" % "` + strings.Repeat("test;", 100) + `"`,
		"overlong in map":        `libraryDependencies ++= Seq("a" % "b" % "1").map(_ % "` + strings.Repeat("test;", 100) + `")`,
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

// TestParseLegacyInConfigAxis covers `libraryDependencies in <scope>`: every
// axis takes the statement out of the grammar, ThisBuild included.
func TestParseLegacyInConfigAxis(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies in Test += "org.scalatest" %% "scalatest" % "3.2.17"
libraryDependencies in ThisBuild += "a" % "b" % "1"
libraryDependencies in Test ++= Seq("c" % "d" % "2")
libraryDependencies in IntegrationTest := Seq("e" % "f" % "3")
`)
	for _, name := range []string{"org.scalatest:scalatest", "a:b", "c:d", "e:f"} {
		row := requireRow(t, payload, name)
		requireField(t, row, "dependency_resolution_state", "partial")
		requireField(t, row, "dependency_scope", "compile")
	}
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 4)
}

// TestParseRepeatedConfigurationIsPartial pins sbt's `%` semantics: the
// configuration operand requires a module with no configuration yet
// (DependencyBuilders.moduleIDConfigurable), so a second `% config` fails to
// load. The row is partial, never the last or the first configuration.
func TestParseRepeatedConfigurationIsPartial(t *testing.T) {
	t.Parallel()

	for _, source := range []string{
		`libraryDependencies += "a" % "b" % "1" % "test" % "compile"`,
		`libraryDependencies += "a" % "b" % "1" % Test % Provided`,
		`libraryDependencies += ("a" % "b" % "1" % Test) % Runtime`,
		`libraryDependencies += "a" % "b" % "1" % Provided exclude("x", "y") % Test`,
	} {
		payload := parseSource(t, source+"\n")
		if state := requireRow(t, payload, "a:b")["dependency_resolution_state"]; state != "partial" {
			t.Errorf("%s: state = %v, want partial", source, state)
		}
	}
}

// TestParseOptionalConfigurationSetsOptionalFlag pins dependency_optional,
// which comes from the configuration resolved once per `% config`.
func TestParseOptionalConfigurationSetsOptionalFlag(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies += "a" % "b" % "1" % "optional"
libraryDependencies += "c" % "d" % "1" % "compile->default;optional"
libraryDependencies ++= Seq("e" % "f" % "1" % "optional")
libraryDependencies += "g" % "h" % "1" % Test
`)
	for name, want := range map[string]bool{"a:b": true, "c:d": true, "e:f": true, "g:h": false} {
		requireField(t, requireRow(t, payload, name), "dependency_optional", want)
	}
}
