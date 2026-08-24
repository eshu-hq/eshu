// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"go/ast"
	"go/parser"
	"go/token"
	"testing"
)

// TestPackageLevelTemplateShapesAreVisibleToTheCypherScan holds
// collectStringValues to the declaration shapes a Cypher template is actually
// written in (#6181).
//
// TestDirectMaterializedEdgePortsMatchTheExecutedCypher claims something total:
// EVERY reducer port reaching a relationship MERGE is a declared direct family.
// That claim is only as wide as the set of declarations the scan can resolve,
// and the scan resolved a package-level const or var only when it was a bare
// string literal or a `+` concatenation. A template grouped into a
// `map[string]string{...}`, a slice, or a struct field — which is how templates
// in go/internal/storage/cypher are routinely grouped — was invisible, so a
// port merging a relationship through one passed.
//
// This test is over the shapes rather than over the production package on
// purpose. The production tree happens to hold no MERGE in a composite literal
// today, so a fixture derived from it would pass with the hole open; that is
// exactly how the hole survived. Each case here is a shape a future writer may
// use, and the negative case keeps the resolver from being widened into
// something that reports text where there is none.
func TestPackageLevelTemplateShapesAreVisibleToTheCypherScan(t *testing.T) {
	t.Parallel()

	const merge = "MERGE (a)-[rel:REVIEW_PROBE_FLOWS_TO]->(b)"

	for _, tc := range []struct {
		name string
		decl string
		want bool
	}{
		{
			name: "bare const",
			decl: "const tmpl = `" + merge + "`",
			want: true,
		},
		{
			name: "concatenation",
			decl: "const tmpl = \"MERGE (a)-\" + \"[rel:REVIEW_PROBE_FLOWS_TO]->(b)\"",
			want: true,
		},
		{
			name: "map literal",
			decl: "var tmpl = map[string]string{\"upsert\": `" + merge + "`}",
			want: true,
		},
		{
			name: "slice literal",
			decl: "var tmpl = []string{`" + merge + "`}",
			want: true,
		},
		{
			name: "struct literal field",
			decl: "var tmpl = struct{ Upsert string }{Upsert: `" + merge + "`}",
			want: true,
		},
		{
			name: "slice of structs",
			decl: "var tmpl = []struct{ Upsert string }{{Upsert: `" + merge + "`}}",
			want: true,
		},
		{
			name: "map key",
			decl: "var tmpl = map[string]int{`" + merge + "`: 1}",
			want: true,
		},
		{
			name: "composite literal with no string text",
			decl: "var tmpl = map[string]int{}",
			want: false,
		},
		{
			name: "non-string const",
			decl: "const tmpl = 3",
			want: false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			values := stringValuesFromSource(t, tc.decl)
			value, resolved := values["tmpl"]
			if !tc.want {
				if resolved {
					if _, merges := relationshipMergeLine(value); merges {
						t.Fatalf("%s resolved to relationship-merging text %q; the resolver invented Cypher that is not there", tc.name, value)
					}
				}
				return
			}
			if !resolved {
				t.Fatalf("collectStringValues did not resolve tmpl from %q; a MERGE written this way is invisible to the scan and the port that reaches it passes undeclared", tc.decl)
			}
			line, merges := relationshipMergeLine(value)
			if !merges {
				t.Fatalf("tmpl resolved to %q, which relationshipMergeLine does not read as a relationship MERGE", value)
			}
			if line != merge {
				t.Errorf("evidence line is %q, want %q; a joined-together value reports a run-on instead of the real source line", line, merge)
			}
		})
	}
}

// stringValuesFromSource parses decl as a package-level declaration and returns
// what collectStringValues resolves from it.
func stringValuesFromSource(t *testing.T, decl string) map[string]string {
	t.Helper()

	file, err := parser.ParseFile(token.NewFileSet(), "shape.go", "package cypher\n\n"+decl+"\n", parser.SkipObjectResolution)
	if err != nil {
		t.Fatalf("parse %q: %v", decl, err)
	}
	out := map[string]string{}
	for _, d := range file.Decls {
		gen, ok := d.(*ast.GenDecl)
		if !ok {
			continue
		}
		collectStringValues(gen, out)
	}
	return out
}

