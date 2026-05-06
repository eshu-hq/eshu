package cypher

import "fmt"

const retractSQLViewReferencesTableEdgesCypher = `MATCH (source:SqlView)-[rel:REFERENCES_TABLE]->()
WHERE source.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLFunctionReferencesTableEdgesCypher = `MATCH (source:SqlFunction)-[rel:REFERENCES_TABLE]->()
WHERE source.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLTableHasColumnEdgesCypher = `MATCH (source:SqlTable)-[rel:HAS_COLUMN]->()
WHERE source.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLTriggerEdgesCypher = `MATCH (source:SqlTrigger)-[rel:TRIGGERS]->()
WHERE source.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

var sqlRelationshipEntityLabels = map[string]struct{}{
	"SqlColumn":   {},
	"SqlFunction": {},
	"SqlIndex":    {},
	"SqlTable":    {},
	"SqlTrigger":  {},
	"SqlView":     {},
}

func buildSQLRelationshipRowMap(
	payload map[string]any,
	evidenceSource string,
) (string, map[string]any, bool) {
	sourceEntityID := payloadString(payload, "source_entity_id")
	targetEntityID := payloadString(payload, "target_entity_id")
	if sourceEntityID == "" || targetEntityID == "" {
		return "", nil, false
	}
	relationshipType := payloadString(payload, "relationship_type")
	rowMap := map[string]any{
		"source_entity_id":  sourceEntityID,
		"target_entity_id":  targetEntityID,
		"relationship_type": relationshipType,
		"evidence_source":   evidenceSource,
	}

	sourceLabel := payloadString(payload, "source_entity_type")
	targetLabel := payloadString(payload, "target_entity_type")
	if isSQLRelationshipEntityLabel(sourceLabel) && isSQLRelationshipEntityLabel(targetLabel) {
		if cypher, ok := labelScopedSQLRelationshipCypher(relationshipType, sourceLabel, targetLabel); ok {
			return cypher, rowMap, true
		}
	}

	switch relationshipType {
	case "REFERENCES_TABLE":
		return batchCanonicalSQLRelationshipUpsertCypher, rowMap, true
	case "HAS_COLUMN":
		return batchCanonicalSQLHasColumnUpsertCypher, rowMap, true
	case "TRIGGERS":
		return batchCanonicalSQLTriggersUpsertCypher, rowMap, true
	default:
		return "", nil, false
	}
}

func isSQLRelationshipEntityLabel(label string) bool {
	_, ok := sqlRelationshipEntityLabels[label]
	return ok
}

func labelScopedSQLRelationshipCypher(
	relationshipType string,
	sourceLabel string,
	targetLabel string,
) (string, bool) {
	switch relationshipType {
	case "REFERENCES_TABLE":
		return buildLabelScopedSQLRelationshipCypher(
			sourceLabel,
			targetLabel,
			"REFERENCES_TABLE",
			"SQL entity metadata resolved a table reference edge",
		), true
	case "HAS_COLUMN":
		return buildLabelScopedSQLRelationshipCypher(
			sourceLabel,
			targetLabel,
			"HAS_COLUMN",
			"SQL entity metadata resolved a table-column containment edge",
		), true
	case "TRIGGERS":
		return buildLabelScopedSQLRelationshipCypher(
			sourceLabel,
			targetLabel,
			"TRIGGERS",
			"SQL entity metadata resolved a trigger edge",
		), true
	default:
		return "", false
	}
}

func buildLabelScopedSQLRelationshipCypher(
	sourceLabel string,
	targetLabel string,
	relationshipType string,
	reason string,
) string {
	return fmt.Sprintf(`UNWIND $rows AS row
MATCH (source:%s {uid: row.source_entity_id})
MATCH (target:%s {uid: row.target_entity_id})
MERGE (source)-[rel:%s]->(target)
SET rel.confidence = 0.95,
    rel.reason = '%s',
    rel.evidence_source = row.evidence_source`, sourceLabel, targetLabel, relationshipType, reason)
}

// BuildRetractSQLRelationshipEdgeStatements builds label-scoped SQL
// relationship retraction statements for grouped reducer execution.
func BuildRetractSQLRelationshipEdgeStatements(repoIDs []string, evidenceSource string) []Statement {
	cyphers := []string{
		retractSQLViewReferencesTableEdgesCypher,
		retractSQLFunctionReferencesTableEdgesCypher,
		retractSQLTableHasColumnEdgesCypher,
		retractSQLTriggerEdgesCypher,
	}
	stmts := make([]Statement, 0, len(cyphers))
	for _, cypher := range cyphers {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    cypher,
			Parameters: map[string]any{
				"repo_ids":        repoIDs,
				"evidence_source": evidenceSource,
			},
		})
	}
	return stmts
}
