// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"reflect"
	"strings"
	"testing"

	"github.com/eshu-hq/eshu/go/internal/replaycoverage"
)

// TestDocumentationFamilyIsCatalogedAndResolvable pins the production
// coverage seam, not just the extractor helper used by the tests in
// materialized_edges_documentation_test.go. A manifest row cannot honestly
// resolve unless the installed binary carries the Odù and the
// materialized-edge resolver dispatches through the family's own exact guard
// -- and, distinctly for #5994's review finding, unless the compiled catalog
// Odù is the SAME facts the committed cassette drives live. Mirrors
// TestCodeCallFamilyIsCatalogedAndResolvable exactly.
func TestDocumentationFamilyIsCatalogedAndResolvable(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)
	catalog := CatalogByName()
	compiled, ok := catalog[documentationFamilyOduName]
	if !ok {
		t.Fatalf("CatalogByName omits %q", documentationFamilyOduName)
	}
	fromCassette, err := loadDocumentationFamilyOdu(documentationFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadDocumentationFamilyOdu: %v", err)
	}
	if !reflect.DeepEqual(compiled, fromCassette) {
		t.Fatalf("compiled catalog Odù drifted from strict cassette projection\ncompiled: %#v\ncassette: %#v", compiled, fromCassette)
	}

	ok, detail := (MaterializedEdgeOduResolver{Catalog: catalog, RepoRoot: repoRoot}).Resolve(replaycoverage.CoverageEntry{
		Surface:      MaterializedEdgeSurfacePrefix + "documentation_edges",
		Scenario:     replaycoverage.ScenarioOdu,
		ScenarioType: replaycoverage.ScenarioTypeBaseline,
		Ref:          documentationFamilyOduName,
	})
	if !ok {
		t.Fatalf("documentation_edges resolver rejected the cataloged Odù: %s", detail)
	}
	if !strings.Contains(detail, "expected 3-edge set exactly") {
		t.Fatalf("resolver detail = %q, want it to name the exact edge count it proved", detail)
	}
}

// TestDocumentationFamilyCassetteDerivesTheExpectedEdgeSet is the offline
// vacuity guard for #5994: the production extractor, over the committed
// cassette, reproduces the hand-derived expected set EXACTLY. Mirrors
// TestCodeCallFamilyCassetteDerivesTheExpectedEdgeSet.
//
// This is deliberately not called coverage by itself. It proves the
// extractor, not the gate: the live edge write is a MATCH-MERGE on endpoint
// uid, so a missing endpoint node would make the write a silent no-op this
// test cannot see. The live ifa-determinism/ifa-fault-injection assertions
// close that half and are what justify the committed coverage rows.
func TestDocumentationFamilyCassetteDerivesTheExpectedEdgeSet(t *testing.T) {
	t.Parallel()
	repoRoot := repoRootDir(t)

	odu, err := loadDocumentationFamilyOdu(documentationFamilyCassetteFullPath(repoRoot))
	if err != nil {
		t.Fatalf("loadDocumentationFamilyOdu: %v", err)
	}

	ok, detail := resolveDocumentationEdgeMaterializedEdges(odu, documentationFamilyExpectedEdgesPath(repoRoot))
	if !ok {
		t.Fatalf("documentation family cassette does not resolve: %s", detail)
	}
	t.Logf("%s", detail)
}

// TestDocumentationEdgesFamilyResolvesLiveEdgeTypes proves
// MaterializedEdgeDomainEdgeTypes resolves "documentation_edges" through the
// SingleTypeMaterializedEdgeTypes fallback (materialized_edge_families.go:84
// registers it), which is what backs `eshu-ifa assert-edges -domain
// documentation_edges` actually knowing which live graph edges belong to this
// family.
func TestDocumentationEdgesFamilyResolvesLiveEdgeTypes(t *testing.T) {
	t.Parallel()
	types, err := MaterializedEdgeDomainEdgeTypes("documentation_edges")
	if err != nil {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(documentation_edges) = error %v, want it to resolve", err)
	}
	if _, ok := types["DOCUMENTS"]; !ok || len(types) != 1 {
		t.Fatalf("MaterializedEdgeDomainEdgeTypes(documentation_edges) = %v, want exactly {DOCUMENTS}", types)
	}
}