// TestRelationshipMergeReadsNestedNodePatternParens holds the
// relationship-MERGE reader to node patterns that contain parentheses of their
// own (#6181).
//
// The reader matched a node pattern with `[^()]*`, so the first `(` INSIDE the
// pattern ended the match early and the whole clause stopped looking like a
// relationship merge. `MERGE (n:Label {id: coalesce($a, $b)})-[r:TYPE]->(m)` is
// ordinary Cypher — a merge keyed on a coalesced identity — and it read as
// node-only. A port whose only write site is such a template is then classified
// node-only, its family never enters the enumeration, and the ledger has no row
// to be missing: the false-green direction the whole guard exists to prevent,
// reproduced inside it.
//
// No production template is written this way today, which is exactly why the
// case has to be stated here rather than derived from the tree: a fixture taken
// from the tree passes with the hole open. The negative cases below are the
// other half — widening the reader until it reports a relationship merge in a
// MATCH, or in a relationship-index DDL, would trade a silent miss for a loud
// wrong answer on every retract port in the package.
func TestRelationshipMergeReadsNestedNodePatternParens(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		cypher string
		want   bool
	}{
		{
			name:   "flat node pattern",
			cypher: "MERGE (a:Thing)-[rel:REVIEW_PROBE_FLOWS_TO]->(b:Thing)",
			want:   true,
		},
		{
			name:   "function call in the node pattern",
			cypher: "MERGE (n:Label {id: coalesce($a, $b)})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:   true,
		},
		{
			name:   "nested function calls in the node pattern",
			cypher: "MERGE (n:Label {id: coalesce(toString($a), $b)})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:   true,
		},
		{
			name:   "left-pointing relationship after a call",
			cypher: "MERGE (n:Label {id: coalesce($a, $b)})<-[rel:REVIEW_PROBE_FLOWS_TO]-(m)",
			want:   true,
		},
		{
			name:   "CREATE with a call in the node pattern",
			cypher: "CREATE (n {id: coalesce($a, $b)})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:   true,
		},
		{
			name:   "node-only merge whose pattern calls a function",
			cypher: "MERGE (n:Label {id: coalesce($a, $b)})",
			want:   false,
		},
		{
			name:   "MATCH is not a merge",
			cypher: "MATCH (n:Label {id: coalesce($a, $b)})-[rel:REVIEW_PROBE_FLOWS_TO]->(m) DELETE rel",
			want:   false,
		},
		{
			name:   "relationship index DDL is not a merge",
			cypher: "CREATE INDEX probe_rel IF NOT EXISTS FOR ()-[rel:REVIEW_PROBE_FLOWS_TO]-() ON (rel.id)",
			want:   false,
		},
		{
			name:   "unbalanced node pattern resolves nothing",
			cypher: "MERGE (n:Label {id: coalesce($a, $b)-[rel:REVIEW_PROBE_FLOWS_TO]->(m)",
			want:   false,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			line, merges := relationshipMergeLine(tc.cypher)
			if merges != tc.want {
				t.Fatalf("relationshipMergeLine(%q) merges = %t, want %t", tc.cypher, merges, tc.want)
			}
			if tc.want && line != tc.cypher {
				t.Errorf("evidence line = %q, want %q", line, tc.cypher)
			}
		})
	}
}

// TestNestedNodePatternMergeIsVisibleThroughAPortDeclaration is the same hole
// stated end to end: a package-level template written the way the case above
// describes has to reach the scan's classification, not merely the line reader.
func TestNestedNodePatternMergeIsVisibleThroughAPortDeclaration(t *testing.T) {
	t.Parallel()

	const decl = "const tmpl = `MERGE (n:Label {id: coalesce($a, $b)})-[rel:REVIEW_PROBE_FLOWS_TO]->(m)`"
	values := stringValuesFromSource(t, decl)
	value, resolved := values["tmpl"]
	if !resolved {
		t.Fatalf("collectStringValues did not resolve tmpl from %q", decl)
	}
	if _, merges := relationshipMergeLine(value); !merges {
		t.Errorf("a port reaching %q is classified node-only, so its family never enters the enumeration and no ledger row is ever missing for it", value)
	}
}
