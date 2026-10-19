// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import (
	"reflect"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/reducer"
	"github.com/eshu-hq/eshu/go/internal/storage/cypher/canonical/node"
)

// BuildCanonicalWorkloadDependencyUpsert builds a Workload DEPENDS_ON edge
// statement.
func BuildCanonicalWorkloadDependencyUpsert(p CanonicalWorkloadDependencyParams, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    CanonicalWorkloadDependencyUpsertCypher,
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
		// The CALLS and REFERENCES templates always read $call_kind; an
		// absent kind is sent as nil so the parameter is never missing.
		"call_kind": nil,
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
		Cypher:    RetractRepoDependencyEdgesCypher,
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
		Cypher:    RetractWorkloadDependencyEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildRetractCodeCallEdgeStatements builds per-source-label code-intel edge
// retraction statements for all source entities owned by the given repositories.
func BuildRetractCodeCallEdgeStatements(repoIDs []string, evidenceSource string) []Statement {
	return wrapNodeEdgeParts(node.BuildCodeCallRetractParts("repo_id", "repo_ids", repoIDs, evidenceSource))
}

// BuildRetractCodeCallEdgeStatementsByFilePath builds per-source-label code-intel
// edge retraction statements for source entities owned by the given
// repo-qualified file paths.
func BuildRetractCodeCallEdgeStatementsByFilePath(filePaths []string, evidenceSource string) []Statement {
	return wrapNodeEdgeParts(node.BuildCodeCallRetractParts("path", "file_paths", filePaths, evidenceSource))
}

// SQL relationship edge retraction is built per source label by
// BuildRetractSQLRelationshipEdgeStatements[ByFilePath] in edge/writer/sql.go
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
		case node.BatchCanonicalRunsOnLegacyIdentityCleanupCypher:
			if stmt.Operation != OperationCanonicalUpsert {
				return false
			}
			rows, ok := runsOnReplayRows(stmt.Parameters, "repo_id")
			if !ok {
				return false
			}
			cleanupRows = append(cleanupRows, rows)
			lastCleanup = index
		case node.BatchCanonicalRunsOnUpsertCypher:
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
	case BatchCanonicalRepoDependencyUpsertCypher,
		node.BatchCanonicalDeploysFromRepoRelationshipUpsertCypher,
		node.BatchCanonicalDiscoversConfigInRepoRelationshipUpsertCypher,
		node.BatchCanonicalProvisionsDependencyForRepoRelationshipUpsertCypher,
		node.BatchCanonicalUsesModuleRepoRelationshipUpsertCypher,
		node.BatchCanonicalReadsConfigFromRepoRelationshipUpsertCypher,
		node.BatchCanonicalRepoEvidenceArtifactUpsertCypher,
		node.BatchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher:
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
