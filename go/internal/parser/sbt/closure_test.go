// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"sort"
	"testing"
)

// requireUndetermined asserts that the single a:b row is partial and counted
// once in unresolved_declaration_count.
func requireUndetermined(t *testing.T, name, source string) {
	t.Helper()
	payload := parseSource(t, source+"\n")
	row := requireRow(t, payload, "a:b")
	if row["dependency_resolution_state"] != "partial" {
		t.Errorf("%s: state = %v, want partial (scope %v)", name, row["dependency_resolution_state"], row["dependency_scope"])
	}
	if got := stateOf(t, payload)["unresolved_declaration_count"]; got != 1 {
		t.Errorf("%s: unresolved_declaration_count = %v, want 1", name, got)
	}
}

// requireResolvedScope asserts that the a:b row is resolved with the scope and
// that nothing is counted as unresolved.
func requireResolvedScope(t *testing.T, name, source, scope string) {
	t.Helper()
	payload := parseSource(t, source+"\n")
	row := requireRow(t, payload, "a:b")
	if row["dependency_scope"] != scope || row["dependency_resolution_state"] != "resolved" {
		t.Errorf("%s: scope/state = %v/%v, want %s/resolved", name, row["dependency_scope"], row["dependency_resolution_state"], scope)
	}
	if got := stateOf(t, payload)["unresolved_declaration_count"]; got != 0 {
		t.Errorf("%s: unresolved_declaration_count = %v, want 0", name, got)
	}
}

// TestParseUnknownTailIsUndeterminedNotCompile pins the class, not one form: any
// method, infix operator, or call in the tail that is not on the scope-neutral
// allowlist leaves the scope undetermined. None of these may read as a resolved
// compile dependency.
func TestParseUnknownTailIsUndeterminedNotCompile(t *testing.T) {
	t.Parallel()

	const lib = `libraryDependencies += `
	const seq = `libraryDependencies ++= Seq("a" % "b" % "1")`
	cases := map[string]string{
		"withConfigurations infix":     lib + `"a" % "b" % "1" withConfigurations(Some("test"))`,
		"withConfigurations dotted":    lib + `("a" % "b" % "1").withConfigurations(Some("test"))`,
		"withConfigurations next line": lib + "\"a\" % \"b\" % \"1\"\n  .withConfigurations(Some(\"test\"))",
		"withConfigurations on map":    seq + `.map(_.withConfigurations(Some("test")))`,
		"withConfigurations after ok":  seq + `.map(_.withSources().withConfigurations(Some("test")))`,
		"configure":                    lib + `"a" % "b" % "1" configure(x)`,
		"unknown dotted call":          lib + `("a" % "b" % "1").withFoo()`,
		"unknown call after config":    lib + `"a" % "b" % "1" % Test withSomething`,
		"user extension":               lib + `"a" % "b" % "1" myExtension`,
		"user extension call":          lib + `"a" % "b" % "1" asTestDependency(true)`,
		"unknown infix %?":             lib + `"a" % "b" % "1" %? Test`,
		"unknown infix %%%%":           lib + `"a" % "b" % "1" %%%% Test`,
		"leading other infix":          lib + "\"a\" % \"b\" % \"1\"\n  %% Test",
		"tuple in parens":              lib + `("a" % "b" % "1", ignored)`,
		"bare group":                   lib + `"a" % "b" % "1" (Test)`,
		"map lambda call":              seq + `.map(dep => dep.withSources())`,
		"map identity":                 seq + `.map(identity)`,
		"map copy":                     seq + `.map(_.copy(x))`,
		"map block lambda":             seq + `.map { d => d.exclude("x", "y") }`,
		"map body then junk":           seq + `.map(_ % Test, extra)`,
		"empty map":                    seq + `.map()`,
		"flatMap":                      seq + `.flatMap(d => Seq(d % Test))`,
		"collect":                      seq + `.collect { case d => d % Test }`,
		"infix zip":                    seq + ` zip other`,
		"map without arguments":        seq + ` map toTest`,
		"lone operator":                lib + `"a" % "b" % "1" =>`,
		"lone operator then neutral":   lib + `"a" % "b" % "1" ++ exclude("x", "y")`,
		"stray string":                 lib + `"a" % "b" % "1" "x"`,
		"stray number":                 lib + `"a" % "b" % "1" 42`,
		"dot without name":             `libraryDependencies ++= Seq(("a" % "b" % "1").)`,
		"empty configuration":          lib + `"a" % "b" % "1" % ""`,
		"empty configuration on map":   seq + `.map(_ % "")`,
		"separator only":               lib + `"a" % "b" % "1" % ","`,
	}
	for name, source := range cases {
		requireUndetermined(t, name, source)
	}
}

