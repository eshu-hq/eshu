// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

const batchCanonicalDeployableUnitCorrelationUpsertCypher = `UNWIND $rows AS row
MATCH (source_repo:Repository {id: row.repo_id})
MATCH (deployment_repo:Repository {id: row.deployment_repo_id})
MERGE (source_repo)-[rel:CORRELATES_DEPLOYABLE_UNIT]->(deployment_repo)
SET rel.confidence = row.confidence,
    rel.reason = row.reason,
    rel.evidence_source = row.evidence_source,
    rel.evidence_type = row.evidence_type,
    rel.relationship_type = 'CORRELATES_DEPLOYABLE_UNIT',
    rel.generation_id = row.generation_id,
    rel.evidence_count = row.evidence_count,
    rel.evidence_kinds = row.evidence_kinds,
    rel.resolution_source = row.resolution_source,
    rel.resolved_id = row.resolved_id,
    rel.deployable_unit_key = row.deployable_unit_key,
    rel.correlation_key = row.correlation_key,
    rel.rule_pack = row.rule_pack,
    rel.admission_state = row.admission_state`

const retractDeployableUnitCorrelationEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source_repo:Repository {id: repo_id})-[rel:CORRELATES_DEPLOYABLE_UNIT]->(:Repository)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

// BuildRetractDeployableUnitCorrelationEdges deletes deployable-unit
// correlation edges for repository acceptance units owned by one evidence
// source.
func BuildRetractDeployableUnitCorrelationEdges(repoIDs []string, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalRetract,
		Cypher:    retractDeployableUnitCorrelationEdgesCypher,
		Parameters: map[string]any{
			"repo_ids":        repoIDs,
			"evidence_source": evidenceSource,
		},
	}
}
