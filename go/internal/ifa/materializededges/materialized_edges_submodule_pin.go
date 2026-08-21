// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/facts"
	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// submodulePinEdgesFamily is the materialized-edge family key this guard
// asserts, matching the "submodule_pin_edges" key registered in
// cypher.singleTypeMaterializedEdgeFamilies. It names the fixture's identity
// contract as well as its edge-type registry: LoadExpectedEdges validates the
// fixture's identity keys against cypher.MaterializedEdgeIdentityProperties
// for this same key, so the two lookups can never be pointed at different
// families by a typo in one of them.
const submodulePinEdgesFamily = "submodule_pin_edges"

// submodulePinExpectedEdgesRelPath is the submodule_pin_edges expected-edge
// fixture, repoRoot-anchored. Moved from ifa's submodule_pin_family_odu.go
// (#6053 convention): nothing else in ifa referenced it once
// MaterializedEdgeOduResolver.Resolve's dispatch moved to this package.
const submodulePinExpectedEdgesRelPath = "go/internal/ifa/testdata/submodulepin/ifa-submodule-pin-family-expected-edges.json"

// submodulePinFamilyExpectedEdgesPath joins repoRoot onto the expected-edge
// fixture. Moved from ifa's submodule_pin_family_odu.go for the same reason
// as submodulePinExpectedEdgesRelPath above.
func submodulePinFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, submodulePinExpectedEdgesRelPath)
}

// resolveSubmodulePinMaterializedEdges is submodule_pin_edges' named vacuity
// guard (#6002), mirroring resolveCodeownersOwnershipMaterializedEdges's
// three-step shape: (1) load the hand-derived expected-edge-set fixture, (2)
// assert it covers every relationship type the family's writer registry
// accepts, (3) run the production extractor over odu.Facts and assert the
// result matches the fixture EXACTLY.
//
// The comparison is property-aware, not a bare (type, source, target)
// triple: a PINS_SUBMODULE relationship's real graph identity includes the
// relationship property the write template MERGEs on --
// `[rel:PINS_SUBMODULE {path: row.submodule_path}]`
// (canonical_submodule_edges.go). That property name is declared once,
// centrally, in cypher.MaterializedEdgeIdentityProperties, and both this
// pure guard and the live `eshu-ifa assert-edges` verb read it from there
// through ExpectedEdge.Identity / ExpectedEdge.Key(). One fixture, one
// loader, one key, so the offline guard and the live gate cannot drift on
// what "the same edge" means.
//
// submodule_pin_edges is single-type (PINS_SUBMODULE only), so step 2 is a
// non-empty check today, not a multi-type coverage check the way code_calls
// needs. It stays a real check, not a no-op: a second submodule_pin_edges
// edge type added later with no matching expected-set entry must flip this
// red on day one, the same fail-closed contract every other family's
// registry-exhaustiveness check gives.
//
// A quarantined fact is fatal, not skipped (mirroring
// resolveCodeownersOwnershipMaterializedEdges): a fixture that no longer
// decodes cleanly against the submodule.pin contract cannot honestly claim
// to prove the edge set it names, so proceeding on the survivors would
// understate the fixture's own claim rather than catch the regression.
func resolveSubmodulePinMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, submodulePinEdgesFamily)
	if err != nil {
		return false, err.Error()
	}
	if len(expected) == 0 {
		return false, fmt.Sprintf("odù %q: submodule-pin expected edges %s declares no edges; an empty expected set would make the gate vacuous", odu.Name, expectedEdgesPath)
	}
	registry, err := MaterializedEdgeDomainEdgeTypes(submodulePinEdgesFamily)
	if err != nil {
		return false, err.Error()
	}
	if missing := missingSubmodulePinExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}

	generationID := submodulePinFamilyGenerationIDFromFacts(odu.Facts)
	rows, quarantined, err := reducer.ExtractSubmodulePinEdgeRowsWithQuarantine(odu.Facts, generationID)
	if err != nil {
		return false, fmt.Sprintf("odù %q: ExtractSubmodulePinEdgeRowsWithQuarantine failed: %v", odu.Name, err)
	}
	if len(quarantined) > 0 {
		return false, fmt.Sprintf("odù %q: %d fact(s) quarantined by the decoder; the fixture no longer validates against the submodule.pin contract, so any edge set derived from the survivors understates what it claims to prove", odu.Name, len(quarantined))
	}
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: production extractor returned zero rows", odu.Name)
	}

	actual := submodulePinRowsToExpectedEdges(rows)
	if mismatch := compareSubmodulePinExpectedEdges(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf("odù %q: ExtractSubmodulePinEdgeRowsWithQuarantine reproduces the expected %d-edge set exactly (by relationship_type/source/target, path identity, and pinned_sha) across all %d registry type(s)", odu.Name, len(expected), len(registry))
}

// submodulePinRowsToExpectedEdges adapts
// ExtractSubmodulePinEdgeRowsWithQuarantine's []map[string]any row shape into
// the shared ExpectedEdge identity the comparison keys on.
//
// generation_id is deliberately left out because it is run-specific.
// pinned_sha is SET-only rather than part of the relationship MERGE key, but
// it is still asserted as mutable truth so a stale graph property cannot pass
// merely because the relationship identity and edge count are correct.
func submodulePinRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	out := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		out = append(out, ExpectedEdge{
			RelationshipType: "PINS_SUBMODULE",
			SourceEntityID:   anyToStringValue(row["parent_repo_id"]),
			TargetEntityID:   anyToStringValue(row["resolved_repo_id"]),
			Identity: map[string]string{
				"path": anyToStringValue(row["submodule_path"]),
			},
			Properties: map[string]string{
				"pinned_sha": anyToStringValue(row["pinned_sha"]),
			},
		})
	}
	return out
}

// submodulePinFamilyGenerationIDFromFacts reads the shared generation ID off
// the Odù's own facts. ExtractSubmodulePinEdgeRowsWithQuarantine only uses it
// to stamp each row's generation_id (a SET-only relationship property this
// guard does not assert), so any non-empty value the fixture actually
// carries is correct; an empty return degrades to the empty string rather
// than failing, matching codeownersOwnershipFamilyGenerationID's fallback
// shape.
func submodulePinFamilyGenerationIDFromFacts(envelopes []facts.Envelope) string {
	for _, env := range envelopes {
		if strings.TrimSpace(env.GenerationID) != "" {
			return env.GenerationID
		}
	}
	return ""
}

// missingSubmodulePinExpectedTypes reports any registry-owned relationship
// type the expected-edge fixture never exercises.
func missingSubmodulePinExpectedTypes(expected []ExpectedEdge, registry map[string]struct{}) []string {
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

// compareSubmodulePinExpectedEdges reports the exact-set (by multiplicity,
// not just membership) mismatch between the hand-derived expectation and
// what the extractor actually produced.
//
// Multiplicity matters here specifically: submodulePinFamilyOdu's PIN A/B
// deliberately produce TWO distinct edges to the SAME target repository, so
// a plain set comparison would collapse them and hide a missing or
// duplicated path. ExpectedEdge.Key()'s Identity component additionally
// proves WHICH path contributed which edge, so a materialization bug that
// cross-wires paths between two pins owned by the same (parent, target)
// pair -- a change no pair-only key can see -- is a mismatch here.
func compareSubmodulePinExpectedEdges(oduName string, expected, actual []ExpectedEdge) string {
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
	return fmt.Sprintf("odù %q: submodule-pin edge set does not match %d hand-derived edge(s); MISSING: %s; EXTRA: %s", oduName, len(expected), strings.Join(missing, ", "), strings.Join(extra, ", "))
}
