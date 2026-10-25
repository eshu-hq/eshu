// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// requireNotResolved asserts that no row named name is resolved. A row read
// with an opaque revision is unresolved; when no row is emitted at all (an
// opaque organization or name), the file must count the declaration.
func requireNotResolved(t *testing.T, label, source, name string) {
	t.Helper()
	payload := parseSource(t, source)
	found := false
	for _, row := range dependencyRows(t, payload) {
		if row["name"] != name {
			continue
		}
		found = true
		if row["dependency_resolution_state"] == "resolved" {
			t.Errorf("%s: %s is resolved (scope %v), want partial or unresolved", label, name, row["dependency_scope"])
		}
	}
	if got, _ := stateOf(t, payload)["unresolved_declaration_count"].(int); !found && got < 1 {
		t.Errorf("%s: no %s row and unresolved_declaration_count = %v, want the declaration counted", label, name, got)
	}
}

// TestEscapedStringsAreOpaque pins that a string holding a backslash is never
// read as a literal coordinate. sbt 1 compiles build.sbt with Scala 2.12, which
// processes unicode escapes before lexing, even inside triple-quoted strings,
// so a triple-quoted organization that spells its dot as a unicode escape
// names org.example there; Scala 2.13.2 and later keep the escape verbatim.
// The scanner cannot know which one runs.
func TestEscapedStringsAreOpaque(t *testing.T) {
	t.Parallel()

	cases := map[string]struct{ source, name string }{
		"triple-quoted unicode org":     {"libraryDependencies += \"\"\"org\\u002Eexample\"\"\" % \"b\" % \"1\"\n", "org\\u002Eexample:b"},
		"triple-quoted backslash name":  {"libraryDependencies += \"a\" % \"\"\"b\\n\"\"\" % \"1\"\n", `a:b\n`},
		"triple-quoted unicode version": {"libraryDependencies += \"a\" % \"b\" % \"\"\"1\\u002E0\"\"\"\n", "a:b"},
		"plain unicode org":             {"libraryDependencies += \"org\\u002Eexample\" % \"b\" % \"1\"\n", "org\\u002Eexample:b"},
		"plain unicode version":         {"libraryDependencies += \"a\" % \"b\" % \"1\\u002E0\"\n", "a:b"},
		"triple-quoted val":             {"val v = \"\"\"1\\u002E0\"\"\"\nlibraryDependencies += \"a\" % \"b\" % v\n", "a:b"},
	}
	for label, tc := range cases {
		requireNotResolved(t, label, tc.source, tc.name)
	}

	requireGrammarResolved(t, "triple-quoted plain", `libraryDependencies += """a""" % "b" % "1"`, "compile")
}

// TestConfigurationStringsAreCaseSensitive pins that a configuration string
// matches an sbt configuration name exactly. Ivy configuration names are case
// sensitive and sbt names its standard ones in lower case (Test is
// config("test"), IntegrationTest is config("it")), so `% "Test"` names no
// standard configuration.
func TestConfigurationStringsAreCaseSensitive(t *testing.T) {
	t.Parallel()

	for _, spelling := range []string{`"Test"`, `"TEST"`, `"Compile"`, `"Provided"`, `"Runtime"`, `"IT"`, `"integrationtest"`, `"Test->default"`, `"it,Test"`, `"Optional"`} {
		requireGrammarPartial(t, spelling, `libraryDependencies += `+grammarDep+` % `+spelling)
	}

	resolved := map[string]string{
		`"test"`:          "test",
		`"it"`:            "test",
		`"compile"`:       "compile",
		`"provided"`:      "provided",
		`"runtime"`:       "runtime",
		`"optional"`:      "compile",
		`Test`:            "test",
		`IntegrationTest`: "test",
		`Compile`:         "compile",
		`Provided`:        "provided",
		`Runtime`:         "runtime",
		`Optional`:        "compile",
	}
	for spelling, scope := range resolved {
		requireGrammarResolved(t, spelling, `libraryDependencies += `+grammarDep+` % `+spelling, scope)
	}

	row := requireRow(t, parseSource(t, `libraryDependencies += `+grammarDep+" % Optional\n"), "a:b")
	requireField(t, row, "sbt_configuration", "Optional")
	requireField(t, row, "dependency_optional", true)
	row = requireRow(t, parseSource(t, `libraryDependencies += `+grammarDep+" % IntegrationTest\n"), "a:b")
	requireField(t, row, "sbt_configuration", "IntegrationTest")
}

// TestKeyMethodCallsSupersede pins that applying a setting method to the key
// (`libraryDependencies.transform(...)`, `.set(...)`) supersedes earlier rows
// the way `~=` does, while a read such as `.value` does not.
func TestKeyMethodCallsSupersede(t *testing.T) {
	t.Parallel()

	base := `libraryDependencies += ` + grammarDep + "\n"
	supersede := map[string]string{
		"transform":          base + "libraryDependencies.transform(_.filterNot(_.name == \"b\"), NoPosition)\n",
		"set":                base + "libraryDependencies.set(Def.value(Nil), NoPosition)\n",
		"transform in block": base + "def drop =\n  libraryDependencies.transform(identity, NoPosition)\n",
		"settings transform": "lazy val core = project.settings(\n  " + base + "  , libraryDependencies.transform(identity, NoPosition)\n)\n",
	}
	for name, source := range supersede {
		payload := parseSource(t, source)
		requireField(t, requireRow(t, payload, "a:b"), "dependency_resolution_state", "partial")
		if got := stateOf(t, payload)["unresolved_declaration_count"]; got == 0 {
			t.Errorf("%s: unresolved_declaration_count = 0, want the call counted", name)
		}
	}

	reads := map[string]string{
		"value read":    base + "name := libraryDependencies.value.size.toString\n",
		"value in task": base + "foo := { println(libraryDependencies.value) }\n",
		"key read":      base + "val k = libraryDependencies.key\n",
	}
	for name, source := range reads {
		row := requireRow(t, parseSource(t, source), "a:b")
		if row["dependency_resolution_state"] != "resolved" {
			t.Errorf("%s: a:b state = %v, want resolved", name, row["dependency_resolution_state"])
		}
	}
}
