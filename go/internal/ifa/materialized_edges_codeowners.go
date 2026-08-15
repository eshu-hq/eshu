// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// resolveCodeownersOwnershipMaterializedEdges is codeowners_ownership_edges'
// named vacuity guard (#5992), mirroring resolveCodeCallMaterializedEdges's
// three-step shape: (1) load the hand-derived expected-edge-set fixture,
// (2) assert it covers every relationship type the family's writer registry
// accepts, (3) run the production extractor over odu.Facts and assert the
// result matches the fixture EXACTLY.
//
// codeowners_ownership_edges is single-type (DECLARES_CODEOWNER only), so
// step 2 is a non-empty check today, not a multi-type coverage check the way
// code_calls needs. It stays a real check, not a no-op: a second
// codeowners edge type added later with no matching expected-set entry must
// flip this red on day one, the same fail-closed contract every other
// family's registry-exhaustiveness check gives.
//
// KNOWN LIMITATION (proven by
// TestExpectedEdgeDetectsCodeownersPropertyCorruption,
// materialized_edges_codeowners_property_gap_test.go, reported to the #5543
// coordinator and out of #5992's scope to fix): ExpectedEdge's identity is
// (RelationshipType, SourceEntityID, TargetEntityID) only.
// DECLARES_CODEOWNER's real graph identity also includes pattern and
// source_path -- the write template's relationship MERGE key
// (canonical_codeowners_edges.go) -- so this guard proves "the extractor
// produces the same NUMBER of DECLARES_CODEOWNER edges between each (repo,
// team) pair the fixture names", not "each individual rule's
// pattern/source_path/order_index is correct". A materialization bug that
// cross-wires those properties between two rules sharing a (repo, team) pair
// (see codeownersFamilyOdu's RULE A/RULE B) would pass this guard undetected.
// This is a structural gap in the shared ExpectedEdge mechanism
// (materialized_edges_assert.go), not something this family's fixture or
// guard can self-correct without extending that shared type.
func resolveCodeownersOwnershipMaterializedEdges(odu Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath)
	if err != nil {
		return false, err.Error()
	}
	registry, err := MaterializedEdgeDomainEdgeTypes("codeowners_ownership_edges")
	if err != nil {
		return false, err.Error()
	}
	if missing := missingCodeownersOwnershipExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}

	generationID := codeownersOwnershipFamilyGenerationID(odu.Facts)
	rows, quarantined, err := reducer.ExtractCodeownersOwnershipEdgeRowsWithQuarantine(odu.Facts, generationID)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractCodeownersOwnershipEdgeRowsWithQuarantine failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the codeowners.ownership contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: production extractor returned zero rows", odu.Name)
	}

	actual := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		actual = append(actual, ExpectedEdge{
			RelationshipType: "DECLARES_CODEOWNER",
			SourceEntityID:   anyToStringValue(row["repo_id"]),
			TargetEntityID:   anyToStringValue(row["owner_ref"]),
		})
	}
	if mismatch := compareCodeownersOwnershipExpectedEdges(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf("odù %q: ExtractCodeownersOwnershipEdgeRowsWithQuarantine reproduces the expected %d-edge set exactly across all %d registry type(s)", odu.Name, len(expected), len(registry))
}

// codeownersOwnershipFamilyGenerationID reads the shared generation ID off
// the Odù's own facts. ExtractCodeownersOwnershipEdgeRowsWithQuarantine only
// uses it to stamp each row's generation_id (a SET-only relationship
// property outside this guard's comparable identity — see the KNOWN
// LIMITATION doc comment above), so any non-empty value the fixture actually
// carries is correct; an empty return degrades to the empty string rather
// than failing, matching every other production caller's fallback shape.
func codeownersOwnershipFamilyGenerationID(envelopes []facts.Envelope) string {
	for _, env := range envelopes {
		if strings.TrimSpace(env.GenerationID) != "" {
			return env.GenerationID
		}
	}
	return ""
}

func missingCodeownersOwnershipExpectedTypes(expected []ExpectedEdge, registry map[string]struct{}) []string {
	present := make(map[string]struct{}, len(expected))
	for _, edge := range expected {
		present[edge.RelationshipType] = struct{}{}
	}
	var missing []string
	for edgeType := range registry {
		if _, ok := present[edgeType]; !ok {
			missing = append(missing, edgeType)
		}
	}
	sort.Strings(missing)
	return missing
}

// compareCodeownersOwnershipExpectedEdges reports the exact-set (by
// multiplicity, not just membership) mismatch between the hand-derived
// expectation and what the extractor actually produced. Multiplicity matters
// here specifically: codeownersFamilyOdu's RULE A/B/C deliberately produce
// THREE distinct edges to the same (repo, team) pair, so a plain set
// comparison would collapse them and hide a missing or duplicated rule.
func compareCodeownersOwnershipExpectedEdges(oduName string, expected, actual []ExpectedEdge) string {
	want := make(map[string]int, len(expected))
	got := make(map[string]int, len(actual))
	for _, edge := range expected {
		want[edge.Key()]++
	}
	for _, edge := range actual {
		got[edge.Key()]++
	}
	var missing, extra []string
	for key, count := range want {
		if got[key] < count {
			missing = append(missing, key)
		}
	}
	for key, count := range got {
		if count > want[key] {
			extra = append(extra, key)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) == 0 && len(extra) == 0 {
		return ""
	}
	return fmt.Sprintf("odù %q: codeowners-ownership edge set does not match %d hand-derived edge(s); MISSING: %s; EXTRA: %s", oduName, len(expected), strings.Join(missing, ", "), strings.Join(extra, ", "))
}
