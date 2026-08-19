// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package ifa

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/relationships"
)

// repoDependencyEdgesFamily is the materialized-edge family key this guard
// asserts. It MUST equal "repo_dependency" -- the domain key
// MaterializedEdgeDomainEdgeTypes switches on (materialized_edges_assert.go)
// and materializedEdgeEndpointsByFamily indexes by
// (go/internal/storage/cypher/materialized_edge_endpoints.go) -- not
// "repo_dependency_edges": unlike every sibling family added under the #5543
// umbrella, this one's surface name has no "_edges" suffix, because it was
// registered before the #5543 naming convention and nothing has renamed it.
const repoDependencyEdgesFamily = "repo_dependency"

// repoDependencyRunsOnType is the one registry type this guard deliberately
// does NOT prove. See this file's package-level doc comment.
const repoDependencyRunsOnType = "RUNS_ON"

// repoDependencyGuardClock is the fixed, deterministic wall-clock reading
// this guard hands to reducer.ExtractRepoDependencyIntentRows for every row's
// CreatedAt. go/internal/ifa/AGENTS.md forbids wall-clock time inside Ifá
// derivation; CreatedAt is never part of the edge-identity comparison this
// guard performs (mirrors deployableUnitGuardClock's role exactly).
var repoDependencyGuardClock = time.Date(2026, 8, 19, 0, 0, 0, 0, time.UTC)

// repoDependencyGuardSourceRunID is the fixed source_run_id this guard hands
// to reducer.ExtractRepoDependencyIntentRows. Production derives this from
// the scope ID being resolved (crossRepoContributionSourceRunID); the guard
// hard-codes it instead because it is never part of the edge-identity
// comparison this guard performs (Payload carries it, but ExpectedEdge only
// compares relationship_type/repo_id/target_repo_id).
const repoDependencyGuardSourceRunID = "ifa-repo-dependency-guard-run"

// repoDependencyExpectedEdgesRelPath is where the family's hand-derived
// expected-edge-set fixture lives. Under go/internal/ifa/testdata/, not
// testdata/cassettes/, for the same reason the sibling families' fixtures
// are: the offline cassette validator globs every testdata/cassettes/*/*.json
// as a replay cassette, and this file is a gate ASSERTION, not a cassette.
const repoDependencyExpectedEdgesRelPath = "go/internal/ifa/testdata/repodependency/ifa-repo-dependency-family-expected-edges.json"

// repoDependencyFamilyExpectedEdgesPath joins repoRoot onto the expected-edge
// fixture.
func repoDependencyFamilyExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, repoDependencyExpectedEdgesRelPath)
}

