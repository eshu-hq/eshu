// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"runtime"
	"sort"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/graph/edgetype"
)

// TestExtractRelationshipTypeTokens is a synthetic-input unit test for the
// tokenizer, independent of production Cypher, covering every relationship
// pattern shape the #5335 edge-materialization gate must handle: anonymous
// and bound relationship variables, either arrow direction, a pipe-separated
// type union, and a variable-length quantifier. It also proves node labels
// (parenthesized, never bracketed) are never extracted.
func TestExtractRelationshipTypeTokens(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		cypher string
		want   []string
	}{
		{"anonymous forward", `MATCH (a)-[:DEPENDS_ON]->(b) RETURN a`, []string{"DEPENDS_ON"}},
		{"anonymous backward", `MATCH (a)<-[:DEPENDS_ON]-(b) RETURN a`, []string{"DEPENDS_ON"}},
		{"bound variable", `MATCH (a)-[r:CONTAINS]->(b) RETURN r`, []string{"CONTAINS"}},
		{"pipe union", `MATCH (a)-[:A|B|C]->(b) RETURN a`, []string{"A", "B", "C"}},
		{"variable length", `MATCH (a)<-[:DEPENDS_ON*1..5]-(b) RETURN a`, []string{"DEPENDS_ON"}},
		{"bound variable-length pipe", `MATCH (a)-[r:A|B*1..3]->(b) RETURN r`, []string{"A", "B"}},
		{"multiple patterns", `MATCH (a)-[:CONTAINS]->(b)-[:INDEXES]->(c) RETURN a`, []string{"CONTAINS", "INDEXES"}},
		{"node label only, no relationship", `MATCH (a:Repository) RETURN a`, nil},
		{"no match", `RETURN 1`, nil},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			got := extractRelationshipTypeTokens(tc.cypher)
			if len(got) != len(tc.want) {
				t.Fatalf("extractRelationshipTypeTokens(%q) = %v, want %v", tc.cypher, got, tc.want)
			}
			for i, w := range tc.want {
				if got[i] != w {
					t.Errorf("extractRelationshipTypeTokens(%q)[%d] = %q, want %q", tc.cypher, i, got[i], w)
				}
			}
		})
	}
}

// impactGateSourceFile resolves impact_blast_radius.go's path from this test
// file's own directory (both live in go/internal/query).
func impactGateSourceFile(t *testing.T) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("runtime.Caller(0) failed")
	}
	return filepath.Join(filepath.Dir(file), "impact_blast_radius.go")
}

// TestImpactBlastRadiusGateQueriesAreLiteralConstants is the #5335
// "LITERAL-ONLY discipline" anti-false-green mitigation: it AST-parses
// impact_blast_radius.go and asserts every impactBlastRadiusGateQueries
// ConstName is declared as `const NAME = "<string literal>"` (Go's own const
// semantics already forbid a non-constant expression like fmt.Sprintf
// there). A tracked name that is missing from a const decl -- because it was
// removed, or because someone had to switch it to a `var` built dynamically
// to compose edge types at runtime -- fails closed instead of the gate
// silently tokenizing stale or empty Cypher.
func TestImpactBlastRadiusGateQueriesAreLiteralConstants(t *testing.T) {
	fileSet := token.NewFileSet()
	file, err := parser.ParseFile(fileSet, impactGateSourceFile(t), nil, 0)
	if err != nil {
		t.Fatalf("parse impact_blast_radius.go: %v", err)
	}

	literalConsts := map[string]bool{}
	for _, decl := range file.Decls {
		genDecl, ok := decl.(*ast.GenDecl)
		if !ok || genDecl.Tok != token.CONST {
			continue
		}
		for _, spec := range genDecl.Specs {
			valueSpec, ok := spec.(*ast.ValueSpec)
			if !ok || len(valueSpec.Names) != 1 || len(valueSpec.Values) != 1 {
				continue
			}
			basicLit, ok := valueSpec.Values[0].(*ast.BasicLit)
			if !ok || basicLit.Kind != token.STRING {
				continue
			}
			literalConsts[valueSpec.Names[0].Name] = true
		}
	}

	for _, q := range impactBlastRadiusGateQueries {
		if !literalConsts[q.ConstName] {
			t.Errorf(
				"%s is not declared as a single string-literal const in impact_blast_radius.go -- "+
					"the #5335 gate requires every audited query to be a checked-in Cypher constant, not "+
					"composed dynamically (e.g. fmt.Sprintf); restructure or extend the gate",
				q.ConstName,
			)
		}
	}
}

