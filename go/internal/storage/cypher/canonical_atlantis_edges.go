// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/projector/canonical"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// This file is a thin adapter over the canonical/node leaf: it owns the
// Statement wrapping for the Atlantis governance edge family, while the leaf
// owns the Cypher templates, entity collection, Go resolution, and
// statement-free parts (node.AtlantisEdgeParts). The explicit append in
// buildStructuralEdgeStatements keeps calling atlantisEdgeStatements, so the
// family order (atlantis < flux < gitlab < helm) and the retract-before-upsert
// order are preserved exactly.

// wrapNodeEdgeParts maps canonical/node leaf parts onto executable
// Statements: retract parts become OperationCanonicalRetract, upsert parts
// OperationCanonicalUpsert, and the Drain flag passes through so the NornicDB
// phase-group executor keeps running retracts as standalone autocommit
// statements. A nil or empty part slice yields nil, so families with no rows
// for this materialization still emit no statements.
func wrapNodeEdgeParts(parts []node.Part) []Statement {
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

// atlantisEdgeStatements wraps the canonical/node leaf's Statement-free
// Atlantis parts into executable statements. Returns nil for non-Atlantis
// repos, mirroring the leaf.
func atlantisEdgeStatements(mat canonical.CanonicalMaterialization) []Statement {
	return wrapNodeEdgeParts(node.AtlantisEdgeParts(mat))
}
