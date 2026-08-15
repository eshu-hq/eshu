// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// codeownersExpectedEdge is this family's OWN hand-derived expected edge
// shape (#5992 review round 2), deliberately richer than the shared
// ifa.ExpectedEdge triple. Identity carries the relationship properties that
// decide a DECLARES_CODEOWNER edge's real graph identity -- "pattern" and
// "source_path", the write template's relationship MERGE key
// (canonical_codeowners_edges.go) -- so this family's own PURE guard can
// prove per-rule property truth even though the SHARED live assert-edges
// path (ifa.ExpectedEdge, materialized_edges_assert.go) cannot: see
// TestExpectedEdgeDetectsCodeownersPropertyCorruption and
// TestExpectedEdgeDetectsMissingEdgeMaskedByUnrelatedDuplicate
// (materialized_edges_codeowners_property_gap_test.go) for the two RED
// proofs of that shared-mechanism gap, reported to the #5543 coordinator.
//
// The field name and shape (a string-keyed Identity map alongside the
// existing relationship_type/source_entity_id/target_entity_id triple) match
// the coordinator-relayed precursor design under owner review as of this
// writing (an Identity map[string]string field on ExpectedEdge itself), so
// extending this fixture stays a mechanical edit rather than a rewrite if
// that precursor lands. Until then this is purely a family-local extension:
// the JSON file's relationship_type/source_entity_id/target_entity_id fields
// alone are ALSO valid input to the shared ifa.LoadExpectedEdges loader (its
// JSON decoding ignores the additional "identity" key), so the SAME
// committed fixture file serves both this property-aware pure guard AND, once
// wired, the live `eshu-ifa assert-edges` verb's coarser 3-tuple comparison
// without maintaining two fixtures. That live path is the "clearly marked
// seam" this family accepts today: it can only prove edge existence/count,
// never per-rule pattern/source_path truth, until the shared type widens.
type codeownersExpectedEdge struct {
	RelationshipType string            `json:"relationship_type"`
	SourceEntityID   string            `json:"source_entity_id"`
	TargetEntityID   string            `json:"target_entity_id"`
	Identity         map[string]string `json:"identity"`
}

type codeownersExpectedEdgesFile struct {
	Odu   string                   `json:"odu"`
	Edges []codeownersExpectedEdge `json:"edges"`
}

// loadCodeownersExpectedEdges reads and parses this family's own richer
// expected-edge-set fixture, mirroring loadSQLRelationshipExpectedEdges's
// shape and fail-closed-on-empty behavior.
func loadCodeownersExpectedEdges(path string) ([]codeownersExpectedEdge, error) {
	raw, err := os.ReadFile(path) // #nosec G304 -- checked-in repo fixture under testdata/, not external input
	if err != nil {
		return nil, fmt.Errorf("ifa: read codeowners expected edges %s: %w", path, err)
	}
	var parsed codeownersExpectedEdgesFile
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return nil, fmt.Errorf("ifa: parse codeowners expected edges %s: %w", path, err)
	}
	if len(parsed.Edges) == 0 {
		return nil, fmt.Errorf("ifa: codeowners expected edges %s has no edges; an empty expected set would make the gate vacuous", path)
	}
	for i, edge := range parsed.Edges {
		if strings.TrimSpace(edge.Identity["pattern"]) == "" || strings.TrimSpace(edge.Identity["source_path"]) == "" {
			return nil, fmt.Errorf("ifa: codeowners expected edges %s edge[%d] (%s|%s|%s) carries no identity.pattern/identity.source_path; this fixture's whole point is proving per-rule property truth the shared ExpectedEdge cannot", path, i, edge.RelationshipType, edge.SourceEntityID, edge.TargetEntityID)
		}
	}
	return parsed.Edges, nil
}

// codeownersEdgeKey builds the canonical set-membership key for one expected
// or derived edge, INCLUDING pattern and source_path -- the property-aware
// identity this family's own guard proves (see codeownersExpectedEdge's doc
// comment for why the shared ifa.ExpectedEdge.Key() cannot do the same).
func codeownersEdgeKey(e codeownersExpectedEdge) string {
	return e.RelationshipType + "|" + e.SourceEntityID + "|" + e.TargetEntityID + "|" + e.Identity["pattern"] + "|" + e.Identity["source_path"]
}

