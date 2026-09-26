// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

import (
	"github.com/eshu-hq/eshu/go/internal/reducer"
)

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
// (CanonicalRepoDependencyUpsertCypher) and workload_dependency writes it
// Workload→Workload (CanonicalWorkloadDependencyUpsertCypher). Asserting either
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
	// EvidenceSource, when non-empty, additionally requires the edge's own
	// evidence_source property to equal it.
	//
	// Labels are not always enough. RUNS_ON is written with the identical
	// (WorkloadInstance)->(Platform) shape by two live writers: the cross-repo
	// resolver for repo_dependency, and workload materialization for its own
	// domain. Nothing about the type or the endpoints separates them, so without
	// provenance the family's exact set counts the other writer's edges as
	// spurious extras on any graph where workload materialization has run.
	//
	// This asserts the boundary production already enforces:
	// RetractRepoRunsOnEdgesCypher deletes `WHERE rel.evidence_source =
	// $evidence_source`, so the family owns the edges it stamped, not the whole
	// RUNS_ON population. The gate now asserts what the retract reaps.
	EvidenceSource string
	// OneEdgePerEndpointPair, when true, declares that every writer of this
	// type converges on ONE canonical edge per (source, target) node pair, so
	// the live gate must count every label-matching edge's multiplicity per
	// pair BEFORE the EvidenceSource filter drops the other writer's edges.
	//
	// RUNS_ON needs it (#6671). Both writers MERGE
	// `[:RUNS_ON {identity_key: 'canonical'}]` and differ only in the
	// evidence_source they stamp, so a second edge on the same pair — the
	// racing copy #6671 observed carried no stamp at all — is a broken shared
	// identity whichever writer created it. Filtering by provenance first made
	// that duplicate invisible: the resolver-stamped copy matched the expected
	// set exactly and the unstamped copy was skipped as "another writer's".
	//
	// Leave it false where distinct stamps are distinct edges by design, or
	// the gate would report legitimate parallel edges as duplicates.
	OneEdgePerEndpointPair bool
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
		// Provenance-scoped: workload materialization writes this exact shape too
		// (workload_materializer.go), stamped EvidenceSourceWorkloads. Referencing
		// the writer's own constant rather than a copied literal means the
		// assertion cannot drift from what the resolver actually stamps.
		//
		// OneEdgePerEndpointPair: both writers share the canonical identity_key,
		// so a pair's multiplicity is counted across every stamp (#6671).
		"RUNS_ON": {
			FromLabel:              "WorkloadInstance",
			ToLabel:                "Platform",
			EvidenceSource:         reducer.CrossRepoEvidenceSource,
			OneEdgePerEndpointPair: true,
		},
	},
	"workload_dependency": {
		"DEPENDS_ON": {
			FromLabel:      "Workload",
			ToLabel:        "Workload",
			EvidenceSource: reducer.EvidenceSourceWorkloads,
		},
	},
	// TARGETS_ENVIRONMENT is shared, and it is the first DIRECT-materialization
	// family to need a constraint (#6228). CanonicalKubernetesNamespaceWithEnvironmentUpsertCypher
	// writes it KubernetesNamespace->Environment;
	// BatchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher
	// (canonical_relationships.go) writes the same type
	// EvidenceArtifact->Environment for the repo-manifest alias path. Both
	// converge on one canonical Environment node per name by design, so the
	// target label cannot separate them and the type alone certainly cannot:
	// asserting this family by type would count every repo-manifest binding in
	// the graph as a spurious extra.
	//
	// The source label already partitions the two producers. EvidenceSource is
	// declared anyway because it is the predicate the family's own retract
	// scopes by (RetractKubernetesNamespaceStaleTargetsEnvironmentCypher
	// deletes `WHERE rel.evidence_source = $evidence_source`), so the gate
	// asserts exactly the population the reducer owns and reaps rather than
	// every KubernetesNamespace->Environment edge a future third producer might
	// add.
	"kubernetes_namespace_environment": {
		"TARGETS_ENVIRONMENT": {
			FromLabel:      "KubernetesNamespace",
			ToLabel:        "Environment",
			EvidenceSource: reducer.KubernetesNamespaceEvidenceSource,
		},
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
