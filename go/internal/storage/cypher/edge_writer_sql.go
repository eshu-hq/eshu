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

// sqlRelationshipRetractRelTypes is the relationship-type disjunction every
// per-label SQL retract statement deletes. Relationship-type disjunction is
// supported on NornicDB; node-label disjunction is not (#5116). The scope
// predicate (source label + repo/path anchor + rel.evidence_source) already
// bounds each statement to this domain's edges, so the broad rel-type list
// cannot over-delete — it exists so every (source label, relationship type)
// pair the write path can create is retractable. READS_FROM was added and
// REFERENCES_TABLE was kept (#5345): the write path no longer produces
// REFERENCES_TABLE (see sqlRelationshipWriteReasons), but a superset retract
// is legal and cheap hygiene that also reaps any pre-#5345 REFERENCES_TABLE
// edges still in the graph from the SqlView/SqlFunction source_tables writer.
// MIGRATES was added in #5346 (SqlMigration -> SqlTable/View/Function/
// Trigger/Index).
const sqlRelationshipRetractRelTypes = "QUERIES_TABLE|REFERENCES_TABLE|READS_FROM|WRITES_TO|HAS_COLUMN|TRIGGERS|EXECUTES|INDEXES|MIGRATES"

// sqlRelationshipRetractSourceLabels lists the source node labels a SQL
// relationship edge retract must cover. It MUST include every label the write
// path accepts as an edge source (sqlRelationshipEntityLabels) — guarded by
// TestSQLRelationshipRetractCoversEveryWriteEndpointLabel — because an edge
// written from a label missing here would survive every reprojection.
var sqlRelationshipRetractSourceLabels = []string{
	"Function", "SqlColumn", "SqlFunction", "SqlIndex", "SqlMigration", "SqlTable", "SqlTrigger", "SqlView",
}

// buildSQLRelationshipRetractStatements emits one retract statement per source
// label.
//
// A single statement cannot bind all source labels on NornicDB: a node-label
// disjunction MATCH (source:SqlTable|SqlView|...) matches zero rows, and on
// NornicDB v1.1.11 an unlabeled MATCH (source) scan is unreliable — it
// silently drops some source labels, inconsistently by internal
// label-iteration state (#5116). Single-label MATCH is the only shape that
// reliably matches every source on both pinned versions, so the retract fans
// out to one statement per label. unwindParam is the Cypher parameter carrying
// scopeValues ("repo_ids" or "file_paths"), alias its per-row UNWIND name, and
// anchorProp the source property the inline anchor binds ("repo_id" or
// "path"); see the #4708 note above for why the anchor is inline.
func buildSQLRelationshipRetractStatements(unwindParam, alias, anchorProp string, scopeValues []string, evidenceSource string) []Statement {
	stmts := make([]Statement, 0, len(sqlRelationshipRetractSourceLabels))
	for _, label := range sqlRelationshipRetractSourceLabels {
		cypher := fmt.Sprintf(
			"UNWIND $%s AS %s\nMATCH (source:%s {%s: %s})-[rel:%s]->()\nWHERE rel.evidence_source = $evidence_source\nDELETE rel",
			unwindParam, alias, label, anchorProp, alias, sqlRelationshipRetractRelTypes,
		)
		stmts = append(stmts, Statement{
			Operation: OperationCanonicalRetract,
			Cypher:    cypher,
			Parameters: map[string]any{
				unwindParam:       scopeValues,
				"evidence_source": evidenceSource,
			},
		})
	}
	return stmts
}

var sqlRelationshipEntityLabels = map[string]struct{}{
	"Function":     {},
	"SqlColumn":    {},
	"SqlFunction":  {},
	"SqlIndex":     {},
	"SqlMigration": {},
	"SqlTable":     {},
	"SqlTrigger":   {},
	"SqlView":      {},
}

// sqlRelationshipWriteReasons is the single source of truth for the
// relationship types the SQL write path accepts, mapping each to the MERGE
// reason its edge carries. Both the label-scoped and fallback write templates
// gate on membership here, and every type MUST appear in
// sqlRelationshipRetractRelTypes — guarded by
// TestSQLRelationshipRetractCoversEveryWriteRelationshipType — or its edges
// would be written but never retracted.
var sqlRelationshipWriteReasons = map[string]string{
	"QUERIES_TABLE":    "Parser embedded SQL evidence resolved a function table query edge",
	"REFERENCES_TABLE": "SQL table metadata resolved a foreign-key table reference edge",
	"READS_FROM":       "SQL view/function metadata resolved a table read edge",
	"WRITES_TO":        "SQL routine metadata resolved a table write edge",
	"HAS_COLUMN":       "SQL entity metadata resolved a table-column containment edge",
	"TRIGGERS":         "SQL entity metadata resolved a trigger edge",
	"EXECUTES":         "SQL trigger metadata resolved a routine execution edge",
	"INDEXES":          "SQL entity metadata resolved an index-to-table edge",
	"MIGRATES":         "SQL migration metadata resolved a migration-to-target edge",
}

// SQLRelationshipMaterializedEdgeTypes returns a defensive copy of
// sqlRelationshipWriteReasons: the graph relationship types the SQL-entity
// edge writer actually accepts, mapped to the write reason recorded on each
// MERGEd edge. It is the authoritative, single source of truth for which
// SQL-domain edge types are materialized in the graph — callers MUST derive
// coverage from this registry rather than hand-maintaining a second list, so
// a type added or removed here automatically flips downstream coverage
// reporting (e.g. the blast-radius honesty registry in go/internal/query,
// #5330) without a second edit.
func SQLRelationshipMaterializedEdgeTypes() map[string]string {
	out := make(map[string]string, len(sqlRelationshipWriteReasons))
	for edgeType, reason := range sqlRelationshipWriteReasons {
		out[edgeType] = reason
	}
	return out
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
	if _, ok := sqlRelationshipWriteReasons[relationshipType]; !ok {
		return "", nil, false
	}
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
	reason, ok := sqlRelationshipWriteReasons[relationshipType]
	if !ok {
		return "", false
	}
	return buildLabelScopedSQLRelationshipCypher(sourceLabel, targetLabel, relationshipType, reason), true
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

// BuildRetractSQLRelationshipEdgeStatements builds per-source-label SQL
// relationship edge retraction statements for all source entities owned by the
// given repositories.
func BuildRetractSQLRelationshipEdgeStatements(repoIDs []string, evidenceSource string) []Statement {
	return buildSQLRelationshipRetractStatements("repo_ids", "repo_id", "repo_id", repoIDs, evidenceSource)
}

// BuildRetractSQLRelationshipEdgeStatementsByFilePath builds per-source-label
// SQL relationship edge retraction statements for source entities owned by the
// given repo-qualified file paths.
func BuildRetractSQLRelationshipEdgeStatementsByFilePath(filePaths []string, evidenceSource string) []Statement {
	return buildSQLRelationshipRetractStatements("file_paths", "file_path", "path", filePaths, evidenceSource)
}
