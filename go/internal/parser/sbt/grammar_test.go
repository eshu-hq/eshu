// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// grammarDep is the coordinate every grammar table case declares.
const grammarDep = `"a" % "b" % "1"`

// requireGrammarResolved asserts that the a:b row is resolved, carries exactly
// the scope, and that nothing is counted as unresolved.
func requireGrammarResolved(t *testing.T, name, source, scope string) {
	t.Helper()
	payload := parseSource(t, source+"\n")
	row := requireRow(t, payload, "a:b")
	if row["dependency_scope"] != scope || row["dependency_resolution_state"] != "resolved" {
		t.Errorf("%s: scope/state = %v/%v, want %s/resolved", name, row["dependency_scope"], row["dependency_resolution_state"], scope)
	}
	if row["value"] != "1" {
		t.Errorf("%s: value = %v, want 1", name, row["value"])
	}
	if got := stateOf(t, payload)["unresolved_declaration_count"]; got != 0 {
		t.Errorf("%s: unresolved_declaration_count = %v, want 0", name, got)
	}
}

// requireGrammarPartial asserts that the a:b row is emitted, is never
// resolved, and is counted in unresolved_declaration_count.
func requireGrammarPartial(t *testing.T, name, source string) {
	t.Helper()
	payload := parseSource(t, source+"\n")
	var row map[string]any
	for _, candidate := range dependencyRows(t, payload) {
		if candidate["name"] == "a:b" {
			row = candidate
		}
	}
	if row == nil {
		t.Errorf("%s: no a:b row; a literal coordinate outside the grammar must still be emitted partial", name)
		return
	}
	if row["dependency_resolution_state"] == "resolved" {
		t.Errorf("%s: state = resolved (scope %v, value %v), want not resolved", name, row["dependency_scope"], row["value"])
	}
	if got, _ := stateOf(t, payload)["unresolved_declaration_count"].(int); got < 1 {
		t.Errorf("%s: unresolved_declaration_count = %v, want at least 1", name, got)
	}
}

