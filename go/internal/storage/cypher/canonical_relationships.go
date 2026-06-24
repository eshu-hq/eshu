// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

import "github.com/eshu-hq/eshu/go/internal/graph/edgetype"

// CanonicalRepoRelationshipParams holds the parameters for a typed repository
// relationship upsert.
type CanonicalRepoRelationshipParams struct {
	RepoID           string
	TargetRepoID     string
	RelationshipType string
	EvidenceType     string
	ResolvedID       string
	GenerationID     string
	EvidenceCount    int
	EvidenceKinds    []string
	ResolutionSource string
	Confidence       float64
	Rationale        string
}

// CanonicalRunsOnParams holds the parameters for a repository-scoped RUNS_ON
// upsert that resolves to workload instances.
type CanonicalRunsOnParams struct {
	RepoID     string
	PlatformID string
}

const canonicalDeploysFromRepoRelationshipUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
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
    rel.rationale = $rationale`

const canonicalDiscoversConfigInRepoRelationshipUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
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
    rel.rationale = $rationale`

const canonicalProvisionsDependencyForRepoRelationshipUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
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
    rel.rationale = $rationale`

const canonicalUsesModuleRepoRelationshipUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
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
    rel.rationale = $rationale`

const canonicalReadsConfigFromRepoRelationshipUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
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
    rel.rationale = $rationale`

const batchCanonicalDeploysFromRepoRelationshipUpsertCypher = `UNWIND $rows AS row
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
    rel.rationale = row.rationale`

const batchCanonicalDiscoversConfigInRepoRelationshipUpsertCypher = `UNWIND $rows AS row
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
    rel.rationale = row.rationale`

const batchCanonicalProvisionsDependencyForRepoRelationshipUpsertCypher = `UNWIND $rows AS row
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
    rel.rationale = row.rationale`

const batchCanonicalUsesModuleRepoRelationshipUpsertCypher = `UNWIND $rows AS row
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
    rel.rationale = row.rationale`

const batchCanonicalReadsConfigFromRepoRelationshipUpsertCypher = `UNWIND $rows AS row
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
    rel.rationale = row.rationale`

const batchCanonicalRepoEvidenceArtifactUpsertCypher = `UNWIND $rows AS row
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
    artifact.evidence_source = row.evidence_source,
    artifact.start_line = row.start_line,
    artifact.end_line = row.end_line,
    artifact.commit_sha = row.commit_sha
MERGE (source_repo)-[source_rel:HAS_DEPLOYMENT_EVIDENCE]->(artifact)
SET source_rel.evidence_source = row.evidence_source,
    source_rel.resolved_id = row.resolved_id,
    source_rel.relationship_type = row.relationship_type
MERGE (artifact)-[target_rel:EVIDENCES_REPOSITORY_RELATIONSHIP]->(target_repo)
SET target_rel.relationship_type = row.relationship_type,
    target_rel.resolved_id = row.resolved_id,
    target_rel.evidence_source = row.evidence_source`

const batchCanonicalRepoEvidenceArtifactWithEnvironmentUpsertCypher = batchCanonicalRepoEvidenceArtifactUpsertCypher + `
MERGE (env:Environment {name: row.environment})
MERGE (artifact)-[env_rel:TARGETS_ENVIRONMENT]->(env)
SET env_rel.evidence_source = row.evidence_source,
    env_rel.resolved_id = row.resolved_id`

const canonicalRunsOnUpsertCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})
MATCH (repo)-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(i:WorkloadInstance)
MATCH (p:Platform {id: row.platform_id})
MERGE (i)-[rel:RUNS_ON]->(p)
SET rel.confidence = 0.97,
    rel.reason = 'Repository workload instance runs on inferred platform',
    rel.evidence_source = row.evidence_source`

const batchCanonicalRunsOnUpsertCypher = canonicalRunsOnUpsertCypher

const retractRepoRelationshipAndRunsOnEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source_repo:Repository {id: repo_id})-[rel:DEPENDS_ON|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|USES_MODULE|READS_CONFIG_FROM]->(:Repository)
WHERE rel.evidence_source = $evidence_source
DELETE rel
WITH DISTINCT repo_id, $evidence_source AS evidence_source
MATCH (repo:Repository {id: repo_id})-[:DEFINES]->(:Workload)<-[:INSTANCE_OF]-(i:WorkloadInstance)-[rel:RUNS_ON]->(:Platform)
WHERE rel.evidence_source = evidence_source
DELETE rel`

const retractRepoEvidenceArtifactsCypher = `UNWIND $repo_ids AS repo_id
MATCH (source_repo:Repository {id: repo_id})-[rel:HAS_DEPLOYMENT_EVIDENCE]->(artifact:EvidenceArtifact)
WHERE rel.evidence_source = $evidence_source
DETACH DELETE artifact`

// BuildCanonicalRepoRelationshipUpsert builds a typed repository relationship statement.
func BuildCanonicalRepoRelationshipUpsert(p CanonicalRepoRelationshipParams, evidenceSource string) Statement {
	cypher := canonicalTypedRepoRelationshipUpsertCypher(p.RelationshipType)
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    cypher,
		Parameters: map[string]any{
			"repo_id":           p.RepoID,
			"target_repo_id":    p.TargetRepoID,
			"relationship_type": p.RelationshipType,
			"evidence_type":     p.EvidenceType,
			"evidence_source":   evidenceSource,
			"resolved_id":       p.ResolvedID,
			"generation_id":     p.GenerationID,
			"evidence_count":    p.EvidenceCount,
			"evidence_kinds":    p.EvidenceKinds,
			"resolution_source": p.ResolutionSource,
			"confidence":        repoRelationshipConfidence(p.Confidence),
			"rationale":         p.Rationale,
		},
	}
}

func repoRelationshipConfidence(value float64) float64 {
	if value <= 0 {
		return 0.9
	}
	return value
}

func canonicalTypedRepoRelationshipUpsertCypher(relationshipType string) string {
	switch relationshipType {
	case string(edgetype.DeploysFrom):
		return canonicalDeploysFromRepoRelationshipUpsertCypher
	case string(edgetype.DiscoversConfigIn):
		return canonicalDiscoversConfigInRepoRelationshipUpsertCypher
	case string(edgetype.ProvisionsDependencyFor):
		return canonicalProvisionsDependencyForRepoRelationshipUpsertCypher
	case string(edgetype.UsesModule):
		return canonicalUsesModuleRepoRelationshipUpsertCypher
	case string(edgetype.ReadsConfigFrom):
		return canonicalReadsConfigFromRepoRelationshipUpsertCypher
	default:
		return canonicalRepoDependencyUpsertCypher
	}
}

func batchCanonicalTypedRepoRelationshipUpsertCypher(relationshipType string) (string, bool) {
	switch relationshipType {
	case string(edgetype.DeploysFrom):
		return batchCanonicalDeploysFromRepoRelationshipUpsertCypher, true
	case string(edgetype.DiscoversConfigIn):
		return batchCanonicalDiscoversConfigInRepoRelationshipUpsertCypher, true
	case string(edgetype.ProvisionsDependencyFor):
		return batchCanonicalProvisionsDependencyForRepoRelationshipUpsertCypher, true
	case string(edgetype.UsesModule):
		return batchCanonicalUsesModuleRepoRelationshipUpsertCypher, true
	case string(edgetype.ReadsConfigFrom):
		return batchCanonicalReadsConfigFromRepoRelationshipUpsertCypher, true
	default:
		return "", false
	}
}

// BuildCanonicalRunsOnUpsert builds a repository-scoped RUNS_ON statement.
func BuildCanonicalRunsOnUpsert(p CanonicalRunsOnParams, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    canonicalRunsOnUpsertCypher,
		Parameters: map[string]any{
			"repo_id":         p.RepoID,
			"platform_id":     p.PlatformID,
			"evidence_source": evidenceSource,
		},
	}
}