// TestParseScopeNeutralTailsKeepScopeResolved is the allowlist: the infix
// ModuleID calls that cannot change the configuration keep the scope the `%`
// operand gave. A dotted call binds to the literal before it unless the
// coordinate is parenthesised, and a parenthesised coordinate is outside the
// grammar, so every dotted spelling is partial.
func TestParseScopeNeutralTailsKeepScopeResolved(t *testing.T) {
	t.Parallel()

	tails := []string{
		`exclude("x", "y")`, `excludeAll(ExclusionRule("x"))`,
		`classifier "tests"`, `classifier("tests")`,
		`intransitive()`, `withSources()`, `withJavadoc()`, `withSources() withJavadoc()`,
		`cross CrossVersion.full`, `cross(CrossVersion.full)`, `force()`, `changing()`,
	}
	for _, tail := range tails {
		requireResolvedScope(t, "plain "+tail, `libraryDependencies += "a" % "b" % "1" `+tail, "compile")
		requireResolvedScope(t, "test "+tail, `libraryDependencies += "a" % "b" % "1" % Test `+tail, "test")
		requireResolvedScope(t, "seq "+tail, `libraryDependencies ++= Seq("a" % "b" % "1" % Provided `+tail+`)`, "provided")
		requireUndetermined(t, "config after "+tail, `libraryDependencies += "a" % "b" % "1" `+tail+` % Provided`)
		requireUndetermined(t, "paren "+tail, `libraryDependencies += ("a" % "b" % "1") `+tail+` % Runtime`)
		requireUndetermined(t, "dotted "+tail, `libraryDependencies += ("a" % "b" % "1").`+tail)
		requireUndetermined(t, "map "+tail, `libraryDependencies ++= Seq("a" % "b" % "1").map(_ `+tail+`)`)
	}
	for _, dropped := range []string{`notTransitive()`, `from("https://example.invalid/a.jar")`, `artifacts(Artifact("a"))`, `withCrossVersion(CrossVersion.binary)`} {
		requireUndetermined(t, "off the list "+dropped, `libraryDependencies += "a" % "b" % "1" `+dropped)
	}

	payload := parseSource(t, `libraryDependencies += "a" % "b" % "1" cross CrossVersion.full`+"\n")
	requireField(t, requireRow(t, payload, "a:b"), "sbt_cross_version", "custom")
}

// TestParseLeadingInfixPercentIsReadOnlyInsideParentheses covers a line that
// starts with `%`. Inside '(' Scala never ends a statement at a line break,
// so it continues the element and the grammar reads it. In the bare file it
// is sbt 2 / Scala 3 leading infix, which Scala 2 reads as a new statement;
// the grammar does not choose, so the row is partial.
func TestParseLeadingInfixPercentIsReadOnlyInsideParentheses(t *testing.T) {
	t.Parallel()

	requireUndetermined(t, "leading Test", "libraryDependencies += \"a\" % \"b\" % \"1\"\n  % Test")
	requireUndetermined(t, "leading string", "libraryDependencies += \"a\" % \"b\" % \"1\"\n  % \"provided\"")
	requireUndetermined(t, "leading on coordinate", "libraryDependencies += \"a\"\n  % \"b\" % \"1\"")
	requireResolvedScope(t, "trailing operator", "libraryDependencies += \"a\" %\n  \"b\" % \"1\" %\n  Test", "test")

	payload := parseSource(t, "libraryDependencies ++= Seq(\n  \"a\" % \"b\" % \"1\"\n    % Test,\n  \"c\" % \"d\" % \"2\"\n)\n")
	requireField(t, requireRow(t, payload, "a:b"), "dependency_scope", "test")
	requireField(t, requireRow(t, payload, "c:d"), "dependency_scope", "compile")
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 0)

	// The next statement is not swallowed by a continuation rule.
	next := parseSource(t, "libraryDependencies += \"a\" % \"b\" % \"1\"\nlibraryDependencies += \"c\" % \"d\" % \"2\" % Test\n")
	requireField(t, requireRow(t, next, "a:b"), "dependency_scope", "compile")
	requireField(t, requireRow(t, next, "c:d"), "dependency_scope", "test")
	requireStateField(t, stateOf(t, next), "unresolved_declaration_count", 0)
}

