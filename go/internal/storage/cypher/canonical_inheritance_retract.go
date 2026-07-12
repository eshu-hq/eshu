// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "fmt"

// inheritanceRetractChildLabels lists the child node labels an inheritance edge
// (INHERITS/OVERRIDES/ALIASES/IMPLEMENTS) can originate from.
var inheritanceRetractChildLabels = []string{"Function", "Class", "Interface", "Trait", "Struct", "Enum", "Protocol"}

// buildInheritanceRetractStatements emits one retract statement per child label.
//
// A single statement cannot bind all child labels on NornicDB: a node-label
// disjunction MATCH (child:Function|Class|...) matches zero rows, and on
// NornicDB v1.1.11 an unlabeled MATCH (child) scan is unreliable — it silently
// drops some child labels. Single-label MATCH is the only shape that reliably
// matches every child on both pinned versions, so the retract fans out to one
// statement per label (#5116/#4367). The relationship-type disjunction and the
// scope predicate are unchanged per statement. scopeField is the child property
// the retract binds ("repo_id" or "path"); scopeParam is the Cypher parameter
// key carrying scopeValues. The statements run sequentially, not grouped (see
// executeInheritanceRetractStatements).
func buildInheritanceRetractStatements(scopeField, scopeParam string, scopeValues []string, evidenceSource string) []Statement {
	stmts := make([]Statement, 0, len(inheritanceRetractChildLabels))
	for _, label := range inheritanceRetractChildLabels {
		cypher := fmt.Sprintf(
			"MATCH (child:%s)-[rel:INHERITS|OVERRIDES|ALIASES|IMPLEMENTS]->()\nWHERE child.%s IN $%s\n  AND rel.evidence_source = $evidence_source\nDELETE rel",
			label, scopeField, scopeParam,
		)
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    cypher,
			Parameters: map[string]any{
				scopeParam:        scopeValues,
				"evidence_source": evidenceSource,
			},
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