// TestGrammarAcceptedFormsResolveWithExactScope is the closed grammar: each
// form here matches it token for token and resolves with the listed scope.
func TestGrammarAcceptedFormsResolveWithExactScope(t *testing.T) {
	t.Parallel()

	const lib = `libraryDependencies += `
	cases := []struct{ name, source, scope string }{
		{"percent", lib + grammarDep, "compile"},
		{"double percent", lib + `"a" %% "b" % "1"`, "compile"},
		{"triple percent", lib + `"a" %%% "b" % "1"`, "compile"},
		{"Test ident", lib + grammarDep + ` % Test`, "test"},
		{"test string", lib + grammarDep + ` % "test"`, "test"},
		{"Compile ident", lib + grammarDep + ` % Compile`, "compile"},
		{"Runtime ident", lib + grammarDep + ` % Runtime`, "runtime"},
		{"Provided ident", lib + grammarDep + ` % Provided`, "provided"},
		{"Optional ident", lib + grammarDep + ` % Optional`, "compile"},
		{"IntegrationTest ident", lib + grammarDep + ` % IntegrationTest`, "test"},
		{"config list", lib + grammarDep + ` % "it,test"`, "test"},
		{"config mapping", lib + grammarDep + ` % "compile->default"`, "compile"},
		{"val version", "val v = \"1\"\n" + lib + `"a" % "b" % v`, "compile"},
		{"operator then newline", "libraryDependencies +=\n  " + grammarDep, "compile"},
		{"coordinate operator then newline", "libraryDependencies += \"a\" %\n  \"b\" % \"1\"", "compile"},
		{"semicolon", lib + grammarDep + `; name := "x"`, "compile"},
		{"seq", `libraryDependencies ++= Seq(` + grammarDep + `)`, "compile"},
		{"list", `libraryDependencies ++= List(` + grammarDep + `)`, "compile"},
		{"vector", `libraryDependencies ++= Vector(` + grammarDep + `)`, "compile"},
		{"assign seq", `libraryDependencies := Seq(` + grammarDep + `)`, "compile"},
		{"seq trailing comma", "libraryDependencies ++= Seq(\n  " + grammarDep + ",\n  \"c\" % \"d\" % \"2\" % Test,\n)", "compile"},
		{"seq config on next line", "libraryDependencies ++= Seq(\n  " + grammarDep + "\n    % Test\n)", "test"},
		{"seq call on next line", "libraryDependencies ++= Seq(\n  " + grammarDep + "\n    exclude(\"x\", \"y\")\n)", "compile"},
		{"exclude", lib + grammarDep + ` exclude("x", "y")`, "compile"},
		{"excludeAll", lib + grammarDep + ` excludeAll(ExclusionRule(organization = "x"))`, "compile"},
		{"classifier infix", lib + grammarDep + ` classifier "tests"`, "compile"},
		{"classifier call", lib + grammarDep + ` classifier("tests")`, "compile"},
		{"intransitive", lib + grammarDep + ` intransitive()`, "compile"},
		{"withSources", lib + grammarDep + ` withSources()`, "compile"},
		{"withJavadoc", lib + grammarDep + ` withJavadoc()`, "compile"},
		{"cross infix", lib + grammarDep + ` cross CrossVersion.full`, "compile"},
		{"cross call", lib + grammarDep + ` cross(CrossVersion.full)`, "compile"},
		{"force", lib + grammarDep + ` force()`, "compile"},
		{"changing", lib + grammarDep + ` changing()`, "compile"},
		{"call chain", lib + grammarDep + ` withSources() withJavadoc() exclude("x", "y")`, "compile"},
		{"config then call", lib + grammarDep + ` % Test exclude("x", "y")`, "test"},
		{"project settings", `lazy val core = project.settings(` + lib + grammarDep + ` % Test)`, "test"},
		{"project in file settings", "lazy val core = (project in file(\"core\"))\n  .settings(\n    name := \"core\",\n    libraryDependencies ++= Seq(" + grammarDep + "),\n  )", "compile"},
		{"project chain settings", "lazy val core = project\n  .in(file(\"core\"))\n  .dependsOn(util)\n  .settings(" + lib + grammarDep + ")", "compile"},
		{"Project apply settings", `lazy val core = Project("core", file("core")).settings(` + lib + grammarDep + `)`, "compile"},
		{"crossProject settings", `lazy val core = crossProject(JVMPlatform).settings(` + lib + `"a" %%% "b" % "1")`, "compile"},
		{"settings config on next line", "lazy val core = project.settings(\n  " + lib + grammarDep + "\n    % Test\n)", "test"},
		{"after a char literal", "val q = '\"'\n" + lib + grammarDep, "compile"},
		{"after an escaped char literal", "val q = '\\''\n" + lib + grammarDep, "compile"},
		{"char literal on the same line", "val q = '\"'; " + lib + grammarDep, "compile"},
		{"escaped char literal on the same line", "val q = '\\\"'; " + lib + grammarDep, "compile"},
	}
	for _, tc := range cases {
		requireGrammarResolved(t, tc.name, tc.source, tc.scope)
	}
}

