// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/flux"
)

// fluxReconcilesFromEdgeStatements wraps the canonical/flux leaf's
// Statement-free parts (flux.EdgeParts) into executable Statements. The
// explicit append in buildStructuralEdgeStatements keeps calling this name,
// so the family order (atlantis < flux < gitlab < helm) and the intra-flux
// retract-before-upsert order are preserved exactly. Returns nil for
// non-Flux repos, mirroring the leaf.
func fluxReconcilesFromEdgeStatements(mat canonical.CanonicalMaterialization) []Statement {
	parts := flux.EdgeParts(mat)
	if len(parts) == 0 {
		return nil
	}
	stmts := make([]Statement, 0, len(parts))
	for _, part := range parts {
		operation := OperationCanonicalUpsert
		if part.IsRetract {
			operation = OperationCanonicalRetract
		}
		stmts = append(stmts, Statement{
			Operation:  operation,
			Cypher:     part.Cypher,
			Parameters: part.Parameters,
			Drain:      part.Drain,
		})
	}
	return stmts
}

// FluxRelationshipMaterializedEdgeTypes forwards to the canonical/flux leaf
// registry: the graph relationship types the Flux canonical edge writer
// actually accepts, mapped to the write reason recorded on each MERGEd edge.
func FluxRelationshipMaterializedEdgeTypes() map[string]string {
	return flux.MaterializedEdgeTypes()
}
