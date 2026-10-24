// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// sbt 2.x reads build.sbt with the Scala 3 dialect, where a braceless block
// (a val or def body, an if/then/else branch, a match case, a for or while
// body) is an indented region at bracket depth zero. A statement inside one is
// not a top-level setting, so only a line that starts at column 0 can open a
// bare-file statement or declare a tracked val.

// TestIndentedBlockStatementsArePartial pins that a dependency statement inside
// a Scala 3 braceless block is never resolved.
func TestIndentedBlockStatementsArePartial(t *testing.T) {
	t.Parallel()

	stmt := `libraryDependencies += ` + grammarDep
	cases := map[string]string{
		"if then body":              "if isCI then\n  println(1)\n  " + stmt,
		"else body":                 "if isCI then\n  println(1)\nelse\n  println(2)\n  " + stmt,
		"def body":                  "def deps =\n  println(1)\n  " + stmt,
		"lazy val body":             "lazy val helper =\n  println(1)\n  " + stmt,
		"val body":                  "val helper =\n  " + stmt,
		"match case body":           "isCI match\n  case true =>\n    println(1)\n    " + stmt + "\n  case _ =>\n    println(2)",
		"for do body":               "for i <- 1 to 2 do\n  println(i)\n  " + stmt,
		"while do body":             "while isCI do\n  println(1)\n  " + stmt,
		"indented bare line":        "  " + stmt,
		"tab indented line":         "\t" + stmt,
		"after semicolon in block":  "def deps =\n  println(1); " + stmt,
		"comment before key":        "/* c */ " + stmt,
		"indented project settings": "def mk =\n  val p = project.settings(" + stmt + ")\n  p",
	}
	for name, source := range cases {
		requireGrammarPartial(t, name, source)
	}
}

// TestIndentedIfBranchesBothPartial pins the minimized review case: neither
// branch of a braceless if is a resolved compile dependency.
func TestIndentedIfBranchesBothPartial(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, "if isCI then\n  println(1)\n  libraryDependencies += \"a\" % \"b\" % \"1\"\n"+
		"else\n  println(2)\n  libraryDependencies += \"c\" % \"d\" % \"1\"\n")
	for _, name := range []string{"a:b", "c:d"} {
		requireField(t, requireRow(t, payload, name), "dependency_resolution_state", "partial")
	}
}

// TestIndentedValDoesNotShadowTopLevel pins that a val declared inside an
// indented block is not tracked, so a top-level reference resolves to the
// top-level value even when the block-local val comes first.
func TestIndentedValDoesNotShadowTopLevel(t *testing.T) {
	t.Parallel()

	sources := map[string]string{
		"lazy val block": "lazy val helper =\n  val v = \"0.9\"\n  v\nval v = \"1.0\"\nlibraryDependencies += \"a\" % \"b\" % v\n",
		"def block":      "def helper =\n  val v = \"0.9\"\n  v\nval v = \"1.0\"\nlibraryDependencies += \"a\" % \"b\" % v\n",
		"if block":       "if isCI then\n  val v = \"0.9\"\n  println(v)\nval v = \"1.0\"\nlibraryDependencies += \"a\" % \"b\" % v\n",
	}
	for name, source := range sources {
		row := requireRow(t, parseSource(t, source), "a:b")
		if row["value"] != "1.0" || row["dependency_resolution_state"] != "resolved" {
			t.Errorf("%s: value/state = %v/%v, want 1.0/resolved", name, row["value"], row["dependency_resolution_state"])
		}
	}
}

// TestIndentedValIsNotTracked pins that a block-local val alone does not
// resolve a top-level reference.
func TestIndentedValIsNotTracked(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, "lazy val helper =\n  val v = \"0.9\"\n  v\nlibraryDependencies += \"a\" % \"b\" % v\n")
	row := requireRow(t, payload, "a:b")
	if row["dependency_resolution_state"] == "resolved" {
		t.Fatalf("a:b state = resolved with value %v, want an unresolved reference", row["value"])
	}
}

// TestColumnZeroFormsStillResolve pins the column-zero forms that stay in the
// grammar: modifier vals, a braceless project whose settings continue on
// indented lines, a statement after ';', and parenthesised continuations.
func TestColumnZeroFormsStillResolve(t *testing.T) {
	t.Parallel()

	stmt := `libraryDependencies += ` + grammarDep
	cases := map[string]string{
		"braceless project settings": "lazy val x = project\n  .settings(\n    " + stmt + "\n  )",
		"project on next line":       "lazy val x =\n  project.settings(\n    " + stmt + "\n  )",
		"after semicolon":            `name := "x"; ` + stmt,
		"collection continues":       "libraryDependencies ++= Seq(\n  " + grammarDep + "\n)",
		"after indented block":       "def helper =\n  println(1)\n" + stmt,
	}
	for name, source := range cases {
		requireGrammarResolved(t, name, source, "compile")
	}

	vals := map[string]string{
		"lazy":            "lazy val v = \"1\"",
		"private lazy":    "private lazy val v = \"1\"",
		"final":           "final val v = \"1\"",
		"override":        "override val v = \"1\"",
		"implicit":        "implicit val v = \"1\"",
		"inline":          "inline val v = \"1\"",
		"after semicolon": "name := \"x\"; val v = \"1\"",
	}
	for name, decl := range vals {
		row := requireRow(t, parseSource(t, decl+"\nlibraryDependencies += \"a\" % \"b\" % v\n"), "a:b")
		if row["value"] != "1" || row["dependency_resolution_state"] != "resolved" {
			t.Errorf("%s val: value/state = %v/%v, want 1/resolved", name, row["value"], row["dependency_resolution_state"])
		}
	}
}
