// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Endpoint-label constraints for materialized-edge families whose relationship
// types are shared with another family (#5543).
//
// `eshu-ifa assert-edges` filters a family's edges by relationship TYPE alone,
// which is weaker than the ownership the writers and retracts actually express:
// the production retracts are label-scoped. That gap is invisible while each
// family owns its types outright, and becomes wrong the moment two families
// share one.
//
// DEPENDS_ON is shared. repo_dependency writes it Repository→Repository
// (canonicalRepoDependencyUpsertCypher) and workload_dependency writes it
// Workload→Workload (canonicalWorkloadDependencyUpsertCypher). Asserting either
// family by type alone would count the other family's edges as spurious
// extras — so proving both in one batched live cell, which is the plan, needs
// the endpoints to partition them.
//
// The constraint is per EDGE TYPE, not per family. A per-family constraint
// would be wrong for repo_dependency: six of its seven types are
// Repository→Repository, but RUNS_ON is (WorkloadInstance)→(Platform), reached
// through DEFINES/INSTANCE_OF and written by workload materialization.

// MaterializedEdgeEndpoint names the source and target node labels an edge of a
// given type must connect for it to belong to the family.
type MaterializedEdgeEndpoint struct {
	// FromLabel is the required source node label.
	FromLabel string
	// ToLabel is the required target node label.
	ToLabel string
}

// materializedEdgeEndpointsByFamily holds constraints only for the families that
// need them. A family absent here owns its types outright and is asserted by
// type alone, exactly as before.
//
// Deliberately narrow: adding a constraint for a family that does NOT share its
// types can only narrow what the gate asserts, which is the silent false-green
// direction. Constraints are added when a type collision is proven, not
// pre-emptively.
var materializedEdgeEndpointsByFamily = map[string]map[string]MaterializedEdgeEndpoint{
	"repo_dependency": {
		"DEPENDS_ON":                {FromLabel: "Repository", ToLabel: "Repository"},
		"DEPLOYS_FROM":              {FromLabel: "Repository", ToLabel: "Repository"},
		"DISCOVERS_CONFIG_IN":       {FromLabel: "Repository", ToLabel: "Repository"},
		"PROVISIONS_DEPENDENCY_FOR": {FromLabel: "Repository", ToLabel: "Repository"},
		"USES_MODULE":               {FromLabel: "Repository", ToLabel: "Repository"},
		"READS_CONFIG_FROM":         {FromLabel: "Repository", ToLabel: "Repository"},
		"RUNS_ON":                   {FromLabel: "WorkloadInstance", ToLabel: "Platform"},
	},
	"workload_dependency": {
		"DEPENDS_ON": {FromLabel: "Workload", ToLabel: "Workload"},
	},
}

// MaterializedEdgeEndpointLabels returns the per-edge-type endpoint constraints
// for a family.
//
// The second return is false when the family has no constraints, meaning its
// edges are matched by relationship type alone. Callers MUST treat "no
// constraints" as "match every edge of the family's types" — never as "match
// nothing", which would make the live gate assert an empty population and pass
// any graph.
func MaterializedEdgeEndpointLabels(family string) (map[string]MaterializedEdgeEndpoint, bool) {
	constraints, ok := materializedEdgeEndpointsByFamily[family]
	if !ok {
		return nil, false
	}
	out := make(map[string]MaterializedEdgeEndpoint, len(constraints))
	for edgeType, endpoint := range constraints {
		out[edgeType] = endpoint
	}
	return out, true
}