// resolveRepoDependencyMaterializedEdges is repo_dependency's named vacuity
// guard, mirroring resolveSQLRelationshipMaterializedEdges /
// resolveCodeCallMaterializedEdges's role for their multi-type families.
//
// Unlike deployable_unit_edges, this family's SIX repo-to-repo relationship
// types (DEPENDS_ON, DEPLOYS_FROM, DISCOVERS_CONFIG_IN,
// PROVISIONS_DEPENDENCY_FOR, USES_MODULE, READS_CONFIG_FROM) derive from an
// Odù's own facts through the SAME two production seams
// deployable_unit_edges runs -- DiscoveredEvidence (relationships.Resolve's
// evidence-discovery wrapper) then relationships.Resolve -- with no third,
// resolved-relationship-dependent extraction step: production's own
// conversion from resolved relationships to repo_dependency intent rows
// (buildResolvedEdgeIntentRow, cross_repo_intent_row.go) is itself a pure
// function of `resolved` alone, exposed to this package as
// reducer.ExtractRepoDependencyIntentRows. A guard that hand-authored a
// relationships.ResolvedRelationship instead would certify an edge no live
// graph backend could actually reach, the same false-green class the #5351
// live-proof finding warns about for endpoint identity that
// resolveDeployableUnitMaterializedEdges's doc comment names; running the
// real resolver closes that gap here too.
//
// RUNS_ON, cypher.RepoDependencyMaterializedEdgeTypes's seventh registry
// type, is a DELIBERATE, DOCUMENTED gap this guard does not prove. Two
// separate reasons compound:
//
//  1. Its endpoint shape is (WorkloadInstance)-[:RUNS_ON]->(Platform), not
//     Repository->Repository, reached through DEFINES/INSTANCE_OF edges a
//     DIFFERENT domain (workload materialization) writes -- an Odù carrying
//     only repository/content facts cannot produce a WorkloadInstance node
//     to anchor the edge on at all.
//  2. Even given a WorkloadInstance, buildResolvedEdgeIntentRow's RelRunsOn
//     arm writes payload["platform_id"] = r.TargetEntityID verbatim, and the
//     live writer (canonicalRunsOnUpsertCypher) MATCHes a Platform node by
//     that id. Production stamps that id from reducer.CanonicalPlatformID,
//     which no fact-only extraction seam can reproduce byte-for-byte outside
//     the real platform-inference pipeline this guard deliberately does not
//     run.
//
// This is a DIFFERENT gap from the three types
// cypher.RepoDependencyMaterializedEdgeTypes never even registers
// (HAS_DEPLOYMENT_EVIDENCE, EVIDENCES_REPOSITORY_RELATIONSHIP,
// TARGETS_ENVIRONMENT -- excluded because their EvidenceArtifact endpoint id
// embeds the generation id and ordinal, so no exact hand-derived set could
// ever pin them). RUNS_ON IS in the registry; this guard's completeness
// check (repoDependencyProvableRegistryTypes) explicitly subtracts it rather
// than silently passing a registry check that would otherwise demand
// coverage this Odù cannot honestly provide.
//
// What this guard DOES catch: a regression in Terraform app_repo/module-
// source, terragrunt config_path, Docker Compose depends_on/build-context, or
// Terraform IAM SSM-read evidence discovery; a regression in
// relationships.Resolve's confidence gating for any of those six evidence
// kinds; a regression in reducer.ExtractRepoDependencyIntentRows'/
// buildResolvedEdgeIntentRow's per-type routing (repo_id/target_repo_id
// payload keys); and a regression in the catalog matcher's same-repo
// exclusion or token-boundary discipline (the self-reference and near-miss-
// alias negative cases -- see repoDependencyFamilyOdu's doc comment). What it
// does NOT catch: anything downstream of the live Cypher writer (that is
// `eshu-ifa assert-edges`' job once a coverage-manifest row and live
// determinism/fault-injection cells exist for this family), and RUNS_ON as
// described above.
func resolveRepoDependencyMaterializedEdges(odu Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, repoDependencyEdgesFamily)
	if err != nil {
		return false, err.Error()
	}
	registry, err := MaterializedEdgeDomainEdgeTypes(repoDependencyEdgesFamily)
	if err != nil {
		return false, err.Error()
	}
	provable := repoDependencyProvableRegistryTypes(registry)
	if missing := missingRepoDependencyExpectedTypes(expected, provable); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover every fixture-provable registry type, missing: %v", odu.Name, missing)
	}
	for _, edge := range expected {
		if edge.RelationshipType == repoDependencyRunsOnType {
			return false, fmt.Sprintf("odù %q: expected-edge fixture carries a %s edge, but this guard deliberately does not prove RUNS_ON (see this file's package doc comment) -- remove it from the fixture or extend the guard's scope first", odu.Name, repoDependencyRunsOnType)
		}
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}

	evidence := DiscoveredEvidence(odu)
	if len(evidence) != len(expected) {
		return false, fmt.Sprintf("odù %q: DiscoveredEvidence produced %d evidence facts, want exactly %d; the self-reference and near-miss-alias negative-case content facts must each produce zero evidence", odu.Name, len(evidence), len(expected))
	}

	_, resolved := relationships.Resolve(evidence, nil, relationships.DefaultConfidenceThreshold)
	if len(resolved) == 0 {
		return false, fmt.Sprintf("odù %q: the production evidence/resolution seams (DiscoveredEvidence -> relationships.Resolve) found zero resolved relationships in its own facts; repo_dependency cannot admit any edge without at least one resolved relationship", odu.Name)
	}

	first := odu.Facts[0]
	rows, _ := reducer.ExtractRepoDependencyIntentRows(resolved, first.ScopeID, repoDependencyGuardSourceRunID, first.GenerationID, repoDependencyGuardClock)
	if len(rows) != len(resolved) {
		return false, fmt.Sprintf("odù %q: reducer.ExtractRepoDependencyIntentRows dropped %d of %d resolved relationships (a dropped row means its resolved relationship carried no target_repo_id/platform_id); every relationship this Odù resolves is expected to route to a row", odu.Name, len(resolved)-len(rows), len(resolved))
	}

	actual := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		actual = append(actual, ExpectedEdge{
			RelationshipType: anyToStringValue(row.Payload["relationship_type"]),
			SourceEntityID:   anyToStringValue(row.Payload["repo_id"]),
			TargetEntityID:   repoDependencyRowTargetEntityID(row),
		})
	}
	if mismatch := compareRepoDependencyExpectedEdges(odu.Name, expected, actual); mismatch != "" {
		return false, mismatch
	}

	return true, fmt.Sprintf(
		"odù %q: DiscoveredEvidence -> relationships.Resolve -> reducer.ExtractRepoDependencyIntentRows reproduces the expected %d-edge set exactly across %d of %d registry types (RUNS_ON excluded by design), and the self-reference/near-miss-alias negative cases produced zero spurious evidence",
		odu.Name, len(expected), len(provable), len(registry),
	)
}

