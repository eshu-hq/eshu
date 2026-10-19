// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package node

// CanonicalDeploysFromRepoRelationshipUpsertCypher is the single-row
// DEPLOYS_FROM repository relationship upsert. It stays exported (like its
// CanonicalRepoDependency and Batch siblings) because the sweep/orphan leaf's
// target-stamping contract test reads it across the package split.
const CanonicalDeploysFromRepoRelationshipUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
ON CREATE SET source_repo.evidence_source = $evidence_source,
              source_repo.generation_id = $generation_id
MERGE (target_repo:Repository {id: $target_repo_id})
ON CREATE SET target_repo.evidence_source = $evidence_source,
              target_repo.generation_id = $generation_id
MERGE (source_repo)-[rel:DEPLOYS_FROM]->(target_repo)
SET rel.confidence = $confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = $evidence_source,
    rel.evidence_type = $evidence_type,
    rel.relationship_type = 'DEPLOYS_FROM',
    rel.resolved_id = $resolved_id,
    rel.generation_id = $generation_id,
    rel.evidence_count = $evidence_count,
    rel.evidence_kinds = $evidence_kinds,
    rel.resolution_source = $resolution_source,
    rel.rationale = $rationale,
    rel.source_tool = $source_tool,
    rel.source_revision = $source_revision,
    rel.first_party_ref_version = $first_party_ref_version`

const CanonicalDiscoversConfigInRepoRelationshipUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
ON CREATE SET source_repo.evidence_source = $evidence_source,
              source_repo.generation_id = $generation_id
MERGE (target_repo:Repository {id: $target_repo_id})
ON CREATE SET target_repo.evidence_source = $evidence_source,
              target_repo.generation_id = $generation_id
MERGE (source_repo)-[rel:DISCOVERS_CONFIG_IN]->(target_repo)
SET rel.confidence = $confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = $evidence_source,
    rel.evidence_type = $evidence_type,
    rel.relationship_type = 'DISCOVERS_CONFIG_IN',
    rel.resolved_id = $resolved_id,
    rel.generation_id = $generation_id,
    rel.evidence_count = $evidence_count,
    rel.evidence_kinds = $evidence_kinds,
    rel.resolution_source = $resolution_source,
    rel.rationale = $rationale,
    rel.source_tool = $source_tool,
    rel.source_revision = $source_revision,
    rel.first_party_ref_version = $first_party_ref_version`

const CanonicalProvisionsDependencyForRepoRelationshipUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
ON CREATE SET source_repo.evidence_source = $evidence_source,
              source_repo.generation_id = $generation_id
MERGE (target_repo:Repository {id: $target_repo_id})
ON CREATE SET target_repo.evidence_source = $evidence_source,
              target_repo.generation_id = $generation_id
MERGE (source_repo)-[rel:PROVISIONS_DEPENDENCY_FOR]->(target_repo)
SET rel.confidence = $confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = $evidence_source,
    rel.evidence_type = $evidence_type,
    rel.relationship_type = 'PROVISIONS_DEPENDENCY_FOR',
    rel.resolved_id = $resolved_id,
    rel.generation_id = $generation_id,
    rel.evidence_count = $evidence_count,
    rel.evidence_kinds = $evidence_kinds,
    rel.resolution_source = $resolution_source,
    rel.rationale = $rationale,
    rel.source_tool = $source_tool,
    rel.source_revision = $source_revision,
    rel.first_party_ref_version = $first_party_ref_version`

const CanonicalUsesModuleRepoRelationshipUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
ON CREATE SET source_repo.evidence_source = $evidence_source,
              source_repo.generation_id = $generation_id
MERGE (target_repo:Repository {id: $target_repo_id})
ON CREATE SET target_repo.evidence_source = $evidence_source,
              target_repo.generation_id = $generation_id
MERGE (source_repo)-[rel:USES_MODULE]->(target_repo)
SET rel.confidence = $confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = $evidence_source,
    rel.evidence_type = $evidence_type,
    rel.relationship_type = 'USES_MODULE',
    rel.resolved_id = $resolved_id,
    rel.generation_id = $generation_id,
    rel.evidence_count = $evidence_count,
    rel.evidence_kinds = $evidence_kinds,
    rel.resolution_source = $resolution_source,
    rel.rationale = $rationale,
    rel.source_tool = $source_tool,
    rel.source_revision = $source_revision,
    rel.first_party_ref_version = $first_party_ref_version`

