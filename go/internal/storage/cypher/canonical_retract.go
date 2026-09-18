// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"fmt"
	"reflect"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
)

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

// SQL relationship edge retraction is built per source label by
// BuildRetractSQLRelationshipEdgeStatements[ByFilePath] in edge_writer_sql.go
// (the SQL sibling of #5116); the old single-statement unlabeled-scan builder
// silently under-deleted on NornicDB v1.1.11 and was removed.

// Platform orphan node cleanup no longer has a dedicated single-statement
// builder here (BuildDeleteOrphanPlatformNodes, removed by #5310): it relied
// on a `NOT (p)--()` predicate that never matches on the pinned NornicDB
// backends and had no production caller. See the comment on the removed
// deleteOrphanPlatformNodesCypher constant in canonical.go.

// isCanonicalRunsOnReplaySafeGroup is the narrow exception for the two
// RUNS_ON canonicalization writers. It gates commit-conflict retries and
// durable transaction-timeout deferral; successful writes keep the same Cypher,
// transaction count, and hot path.
func isCanonicalRunsOnReplaySafeGroup(stmts []Statement) bool {
	if len(stmts) == 3 {
		group := make([]reducer.CypherGroupStatement, len(stmts))
		for i, stmt := range stmts {
			if stmt.Operation != OperationCanonicalUpsert {
				break
			}
			group[i] = reducer.CypherGroupStatement{
				Cypher: stmt.Cypher, Parameters: stmt.Parameters,
			}
		}
		if reducer.IsWorkloadRunsOnReplayGroup(group) {
			return true
		}
	}
	return isCrossRepoRunsOnReplaySafeGroup(stmts)
}

// isCrossRepoRunsOnReplaySafeGroup requires every legacy-cleanup chunk to have
// a later canonical upsert for identical rows in the same atomic transaction.
// Other statements must be the exact canonical repo-dependency or evidence
// templates emitted by the same writer, never arbitrary MERGE-shaped Cypher.
func isCrossRepoRunsOnReplaySafeGroup(stmts []Statement) bool {
	var cleanupRows, upsertRows [][]map[string]any
	lastCleanup, firstUpsert := -1, len(stmts)
	for index, stmt := range stmts {
		switch stmt.Cypher {
		case batchCanonicalRunsOnLegacyIdentityCleanupCypher:
			if stmt.Operation != OperationCanonicalUpsert {
				return false
			}
			rows, ok := runsOnReplayRows(stmt.Parameters, "repo_id")
			if !ok {
				return false
			}
			cleanupRows = append(cleanupRows, rows)
			lastCleanup = index
		case batchCanonicalRunsOnUpsertCypher:
			if stmt.Operation != OperationCanonicalUpsert {
				return false
			}
			rows, ok := runsOnReplayRows(stmt.Parameters, "repo_id")
			if !ok {
				return false
			}
			upsertRows = append(upsertRows, rows)
			if index < firstUpsert {
				firstUpsert = index
			}
		default:
			if !isCanonicalRepoDependencyReplayStatement(stmt) {
				return false
			}
		}
	}
	if len(cleanupRows) == 0 || len(cleanupRows) != len(upsertRows) ||
		lastCleanup >= firstUpsert {
		return false
	}
	seenPairs := make(map[string]map[string]any)
	for index := range cleanupRows {
		if !reflect.DeepEqual(cleanupRows[index], upsertRows[index]) {
			return false
		}
		for _, row := range cleanupRows[index] {
			pair := row["repo_id"].(string) + "\x00" + row["platform_id"].(string)
			if previous, exists := seenPairs[pair]; exists && !reflect.DeepEqual(previous, row) {
				return false
			}
			seenPairs[pair] = row
		}
	}
	return true
}

// isCanonicalRepoDependencyReplayStatement admits only the other deterministic
// route templates the repo-dependency writer can place beside RUNS_ON.
func isCanonicalRepoDependencyReplayStatement(stmt Statement) bool {
	if stmt.Operation != OperationCanonicalUpsert {
		return false
	}
	switch stmt.Cypher {
	case batchCanonicalRepoDependencyUpsertCypher,
		batchCanonicalDeploysFromRepoRelationshipUpsertCypher,
		batchCanonicalDiscoversConfigInRepoRelationshipUpsertCypher,
		batchCanonicalProvisionsDependencyForRepoRelationshipUpsertCypher,
		batchCanonicalUsesModuleRepoRelationshipUpsertCypher,
		batchCanonicalReadsConfigFromRepoRelationshipUpsertCypher,
		batchCanonicalRepoEvidenceArtifactUpsertCypher,
		batchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher:
		return true
	default:
		return false
	}
}

func runsOnReplayRows(params map[string]any, identityKey string) ([]map[string]any, bool) {
	rows, ok := params["rows"].([]map[string]any)
	if !ok || len(rows) == 0 {
		return nil, false
	}
	seenPairs := make(map[string]map[string]any, len(rows))
	for _, row := range rows {
		for _, key := range []string{identityKey, "platform_id", "evidence_source"} {
			value, ok := row[key].(string)
			if !ok || strings.TrimSpace(value) == "" {
				return nil, false
			}
		}
		pair := row[identityKey].(string) + "\x00" + row["platform_id"].(string)
		if previous, exists := seenPairs[pair]; exists && !reflect.DeepEqual(previous, row) {
			return nil, false
		}
		seenPairs[pair] = row
	}
	return rows, true
}
