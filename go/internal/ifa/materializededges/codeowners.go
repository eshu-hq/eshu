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

// codeownersOwnershipFamily is the materialized-edge family key this guard
// asserts. It names the fixture's identity contract as well as its edge-type
// registry: LoadExpectedEdges validates the fixture's identity keys against
// cypher.MaterializedEdgeIdentityProperties for this same key, so the two
// lookups can never be pointed at different families by a typo in one of them.
const codeownersOwnershipFamily = "codeowners_ownership_edges"

// codeownersExpectedEdgesRelPath is the codeowners_ownership_edges expected-edge
// fixture, repoRoot-anchored. Moved from ifa's codeowners_family_odu.go
// (#6053): nothing else in ifa referenced it once
// MaterializedEdgeOduResolver.Resolve's dispatch moved to this package.
const codeownersExpectedEdgesRelPath = "go/internal/ifa/testdata/codeowners/ifa-codeowners-family-expected-edges.json"

// codeownersFamilyExpectedEdgesPath joins repoRoot onto the expected-edge
// fixture. Moved from ifa's codeowners_family_odu.go for the same reason as
// codeownersExpectedEdgesRelPath above.
func codeownersFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeownersExpectedEdgesRelPath)
}

// resolveCodeownersOwnershipMaterializedEdges is codeowners_ownership_edges'
// named vacuity guard (#5992), mirroring resolveDocumentationEdgeMaterializedEdges's
// three-step shape: (1) load the hand-derived expected-edge-set fixture,
// (2) assert it covers every relationship type the family's writer registry
// accepts, (3) run the production extractor over odu.Facts and assert the
// result matches the fixture EXACTLY.
//
// The comparison is property-aware, not a bare (type, source, target) triple:
// a DECLARES_CODEOWNER relationship's real graph identity includes the
// relationship properties the write template MERGEs on --
// `[rel:DECLARES_CODEOWNER {pattern: row.pattern, source_path: row.source_path}]`
// (canonical_codeowners_edges.go). That is not a family-local extension: the
// two property names are declared once, centrally, in
// cypher.MaterializedEdgeIdentityProperties, and both this pure guard and the
// live `eshu-ifa assert-edges` verb read them from there through
// ExpectedEdge.Identity / ExpectedEdge.Key(). One fixture, one loader, one
// key, so the offline guard and the live gate cannot drift on what "the same
// edge" means.
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
func resolveCodeownersOwnershipMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, codeownersOwnershipFamily)
	if err != nil {
		return false, err.Error()
	}
	if len(expected) == 0 {
		return false, fmt.Sprintf("odù %q: codeowners expected edges %s declares no edges; an empty expected set would make the gate vacuous", odu.Name, expectedEdgesPath)
	}
	registry, err := MaterializedEdgeDomainEdgeTypes(codeownersOwnershipFamily)
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

	actual := codeownersOwnershipRowsToExpectedEdges(rows)
	if mismatch := compareCodeownersOwnershipExpectedEdges(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf("odù %q: ExtractCodeownersOwnershipEdgeRowsWithQuarantine reproduces the expected %d-edge set exactly (by relationship_type/source/target plus the declared pattern/source_path identity) across all %d registry type(s)", odu.Name, len(expected), len(registry))
}

// codeownersOwnershipRowsToExpectedEdges adapts
// ExtractCodeownersOwnershipEdgeRowsWithQuarantine's []map[string]any row
// shape into the shared ExpectedEdge identity the comparison keys on.
//
// order_index and generation_id are deliberately left out: both are SET-only
// relationship properties, not part of the write template's relationship MERGE
// key, so including either would assert an identity the graph does not
// actually key on.
func codeownersOwnershipRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	out := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		out = append(out, ExpectedEdge{
			RelationshipType: "DECLARES_CODEOWNER",
			SourceEntityID:   anyToStringValue(row["repo_id"]),
			TargetEntityID:   anyToStringValue(row["owner_ref"]),
			Identity: map[string]string{
				"pattern":     anyToStringValue(row["pattern"]),
				"source_path": anyToStringValue(row["source_path"]),
			},
		})
	}
	return out
}

// codeownersOwnershipFamilyGenerationID reads the shared generation ID off
// the Odù's own facts. ExtractCodeownersOwnershipEdgeRowsWithQuarantine only
// uses it to stamp each row's generation_id (a SET-only relationship
// property this guard does not assert), so any non-empty value the fixture
// actually carries is correct; an empty return degrades to the empty string
// rather than failing, matching every other production caller's fallback
// shape.
func codeownersOwnershipFamilyGenerationID(envelopes []facts.Envelope) string {
	for _, env := range envelopes {
		if strings.TrimSpace(env.GenerationID) != "" {
			return env.GenerationID
		}
	}
	return ""
}

// missingCodeownersOwnershipExpectedTypes reports any registry-owned
// relationship type the expected-edge fixture never exercises.
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
// expectation and what the extractor actually produced.
//
// Multiplicity matters here specifically: ifa.CodeownersFamilyOdu's RULE A/B/C
// deliberately produce THREE distinct edges to the same (repo, team) pair, so
// a plain set comparison would collapse them and hide a missing or duplicated
// rule. ExpectedEdge.Key()'s Identity component additionally proves WHICH rule
// contributed which edge, so a materialization bug that cross-wires
// pattern/source_path between two rules owned by the same team -- a change no
// triple-only key can see -- is a mismatch here.
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
