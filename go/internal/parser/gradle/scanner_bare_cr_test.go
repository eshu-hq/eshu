// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package gradle

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/parser/shared"
)

// TestStripCommentsEndsLineCommentAtBareCR is the #6268 regression for the
// Gradle scanner. A classic-Mac build script separates lines with a bare '\r'
// and carries no '\n', so a `//` scan that only stops at '\n' erases the rest
// of the file -- every dependency after the first comment silently disappears
// instead of failing loudly.
func TestStripCommentsEndsLineCommentAtBareCR(t *testing.T) {
	t.Parallel()

	source := "dependencies {\r// pinned by the platform team\rimplementation 'com.example:lib:1.2.3'\r}\r"
	stripped := stripCommentsAndStringInteriorsKept(source)
	if !strings.Contains(stripped, "com.example:lib:1.2.3") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(%q) = %q, want the declaration after the comment kept", source, stripped)
	}
	if strings.Contains(stripped, "pinned by the platform team") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(%q) = %q, want the comment body removed", source, stripped)
	}
	if got := strings.Count(stripped, "\r"); got != 4 {
		t.Fatalf("stripCommentsAndStringInteriorsKept(%q) kept %d '\\r' bytes, want 4: the terminator belongs to the line, not the comment", source, got)
	}
}

// TestStripCommentsKeepsCRLFSourceIntact is the control: CRLF build scripts
// already worked, so this stays green on both sides of the #6268 fix.
func TestStripCommentsKeepsCRLFSourceIntact(t *testing.T) {
	t.Parallel()

	source := "dependencies {\r\n// pinned by the platform team\r\nimplementation 'com.example:lib:1.2.3'\r\n}\r\n"
	stripped := stripCommentsAndStringInteriorsKept(source)
	if !strings.Contains(stripped, "com.example:lib:1.2.3") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(CRLF) = %q, want the declaration kept", stripped)
	}
	if strings.Contains(stripped, "pinned by the platform team") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(CRLF) = %q, want the comment body removed", stripped)
	}
}

// TestUnclosedStringTerminatesAtBareCR pins the sibling of the comment scan in
// the same function. copyStringLiteral's doc promises an unclosed single-line
// string ends "at the newline so subsequent lines parse cleanly rather than
// getting absorbed into a runaway string"; under bare-CR line endings that
// promise only holds once '\r' counts as a newline too (#6268).
//
// String interiors are copied verbatim, so a runaway string does not delete
// text -- it re-labels it. The observable damage is that everything it
// swallows stops being scanned, which is why this asserts on a comment that
// must still be stripped on the line after the unclosed quote rather than on
// the declaration text itself.
func TestUnclosedStringTerminatesAtBareCR(t *testing.T) {
	t.Parallel()

	source := "def a = 'unterminated\rdef b = 'x' // must still be stripped\r"
	stripped := stripCommentsAndStringInteriorsKept(source)
	if strings.Contains(stripped, "must still be stripped") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(%q) = %q, want the later comment stripped rather than swallowed by a runaway string", source, stripped)
	}
	if !strings.Contains(stripped, "def b = 'x'") {
		t.Fatalf("stripCommentsAndStringInteriorsKept(%q) = %q, want the line after the unclosed quote kept", source, stripped)
	}
}

// bareCRBuildGradle is a classic-Mac build script: every line break is a bare
// '\r' and there is no '\n' anywhere. The `dependencies` block is not the
// first block, and it holds three declarations after a comment -- the shape
// that proves the fix reaches Parse. collectBlocks advanced to the next line
// only via '\n', so it returned at the first non-header CR line and never
// found `dependencies` at all; splitDependencyStatements flushed only on '\n'
// or ';', so even a found block glued all three declarations into one
// statement (#6268).
const bareCRBuildGradle = "plugins { id 'java' }\rdependencies {\r// pinned by the platform team\rimplementation 'com.example:lib:1.2.3'\rimplementation 'com.example:other:4.5.6'\rapi 'com.example:third:7.8.9'\r}\r"