// resolveCodeownersOwnershipMaterializedEdges is codeowners_ownership_edges'
// named vacuity guard (#5992), mirroring resolveCodeCallMaterializedEdges's
// three-step shape: (1) load the hand-derived expected-edge-set fixture,
// (2) assert it covers every relationship type the family's writer registry
// accepts, (3) run the production extractor over odu.Facts and assert the
// result matches the fixture EXACTLY -- by the family's own property-aware
// identity (relationship_type, source, target, pattern, source_path), a
// strictly STRONGER proof than the shared ExpectedEdge triple other
// families' guards settle for, because ExtractCodeownersOwnershipEdgeRowsWithQuarantine's
// rows already carry pattern/source_path and this guard does not have to
// throw that data away just because the live-gate seam cannot yet consume
// it (see codeownersExpectedEdge's doc comment).
//
// codeowners_ownership_edges is single-type (DECLARES_CODEOWNER only), so
// step 2 is a non-empty check today, not a multi-type coverage check the way
// code_calls needs. It stays a real check, not a no-op: a second
// codeowners edge type added later with no matching expected-set entry must
// flip this red on day one, the same fail-closed contract every other
// family's registry-exhaustiveness check gives.
//
// A quarantined fact is fatal, not skipped (mirroring
// resolveDocumentationEdgeMaterializedEdges): a fixture that no longer
// decodes cleanly against the codeowners.ownership contract cannot honestly
// claim to prove the edge set it names, so proceeding on the survivors would
// understate the fixture's own claim rather than catch the regression.
func resolveCodeownersOwnershipMaterializedEdges(odu Odu, expectedEdgesPath string) (bool, string) {
	expected, err := loadCodeownersExpectedEdges(expectedEdgesPath)
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

	actual := make([]codeownersExpectedEdge, 0, len(rows))
	for _, row := range rows {
		actual = append(actual, codeownersExpectedEdge{
			RelationshipType: "DECLARES_CODEOWNER",
			SourceEntityID:   anyToStringValue(row["repo_id"]),
			TargetEntityID:   anyToStringValue(row["owner_ref"]),
			Identity: map[string]string{
				"pattern":     anyToStringValue(row["pattern"]),
				"source_path": anyToStringValue(row["source_path"]),
			},
		})
	}
	if mismatch := compareCodeownersOwnershipExpectedEdges(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf("odù %q: ExtractCodeownersOwnershipEdgeRowsWithQuarantine reproduces the expected %d-edge set exactly (by relationship_type/source/target/pattern/source_path) across all %d registry type(s)", odu.Name, len(expected), len(registry))
}

// codeownersOwnershipFamilyGenerationID reads the shared generation ID off
// the Odù's own facts. ExtractCodeownersOwnershipEdgeRowsWithQuarantine only
// uses it to stamp each row's generation_id (a SET-only relationship
// property this guard does not assert -- order_index and generation_id stay
// outside even this family's own richer identity, since neither is part of
// the write template's relationship MERGE key), so any non-empty value the
// fixture actually carries is correct; an empty return degrades to the empty
// string rather than failing, matching every other production caller's
// fallback shape.
func codeownersOwnershipFamilyGenerationID(envelopes []facts.Envelope) string {
	for _, env := range envelopes {
		if strings.TrimSpace(env.GenerationID) != "" {
			return env.GenerationID
		}
	}
	return ""
}

func missingCodeownersOwnershipExpectedTypes(expected []codeownersExpectedEdge, registry map[string]struct{}) []string {
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
// expectation and what the extractor actually produced, keyed on this
// family's own property-aware identity (codeownersEdgeKey). Multiplicity
// matters here specifically: codeownersFamilyOdu's RULE A/B/C deliberately
// produce THREE distinct edges to the same (repo, team) pair, so a plain set
// comparison would collapse them and hide a missing or duplicated rule; the
// pattern/source_path component of the key additionally proves WHICH rule
// contributed which edge, closing the gap the shared ExpectedEdge mechanism
// cannot (see this file's other doc comments).
func compareCodeownersOwnershipExpectedEdges(oduName string, expected, actual []codeownersExpectedEdge) string {
	want := make(map[string]int, len(expected))
	got := make(map[string]int, len(actual))
	for _, edge := range expected {
		want[codeownersEdgeKey(edge)]++
	}
	for _, edge := range actual {
		got[codeownersEdgeKey(edge)]++
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
