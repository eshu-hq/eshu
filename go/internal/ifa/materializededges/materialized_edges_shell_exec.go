// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// shellExecFamily is the materialized-edge family key this guard asserts,
// mirroring codeownersOwnershipFamily's role for its own family: it names the
// fixture's identity contract as well as its edge-type registry, so
// MaterializedEdgeOduResolver.Resolve's dispatch and this file's own
// LoadExpectedEdges/MaterializedEdgeDomainEdgeTypes calls can never be
// pointed at different families by a typo in one of them. Moved here
// entirely (not duplicated) from ifa's shell_exec_family_odu.go: nothing in
// ifa needs this identifier any more.
const shellExecFamily = "shell_exec"

// resolveShellExecMaterializedEdges is the shell_exec family's vacuity guard
// (#6001), mirroring resolveDocumentationEdgeMaterializedEdges's three-step
// shape: (1) load the hand-derived expected-edge-set fixture and assert it is
// non-empty, (2) assert it covers every relationship type the family's writer
// registry accepts, (3) run the production extractor over odu.Facts and
// assert the result matches the fixture EXACTLY, then check the reported
// repository fan-out the way resolveRationaleEdgeMaterializedEdges does --
// reducer.ExtractShellExecRows shares that function's (repoIDs, rows)
// signature, not documentation's WithQuarantine shape.
//
// shell_exec is single-type (EXECUTES_SHELL only) with no declared
// relationship-MERGE identity properties
// (cypher.MaterializedEdgeIdentityProperties("shell_exec") returns an empty
// map), so this guard uses the shared ExpectedEdge {relationship_type,
// source_entity_id, target_entity_id} shape directly, the same fixture format
// the live `eshu-ifa assert-edges -domain shell_exec` gate reads through
// LoadExpectedEdges -- no family-specific node-record schema like rationale
// needs.
func resolveShellExecMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, shellExecFamily)
	if err != nil {
		return false, err.Error()
	}
	if len(expected) == 0 {
		return false, fmt.Sprintf("odù %q: shell exec expected edges %s declares no edges; an empty expected set would make the gate vacuous", odu.Name, expectedEdgesPath)
	}
	registry, err := MaterializedEdgeDomainEdgeTypes(shellExecFamily)
	if err != nil {
		return false, err.Error()
	}
	if missing := missingShellExecExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}

	repoIDs, rows := reducer.ExtractShellExecRows(odu.Facts)
	actual := shellExecRowsToExpectedEdges(rows)
	if mismatch := compareShellExecExpectedEdges(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}

	// Every expected edge names a repo (via its source/target uids sharing the
	// fixture's single repository), so the reported repo set must be exactly
	// the one repository the fixture's file facts declare. A stale or empty
	// list would scope the emitted durable intents to the wrong repositories
	// while the edge set itself looked correct.
	wantRepos := map[string]struct{}{ifa.ShellExecFamilyRepoID: {}}
	gotRepos := map[string]struct{}{}
	for _, r := range repoIDs {
		gotRepos[r] = struct{}{}
	}
	if len(gotRepos) != len(wantRepos) {
		return false, fmt.Sprintf("odù %q: extractor reports %d repository/ies, want exactly %d; the durable intents would be scoped to the wrong repositories",
			odu.Name, len(gotRepos), len(wantRepos))
	}
	for repo := range wantRepos {
		if _, ok := gotRepos[repo]; !ok {
			return false, fmt.Sprintf("odù %q: expected edges name repository %q, which the extractor does not report", odu.Name, repo)
		}
	}

	return true, fmt.Sprintf("odù %q: ExtractShellExecRows reproduces the expected %d-edge set exactly across %d repository/ies, from %d fact envelope(s)",
		odu.Name, len(expected), len(wantRepos), len(odu.Facts))
}

// missingShellExecExpectedTypes reports any registry-owned EXECUTES_SHELL
// relationship type the expected-edge fixture never exercises. Single-type
// today (only "EXECUTES_SHELL"), but registry-derived rather than
// hand-checked so a second shell-exec writer relationship type cannot go
// silently unasserted, mirroring missingDocumentationExpectedTypes.
func missingShellExecExpectedTypes(expected []ExpectedEdge, registry map[string]struct{}) []string {
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

// shellExecRowsToExpectedEdges adapts ExtractShellExecRows's []map[string]any
// row shape into the shared ExpectedEdge identity the comparison keys on.
// shell_exec declares no relationship-MERGE identity properties, so Identity
// is left empty. RelationshipType is read from the row (reducer.ExtractShellExecRows
// stamps "relationship_type") rather than hardcoded, mirroring
// inheritanceRowsToExpectedEdges: a hardcoded literal here would make
// compareShellExecExpectedEdges compare a fixed string against itself, unable
// to catch the extractor emitting anything other than EXECUTES_SHELL.
func shellExecRowsToExpectedEdges(rows []map[string]any) []ExpectedEdge {
	out := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		out = append(out, ExpectedEdge{
			RelationshipType: anyToStringValue(row["relationship_type"]),
			SourceEntityID:   anyToStringValue(row["source_entity_id"]),
			TargetEntityID:   anyToStringValue(row["target_entity_id"]),
		})
	}
	return out
}

// compareShellExecExpectedEdges reports the exact-set mismatch between the
// hand-derived expectation and what the extractor actually produced, naming
// both directions -- mirroring compareDocumentationExpectedEdges. MISSING
// means the extractor stopped projecting an edge the graph is supposed to
// carry; EXTRA means it started projecting one nobody derived, the direction
// a "contains all expected" check would miss.
func compareShellExecExpectedEdges(oduName string, expected, actual []ExpectedEdge) string {
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
	return fmt.Sprintf("odù %q: shell exec edge set does not match %d hand-derived edge(s); MISSING: %s; EXTRA: %s",
		oduName, len(expected), strings.Join(missing, ", "), strings.Join(extra, ", "))
}