// repoDependencyRowTargetEntityID reads a row's target entity id off the
// payload key its relationship_type actually populates.
// buildResolvedEdgeIntentRow (cross_repo_intent_row.go) writes RelRunsOn rows
// under "platform_id" and every other repo_dependency type under
// "target_repo_id" -- this guard never produces a RUNS_ON row (see this
// file's package doc comment), but reading the wrong key defensively here
// would silently render an empty TargetEntityID instead of failing loudly, so
// the dispatch is explicit rather than a single hard-coded key.
func repoDependencyRowTargetEntityID(row reducer.SharedProjectionIntentRow) string {
	if anyToStringValue(row.Payload["relationship_type"]) == repoDependencyRunsOnType {
		return anyToStringValue(row.Payload["platform_id"])
	}
	return anyToStringValue(row.Payload["target_repo_id"])
}

// repoDependencyProvableRegistryTypes returns registry minus RUNS_ON -- the
// subset of cypher.RepoDependencyMaterializedEdgeTypes this guard can
// honestly require an expected-edge fixture to cover. See
// resolveRepoDependencyMaterializedEdges's doc comment for why RUNS_ON is
// excluded.
func repoDependencyProvableRegistryTypes(registry map[string]struct{}) map[string]struct{} {
	out := make(map[string]struct{}, len(registry))
	for edgeType := range registry {
		if edgeType == repoDependencyRunsOnType {
			continue
		}
		out[edgeType] = struct{}{}
	}
	return out
}

func missingRepoDependencyExpectedTypes(expected []ExpectedEdge, provable map[string]struct{}) []string {
	present := make(map[string]struct{}, len(expected))
	for _, edge := range expected {
		present[edge.RelationshipType] = struct{}{}
	}
	var missing []string
	for edgeType := range provable {
		if _, ok := present[edgeType]; !ok {
			missing = append(missing, edgeType)
		}
	}
	sort.Strings(missing)
	return missing
}

func compareRepoDependencyExpectedEdges(oduName string, expected, actual []ExpectedEdge) string {
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
	return fmt.Sprintf("odù %q: repo-dependency edge set does not match %d hand-derived edge(s); MISSING: %s; EXTRA: %s", oduName, len(expected), strings.Join(missing, ", "), strings.Join(extra, ", "))
}
