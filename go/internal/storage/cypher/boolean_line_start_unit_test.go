// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"go/parser"
	"go/token"
	"slices"
	"testing"
)

// TestHasLineLedBooleanOperator is the seeded RED/GREEN proof for the X4
// guard: every planted violation must be flagged and every correct or
// non-Cypher form must pass.
func TestHasLineLedBooleanOperator(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name  string
		value string
		want  bool
	}{
		{name: "AND at column 0", value: "MATCH (n:Workload) WHERE n.id = $x\nAND n.repo_id = $r", want: true},
		{name: "gofmt tab-indented AND", value: "MATCH (n:Workload) WHERE n.id = $x\n\t\t\tAND n.repo_id = $r", want: true},
		{name: "OR after a tab", value: "MATCH (n) WHERE n.id = $x\n\tOR n.id = $y", want: true},
		{name: "XOR after a newline", value: "MATCH (n) WHERE n.a = $x\nXOR n.b = $y", want: true},
		{name: "space then tab before AND", value: "MATCH (n) WHERE n.id = $x\n \tAND n.k = $r", want: true},
		{name: "fragment with a named parameter", value: "\n\t\t\tAND (w.repo_id IN $allowed_repository_ids)", want: true},
		{name: "space-indented AND", value: "MATCH (n:Workload) WHERE n.id = $x\n  AND n.repo_id = $r", want: false},
		{name: "tab then space before AND", value: "MATCH (n) WHERE n.id = $x\n\t AND n.k = $r", want: false},
		{name: "one line", value: "MATCH (n) WHERE n.id = $x AND n.k = $r", want: false},
		{name: "ORDER BY after a tab is not OR", value: "MATCH (n) RETURN n.id\n\tORDER BY n.id", want: false},
		{name: "Postgres SQL with positional params", value: "SELECT 1 FROM t\nWHERE a = $1\n\tOR b = $2", want: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			if got := hasLineLedBooleanOperator(tc.value); got != tc.want {
				t.Fatalf("hasLineLedBooleanOperator(%q) = %v, want %v", tc.value, got, tc.want)
			}
		})
	}
}

// TestScanFileLineLedBooleanHitsFoldsConcatenation proves the scan reports
// the right line for a planted raw-string violation and for one only visible
// after folding a "+" chain, and stays silent on a clean file.
func TestScanFileLineLedBooleanHitsFoldsConcatenation(t *testing.T) {
	t.Parallel()

	const src = "package fixture\n" +
		"\n" +
		"const clean = `MATCH (n) WHERE n.id = $x\n  AND n.k = $r`\n" +
		"\n" +
		"const raw = `MATCH (n) WHERE n.id = $x\n\tAND n.k = $r`\n" +
		"\n" +
		"var folded = \"MATCH (n) WHERE n.id = $x\\n\" + \"\\tAND n.k = $r\"\n"
	fset := token.NewFileSet()
	file, err := parser.ParseFile(fset, "fixture.go", src, 0)
	if err != nil {
		t.Fatalf("parse fixture: %v", err)
	}
	got := scanFileLineLedBooleanHits(fset, "fixture.go", file)
	want := []string{"fixture.go:6", "fixture.go:9"}
	if !slices.Equal(got, want) {
		t.Fatalf("scanFileLineLedBooleanHits = %v, want %v", got, want)
	}
}