// TestGrammarRejectedFormsAreNeverResolved lists forms outside the grammar.
// Every coordinate literal in them is emitted partial and counted, never
// resolved. H1-H5 are the five blocking classes of the final review rounds.
func TestGrammarRejectedFormsAreNeverResolved(t *testing.T) {
	t.Parallel()

	const lib = `libraryDependencies += `
	const seq = `libraryDependencies ++= Seq(` + grammarDep + `)`
	cases := map[string]string{
		// H1: configuration-scoped braces and brace argument lists.
		"H1 inConfig brace":           `inConfig(Test) { ` + lib + grammarDep + ` }`,
		"H1 settings brace":           `lazy val core = project.settings { ` + lib + grammarDep + ` }`,
		"H1 settings brace argument":  `lazy val core = project.settings({ ` + lib + grammarDep + ` })`,
		"H1 settings nested brace":    "lazy val core = project.settings(\n  {\n    " + lib + grammarDep + "\n  }\n)",
		"H1 inConfig paren":           `inConfig(Test)(` + lib + grammarDep + `)`,
		"H1 inTask":                   `inTask(update)(` + lib + grammarDep + `)`,
		"H1 inScope":                  `inScope(ThisScope)(` + lib + grammarDep + `)`,
		"H1 inThisBuild":              `inThisBuild(` + lib + grammarDep + `)`,
		"H1 inConfig in settings":     `lazy val core = project.settings(inConfig(Test)(` + lib + grammarDep + `))`,
		"H2 newline infix in seq":     "libraryDependencies ++= Seq(\n  " + grammarDep + "\n    withConfigurations(Some(\"test\"))\n)",
		"H2 newline infix settings":   "lazy val core = project.settings(\n  " + lib + grammarDep + "\n    withConfigurations(Some(\"test\")))",
		"H2 newline plus in seq":      "libraryDependencies ++= Seq(\n  " + grammarDep + "\n  + baz\n)",
		"H3 withRevision":             lib + `("a" % "b" % "1").withRevision("2") % Test`,
		"H3 withName":                 lib + `("a" % "b" % "1").withName("x") % "compile"`,
		"H3 withRevision map":         `libraryDependencies ++= Seq(("a" % "b" % "1").withRevision("2")).map(_ % Compile)`,
		"H3 flatMap withName":         seq + `.flatMap(m => Seq(m, m.withName("x"))).map(_ % Test)`,
		"H4 infix map over chain":     `libraryDependencies ++= Seq(` + grammarDep + `) ++ Seq("c" % "d" % "2") map (_ % Test)`,
		"H4 map withConfigurations":   seq + ` map (_.withConfigurations(Some("test")))`,
		"H5 distinct infix map":       seq + `.distinct map (_ % "test")`,
		"H5 toSeq flatMap":            seq + `.toSeq flatMap (m => Seq(m % Test))`,
		"withConfigurations infix":    lib + grammarDep + ` withConfigurations(Some("test"))`,
		"withConfigurations dotted":   lib + `("a" % "b" % "1").withConfigurations(Some("test"))`,
		"configure":                   lib + grammarDep + ` configure(x)`,
		"unknown infix call":          lib + grammarDep + ` asTestDependency(true)`,
		"unknown operator":            lib + grammarDep + ` %? Test`,
		"leading infix config":        lib + grammarDep + "\n  % Test",
		"leading dot call":            lib + grammarDep + "\n  .withSources()",
		"dot call on version":         lib + `"a" % "b" % "1".trim`,
		"dot call on config":          lib + grammarDep + ` % Test.extend(Compile)`,
		"Test axis":                   `Test / ` + lib + grammarDep,
		"ThisBuild axis":              `ThisBuild / ` + lib + grammarDep,
		"Global axis":                 `Global / ` + lib + grammarDep,
		"project axis":                "lazy val core = project\ncore / " + lib + grammarDep,
		"legacy in Test":              `libraryDependencies in Test += ` + grammarDep,
		"dot in Test":                 `libraryDependencies.in(Test) += ` + grammarDep,
		"map config":                  seq + `.map(_ % Test)`,
		"map neutral call":            seq + `.map(_.withSources())`,
		"infix map":                   seq + ` map (_ % Test)`,
		"filterNot":                   seq + `.filterNot(_.name == "x")`,
		"toSeq":                       seq + `.toSeq`,
		"distinct":                    seq + `.distinct`,
		"concat":                      seq + ` ++ Seq("c" % "d" % "2")`,
		"concat on next line":         seq + "\n  ++ Seq(\"c\" % \"d\" % \"2\")",
		"typed seq":                   `libraryDependencies ++= Seq[ModuleID](` + grammarDep + `)`,
		"set":                         `libraryDependencies ++= Set(` + grammarDep + `)`,
		"plus-equals seq":             lib + `Seq(` + grammarDep + `)`,
		"assign single element":       `libraryDependencies := ` + grammarDep,
		"parenthesised config":        lib + `(` + grammarDep + `) % Test`,
		"parenthesised element":       lib + `(` + grammarDep + `)`,
		"repeated config":             lib + grammarDep + ` % Test % Provided`,
		"repeated config same":        lib + grammarDep + ` % "test" % "compile"`,
		"empty config":                lib + grammarDep + ` % ""`,
		"separator config":            lib + grammarDep + ` % ","`,
		"custom config ident":         lib + grammarDep + ` % Custom`,
		"variable config":             lib + grammarDep + ` % testConfig`,
		"unknown config string":       lib + grammarDep + ` % "e2e"`,
		"plugin config":               lib + grammarDep + ` % "plugin->default(compile)"`,
		"flag without parens":         lib + grammarDep + ` intransitive`,
		"exclude non-literal":         lib + grammarDep + ` exclude(org, name)`,
		"withRevision infix":          lib + grammarDep + ` withRevision "2"`,
		"from":                        lib + grammarDep + ` from "https://example.invalid/a.jar"`,
		"next line brace":             lib + grammarDep + "\n{ x }",
		"same line junk":              lib + grammarDep + ` foo`,
		"common settings val":         "lazy val testSettings = Seq(\n  " + lib + grammarDep + "\n)\nlazy val core = project.settings(inConfig(Test)(testSettings))",
		"val of a setting":            `lazy val ts = ` + lib + grammarDep,
		"val then newline":            "lazy val ts =\n  " + lib + grammarDep,
		"def of a setting":            `def ts = ` + lib + grammarDep,
		"if condition newline":        "if (sys.env.contains(\"CI\"))\n  " + lib + grammarDep,
		"if then":                     "if (cond) " + lib + grammarDep,
		"settings on non-project":     `lazy val core = other.settings(` + lib + grammarDep + `)`,
		"jvmSettings":                 `lazy val core = crossProject(JVMPlatform).jvmSettings(` + lib + grammarDep + `)`,
		"settings with concat":        `lazy val core = project.settings(commonSettings ++ Seq(` + lib + grammarDep + `))`,
		"settings missing comma":      "lazy val core = project.settings(\n  " + lib + grammarDep + "\n  name := \"x\"\n)",
		"seq element missing comma":   "libraryDependencies ++= Seq(\n  " + grammarDep + "\n  \"c\" % \"d\" % \"2\"\n)",
		"conditional":                 `libraryDependencies ++= (if (cond) Seq(` + grammarDep + `) else Seq())`,
		"called version":              "val v = \"1\"\n" + lib + `"a" % "b" % v("x")`,
		"settings statement on a val": "lazy val core = project\ncore.settings(" + lib + grammarDep + ")",
	}
	for name, source := range cases {
		requireGrammarPartial(t, name, source)
	}
}

