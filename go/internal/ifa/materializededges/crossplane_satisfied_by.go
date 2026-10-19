// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/crossplane"
)

// crossplaneSatisfiedByFamily is the materialized-edge family key this guard
// asserts (#6228), matching the key registered in
// cypher.singleTypeMaterializedEdgeFamilies and the domain
// `eshu-ifa assert-edges -domain <family>` addresses.
const crossplaneSatisfiedByFamily = "crossplane_satisfied_by"

// crossplaneSatisfiedByRelationshipType is the single relationship type
// SatisfiedByEdgeUpsertCypher MERGEs: the one member of the writer's closed
// vocabulary, a static token kept out of the MERGE property map.
//
// Duplicated here as a plain literal rather than imported, and it fails
// CLOSED: missingDirectFamilyExpectedTypes compares the FIXTURE against the
// REGISTRY, so a stale literal here yields a fixture whose type the registry
// does not contain and the guard reds instead of quietly asserting less.
const crossplaneSatisfiedByRelationshipType = "SATISFIED_BY"

// crossplaneSatisfiedByResolutionMode is the writer's only resolution mode,
// stamped on every row. The extractor resolves a Claim candidate against
// exactly one XRD by (group, kind) equality; pinning the single mode catches
// a future second mode silently changing what the rows claim.
const crossplaneSatisfiedByResolutionMode = "group_claim_kind"

// crossplaneSatisfiedByExpectedEdgesRelPath is the family's hand-derived
// expected-edge fixture, repoRoot-anchored.
const crossplaneSatisfiedByExpectedEdgesRelPath = "go/internal/ifa/testdata/crossplanesatisfiedby/ifa-crossplane-satisfied-by-family-expected-edges.json"

// crossplaneSatisfiedByExpectedEdgesPath joins repoRoot onto the family's
// expected-edge fixture.
func crossplaneSatisfiedByExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, crossplaneSatisfiedByExpectedEdgesRelPath)
}

// resolveCrossplaneSatisfiedByMaterializedEdges is crossplane_satisfied_by's
// named vacuity guard (#6228).
//
// The family's extractor already emits exactly the rows the write template
// UNWINDs, so the rows-to-edges mapping is one-for-one with no routing
// predicate to reproduce. What the fixture proves instead is the extractor's
// join behaviour: two claims resolve against one XRD by exact (group, kind)
// equality — across apiVersion segments and across the claims/platform scope
// split — while a claim matching two XRDs (ambiguous), a claim matching
// none, a core-group claim with no group segment, a claim_kind-less XRD, and
// a tombstoned claim each resolve to nothing rather than to a fabricated
// edge. The resolution mode is pinned on every row (single-mode contract).
//
// The extractor takes the flat envelope list directly (no fact-kind
// partition step to reproduce — its only filter is the content_entity kind
// plus the tombstone skip, both read off the envelope itself), and it
// cannot quarantine: malformed content rows simply fail candidate
// construction and resolve to nothing. A zero-row result is fatal here for
// the same reason as every sibling guard: a fixture that stopped decoding
// against the content_entity contract cannot prove the set it names.
func resolveCrossplaneSatisfiedByMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, registry, problem := loadDirectFamilyExpectedEdges(
		expectedEdgesPath, crossplaneSatisfiedByFamily, odu.Name,
	)
	if problem != "" {
		return false, problem
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows, _, err := crossplane.ExtractCrossplaneSatisfiedByEdgeRows(odu.Facts)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractCrossplaneSatisfiedByEdgeRows failed: %v", odu.Name, err)
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractCrossplaneSatisfiedByEdgeRows produced zero SATISFIED_BY rows; this fixture cannot prove anything", odu.Name)
	}
	for index, row := range rows {
		if got := anyToStringValue(row["rel_type"]); got != crossplaneSatisfiedByRelationshipType {
			return false, fmt.Sprintf("odù %q: row %d carries rel_type %q, want %q (the writer's single static token)", odu.Name, index, got, crossplaneSatisfiedByRelationshipType)
		}
		if got := anyToStringValue(row["resolution_mode"]); got != crossplaneSatisfiedByResolutionMode {
			return false, fmt.Sprintf("odù %q: row %d carries resolution_mode %q, want %q (the writer's single-mode contract)", odu.Name, index, got, crossplaneSatisfiedByResolutionMode)
		}
	}

	actual := crossplaneSatisfiedByRowsToExpectedEdges(rows)
	if mismatch := compareDirectFamilyExpectedEdges(odu.Name, crossplaneSatisfiedByFamily, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractCrossplaneSatisfiedByEdgeRows reproduces the expected %d-edge SATISFIED_BY set exactly across all %d registry type(s) and %d fact envelope(s); the ambiguous, unmatched, core-group, claim_kind-less and tombstoned candidates produced no spurious rows",
		odu.Name, len(expected), len(registry), len(odu.Facts),
	)
}

// crossplaneSatisfiedByRowsToExpectedEdges converts the extractor's rows
// one-for-one into the edge identity the write template MERGEs.
//
// claim_uid and xrd_uid are the :K8sResource and :CrossplaneXRD uids the
// template's two MATCH clauses bind, so they are the edge's source and
// target identity. resolution_mode, claim_group and claim_kind are SET
// after the MERGE — deliberately outside the relationship key — and are
// asserted here as mutable pins so a stale join decision cannot pass on
// edge identity alone. scope_id, generation_id and evidence_source are
// stamped by the WRITER from its own arguments rather than carried on the
// extractor's rows, so they are outside what this guard can or should
// assert. The live assert reads only fixture-declared property keys off
// each rel, so extra live props never collide with these pins.
func crossplaneSatisfiedByRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edge := ExpectedEdge{
			RelationshipType: anyToStringValue(row["rel_type"]),
			SourceEntityID:   anyToStringValue(row["claim_uid"]),
			TargetEntityID:   anyToStringValue(row["xrd_uid"]),
			Properties: map[string]string{
				"resolution_mode": anyToStringValue(row["resolution_mode"]),
				"claim_group":     anyToStringValue(row["claim_group"]),
				"claim_kind":      anyToStringValue(row["claim_kind"]),
			},
		}
		edges = append(edges, edge)
	}
	return edges
}
