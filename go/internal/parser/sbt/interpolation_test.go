// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import "testing"

// The interpolated-string scanner must track `${...}` brace depth and the
// strings nested inside it. A nested string holding an unbalanced bracket
// (issue #8980) used to end the opaque string early, so a `}` that is string
// content popped a real opener and the #8938 closer rule marked the rest of
// the indented line as top level.

// TestInterpolationNestedQuoteKeepsBlockClosed is repro 1 of #8980: the `}`
// inside the nested "}" string is not a real closer, so the dependency on
// the rest of the indented line must stay partial.
func TestInterpolationNestedQuoteKeepsBlockClosed(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, "foo {\n  s\"${\"}\"}\"; libraryDependencies += \"a\" % \"b\" % \"1\"; name := \"x\"\n}\n")
	row := requireRow(t, payload, "a:b")
	if row["dependency_resolution_state"] == "resolved" {
		t.Errorf("a:b state = resolved (scope %v), want partial: the } in s\"${\"}\"}\" is string content", row["dependency_scope"])
	}
	if got := stateOf(t, payload)["unresolved_declaration_count"]; got == 0 {
		t.Errorf("unresolved_declaration_count = 0, want at least 1")
	}
}

// TestInterpolationNestedQuoteDoesNotLeakVal is repro 2 of #8980: the val on
// the rest of the indented line is inside the block and must not be tracked,
// so the top-level val wins.
func TestInterpolationNestedQuoteDoesNotLeakVal(t *testing.T) {
	t.Parallel()

	payload := parseSource(t, "foo {\n  s\"${\"}\"}\"; val v = \"0.9\"\n}\nval v = \"1.0\"\nlibraryDependencies += \"a\" % \"b\" % v\n")
	row := requireRow(t, payload, "a:b")
	if row["dependency_resolution_state"] != "resolved" || row["value"] != "1.0" {
		t.Errorf("a:b value/state = %v/%v, want 1.0/resolved: the block-local val leaked", row["value"], row["dependency_resolution_state"])
	}
}

// interpolationBlockProbes wraps a hostile host line in a foo block with a
// dependency probe and a val probe continuing the host's line, then a
// top-level val and dependency. A host that desyncs brackets resolves a:b or
// leaks 0.9 into c:d; with the fix a:b is partial and c:d is 1.0.
func interpolationBlockProbes(host string) string {
	return "foo {\n  " + host + "; libraryDependencies += \"a\" % \"b\" % \"1\"; name := \"x\"\n" +
		"  " + host + "; val v = \"0.9\"\n}\nval v = \"1.0\"\nlibraryDependencies += \"c\" % \"d\" % v\n"
}

// TestInterpolationHolesScanWithDepth covers nested strings, nested
// interpolated strings, and brace characters inside `${...}`.
func TestInterpolationHolesScanWithDepth(t *testing.T) {
	t.Parallel()

	hosts := map[string]string{
		"nested string with close brace": `val h = s"${"}"}"`,
		"dollar ident then hole":         `val h = s"$x${"}"}"`,
		"braces across nested strings":   `val h = s"${if (ok) "}" else "{"}"`,
		"nested interpolated string":     `val h = s"${s"${x}"}"`,
		"comment holding quote":          `val h = s"${f(1 /* " */)}"`,
		"nested triple-quoted string":    "val h = s\"${\"\"\"x\"\"\"}}\"",
		"nested string with open brace":  `val h = s"${"{"}"`,
		"nested string with bracket":     `val h = s"${"["}"`,
		"escaped dollars then hole":      `val h = s"$$${x}"`,
		"plain hole":                     `val h = s"${f(1)}"`,
		"triple-quoted outer":            `val h = s"""${"}"}"""`,
	}
	for name, host := range hosts {
		payload := parseSource(t, interpolationBlockProbes(host))
		if row := requireRow(t, payload, "a:b"); row["dependency_resolution_state"] == "resolved" {
			t.Errorf("%s: a:b resolved inside foo block, want partial", name)
		}
		row := requireRow(t, payload, "c:d")
		if row["dependency_resolution_state"] != "resolved" || row["value"] != "1.0" {
			t.Errorf("%s: c:d value/state = %v/%v, want 1.0/resolved", name, row["value"], row["dependency_resolution_state"])
		}
	}
}

