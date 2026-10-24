// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package sbt

import (
	"strings"
	"testing"
)

func TestParseBoundsHundredKilobyteSingleLine(t *testing.T) {
	t.Parallel()

	// 100 KB of one line that never forms a declaration.
	noise := parseSource(t, strings.Repeat("a % b ", 17_000)+"\n")
	if rows := dependencyRows(t, noise); len(rows) != 0 {
		t.Fatalf("noise rows = %d, want 0", len(rows))
	}

	// 100 KB single string literal as a coordinate field: not a coordinate.
	long := parseSource(t, `libraryDependencies += "`+strings.Repeat("x", 100_000)+`" % "n" % "1"`+"\n")
	if rows := dependencyRows(t, long); len(rows) != 0 {
		t.Fatalf("oversized org produced rows: %d", len(rows))
	}
	requireStateField(t, stateOf(t, long), "unresolved_declaration_count", 1)

	// A single line holding many valid declarations in one Seq is parsed.
	var line strings.Builder
	line.WriteString("libraryDependencies ++= Seq(")
	for index := 0; index < 1500; index++ {
		if index > 0 {
			line.WriteString(", ")
		}
		line.WriteString(`"o" % "n` + itoa(index) + `" % "1"`)
	}
	line.WriteString(")\n")
	wide := parseSource(t, line.String())
	if rows := dependencyRows(t, wide); len(rows) != 1500 {
		t.Fatalf("wide Seq rows = %d, want 1500", len(rows))
	}
}

func TestParseBoundsDeepParentheses(t *testing.T) {
	t.Parallel()

	depth := 200_000
	deep := parseSource(t, `libraryDependencies += `+strings.Repeat("(", depth)+`"a" % "b" % "1"`+strings.Repeat(")", depth)+"\n")
	if got := stateOf(t, deep)["state"]; got != "parsed" {
		t.Fatalf("state = %#v, want parsed", got)
	}
	if rows := dependencyRows(t, deep); len(rows) > 1 {
		t.Fatalf("deep parens rows = %d, want at most 1", len(rows))
	}

	unclosed := parseSource(t, `libraryDependencies ++= Seq(`+strings.Repeat("(", depth)+"\n")
	if rows := dependencyRows(t, unclosed); len(rows) != 0 {
		t.Fatalf("unclosed rows = %d, want 0", len(rows))
	}
}

func TestParseCapsRowsOnHundredThousandDependencyLines(t *testing.T) {
	t.Parallel()

	const total = 100_000
	var source strings.Builder
	for index := 0; index < total; index++ {
		source.WriteString(`libraryDependencies += "org" % "n` + itoa(index) + `" % "1.0"` + "\n")
	}
	payload := parseSource(t, source.String())
	rows := dependencyRows(t, payload)
	if len(rows) != MaxDependencyRows {
		t.Fatalf("rows = %d, want cap %d", len(rows), MaxDependencyRows)
	}
	state := stateOf(t, payload)
	requireStateField(t, state, "dependency_count", MaxDependencyRows)
	requireStateField(t, state, "dependency_overflow_count", total-MaxDependencyRows)
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	digits := make([]byte, 0, 8)
	for value > 0 {
		digits = append([]byte{byte('0' + value%10)}, digits...)
		value /= 10
	}
	return string(digits)
}

// TestParseBoundsLongCollectionCallChains drives the trailing-call loops with
// inputs that would be quadratic or unbounded if a loop re-read tokens.
func TestParseBoundsLongCollectionCallChains(t *testing.T) {
	t.Parallel()

	const calls = 50_000
	maps := parseSource(t, `libraryDependencies ++= Seq("a" % "b" % "1")`+strings.Repeat(".map(_.withSources())", calls)+".map(_ % Test)\n")
	requireField(t, requireRow(t, maps, "a:b"), "dependency_scope", "test")

	concat := parseSource(t, `libraryDependencies ++= Seq("a" % "b" % "1")`+strings.Repeat(" ++ Seq()", calls)+"\n")
	if rows := dependencyRows(t, concat); len(rows) != 1 {
		t.Fatalf("concat rows = %d, want 1", len(rows))
	}

	dots := parseSource(t, `libraryDependencies ++= Seq("a" % "b" % "1")`+strings.Repeat(".", calls)+"\n")
	if rows := dependencyRows(t, dots); len(rows) != 1 {
		t.Fatalf("dots rows = %d, want 1", len(rows))
	}

	// A 10,000-token lambda body is read once and never kept.
	body := parseSource(t, `libraryDependencies ++= Seq("a" % "b" % "1").map(`+strings.Repeat("x % ", 10_000)+"y)\n")
	requireField(t, requireRow(t, body, "a:b"), "dependency_resolution_state", "partial")
}
