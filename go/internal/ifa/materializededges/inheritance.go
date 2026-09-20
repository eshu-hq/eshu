// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer/inheritance"
)

// inheritanceExpectedEdgesRelPath is the repo-root-relative path to the
// hand-derived expected inheritance_edges edge set (#5996).
//
// It lives under this package's own testdata/ tree, not under
// testdata/cassettes/, for the same reason the SQL, code-call, and rationale
// families' fixtures do: the offline cassette validator globs every
// testdata/cassettes/*/*.json as a replay cassette
// (internal/replay/schema's committed-cassette check), and this file is a gate
// ASSERTION (fields odu/note/edges), not a cassette (schema_version + scopes).
const inheritanceExpectedEdgesRelPath = "go/internal/ifa/testdata/inheritance/ifa-inheritance-family-expected-edges.json"

// inheritanceFamilyExpectedEdgesPath joins repoRoot onto the fixture path.
func inheritanceFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, inheritanceExpectedEdgesRelPath)
}

// inheritanceRowsToExpectedEdges adapts inheritance.ExtractRows' row
// shape onto the exported ExpectedEdge identity triple. inheritance_edges
// declares an empty relationship-MERGE identity
// (cypher.materializedEdgeIdentityByFamily["inheritance_edges"] = {} --
// batchCanonicalInheritanceEdgeUpsertCypher/-OverrideUpsertCypher/
// -AliasUpsertCypher/batchCanonicalImplementsEdgeUpsertCypher all MERGE on
// their two endpoint nodes alone), so every derived edge here carries a nil
// Identity and compares through ExpectedEdge.Key()'s empty-Identity path.
func inheritanceRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	out := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		out = append(out, ExpectedEdge{
			RelationshipType: anyToStringValue(row["relationship_type"]),
			SourceEntityID:   anyToStringValue(row["child_entity_id"]),
			TargetEntityID:   anyToStringValue(row["parent_entity_id"]),
		})
	}
	return out
}

// missingInheritanceExpectedTypes reports which of registry's relationship
// types (cypher.InheritanceMaterializedEdgeTypes(), read through
// MaterializedEdgeDomainEdgeTypes so the check stays registry-derived) have no
// edge at all in expected -- the exhaustiveness half of the vacuity guard: a
// fifth writer type added later with no matching expected-set entry flips this
// red before it can be certified as covered by a stale fixture.
func missingInheritanceExpectedTypes(expected []ExpectedEdge, registry map[string]struct{}) []string {
	present := make(map[string]struct{}, len(expected))
	for _, e := range expected {
		present[e.RelationshipType] = struct{}{}
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

// compareInheritanceExpectedSets reports an exact-set mismatch in both
// directions, mirroring compareRationaleExpectedSets/
// compareSQLRelationshipExpectedSets: MISSING means the extractor stopped
// deriving an edge the graph is supposed to carry; EXTRA means it started
// deriving one nobody derived by hand -- the direction a "contains all
// expected" subset check would miss.
func compareInheritanceExpectedSets(label string, expected, actual []ExpectedEdge) string {
	expectedSet := make(map[string]struct{}, len(expected))
	for _, e := range expected {
		expectedSet[e.Key()] = struct{}{}
	}
	actualSet := make(map[string]struct{}, len(actual))
	for _, e := range actual {
		actualSet[e.Key()] = struct{}{}
	}

	var missing, extra []string
	for _, e := range expected {
		if _, ok := actualSet[e.Key()]; !ok {
			missing = append(missing, inheritanceEdgeLabel(e))
		}
	}
	for _, e := range actual {
		if _, ok := expectedSet[e.Key()]; !ok {
			extra = append(extra, inheritanceEdgeLabel(e))
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)

	if len(missing) == 0 && len(extra) == 0 {
		if len(actual) != len(expected) {
			return fmt.Sprintf("odù %q: extractor produced %d row(s) for %d distinct expected edge(s); inheritance.ExtractRows' own seenEdges dedup should have collapsed them",
				label, len(actual), len(expected))
		}
		return ""
	}
	var b strings.Builder
	fmt.Fprintf(&b, "odù %q: inheritance edge set does not match %d hand-derived edge(s)", label, len(expected))
	if len(missing) > 0 {
		fmt.Fprintf(&b, "; MISSING (the extractor no longer derives these): %s", strings.Join(missing, ", "))
	}
	if len(extra) > 0 {
		fmt.Fprintf(&b, "; EXTRA (nobody derived these by hand): %s", strings.Join(extra, ", "))
	}
	return b.String()
}

func inheritanceEdgeLabel(e ExpectedEdge) string {
	return fmt.Sprintf("%s -[%s]-> %s", e.SourceEntityID, e.RelationshipType, e.TargetEntityID)
}

// resolveInheritanceMaterializedEdges is the materialized_edges:
// inheritance_edges family's vacuity guard (#5996), mirroring
// resolveRationaleEdgeMaterializedEdges's shape: it resolves true iff the
// hand-derived expected-edge-set file exists, names at least one edge of
// EVERY registry relationship type (the exhaustiveness half), and running
// odu's own facts through the pure, backend-free
// inheritance.ExtractRows seam reproduces that expected set EXACTLY
// from the family's one intended repository scope (the vacuity half) -- a
// fixture that merely looks right (a bound Odù name) but whose facts don't
// actually derive the claimed edges cannot pass. Exact repository identity is
// part of the contract because production emits a refresh/retract intent for
// every extracted scope, including one with no derived inheritance edges.
func resolveInheritanceMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, "inheritance_edges")
	if err != nil {
		return false, err.Error()
	}

	registry, err := MaterializedEdgeDomainEdgeTypes("inheritance_edges")
	if err != nil {
		return false, err.Error()
	}
	if missing := missingInheritanceExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge-set %s does not cover every registry edge type, missing: %v", odu.Name, expectedEdgesPath, missing)
	}

	repoIDs, rows := inheritance.ExtractRows(odu.Facts)
	if len(repoIDs) != 1 || repoIDs[0] != ifa.InheritanceFamilyRepoID {
		return false, fmt.Sprintf(
			"odù %q: inheritance.ExtractRows reports repository scope %v, want exactly [%s]",
			odu.Name,
			repoIDs,
			ifa.InheritanceFamilyRepoID,
		)
	}
	actual := inheritanceRowsToExpectedEdges(rows)
	if mismatch := compareInheritanceExpectedSets(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}

	return true, fmt.Sprintf("odù %q: inheritance.ExtractRows reproduces the expected %d-edge set exactly, covering all %d registry types", odu.Name, len(expected), len(registry))
}
