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
	singleTypeMaterializedEdgeFamilies["runtime_declaration"] = materializedEdgeFamily{
		EdgeTypes: map[string]string{
			"DECLARES_RUNTIME": "repository-to-declaration edge (RuntimeDeclarationWriter in edge/writer/runtime_declaration.go)",
		},
		RetractCypher:   edgewriter.RetractRuntimeDeclarationsCypher,
		RetractVariants: []string{edgewriter.RetractRuntimeDeclarationsByPathCypher},
		IdentityCypher:  edgewriter.RuntimeDeclarationUpsertCypher,
	}
}
