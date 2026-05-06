package cypher

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

// BuildRetractInfrastructurePlatformEdges builds a PROVISIONS_PLATFORM edge
// retraction statement.
func BuildRetractInfrastructurePlatformEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractInfrastructurePlatformEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

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

// BuildRetractCodeCallEdges builds a code-intel edge retraction statement for
// all source entities owned by the given repositories.
func BuildRetractCodeCallEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractCodeCallEdgesCypher(evidenceSource),
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

func retractCodeCallEdgesCypher(evidenceSource string) string {
	switch evidenceSource {
	case "parser/code-calls":
		return retractCodeCallParserEdgesCypher
	case "parser/python-metaclass":
		return retractCodeCallMetaclassEdgesCypher
	default:
		return retractCodeCallFallbackEdgesCypher
	}
}

// BuildRetractInheritanceEdges builds an INHERITS edge retraction statement.
func BuildRetractInheritanceEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractInheritanceEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildRetractSQLRelationshipEdges builds a SQL relationship edge retraction
// statement for REFERENCES_TABLE, HAS_COLUMN, and TRIGGERS edges.
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