// TestImpactBlastRadiusQueriesOnlyTraverseDisclosedEdges is the #5335 GATE 2
// CI-enforced edge-materialization gate. It rides `go test ./internal/query`,
// the same CI floor as every other Go package test -- no separate workflow.
//
// For each production Cypher constant in impactBlastRadiusGateQueries, it
// tokenizes every relationship-type token the query traverses and asserts
// each one is disclosed rather than silent: present in that query's own
// coverage-edge-type list (sqlTableBlastRadiusEdgeTypes,
// crossplaneXrdBlastRadiusEdgeTypes -- disclosed via the API response's
// coverage/complete fields either way), genuinely materialized per
// EdgeMaterializationCoverage, or explicitly annotated in
// unmaterializedAnnotatedImpactEdgeTypes. This is the invariant "no edge is
// traversed SILENTLY" -- not "every edge has a writer": an annotated,
// disclosed, unwritten edge (SATISFIED_BY today) passes.
//
// A positive-extraction floor (MinDistinctEdgeTypes) guards against the
// tokenizer itself regressing to extract fewer tokens than it does today,
// which would otherwise make this test vacuously green.
//
// Scope limits (documented here per #5335's scope-honesty requirement):
//   - Only the Cypher constants in impact_blast_radius.go that feed
//     blastRadiusAffected's target_type switch, plus the shared tier-lookup
//     query, are audited -- not every "impact"-named query in the package
//     (impact.go's dependency-path explainer, exposure_path.go, etc).
//   - Only relationship-type tokens are extracted. Node labels are out of
//     scope for v1.
func TestImpactBlastRadiusQueriesOnlyTraverseDisclosedEdges(t *testing.T) {
	for _, q := range impactBlastRadiusGateQueries {
		q := q
		t.Run(q.ConstName, func(t *testing.T) {
			t.Parallel()
			allTokens := extractRelationshipTypeTokens(q.Cypher)
			distinct := distinctStrings(allTokens)
			if len(distinct) < q.MinDistinctEdgeTypes {
				t.Fatalf(
					"%s: extracted %d distinct relationship-type token(s) (%v), want at least %d -- "+
						"this floor is seeded from today's known tokens; a lower count means the tokenizer "+
						"regressed, not that the query traverses fewer edges",
					q.ConstName, len(distinct), distinct, q.MinDistinctEdgeTypes,
				)
			}
			for _, edgeType := range distinct {
				if ok, reason := resolveImpactEdgeType(q, edgeType); !ok {
					t.Error(reason)
				}
			}
		})
	}
}

// TestImpactBlastRadiusCoverageEdgeTypesAreStillTraversed is GATE 2's
// reverse-direction (bidirectional) staleness check: a coverage-edge-type
// list entry (sqlTableBlastRadiusEdgeTypes, crossplaneXrdBlastRadiusEdgeTypes)
// must be either actually traversed by an impact/blast-radius Cypher
// constant, or a real, known graph relationship type registered in
// internal/graph/edgetype -- the #5330 sql_table honesty pattern
// deliberately lists READS_FROM, MIGRATES, and MAPS_TO_TABLE even though no
// UNION branch traverses them, specifically so the API response's
// coverage/complete fields disclose the gap instead of a silent
// undercount. edgetype.IsRegistered distinguishes that legitimate,
// documented pattern from a genuinely stale entry: a coverage-list value
// that is neither traversed nor even a recognized relationship-type constant
// anywhere in the codebase (a typo, a renamed/removed edge type, or a copy-
// paste mistake) fails.
func TestImpactBlastRadiusCoverageEdgeTypesAreStillTraversed(t *testing.T) {
	traversed := map[string]bool{}
	for _, q := range impactBlastRadiusGateQueries {
		for _, edgeType := range extractRelationshipTypeTokens(q.Cypher) {
			traversed[edgeType] = true
		}
	}

	var stale []string
	for _, q := range impactBlastRadiusGateQueries {
		for _, edgeType := range q.CoverageEdgeTypes {
			if traversed[edgeType] || edgetype.IsRegistered(edgeType) {
				continue
			}
			stale = append(stale, q.ConstName+":"+edgeType)
		}
	}
	sort.Strings(stale)
	for _, entry := range stale {
		t.Errorf(
			"coverage-edge-type list entry %q is not traversed by any impact_blast_radius.go query "+
				"and is not a registered graph relationship type (internal/graph/edgetype) -- stale coverage claim, remove it",
			entry,
		)
	}
}