// crlfBuildGradle is the same script with CRLF endings: the control that
// proves the terminator change did not alter ordinary Windows checkouts.
const crlfBuildGradle = "plugins { id 'java' }\r\ndependencies {\r\n// pinned by the platform team\r\nimplementation 'com.example:lib:1.2.3'\r\nimplementation 'com.example:other:4.5.6'\r\napi 'com.example:third:7.8.9'\r\n}\r\n"

// assertGradleParsesEveryDependency drives the production Parse entry point
// and requires all three declarations, each with its own configuration. A
// single-dependency fixture cannot tell "the block was found" apart from
// "the block was found and its statements were split", which is why both the
// bare-CR case and its CRLF control run the same three-row assertion.
func assertGradleParsesEveryDependency(t *testing.T, name string, body string) {
	t.Helper()

	payload, err := Parse(writeFixture(t, name, body), false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	want := map[string]string{
		"com.example:lib":   "implementation",
		"com.example:other": "implementation",
		"com.example:third": "api",
	}
	rows, ok := payload["variables"].([]map[string]any)
	if !ok {
		t.Fatalf("variables = %T, want []map[string]any", payload["variables"])
	}
	got := make(map[string]string, len(rows))
	for _, row := range rows {
		rowName, _ := row["name"].(string)
		section, _ := row["section"].(string)
		got[rowName] = section
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse(%q) dependency rows = %#v, want %#v", body, got, want)
	}
}

// TestParseBareCRBuildGradleEmitsEveryDependency is the end-to-end regression
// the stripping-helper test could not catch.
func TestParseBareCRBuildGradleEmitsEveryDependency(t *testing.T) {
	t.Parallel()

	assertGradleParsesEveryDependency(t, "build.gradle", bareCRBuildGradle)
}

// TestParseCRLFBuildGradleEmitsEveryDependency is its control.
func TestParseCRLFBuildGradleEmitsEveryDependency(t *testing.T) {
	t.Parallel()

	assertGradleParsesEveryDependency(t, "build.gradle", crlfBuildGradle)
}

// TestStripCommentsCRLFBytesAreExact replaces the PR's unproven "the same
// bytes come out" claim for the Gradle scanner. The '\r' of a CRLF line
// comment used to be swallowed and is now emitted, so the stripped result
// differs by that byte. It is whitespace to every downstream regex, but the
// old "content found + comment removed" control could not have noticed
// either way.
func TestStripCommentsCRLFBytesAreExact(t *testing.T) {
	t.Parallel()

	source := "dependencies {\r\n// pinned by the platform team\r\nimplementation 'com.example:lib:1.2.3'\r\n}\r\n"
	want := "dependencies {\r\n\r\nimplementation 'com.example:lib:1.2.3'\r\n}\r\n"
	if got := stripCommentsAndStringInteriorsKept(source); got != want {
		t.Fatalf("stripCommentsAndStringInteriorsKept(CRLF) = %q, want %q", got, want)
	}
}

// TestParseBareCRBuildGradleRecoversFromUnclosedString covers the third
// bare-CR condition in splitDependencyStatements: the unclosed-string
// recovery branch. stripCommentsAndStringInteriorsKept leaves the dangling
// quote in place, so without '\r' ending the runaway string the *next*
// line's opening quote is mistaken for its closing quote and every following
// declaration is mis-split. The first mutation run proved the multi-
// dependency fixture alone does not reach this branch, so it gets its own
// fixture (#6268).
func TestParseBareCRBuildGradleRecoversFromUnclosedString(t *testing.T) {
	t.Parallel()

	body := "dependencies {\rimplementation 'com.example:broken\rimplementation 'com.example:after:1.0.0'\rapi 'com.example:last:2.0.0'\r}\r"
	payload, err := Parse(writeFixture(t, "build.gradle", body), false, shared.Options{})
	if err != nil {
		t.Fatalf("Parse() error = %v, want nil", err)
	}
	rows, _ := payload["variables"].([]map[string]any)
	got := make(map[string]string, len(rows))
	for _, row := range rows {
		name, _ := row["name"].(string)
		section, _ := row["section"].(string)
		got[name] = section
	}
	want := map[string]string{
		"com.example:after": "implementation",
		"com.example:last":  "api",
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("Parse(%q) dependency rows = %#v, want %#v: the declarations after an unclosed quote must still split on the bare CR", body, got, want)
	}
}
