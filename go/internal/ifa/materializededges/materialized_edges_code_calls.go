// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// codeCallExpectedEdgesRelPath is the code_calls expected-edge fixture,
// repoRoot-anchored. MOVED here from ifa's code_call_family_odu.go (#6053)
// along with the guard that was its only consumer; ifa carries a tombstone
// where it used to sit. It is not a duplicate -- nothing in ifa reads it any
// more, so leaving a copy behind would have been two values free to drift.
//
// The fixture itself stays under go/internal/ifa/testdata/ rather than
// testdata/cassettes/: the offline cassette validator globs every
// testdata/cassettes/*/*.json as a replay cassette, and this file is a gate
// ASSERTION, not a cassette.
const codeCallExpectedEdgesRelPath = "go/internal/ifa/testdata/codecalls/ifa-code-call-family-expected-edges.json"

// codeCallFamilyExpectedEdgesPath joins repoRoot onto the expected-edge
// fixture. Moved with codeCallExpectedEdgesRelPath above.
func codeCallFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, codeCallExpectedEdgesRelPath)
}

// codeCallFamilyOduName and codeCallFamilyGenerationID duplicate the code_calls
// family's Odù name and generation-ID identity from ifa's code_call_family_odu.go
// / code_call_family_catalog.go: this package cannot reach those unexported
// constants across the package boundary, and code_call_family_odu.go /
// code_call_family_catalog.go still need their own copies to stay in ifa (they
// seed the compiled catalog), so this is a copy, not a move.
const (
	codeCallFamilyOduName      = "odu:ifa-code-call-family"
	codeCallFamilyGenerationID = "gen-ifa-code-call-family-1"
)

// resolveCodeCallMaterializedEdges is code_calls' named vacuity guard. It
// invokes the production extractor, requires the hand-derived expected set to
// exercise every writer-registry type, and compares the resulting edge
// multiset exactly.
func resolveCodeCallMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, "code_calls")
	if err != nil {
		return false, err.Error()
	}
	registry, err := MaterializedEdgeDomainEdgeTypes("code_calls")
	if err != nil {
		return false, err.Error()
	}
	if missing := missingCodeCallExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}

	codeRepoIDs, codeRows, metaclassRepoIDs, metaclassRows := reducer.ExtractAllCodeRelationshipRows(odu.Facts)
	if len(codeRepoIDs) == 0 || len(metaclassRepoIDs) == 0 {
		return false, fmt.Sprintf("odù %q: production extractor returned empty repository scope (code=%v metaclass=%v)", odu.Name, codeRepoIDs, metaclassRepoIDs)
	}
	actual := make([]ExpectedEdge, 0, len(codeRows)+len(metaclassRows))
	for _, row := range codeRows {
		actual = append(actual, ExpectedEdge{
			RelationshipType: normalizeCodeCallRelationshipType(anyToStringValue(row["relationship_type"])),
			SourceEntityID:   anyToStringValue(row["caller_entity_id"]),
			TargetEntityID:   anyToStringValue(row["callee_entity_id"]),
		})
	}
	for _, row := range metaclassRows {
		actual = append(actual, ExpectedEdge{
			RelationshipType: normalizeCodeCallRelationshipType(anyToStringValue(row["relationship_type"])),
			SourceEntityID:   anyToStringValue(row["source_entity_id"]),
			TargetEntityID:   anyToStringValue(row["target_entity_id"]),
		})
	}
	if mismatch := compareCodeCallExpectedEdges(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf("odù %q: ExtractAllCodeRelationshipRows reproduces the expected %d-edge set exactly across all %d registry types", odu.Name, len(expected), len(registry))
}

func missingCodeCallExpectedTypes(expected []ExpectedEdge, registry map[string]struct{}) []string {
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

func compareCodeCallExpectedEdges(oduName string, expected, actual []ExpectedEdge) string {
	want := make(map[string]int, len(expected))
	got := make(map[string]int, len(actual))
	for _, edge := range expected {
		want[codeCallEdgeKey(edge.RelationshipType, edge.SourceEntityID, edge.TargetEntityID)]++
	}
	for _, edge := range actual {
		got[codeCallEdgeKey(edge.RelationshipType, edge.SourceEntityID, edge.TargetEntityID)]++
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
	return fmt.Sprintf("odù %q: code-call edge set does not match %d hand-derived edge(s); MISSING: %s; EXTRA: %s", oduName, len(expected), strings.Join(missing, ", "), strings.Join(extra, ", "))
}

func normalizeCodeCallRelationshipType(relationshipType string) string {
	if strings.TrimSpace(relationshipType) == "" {
		return "CALLS"
	}
	return relationshipType
}

// codeCallEdgeKey uses the same length-prefixed encoding as
// sqlRelationshipEdgeKey and ExpectedEdge.Key() (writeLengthPrefixedField,
// materialized_edges_assert.go) rather than a raw "|"-joined string. code_calls
// is already proven live on both live gates; a raw join is not injective, so a
// "|" inside a code-entity uid (legal -- these uids are path-derived) could
// silently collapse two distinct edges (see TestCodeCallEdgeKeyIsInjective).
func codeCallEdgeKey(relationshipType, source, target string) string {
	var b strings.Builder
	writeLengthPrefixedField(&b, normalizeCodeCallRelationshipType(relationshipType))
	writeLengthPrefixedField(&b, source)
	writeLengthPrefixedField(&b, target)
	return b.String()
}
