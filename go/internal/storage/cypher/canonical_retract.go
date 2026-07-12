// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "fmt"

// BuildCanonicalWorkloadDependencyUpsert builds a Workload DEPENDS_ON edge
// statement.
func BuildCanonicalWorkloadDependencyUpsert(p CanonicalWorkloadDependencyParams, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    canonicalWorkloadDependencyUpsertCypher,
		Parameters: map[string]any{
			"workload_id":        p.WorkloadID,
			"target_workload_id": p.TargetWorkloadID,
			"evidence_source":    evidenceSource,
		},
	}
}

// BuildCanonicalCodeCallUpsert builds a code relationship statement between
// two canonical code entities.
func BuildCanonicalCodeCallUpsert(p CanonicalCodeCallParams, evidenceSource string) Statement {
	cypher := canonicalCodeCallUpsertCypher
	if p.RelationshipType == "USES_METACLASS" {
		cypher = canonicalMetaclassUpsertCypher
	} else if p.CallKind == "jsx_component" {
		cypher = canonicalJSXComponentReferenceUpsertCypher
	}
	params := map[string]any{
		"caller_entity_id": p.CallerEntityID,
		"callee_entity_id": p.CalleeEntityID,
		"evidence_source":  evidenceSource,
	}
	if p.CallKind != "" {
		params["call_kind"] = p.CallKind
	}
	if p.RelationshipType != "" {
		params["relationship_type"] = p.RelationshipType
	}
	return Statement{
		Operation:  OperationCanonicalUpsert,
		Cypher:     cypher,
		Parameters: params,
	}
}

// --- Retraction builders ---

// BuildRetractRepoDependencyEdges builds a Repository DEPENDS_ON edge
// retraction statement.
func BuildRetractRepoDependencyEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractRepoDependencyEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildRetractWorkloadDependencyEdges builds a Workload DEPENDS_ON edge
// retraction statement.
func BuildRetractWorkloadDependencyEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractWorkloadDependencyEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// codeCallRetractSourceLabels lists the source node labels a code-call edge
// (CALLS/REFERENCES/INSTANTIATES) can originate from.
var codeCallRetractSourceLabels = []string{"Function", "Class", "Struct", "Interface", "TypeAlias", "File"}

// codeCallMetaclassRetractSourceLabels lists the source labels a USES_METACLASS
// edge can originate from — a narrower set than the code-call edges.
var codeCallMetaclassRetractSourceLabels = []string{"Function", "Class", "File"}

// codeCallRetractRelTypes returns the relationship-type disjunction the retract
// deletes for the given evidence source. Relationship-type disjunction is
// supported on NornicDB; node-label disjunction is not (#5116).
func codeCallRetractRelTypes(evidenceSource string) string {
	switch evidenceSource {
	case "parser/code-calls":
		return "CALLS|REFERENCES|INSTANTIATES"
	case "parser/python-metaclass":
		return "USES_METACLASS"
	default:
		return "CALLS|REFERENCES|USES_METACLASS|INSTANTIATES"
	}
}

// codeCallRetractSourceLabelsFor returns the source labels the retract must
// cover for the given evidence source.
func codeCallRetractSourceLabelsFor(evidenceSource string) []string {
	if evidenceSource == "parser/python-metaclass" {
		return codeCallMetaclassRetractSourceLabels
	}
	return codeCallRetractSourceLabels
}

// buildCodeCallRetractStatements emits one retract statement per source label.
//
// A single statement cannot bind all source labels on NornicDB: a node-label
// disjunction MATCH (source:Function|Class|...) matches zero rows, and on
// NornicDB v1.1.11 an unlabeled MATCH (source) scan is unreliable — it silently
// drops some source labels (e.g. File-sourced REFERENCES), inconsistently by
// internal label-iteration state (#5116). Single-label MATCH is the only shape
// that reliably matches every source on both pinned versions, so the retract
// fans out to one statement per label. The relationship-type disjunction and the
// scope predicate are unchanged per statement. scopeField is the source property
// the retract binds ("repo_id" or "path"); scopeParam is the Cypher parameter
// key carrying scopeValues.
func buildCodeCallRetractStatements(scopeField, scopeParam string, scopeValues []string, evidenceSource string) []Statement {
	relTypes := codeCallRetractRelTypes(evidenceSource)
	labels := codeCallRetractSourceLabelsFor(evidenceSource)
	stmts := make([]Statement, 0, len(labels))
	for _, label := range labels {
		cypher := fmt.Sprintf(
			"MATCH (source:%s)-[rel:%s]->()\nWHERE source.%s IN $%s\n  AND rel.evidence_source = $evidence_source\nDELETE rel",
			label, relTypes, scopeField, scopeParam,
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

// BuildRetractCodeCallEdgeStatements builds per-source-label code-intel edge
// retraction statements for all source entities owned by the given repositories.
func BuildRetractCodeCallEdgeStatements(repoIDs []string, evidenceSource string) []Statement {
	return buildCodeCallRetractStatements("repo_id", "repo_ids", repoIDs, evidenceSource)
}

// BuildRetractCodeCallEdgeStatementsByFilePath builds per-source-label code-intel
// edge retraction statements for source entities owned by the given
// repo-qualified file paths.
func BuildRetractCodeCallEdgeStatementsByFilePath(filePaths []string, evidenceSource string) []Statement {
	return buildCodeCallRetractStatements("path", "file_paths", filePaths, evidenceSource)
}

// BuildRetractSQLRelationshipEdges builds a SQL relationship edge retraction
// statement for SQL table query, table reference, containment, trigger, and
// routine execution edges.
func BuildRetractSQLRelationshipEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractSQLRelationshipEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildDeleteOrphanPlatformNodes builds an orphan Platform node cleanup
// statement.
func BuildDeleteOrphanPlatformNodes(evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    deleteOrphanPlatformNodesCypher,
		Parameters: map[string]any{
			"evidence_source": evidenceSource,
		},
	}
}
