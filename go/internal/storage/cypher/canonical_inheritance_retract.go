// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/inheritance"
)

// This file is a thin adapter over the canonical/inheritance leaf: it owns
// the Statement wrapping for the inheritance edge retract, while the leaf
// owns the child-label list, the per-label fan-out, and the statement-free
// parts (inheritance.InheritanceRetractParts). The Ifá live baseline gate
// reads inheritance.MaterializedEdgeTypes; the root registry test
// (TestInheritanceRegistryMatchesRetractDisjunction) keeps binding that
// registry to the retract disjunction through buildInheritanceRetractStatements
// below, unchanged.

// buildInheritanceRetractStatements emits one retract statement per child
// label by mapping the leaf's parts onto Statements.
func buildInheritanceRetractStatements(scopeField, scopeParam string, scopeValues []string, evidenceSource string) []Statement {
	parts := inheritance.InheritanceRetractParts(scopeField, scopeParam, scopeValues, evidenceSource)
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
		})
	}
	return stmts
}

// BuildRetractInheritanceEdgeStatements builds per-child-label inheritance edge
// retraction statements for child entities owned by the given repositories.
func BuildRetractInheritanceEdgeStatements(repoIDs []string, evidenceSource string) []Statement {
	return buildInheritanceRetractStatements("repo_id", "repo_ids", repoIDs, evidenceSource)
}

// BuildRetractInheritanceEdgeStatementsByFilePath builds per-child-label
// inheritance edge retraction statements for child entities owned by the given
// repo-qualified file paths.
func BuildRetractInheritanceEdgeStatementsByFilePath(filePaths []string, evidenceSource string) []Statement {
	return buildInheritanceRetractStatements("path", "file_paths", filePaths, evidenceSource)
}
