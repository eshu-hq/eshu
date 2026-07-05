// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "fmt"

// The whole-scope retracts UNWIND $repo_ids and anchor the source node with an
// inline {repo_id: ...} property, and the by-file retracts UNWIND $file_paths
// with an inline {path: ...} anchor, rather than a `WHERE source.repo_id IN
// $repo_ids` / `WHERE source.path IN $file_paths` predicate (#4708). On NornicDB
// a compound `WHERE <node.prop> AND <rel.prop>` is not split, so the start-node
// property index is not consulted and the delete degrades to a full label scan.
// For the large indexed source label (`:Function`, which has repo_id/path node
// indexes) the inline anchor binds via the index and expands from the tiny bound
// set — ~11s -> ~0.002s. The small SQL entity labels (SqlTable/SqlView/... , all
// under ~1k nodes here and without a repo_id/path index) still label-scan, but
// over a tiny population that is already cheap and no worse than the old
// predicate. Either way the identical edge set is deleted (proven 0/0 on live
// data).

const retractSQLFunctionQueriesTableEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source:Function {repo_id: repo_id})-[rel:QUERIES_TABLE]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLFunctionQueriesTableEdgesByFileCypher = `UNWIND $file_paths AS file_path
MATCH (source:Function {path: file_path})-[rel:QUERIES_TABLE]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLViewReferencesTableEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source:SqlView {repo_id: repo_id})-[rel:REFERENCES_TABLE]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLViewReferencesTableEdgesByFileCypher = `UNWIND $file_paths AS file_path
MATCH (source:SqlView {path: file_path})-[rel:REFERENCES_TABLE]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLFunctionReferencesTableEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source:SqlFunction {repo_id: repo_id})-[rel:REFERENCES_TABLE]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLFunctionReferencesTableEdgesByFileCypher = `UNWIND $file_paths AS file_path
MATCH (source:SqlFunction {path: file_path})-[rel:REFERENCES_TABLE]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLTableHasColumnEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source:SqlTable {repo_id: repo_id})-[rel:HAS_COLUMN]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLTableHasColumnEdgesByFileCypher = `UNWIND $file_paths AS file_path
MATCH (source:SqlTable {path: file_path})-[rel:HAS_COLUMN]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLTriggerEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source:SqlTrigger {repo_id: repo_id})-[rel:TRIGGERS]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLTriggerEdgesByFileCypher = `UNWIND $file_paths AS file_path
MATCH (source:SqlTrigger {path: file_path})-[rel:TRIGGERS]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLTriggerExecutesEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source:SqlTrigger {repo_id: repo_id})-[rel:EXECUTES]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLTriggerExecutesEdgesByFileCypher = `UNWIND $file_paths AS file_path
MATCH (source:SqlTrigger {path: file_path})-[rel:EXECUTES]->()
WHERE rel.evidence_source = $evidence_source
DELETE rel`

var sqlRelationshipEntityLabels = map[string]struct{}{
	"Function":    {},
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
	case "QUERIES_TABLE":
		return batchCanonicalSQLQueriesTableUpsertCypher, rowMap, true
	case "REFERENCES_TABLE":
		return batchCanonicalSQLRelationshipUpsertCypher, rowMap, true
	case "HAS_COLUMN":
		return batchCanonicalSQLHasColumnUpsertCypher, rowMap, true
	case "TRIGGERS":
		return batchCanonicalSQLTriggersUpsertCypher, rowMap, true
	case "EXECUTES":
		return batchCanonicalSQLExecutesUpsertCypher, rowMap, true
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
	case "QUERIES_TABLE":
		return buildLabelScopedSQLRelationshipCypher(
			sourceLabel,
			targetLabel,
			"QUERIES_TABLE",
			"Parser embedded SQL evidence resolved a function table query edge",
		), true
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
	case "EXECUTES":
		return buildLabelScopedSQLRelationshipCypher(
			sourceLabel,
			targetLabel,
			"EXECUTES",
			"SQL trigger metadata resolved a routine execution edge",
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
		retractSQLFunctionQueriesTableEdgesCypher,
		retractSQLViewReferencesTableEdgesCypher,
		retractSQLFunctionReferencesTableEdgesCypher,
		retractSQLTableHasColumnEdgesCypher,
		retractSQLTriggerEdgesCypher,
		retractSQLTriggerExecutesEdgesCypher,
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

// BuildRetractSQLRelationshipEdgeStatementsByFilePath builds label-scoped SQL
// relationship retraction statements for repo-qualified source file paths.
func BuildRetractSQLRelationshipEdgeStatementsByFilePath(filePaths []string, evidenceSource string) []Statement {
	cyphers := []string{
		retractSQLFunctionQueriesTableEdgesByFileCypher,
		retractSQLViewReferencesTableEdgesByFileCypher,
		retractSQLFunctionReferencesTableEdgesByFileCypher,
		retractSQLTableHasColumnEdgesByFileCypher,
		retractSQLTriggerEdgesByFileCypher,
		retractSQLTriggerExecutesEdgesByFileCypher,
	}
	stmts := make([]Statement, 0, len(cyphers))
	for _, cypher := range cyphers {
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    cypher,
			Parameters: map[string]any{
				"file_paths":      filePaths,
				"evidence_source": evidenceSource,
			},
		})
	}
	return stmts
}