const CanonicalReadsConfigFromRepoRelationshipUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
ON CREATE SET source_repo.evidence_source = $evidence_source,
              source_repo.generation_id = $generation_id
MERGE (target_repo:Repository {id: $target_repo_id})
ON CREATE SET target_repo.evidence_source = $evidence_source,
              target_repo.generation_id = $generation_id
MERGE (source_repo)-[rel:READS_CONFIG_FROM]->(target_repo)
SET rel.confidence = $confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = $evidence_source,
    rel.evidence_type = $evidence_type,
    rel.relationship_type = 'READS_CONFIG_FROM',
    rel.resolved_id = $resolved_id,
    rel.generation_id = $generation_id,
    rel.evidence_count = $evidence_count,
    rel.evidence_kinds = $evidence_kinds,
    rel.resolution_source = $resolution_source,
    rel.rationale = $rationale,
    rel.source_tool = $source_tool,
    rel.source_revision = $source_revision,
    rel.first_party_ref_version = $first_party_ref_version`

const BatchCanonicalDeploysFromRepoRelationshipUpsertCypher = `UNWIND $rows AS row
MERGE (source_repo:Repository {id: row.repo_id})
ON CREATE SET source_repo.evidence_source = row.evidence_source,
              source_repo.generation_id = row.generation_id
MERGE (target_repo:Repository {id: row.target_repo_id})
ON CREATE SET target_repo.evidence_source = row.evidence_source,
              target_repo.generation_id = row.generation_id
MERGE (source_repo)-[rel:DEPLOYS_FROM]->(target_repo)
SET rel.confidence = row.confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = row.evidence_source,
    rel.evidence_type = row.evidence_type,
    rel.relationship_type = 'DEPLOYS_FROM',
    rel.resolved_id = row.resolved_id,
    rel.generation_id = row.generation_id,
    rel.evidence_count = row.evidence_count,
    rel.evidence_kinds = row.evidence_kinds,
    rel.resolution_source = row.resolution_source,
    rel.rationale = row.rationale,
    rel.source_tool = row.source_tool,
    rel.source_revision = row.source_revision,
    rel.first_party_ref_version = row.first_party_ref_version`

const BatchCanonicalDiscoversConfigInRepoRelationshipUpsertCypher = `UNWIND $rows AS row
MERGE (source_repo:Repository {id: row.repo_id})
ON CREATE SET source_repo.evidence_source = row.evidence_source,
              source_repo.generation_id = row.generation_id
MERGE (target_repo:Repository {id: row.target_repo_id})
ON CREATE SET target_repo.evidence_source = row.evidence_source,
              target_repo.generation_id = row.generation_id
MERGE (source_repo)-[rel:DISCOVERS_CONFIG_IN]->(target_repo)
SET rel.confidence = row.confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = row.evidence_source,
    rel.evidence_type = row.evidence_type,
    rel.relationship_type = 'DISCOVERS_CONFIG_IN',
    rel.resolved_id = row.resolved_id,
    rel.generation_id = row.generation_id,
    rel.evidence_count = row.evidence_count,
    rel.evidence_kinds = row.evidence_kinds,
    rel.resolution_source = row.resolution_source,
    rel.rationale = row.rationale,
    rel.source_tool = row.source_tool,
    rel.source_revision = row.source_revision,
    rel.first_party_ref_version = row.first_party_ref_version`

const BatchCanonicalProvisionsDependencyForRepoRelationshipUpsertCypher = `UNWIND $rows AS row
MERGE (source_repo:Repository {id: row.repo_id})
ON CREATE SET source_repo.evidence_source = row.evidence_source,
              source_repo.generation_id = row.generation_id
MERGE (target_repo:Repository {id: row.target_repo_id})
ON CREATE SET target_repo.evidence_source = row.evidence_source,
              target_repo.generation_id = row.generation_id
