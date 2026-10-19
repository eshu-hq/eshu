// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package node

const BatchCanonicalDeployableUnitCorrelationUpsertCypher = `UNWIND $rows AS row
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

const RetractDeployableUnitCorrelationEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source_repo:Repository {id: repo_id})-[rel:CORRELATES_DEPLOYABLE_UNIT]->(:Repository)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

// DeployableUnitCorrelationIncidentReposCypher lists the admitted source
// repositories that still own at least one outgoing CORRELATES_DEPLOYABLE_UNIT
// edge (#8316). The shape mirrors the orphan sweep's proven S2 connected-keys
// read: one UNWIND-anchored concrete-relationship-variable MATCH per key,
// which is the only relationship primitive proven reliable on both pinned
// backends. The directed outgoing pattern matches the handler's write
// direction (workload repository to deployment repository): an anchor whose
// own outgoing edge was lost is a gap even when some other repository still
// correlates into it. RETURN DISTINCT keeps one row per connected anchor, so
// the result stays bounded by the input size.
const DeployableUnitCorrelationIncidentReposCypher = `UNWIND $repo_ids AS requested_repo_id
MATCH (s:Repository {id: requested_repo_id})-[rel:CORRELATES_DEPLOYABLE_UNIT]->(:Repository)
RETURN DISTINCT s.id AS repo_id`
