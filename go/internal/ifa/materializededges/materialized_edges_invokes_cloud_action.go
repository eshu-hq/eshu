// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materializededges

import (
	"fmt"
	"path/filepath"

	"github.com/eshu-hq/eshu/go/internal/ifa"
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

// invokesCloudActionFamily is the materialized-edge family key this guard
// asserts (#5997), matching the domain key MaterializedEdgeDomainEdgeTypes
// switches on and the registry entry cypher.SingleTypeMaterializedEdgeTypes
// resolves it through.
const invokesCloudActionFamily = "invokes_cloud_action"

// invokesCloudActionExpectedEdgesPath joins repoRoot onto the family's
// hand-derived expected-edge-set fixture.
func invokesCloudActionExpectedEdgesPath(repoRoot string) string {
	return filepath.Join(repoRoot, "go/internal/ifa/testdata/invokescloudaction/ifa-invokes-cloud-action-family-expected-edges.json")
}

// resolveInvokesCloudActionMaterializedEdges is invokes_cloud_action's named
// vacuity guard (#5997).
//
// Unlike its two siblings, INVOKES_CLOUD_ACTION is a direct 1:1 row-to-edge
// mapping: its intent-level dedupe key (function_id, action) already equals
// its graph MERGE identity (Function, INVOKES_CLOUD_ACTION, CloudAction), and
// its CloudAction target is created inline by the writer's own MERGE ON
// CREATE (cloud-action:<action>, a deterministic id with no cross-
// acceptance-unit MATCH dependency) -- unlike HANDLES_ROUTE's Endpoint or
// RUNS_IN's Workload, it needs no separate workload-materialization
// projection to resolve its target. No family-specific dedupe or fan-out
// helper is needed; the shared rows-to-edges conversion below is a plain
// one-for-one mapping.
func resolveInvokesCloudActionMaterializedEdges(odu ifa.Odu, expectedEdgesPath string) (bool, string) {
	expected, err := LoadExpectedEdges(expectedEdgesPath, invokesCloudActionFamily)
	if err != nil {
		return false, err.Error()
	}
	registry, err := MaterializedEdgeDomainEdgeTypes(invokesCloudActionFamily)
	if err != nil {
		return false, err.Error()
	}
	if missing := missingSymbolRuntimeExpectedTypes(expected, registry); len(missing) > 0 {
		return false, fmt.Sprintf("odù %q: expected-edge set does not cover all registry types, missing: %v", odu.Name, missing)
	}
	relType, err := singleRegistryEdgeType(invokesCloudActionFamily, registry)
	if err != nil {
		return false, err.Error()
	}

	if len(odu.Facts) == 0 {
		return false, fmt.Sprintf("odù %q: carries no facts", odu.Name)
	}
	rows := symbolRuntimeUpsertRows(odu.Facts, reducer.DomainInvokesCloudAction)
	if len(rows) == 0 {
		return false, fmt.Sprintf("odù %q: ExtractSymbolRuntimeIntentRows produced zero INVOKES_CLOUD_ACTION upsert rows; this fixture cannot prove anything", odu.Name)
	}

	actual := invokesCloudActionRowsToExpectedEdges(rows, relType)
	if mismatch := compareSymbolRuntimeExpectedEdges(odu.Name, "invokes cloud action", expected, actual); mismatch != "" {
		return false, mismatch
	}
	return true, fmt.Sprintf(
		"odù %q: ExtractSymbolRuntimeIntentRows reproduces the expected %d-edge INVOKES_CLOUD_ACTION set exactly across %d intent row(s) and %d fact envelope(s); the non-catalog (widget, Frobnicate) call on the same caller function produced zero spurious rows",
		odu.Name, len(expected), len(rows), len(odu.Facts),
	)
}

// invokesCloudActionRowsToExpectedEdges converts INVOKES_CLOUD_ACTION upsert
// rows one-for-one into ExpectedEdge, reading the resolved action id off
// "action_id" (never "action": that payload key is reserved as the
// upsert/refresh/delete discriminator, see
// go/internal/reducer/invokes_cloud_action_intents.go's own comment). No
// dedupe is needed here: the extractor's own (function_id, action) dedupe
// key already equals the edge identity.
func invokesCloudActionRowsToExpectedEdges(rows []reducer.SharedProjectionIntentRow, relationshipType string) []ExpectedEdge {
	edges := make([]ExpectedEdge, 0, len(rows))
	for _, row := range rows {
		edges = append(edges, ExpectedEdge{
			RelationshipType: relationshipType,
			SourceEntityID:   anyToStringValue(row.Payload["function_id"]),
			TargetEntityID:   anyToStringValue(row.Payload["action_id"]),
		})
	}
	return edges
}