// TestParseScopedAxisIsUndeterminedNotCompile covers the project-level wrappers.
// sbt resolves dependencies from the project-level key, so a key under a
// configuration axis, an `inConfig(...)` wrapper, or an unknown axis cannot be
// mapped to a scope by reading the file. An explicit `% Test` does not rescue it.
func TestParseScopedAxisIsUndeterminedNotCompile(t *testing.T) {
	t.Parallel()

	const dep = `"a" % "b" % "1"`
	cases := map[string]string{
		"Test axis":                 `Test / libraryDependencies += ` + dep,
		"Compile axis":              `Compile / libraryDependencies += ` + dep,
		"IntegrationTest axis":      `IntegrationTest / libraryDependencies += ` + dep,
		"Provided axis":             `Provided / libraryDependencies ++= Seq(` + dep + `)`,
		"ThisBuild then Test":       `ThisBuild / Test / libraryDependencies += ` + dep,
		"task axis":                 `Test / update / libraryDependencies += ` + dep,
		"explicit config no rescue": `Test / libraryDependencies += ` + dep + ` % Test`,
		"map no rescue":             `Test / libraryDependencies ++= Seq(` + dep + `).map(_ % Test)`,
		"legacy in Test":            `libraryDependencies in Test += ` + dep,
		"legacy in Compile":         `libraryDependencies in Compile ++= Seq(` + dep + `)`,
		"dot in Test":               `libraryDependencies.in(Test) += ` + dep,
		"dot in group":              `libraryDependencies.in(core, Test) += ` + dep,
		"custom config":             "lazy val Perf = config(\"perf\")\nPerf / libraryDependencies += " + dep,
		"undeclared project":        `ghost / libraryDependencies += ` + dep,
		"inConfig":                  `inConfig(Test)(libraryDependencies += ` + dep + `)`,
		"inConfig seq":              `inConfig(Test)(Seq(libraryDependencies += ` + dep + `, name := "x"))`,
		"inConfig ++=":              `inConfig(Test)(libraryDependencies ++= Seq(` + dep + `))`,
		"inConfig custom":           `inConfig(Perf)(Defaults.testSettings ++ Seq(libraryDependencies += ` + dep + `))`,
		"inTask":                    `inTask(update)(libraryDependencies += ` + dep + `)`,
		"inScope":                   `inScope(Scope.GlobalScope)(libraryDependencies += ` + dep + `)`,
		"inConfig inside settings":  `lazy val core = project.settings(inConfig(Test)(libraryDependencies += ` + dep + `))`,
		"nested inThisBuild":        `inConfig(Test)(inThisBuild(libraryDependencies += ` + dep + `))`,
	}
	for name, source := range cases {
		requireUndetermined(t, name, source)
	}
}

// TestParseScopedAxisRegionEnds checks the wrapper rule does not leak: a
// dependency after the wrapper closes is read normally.
func TestParseScopedAxisRegionEnds(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `inConfig(Test)(libraryDependencies += "a" % "b" % "1")
libraryDependencies += "c" % "d" % "2" % Test
lazy val core = project.settings(libraryDependencies += "e" % "f" % "3")
`)
	if got := requireRow(t, payload, "a:b")["dependency_resolution_state"]; got != "partial" {
		t.Errorf("a:b state = %v, want partial", got)
	}
	for name, scope := range map[string]string{"c:d": "test", "e:f": "compile"} {
		row := requireRow(t, payload, name)
		if row["dependency_scope"] != scope || row["dependency_resolution_state"] != "resolved" {
			t.Errorf("%s scope/state = %v/%v, want %s/resolved", name, row["dependency_scope"], row["dependency_resolution_state"], scope)
		}
	}
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 1)
}

// TestParseOnlyTheTwoGrammarContextsResolve lists where a key may sit: a
// top-level statement of the bare file, or an argument of a project val's
// `.settings(...)`. Every axis, including the build-wide ones and a declared
// project, takes the statement out of the grammar.
func TestParseOnlyTheTwoGrammarContextsResolve(t *testing.T) {
	t.Parallel()

	const dep = `"a" % "b" % "1"`
	requireResolvedScope(t, "plain", `libraryDependencies += `+dep, "compile")
	requireResolvedScope(t, "in settings", `lazy val core = project.settings(libraryDependencies += `+dep+`)`, "compile")
	requireResolvedScope(t, "second settings argument", `lazy val core = project.settings(name := "x", libraryDependencies += `+dep+`)`, "compile")
	cases := map[string]string{
		"ThisBuild":            `ThisBuild / libraryDependencies += ` + dep,
		"Global":               `Global / libraryDependencies += ` + dep,
		"ThisProject":          `ThisProject / libraryDependencies += ` + dep,
		"declared project":     "lazy val core = (project in file(\"core\"))\ncore / libraryDependencies += " + dep,
		"declared project dot": "lazy val core = project.in(file(\"core\"))\ncore / libraryDependencies += " + dep,
		"crossProject axis":    "lazy val core = crossProject(JVMPlatform)\ncore / libraryDependencies += " + dep,
		"legacy ThisBuild":     `libraryDependencies in ThisBuild += ` + dep,
		"dot in ThisBuild":     `libraryDependencies.in(ThisBuild) += ` + dep,
		"inThisBuild":          `inThisBuild(libraryDependencies += ` + dep + `)`,
		"legacy declared":      "lazy val core = project\nlibraryDependencies in core += " + dep,
	}
	for name, source := range cases {
		requireUndetermined(t, name, source)
	}
}

// TestParseUndeterminedAxisAndTailAreBothCounted keeps the two causes apart: an
// unknown axis stays partial even when a later `% config` is readable, and an
// unknown tail after a readable config is partial too.
func TestParseUndeterminedAxisAndTailAreBothCounted(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `Test / libraryDependencies += "a" % "b" % "1" % Test
libraryDependencies += "c" % "d" % "2" % Test withSomething
`)
	var names []string
	for _, row := range dependencyRows(t, payload) {
		names = append(names, row["name"].(string))
		if row["dependency_resolution_state"] != "partial" {
			t.Errorf("%v state = %v, want partial", row["name"], row["dependency_resolution_state"])
		}
	}
	sort.Strings(names)
	if len(names) != 2 || names[0] != "a:b" || names[1] != "c:d" {
		t.Fatalf("rows = %v, want a:b and c:d", names)
	}
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 2)
}