MERGE (source_repo)-[rel:PROVISIONS_DEPENDENCY_FOR]->(target_repo)
SET rel.confidence = row.confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = row.evidence_source,
    rel.evidence_type = row.evidence_type,
    rel.relationship_type = 'PROVISIONS_DEPENDENCY_FOR',
    rel.resolved_id = row.resolved_id,
    rel.generation_id = row.generation_id,
    rel.evidence_count = row.evidence_count,
    rel.evidence_kinds = row.evidence_kinds,
    rel.resolution_source = row.resolution_source,
    rel.rationale = row.rationale,
    rel.source_tool = row.source_tool,
    rel.source_revision = row.source_revision,
    rel.first_party_ref_version = row.first_party_ref_version`

const BatchCanonicalUsesModuleRepoRelationshipUpsertCypher = `UNWIND $rows AS row
MERGE (source_repo:Repository {id: row.repo_id})
ON CREATE SET source_repo.evidence_source = row.evidence_source,
              source_repo.generation_id = row.generation_id
MERGE (target_repo:Repository {id: row.target_repo_id})
ON CREATE SET target_repo.evidence_source = row.evidence_source,
              target_repo.generation_id = row.generation_id
MERGE (source_repo)-[rel:USES_MODULE]->(target_repo)
SET rel.confidence = row.confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = row.evidence_source,
    rel.evidence_type = row.evidence_type,
    rel.relationship_type = 'USES_MODULE',
    rel.resolved_id = row.resolved_id,
    rel.generation_id = row.generation_id,
    rel.evidence_count = row.evidence_count,
    rel.evidence_kinds = row.evidence_kinds,
    rel.resolution_source = row.resolution_source,
    rel.rationale = row.rationale,
    rel.source_tool = row.source_tool,
    rel.source_revision = row.source_revision,
    rel.first_party_ref_version = row.first_party_ref_version`

const BatchCanonicalReadsConfigFromRepoRelationshipUpsertCypher = `UNWIND $rows AS row
MERGE (source_repo:Repository {id: row.repo_id})
ON CREATE SET source_repo.evidence_source = row.evidence_source,
              source_repo.generation_id = row.generation_id
MERGE (target_repo:Repository {id: row.target_repo_id})
ON CREATE SET target_repo.evidence_source = row.evidence_source,
              target_repo.generation_id = row.generation_id
MERGE (source_repo)-[rel:READS_CONFIG_FROM]->(target_repo)
SET rel.confidence = row.confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = row.evidence_source,
    rel.evidence_type = row.evidence_type,
    rel.relationship_type = 'READS_CONFIG_FROM',
    rel.resolved_id = row.resolved_id,
    rel.generation_id = row.generation_id,
    rel.evidence_count = row.evidence_count,
    rel.evidence_kinds = row.evidence_kinds,
    rel.resolution_source = row.resolution_source,
    rel.rationale = row.rationale,
    rel.source_tool = row.source_tool,
    rel.source_revision = row.source_revision,
    rel.first_party_ref_version = row.first_party_ref_version`

const BatchCanonicalRepoEvidenceArtifactUpsertCypher = `UNWIND $rows AS row
MATCH (source_repo:Repository {id: row.repo_id})
MATCH (target_repo:Repository {id: row.target_repo_id})
MERGE (artifact:EvidenceArtifact {id: row.artifact_id})
SET artifact.name = row.name,
    artifact.domain = 'deployment',
    artifact.path = row.path,
    artifact.evidence_kind = row.evidence_kind,
    artifact.artifact_family = row.artifact_family,
    artifact.extractor = row.extractor,
    artifact.relationship_type = row.relationship_type,
    artifact.resolved_id = row.resolved_id,
    artifact.generation_id = row.generation_id,
    artifact.confidence = row.confidence,
    artifact.environment = row.environment,
    artifact.runtime_platform_kind = row.runtime_platform_kind,
    artifact.matched_alias = row.matched_alias,
    artifact.matched_value = row.matched_value,
	artifact.flux_git_repository_name = row.flux_git_repository_name,
	artifact.flux_git_repository_namespace = row.flux_git_repository_namespace,
    artifact.evidence_source = row.evidence_source,
    artifact.start_line = row.start_line,
    artifact.end_line = row.end_line,
    artifact.commit_sha = row.commit_sha,
    artifact.ref_value = row.ref_value,
    artifact.ref_pinned = row.ref_pinned