// TestInterpolationUnterminatedHoleFailsClosed pins that a string the
// scanner cannot close fails closed: a hole is a BlockExpr whose newlines
// are significant, so an unclosed hole keeps consuming (nested strings,
// depth and all) until its `}`. The swallowed statements emit no rows and no
// row resolves, so a broken string can never desync the brackets the other
// way into a false resolve.
func TestInterpolationUnterminatedHoleFailsClosed(t *testing.T) {
	t.Parallel()

	sources := map[string]string{
		"unclosed hole":          `val h = s"${x"`,
		"unclosed nested string": `val h = s"${"x}"`,
	}
	for name, host := range sources {
		source := "foo {\n  " + host + "\n  libraryDependencies += \"a\" % \"b\" % \"1\"\n  val v = \"0.9\"\n}\nval v = \"1.0\"\nlibraryDependencies += \"c\" % \"d\" % v\n"
		payload := parseSource(t, source)
		for _, row := range dependencyRows(t, payload) {
			if row["dependency_resolution_state"] == "resolved" {
				t.Errorf("%s: %v resolved, want nothing resolved", name, row["name"])
			}
		}
		if got := stateOf(t, payload)["unresolved_declaration_count"]; got == 0 {
			t.Errorf("%s: unresolved_declaration_count = 0, want at least 1", name)
		}
	}
}

// TestInterpolationMultilineHoleKeepsSync pins that a `${...}` hole may span
// lines: the newlines update the line count and no bracket inside a nested
// string leaks.
func TestInterpolationMultilineHoleKeepsSync(t *testing.T) {
	t.Parallel()

	source := "foo {\n  val h = s\"\"\"${f(\n    \"}\")}\"\"\"\n  libraryDependencies += \"a\" % \"b\" % \"1\"\n  val v = \"0.9\"\n}\nval v = \"1.0\"\nlibraryDependencies += \"c\" % \"d\" % v\n"
	payload := parseSource(t, source)
	if row := requireRow(t, payload, "a:b"); row["dependency_resolution_state"] == "resolved" {
		t.Errorf("a:b resolved inside foo block, want partial")
	}
	row := requireRow(t, payload, "c:d")
	if row["dependency_resolution_state"] != "resolved" || row["value"] != "1.0" || row["line_number"] != 8 {
		t.Errorf("c:d value/state/line = %v/%v/%v, want 1.0/resolved/8", row["value"], row["dependency_resolution_state"], row["line_number"])
	}
}

// TestInterpolationAtTopLevelKeepsFollowingStatements pins the positive
// side: a hostile interpolated string at column 0 neither resolves through
// its opaque value nor desyncs the statements after it.
func TestInterpolationAtTopLevelKeepsFollowingStatements(t *testing.T) {
	t.Parallel()

	sources := map[string]string{
		"nested quote":        "val h = s\"${\"}\"}\";\n",
		"nested interpolated": "val h = s\"${s\"${x}\"}}\";\n",
		"hole with map":       "val h = s\"${Map(1 -> 2)}\";\n",
	}
	for name, host := range sources {
		payload := parseSource(t, host+"val v = \"1.0\"\nlibraryDependencies += \"a\" % \"b\" % v\n")
		row := requireRow(t, payload, "a:b")
		if row["dependency_resolution_state"] != "resolved" || row["value"] != "1.0" {
			t.Errorf("%s: a:b value/state = %v/%v, want 1.0/resolved", name, row["value"], row["dependency_resolution_state"])
		}
	}
}
