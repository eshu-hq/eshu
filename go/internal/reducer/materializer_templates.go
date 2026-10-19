// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package reducer

// Batched UNWIND Cypher templates for bulk operations.
const (
	batchWorkloadNodeUpsertCypher = `UNWIND $rows AS row
MERGE (w:Workload {id: row.workload_id})
SET w.type = 'workload',
    w.name = row.workload_name,
    w.kind = row.workload_kind,
    w.classification = row.classification,
    w.repo_id = row.repo_id,
    w.materialization_confidence = row.materialization_confidence,
    w.materialization_provenance = row.materialization_provenance,
    w.evidence_source = row.evidence_source,
    w.scope_id = row.scope_id,
    w.generation_id = row.generation_id`

	batchWorkloadDefinesEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})
MATCH (w:Workload {id: row.workload_id})
MERGE (repo)-[rel:DEFINES]->(w)
SET rel.confidence = row.materialization_confidence,
    rel.reason = 'Repository defines workload',
    rel.evidence_source = row.evidence_source`

	batchWorkloadInstanceNodeUpsertCypher = `UNWIND $rows AS row
MERGE (i:WorkloadInstance {id: row.instance_id})
SET i.type = 'workload_instance',
    i.name = row.workload_name,
    i.kind = row.workload_kind,
    i.classification = row.classification,
    i.environment = row.environment,
    i.workload_id = row.workload_id,
    i.repo_id = row.repo_id,
    i.materialization_confidence = row.materialization_confidence,
    i.materialization_provenance = row.materialization_provenance,
    i.evidence_source = row.evidence_source,
    i.scope_id = row.scope_id,
    i.generation_id = row.generation_id`

	batchWorkloadInstanceOfEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (i:WorkloadInstance {id: row.instance_id})
MATCH (w:Workload {id: row.workload_id})
MERGE (i)-[rel:INSTANCE_OF]->(w)
SET rel.confidence = row.materialization_confidence,
    rel.reason = 'Workload instance belongs to workload',
    rel.evidence_source = row.evidence_source`

	batchDeploymentSourceUpsertCypher = `UNWIND $rows AS row
MATCH (i:WorkloadInstance {id: row.instance_id})
MATCH (deployment_repo:Repository {id: row.deployment_repo_id})
MERGE (i)-[rel:DEPLOYMENT_SOURCE]->(deployment_repo)
SET rel.confidence = row.deployment_confidence,
    rel.reason = 'Deployment manifests for workload instance live in deployment repository',
    rel.provenance = row.deployment_provenance,
    rel.evidence_source = row.evidence_source`

	batchRuntimePlatformNodeUpsertCypher = `UNWIND $rows AS row
MERGE (p:Platform {id: row.platform_id})
ON CREATE SET p.evidence_source = row.evidence_source
SET p.type = 'platform',
    p.name = row.platform_name,
    p.kind = row.platform_kind,
    p.provider = row.platform_provider,
    p.environment = row.environment,
    p.region = row.platform_region,
    p.locator = row.platform_locator`

	// RUNS_ON has two writers. This first statement only establishes the shared
	// edge identity; the second statement stamps or refreshes the complete
	// workload-owned property tuple. Keeping the ownership predicate in a
	// separate statement in the same transaction is required on pinned
	// NornicDB: relationship properties
	// read from the same MERGE statement do not reliably expose the preexisting
	// tuple. Atomic grouping prevents an unstamped edge from becoming visible.
	// The cross-repo writer's unconditional full-tuple update either runs before
	// this transaction, in which case the predicate preserves it, or afterward,
	// in which case it wins.
	batchRuntimePlatformRunsOnEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (i:WorkloadInstance {id: row.instance_id})
MATCH (p:Platform {id: row.platform_id})
MERGE (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)`

	batchRuntimePlatformRunsOnOwnedEdgePropertiesCypher = `UNWIND $rows AS row
MATCH (i:WorkloadInstance {id: row.instance_id})
MATCH (p:Platform {id: row.platform_id})
MATCH (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)
WHERE rel.evidence_source IS NULL OR rel.evidence_source = row.evidence_source
SET rel.confidence = row.platform_confidence,
    rel.reason = 'Workload instance runs on inferred platform',
    rel.evidence_source = row.evidence_source,
    rel.source_tool = null`

	batchRepoDependencyUpsertCypher = `UNWIND $rows AS row
MATCH (source_repo:Repository {id: row.repo_id})
MATCH (target_repo:Repository {id: row.target_repo_id})
MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)
SET rel.confidence = $edge_confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = row.evidence_source`

	batchWorkloadDependencyUpsertCypher = `UNWIND $rows AS row
MATCH (source:Workload {id: row.workload_id})
MATCH (target:Workload {id: row.target_workload_id})
MERGE (source)-[rel:DEPENDS_ON {identity_key: 'canonical'}]->(target)
SET rel.confidence = $edge_confidence,
    rel.reason = 'Runtime services list declares workload dependency',
    rel.evidence_source = row.evidence_source`

	batchAPIEndpointNodeUpsertCypher = `UNWIND $rows AS row
MERGE (endpoint:Endpoint {id: row.endpoint_id})
SET endpoint.type = 'endpoint',
    endpoint.path = row.path,
    endpoint.methods = row.methods,
    endpoint.operation_ids = row.operation_ids,
    endpoint.repo_id = row.repo_id,
    endpoint.workload_id = row.workload_id,
    endpoint.workload_name = row.workload_name,
    endpoint.source_kinds = row.source_kinds,
    endpoint.source_paths = row.source_paths,
    endpoint.spec_versions = row.spec_versions,
    endpoint.api_versions = row.api_versions,
    endpoint.evidence_source = row.evidence_source,
    endpoint.scope_id = row.scope_id,
    endpoint.generation_id = row.generation_id`

	batchAPIEndpointRepoEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})
MATCH (endpoint:Endpoint {id: row.endpoint_id})
MERGE (repo)-[repo_rel:EXPOSES_ENDPOINT]->(endpoint)
SET repo_rel.evidence_source = row.evidence_source,
    repo_rel.reason = 'Repository exposes API endpoint'`

	batchAPIEndpointWorkloadEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (workload:Workload {id: row.workload_id})
MATCH (endpoint:Endpoint {id: row.endpoint_id})
MERGE (workload)-[workload_rel:EXPOSES_ENDPOINT]->(endpoint)
SET workload_rel.evidence_source = row.evidence_source,
    workload_rel.reason = 'Workload exposes API endpoint'`
)