MERGE (source_repo)-[source_rel:HAS_DEPLOYMENT_EVIDENCE]->(artifact)
SET source_rel.evidence_source = row.evidence_source,
    source_rel.resolved_id = row.resolved_id,
    source_rel.relationship_type = row.relationship_type
MERGE (artifact)-[target_rel:EVIDENCES_REPOSITORY_RELATIONSHIP]->(target_repo)
SET target_rel.relationship_type = row.relationship_type,
    target_rel.resolved_id = row.resolved_id,
    target_rel.evidence_source = row.evidence_source`

const BatchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher = BatchCanonicalRepoEvidenceArtifactUpsertCypher + `
MERGE (env:Environment {name: row.environment})
MERGE (artifact)-[env_rel:TARGETS_ENVIRONMENT]->(env)
SET env_rel.evidence_source = row.evidence_source,
    env_rel.resolved_id = row.resolved_id`

// The Repository->Workload<-WorkloadInstance traversal is split into two
// single-hop MATCHes sharing the workload variable: on NornicDB v1.1.11 a
// multi-hop path pattern with a direction reversal
// ((repo)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(i)) matches zero rows
// (probed — the chained shape wrote nothing while the split shape writes),
// so the chained template silently wrote no RUNS_ON edge.
const CanonicalRunsOnUpsertCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})-[:DEFINES]->(w:Workload)
MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w)
MATCH (p:Platform {id: row.platform_id})
MERGE (i)-[rel:RUNS_ON {identity_key: 'canonical'}]->(p)
SET rel.confidence = 0.97,
    rel.reason = 'Repository workload instance runs on inferred platform',
    rel.evidence_source = row.evidence_source,
	 rel.source_tool = row.source_tool`

const BatchCanonicalRunsOnUpsertCypher = CanonicalRunsOnUpsertCypher

const BatchCanonicalRunsOnLegacyIdentityCleanupCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})-[:DEFINES]->(w:Workload)
MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w)
MATCH (p:Platform {id: row.platform_id})
MATCH (i)-[rel:RUNS_ON]->(p)
WHERE rel.identity_key IS NULL
DELETE rel`

const RepoDependencyRelationshipEdgeTypes = "DEPENDS_ON|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|" +
	"PROVISIONS_DEPENDENCY_FOR|USES_MODULE|READS_CONFIG_FROM"

const RetractRepoRelationshipEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source_repo:Repository {id: repo_id})
MATCH (source_repo)-[rel:` + RepoDependencyRelationshipEdgeTypes + `]->(:Repository)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const RetractSingleRepoRelationshipEdgesCypher = `MATCH (source_repo:Repository {id: $repo_id})
MATCH (source_repo)-[rel:` + RepoDependencyRelationshipEdgeTypes + `]->(:Repository)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

// Split single-hop MATCHes for the same NornicDB v1.1.11 reason as the
// RUNS_ON write template above: the chained direction-reversing path deletes
// nothing (probed), so the retract fans the traversal into shared-variable
// hops.
const RetractRepoRunsOnEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (repo:Repository {id: repo_id})-[:DEFINES]->(w:Workload)
MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w)
MATCH (i)-[rel:RUNS_ON]->(:Platform)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const RetractSingleRepoRunsOnEdgesCypher = `MATCH (repo:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
MATCH (i:WorkloadInstance)-[:INSTANCE_OF]->(w)
MATCH (i)-[rel:RUNS_ON]->(:Platform)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const RetractRepoEvidenceArtifactsCypher = `UNWIND $repo_ids AS repo_id
MATCH (source_repo:Repository {id: repo_id})
MATCH (source_repo)-[rel:HAS_DEPLOYMENT_EVIDENCE]->(artifact:EvidenceArtifact)
WHERE rel.evidence_source = $evidence_source
DETACH DELETE artifact`

const RetractSingleRepoEvidenceArtifactsCypher = `MATCH (source_repo:Repository {id: $repo_id})
MATCH (source_repo)-[rel:HAS_DEPLOYMENT_EVIDENCE]->(artifact:EvidenceArtifact)
WHERE rel.evidence_source = $evidence_source
DETACH DELETE artifact`