// TestGrammarNonLiteralVersionKeepsSiblingsResolved pins the version slot: it
// accepts a string, an interpolated string, or an identifier selection, which
// are all String-typed and cannot touch a configuration. Such a row is
// unresolved; its siblings in the same statement stay resolved.
func TestGrammarNonLiteralVersionKeepsSiblingsResolved(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies ++= Seq(
  "a" % "b" % "1" % Test,
  "c" % "d" % Versions.d,
  "e" % "f" % s"${v}.0",
  "g" % "h" % unknownVersion
)
`)
	row := requireRow(t, payload, "a:b")
	requireField(t, row, "dependency_resolution_state", "resolved")
	requireField(t, row, "dependency_scope", "test")
	for name, key := range map[string]string{"c:d": "Versions.d", "e:f": `s"${v}.0"`, "g:h": "unknownVersion"} {
		dep := requireRow(t, payload, name)
		requireField(t, dep, "dependency_resolution_state", "unresolved")
		assertKeys(t, dep, []string{key})
	}
}

// TestGrammarAssignmentSupersedesEarlierRows pins `:=`, `~=`, `-=` and `--=`:
// they replace or transform what earlier rows of the same project declared, so
// those rows are partial. Rows in another project are untouched.
func TestGrammarAssignmentSupersedesEarlierRows(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, `libraryDependencies += "a" % "b" % "1"
libraryDependencies := Seq("c" % "d" % "2")
libraryDependencies += "e" % "f" % "3"
`)
	requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "partial")
	requireField(t, requireRow(t, payload, "c:d"), "dependency_resolution_state", "resolved")
	requireField(t, requireRow(t, payload, "e:f"), "dependency_resolution_state", "resolved")
	requireStateField(t, stateOf(t, payload), "unresolved_declaration_count", 1)

	transform := parseSource(t, `libraryDependencies += "a" % "b" % "1"
libraryDependencies ~= (_.map(_.withSources()))
`)
	requireField(t, requireRow(t, transform, "a:b"), "dependency_resolution_state", "partial")

	removed := parseSource(t, `libraryDependencies += "a" % "b" % "1"
libraryDependencies --= Seq("x" % "y" % "1")
`)
	requireField(t, requireRow(t, removed, "a:b"), "dependency_resolution_state", "partial")
	if got := len(dependencyRows(t, removed)); got != 1 {
		t.Fatalf("rows = %d, want 1: a removal operand is not a dependency", got)
	}

	projects := parseSource(t, `lazy val core = project.settings(libraryDependencies += "a" % "b" % "1", libraryDependencies := Seq("c" % "d" % "2"))
lazy val util = project.settings(libraryDependencies += "e" % "f" % "3")
lazy val app = project.settings(libraryDependencies := Seq("g" % "h" % "4"))
`)
	requireField(t, requireRow(t, projects, "a:b"), "dependency_resolution_state", "partial")
	for _, name := range []string{"c:d", "e:f", "g:h"} {
		requireField(t, requireRow(t, projects, name), "dependency_resolution_state", "resolved")
	}

	// A bare-file `:=` applies to the root project, which may be any project
	// the file declares, so project rows are partial wherever they appear.
	root := parseSource(t, `lazy val core = project.settings(libraryDependencies += "a" % "b" % "1")
libraryDependencies := Seq("c" % "d" % "2")
`)
	requireField(t, requireRow(t, root, "a:b"), "dependency_resolution_state", "partial")
	requireField(t, requireRow(t, root, "c:d"), "dependency_resolution_state", "resolved")
}

// TestGrammarResetOutsideAContextSupersedesEveryRow covers a reset the
// scanner cannot place: a `:=`, `~=`, `-=` or `--=` in a settings val, a
// wrapper, or under an axis may be applied to any project after its rows, so
// every row in the file is partial.
func TestGrammarResetOutsideAContextSupersedesEveryRow(t *testing.T) {
	t.Parallel()

	for name, source := range map[string]string{
		"settings val assign": "lazy val common = Seq(libraryDependencies := Seq(\"c\" % \"d\" % \"2\"))\nlazy val core = project.settings(libraryDependencies += \"a\" % \"b\" % \"1\", common)",
		"settings val filter": "libraryDependencies += \"a\" % \"b\" % \"1\"\nlazy val common = Seq(libraryDependencies ~= (_.filterNot(_.name == \"b\")))",
		"wrapped removal":     "libraryDependencies += \"a\" % \"b\" % \"1\"\ninConfig(Compile)(libraryDependencies --= Seq(\"a\" % \"b\" % \"1\"))",
	} {
		payload := parseSource(t, source+"\n")
		if state := requireRow(t, payload, "a:b")["dependency_resolution_state"]; state == "resolved" {
			t.Errorf("%s: a:b resolved; a reset the scanner cannot place may supersede it", name)
		}
	}
}

// TestGrammarValFollowedByContinuationIsNotLiteral pins the val rule: a val
// whose literal continues on the next line is not that literal.
func TestGrammarValFollowedByContinuationIsNotLiteral(t *testing.T) {
	t.Parallel()

	for name, val := range map[string]string{
		"leading dot":   "val v = \"1\"\n  .concat(\"-M1\")",
		"leading infix": "val v = \"1\"\n  + \"-M1\"",
		"same line dot": "val v = \"1\".concat(\"-M1\")",
	} {
		payload := parseSource(t, val+"\nlibraryDependencies += \"a\" % \"b\" % v\n")
		row := requireRow(t, payload, "a:b")
		if row["dependency_resolution_state"] == "resolved" {
			t.Errorf("%s: v resolved to %v; the val is not the literal", name, row["value"])
		}
	}
}

// TestGrammarIvyRangesAreNotExactVersions covers every Ivy range spelling.
func TestGrammarIvyRangesAreNotExactVersions(t *testing.T) {
	t.Parallel()

	for _, version := range []string{"[1.0,2.0]", "[1.0,2.0[", "]1.0,2.0]", "]1.0,2.0[", "(,2.0]", "[1.0,)", "1.+", "latest.integration"} {
		payload := parseSource(t, `libraryDependencies += "a" % "b" % "`+version+`"`+"\n")
		if got := requireRow(t, payload, "a:b")["dependency_resolution_state"]; got != "partial" {
			t.Errorf("version %s: state = %v, want partial", version, got)
		}
	}
}

// TestGrammarBinaryHintNeedsScalaTwoTenOrLater pins the `%%` suffix hint:
// Scala before 2.10 cross-built with the full version, so no hint is derived.
func TestGrammarBinaryHintNeedsScalaTwoTenOrLater(t *testing.T) {
	t.Parallel()

	for version, want := range map[string]string{"2.9.2": "", "2.8.1": "", "2.10.7": "o:n_2.10", "2.13.12": "o:n_2.13", "3.3.1": "o:n_3"} {
		payload := parseSource(t, "scalaVersion := \""+version+"\"\nlibraryDependencies += \"o\" %% \"n\" % \"1\"\n")
		got, _ := requireRow(t, payload, "o:n")["sbt_binary_artifact"].(string)
		if got != want {
			t.Errorf("scalaVersion %s: sbt_binary_artifact = %q, want %q", version, got, want)
		}
	}
}
