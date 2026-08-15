// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// propertyKeyedMergeTypePattern finds a relationship-MERGE property map in
// MERGE-pattern edge-bracket position (`-[var:TYPE {`), capturing TYPE. It is
// deliberately a separate regex from identityMergePropertyOpen
// (materialized_edge_families_test.go), which has no capture group because
// its caller already knows the type it is checking; this scan does not, so
// it needs one.
var propertyKeyedMergeTypePattern = regexp.MustCompile(`-\[\s*[A-Za-z_][A-Za-z0-9_]*\s*:\s*([A-Z][A-Z0-9_]*)\s*\{`)

// TestPropertyKeyedRelationshipMergesMatchKnownAllowList closes a gap
// TestSingleTypeFamilyIdentityMatchesWriteCypher cannot: that guard only
// iterates singleTypeMaterializedEdgeFamilies, so it never looks at the four
// multi-type families that declare an empty identity in
// materializedEdgeIdentityByFamily (repo_dependency, code_calls,
// sql_relationships, inheritance_edges) or at the five direct-materialization
// writers those families' registries never enumerate at all (DERIVED_FROM,
// TAINT_FLOWS_TO, PUBLISHES x2, BUILT_FROM). Nothing would catch someone
// folding a property into CALLS, QUERIES_TABLE, INHERITS, or DEPENDS_ON —
// the exact silent-collapse bug this precursor exists to prevent — because
// those types are never checked against source at all.
//
// This test scans every non-test .go file directly in this package for a
// relationship-MERGE property map (`-[var:TYPE {...}]`) and asserts the
// total inventory, by relationship type, equals a fixed seven-occurrence
// allow-list: the two gated identities (DECLARES_CODEOWNER, PINS_SUBMODULE,
// already proven by TestSingleTypeFamilyIdentityMatchesWriteCypher) plus the
// five out-of-scope writers. It fails the moment an eighth occurrence
// appears anywhere in the package — whether that is a genuinely new
// property-keyed type or an unexpected third occurrence of an
// already-allow-listed one (PUBLISHES already has two legitimate templates,
// one per target label, so a third would be exactly the kind of drift this
// count-based check catches that a type-only set would miss).
//
// AST-based, not a raw-text scan: every *ast.BasicLit STRING node in the
// file is inspected, wherever it appears — a const/var declaration, a
// fmt.Sprintf call argument (the label-scoped code-call, SQL, and
// inheritance templates build their Cypher this way), or anywhere else a
// string literal can occur — so a doc comment that quotes example Cypher can
// never be mistaken for a production template: comments are never part of
// the AST's expression tree at all, unlike a raw-text scan, which would need
// exclusion logic to tell the two apart. An earlier version of this scan
// filtered to *ast.ValueSpec values only, which is a strict subset of "every
// string literal" and missed every Sprintf-built template — three of the
// four multi-type families this test exists to cover build their Cypher that
// way (code_calls, sql_relationships, inheritance_edges).
func TestPropertyKeyedRelationshipMergesMatchKnownAllowList(t *testing.T) {
	t.Parallel()

	want := map[string]int{
		"DECLARES_CODEOWNER": 1, // canonical_codeowners_edges.go -- gated, covered above.
		"PINS_SUBMODULE":     1, // canonical_submodule_edges.go -- gated, covered above.
		"TAINT_FLOWS_TO":     1, // code_interproc_evidence_writer.go -- outside the 14-family scope.
		"DERIVED_FROM":       1, // derived_from_edge_writer.go -- outside the 14-family scope.
		"PUBLISHES":          2, // provenance_edge_writer.go -- Package + PackageVersion targets.
		"BUILT_FROM":         1, // provenance_edge_writer.go -- outside the 14-family scope.
	}

	got := scanPropertyKeyedRelationshipMergeTypes(t)

	if !reflect.DeepEqual(got, want) {
		t.Errorf("property-keyed relationship MERGE inventory = %v, want %v (a relationship type gained or lost a property-keyed MERGE occurrence somewhere in this package's non-test source)", got, want)
	}
}

// scanPropertyKeyedRelationshipMergeTypes parses every non-test .go file
// directly in this package directory and counts each relationship type's
// property-keyed MERGE occurrences across every string literal in the file,
// regardless of the expression context it appears in.
func scanPropertyKeyedRelationshipMergeTypes(t *testing.T) map[string]int {
	t.Helper()

	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package directory: %v", err)
	}

	fset := token.NewFileSet()
	counts := map[string]int{}
	for _, entry := range entries {
		name := entry.Name()
		if entry.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}

		file, err := parser.ParseFile(fset, name, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}

		ast.Inspect(file, func(n ast.Node) bool {
			lit, ok := n.(*ast.BasicLit)
			if !ok || lit.Kind != token.STRING {
				return true
			}
			text, err := strconv.Unquote(lit.Value)
			if err != nil {
				return true
			}
			for _, m := range propertyKeyedMergeTypePattern.FindAllStringSubmatch(text, -1) {
				counts[m[1]]++
			}
			return true
		})
	}
	return counts
}
