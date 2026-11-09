// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package materialized

import edgewriter "github.com/eshu-hq/eshu/go/internal/storage/cypher/edge/writer"

// init registers the runtime_declaration family (#8816) in
// singleTypeMaterializedEdgeFamilies from its own file, because
// single_type_families.go already sits past the 500-line cap. The entry is an
// ordinary single-type row: the registry, the drift guards and
// SingleTypeMaterializedEdgeTypes read it exactly as if it were in the table
// literal, since a package-level map is initialized before any init runs.
//
// The family writes one relationship type, DECLARES_RUNTIME, from the owning
// Repository to a RuntimeDeclaration node only this writer MERGEs. Both held
// retract variants name the type in their Repository-anchored expand and
// DETACH DELETE the owned nodes, so the edges go with them; the retracts scope
// by repository id (a node property the writer stamps), not by an edge
// scope_id, which is the pre-contract predicate shape this table permits. The
// edge MERGE carries no property map, so the family has no identity
// properties beyond its two endpoints.
func init() {
	// advisory_group (#10411) shares this init because the port census
	// keys scanned symbols by name: a second init in this package would
	// collide with this one. The family writes one relationship type,
	// AFFECTED_BY, from a Repository to an AdvisoryGroup node only this
	// writer MERGEs, keyed by the edge's package_id identity property.
	// Its stale retract anchors on the group node and complements a
	// $keep pair list; the held variants delete an edgeless group and,
	// for a group with no qualifying winners, delete the node with its
	// edges in the same shape. No evidence_source, scope, or generation
	// stamps exist on node or edge, so no retract scopes by them: the
	// group id anchor is the scope.
	singleTypeMaterializedEdgeFamilies["advisory_group"] = materializedEdgeFamily{
		EdgeTypes: map[string]string{
			"AFFECTED_BY": "repository-to-advisory-group edge (AdvisoryGroupWriter in edge/writer/advisory_group.go)",
		},
		RetractCypher: edgewriter.AdvisoryGroupStaleEdgeRetractCypher,
		RetractVariants: []string{
			edgewriter.AdvisoryGroupDeleteEdgelessCypher,
			edgewriter.AdvisoryGroupEmptyRetractCypher,
		},
		IdentityCypher:     edgewriter.AdvisoryGroupUpsertCypher,
		IdentityProperties: []string{"package_id"},
	}

	singleTypeMaterializedEdgeFamilies["runtime_declaration"] = materializedEdgeFamily{
		EdgeTypes: map[string]string{
			"DECLARES_RUNTIME": "repository-to-declaration edge (RuntimeDeclarationWriter in edge/writer/runtime_declaration.go)",
		},
		RetractCypher:   edgewriter.RetractRuntimeDeclarationsCypher,
		RetractVariants: []string{edgewriter.RetractRuntimeDeclarationsByPathCypher},
		IdentityCypher:  edgewriter.RuntimeDeclarationUpsertCypher,
	}
}
