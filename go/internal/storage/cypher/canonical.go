// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package cypher

// Canonical domain Cypher statements. These match the Python
// resolution/workloads/batches.py patterns exactly, porting UNWIND-batch
// writes to single-row parameterised statements suitable for the Go
// Executor interface.

const (
	// OperationCanonicalUpsert writes or refreshes one canonical domain node
	// or edge.
	OperationCanonicalUpsert Operation = "canonical_upsert"

	// OperationCanonicalRetract removes canonical domain edges or orphan
	// nodes.
	OperationCanonicalRetract Operation = "canonical_retract"
)

// --- Cypher templates ---

const canonicalWorkloadUpsertCypher = `MATCH (repo:Repository {id: $repo_id})
MERGE (w:Workload {id: $workload_id})
SET w.type = 'workload',
    w.name = $workload_name,
    w.kind = $workload_kind,
    w.repo_id = $repo_id,
    w.evidence_source = $evidence_source
MERGE (repo)-[rel:DEFINES]->(w)
SET rel.confidence = 1.0,
    rel.reason = 'Repository defines workload',
    rel.evidence_source = $evidence_source`

const canonicalWorkloadInstanceUpsertCypher = `MATCH (w:Workload {id: $workload_id})
MERGE (i:WorkloadInstance {id: $instance_id})
SET i.type = 'workload_instance',
    i.name = $workload_name,
    i.kind = $workload_kind,
    i.environment = $environment,
    i.workload_id = $workload_id,
    i.repo_id = $repo_id,
    i.evidence_source = $evidence_source
MERGE (i)-[rel:INSTANCE_OF]->(w)
SET rel.confidence = 1.0,
    rel.reason = 'Workload instance belongs to workload',
    rel.evidence_source = $evidence_source`

const canonicalRuntimePlatformUpsertCypher = `MATCH (i:WorkloadInstance {id: $instance_id})
MERGE (p:Platform {id: $platform_id})
ON CREATE SET p.evidence_source = $evidence_source,
              p.generation_id = $generation_id
SET p.type = 'platform',
    p.name = $platform_name,
    p.kind = $platform_kind,
    p.provider = $platform_provider,
    p.environment = $environment,
    p.region = $platform_region,
    p.locator = $platform_locator
MERGE (i)-[rel:RUNS_ON]->(p)
SET rel.confidence = 1.0,
    rel.reason = 'Workload instance runs on inferred platform',
    rel.evidence_source = $evidence_source`

const canonicalDeploymentSourceUpsertCypher = `MATCH (i:WorkloadInstance {id: $instance_id})
MATCH (deployment_repo:Repository {id: $deployment_repo_id})
MERGE (i)-[rel:DEPLOYMENT_SOURCE]->(deployment_repo)
SET rel.confidence = 0.98,
    rel.reason = 'Deployment manifests for workload instance live in deployment repository',
    rel.evidence_source = $evidence_source`

const canonicalRepoDependencyUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
ON CREATE SET source_repo.evidence_source = $evidence_source,
              source_repo.generation_id = $generation_id
MERGE (target_repo:Repository {id: $target_repo_id})
ON CREATE SET target_repo.evidence_source = $evidence_source,
              target_repo.generation_id = $generation_id
MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)
SET rel.confidence = $confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = $evidence_source,
    rel.evidence_type = $evidence_type,
    rel.relationship_type = 'DEPENDS_ON',
    rel.resolved_id = $resolved_id,
    rel.generation_id = $generation_id,
    rel.evidence_count = $evidence_count,
    rel.evidence_kinds = $evidence_kinds,
    rel.resolution_source = $resolution_source,
    rel.rationale = $rationale,
    rel.source_tool = $source_tool`

const canonicalWorkloadDependencyUpsertCypher = `MATCH (source:Workload {id: $workload_id})
MATCH (target:Workload {id: $target_workload_id})
MERGE (source)-[rel:DEPENDS_ON]->(target)
SET rel.confidence = 0.9,
    rel.reason = 'Runtime services list declares workload dependency',
    rel.evidence_source = $evidence_source`

// The single-row canonical code-edge templates below are the legacy non-batched
// builders (BuildCanonicalCodeCallUpsert); they have no production caller. The
// live graph-write path uses the batched UNWIND templates in
// canonical_code_call_edges.go, which carry the per-edge resolution provenance
// and tiered confidence from ADR #2222.
const canonicalCodeCallUpsertCypher = `MATCH (source {id: $caller_entity_id})
MATCH (target {id: $callee_entity_id})
MERGE (source)-[rel:CALLS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a code call edge',
    rel.evidence_source = $evidence_source,
    rel.call_kind = $call_kind`

const canonicalJSXComponentReferenceUpsertCypher = `MATCH (source {id: $caller_entity_id})
MATCH (target {id: $callee_entity_id})
MERGE (source)-[rel:REFERENCES]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a TSX component reference edge',
    rel.evidence_source = $evidence_source,
    rel.call_kind = $call_kind`

const canonicalMetaclassUpsertCypher = `MATCH (source {id: $caller_entity_id})
MATCH (target {id: $callee_entity_id})
MERGE (source)-[rel:USES_METACLASS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a Python metaclass edge',
    rel.evidence_source = $evidence_source,
    rel.relationship_type = $relationship_type`

// --- Batched UNWIND Cypher (shared projection) ---

const batchCanonicalRepoDependencyUpsertCypher = `UNWIND $rows AS row
MERGE (source_repo:Repository {id: row.repo_id})
ON CREATE SET source_repo.evidence_source = row.evidence_source,
              source_repo.generation_id = row.generation_id
MERGE (target_repo:Repository {id: row.target_repo_id})
ON CREATE SET target_repo.evidence_source = row.evidence_source,
              target_repo.generation_id = row.generation_id
MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)
SET rel.confidence = row.confidence,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = row.evidence_source,
    rel.evidence_type = row.evidence_type,
    rel.relationship_type = 'DEPENDS_ON',
    rel.resolved_id = row.resolved_id,
    rel.generation_id = row.generation_id,
    rel.evidence_count = row.evidence_count,
    rel.evidence_kinds = row.evidence_kinds,
    rel.resolution_source = row.resolution_source,
    rel.rationale = row.rationale,
    rel.source_tool = row.source_tool`

const batchCanonicalWorkloadDependencyUpsertCypher = `UNWIND $rows AS row
MATCH (source:Workload {id: row.workload_id})
MATCH (target:Workload {id: row.target_workload_id})
MERGE (source)-[rel:DEPENDS_ON]->(target)
SET rel.confidence = 0.9,
    rel.reason = 'Runtime services list declares workload dependency',
    rel.evidence_source = row.evidence_source`

// --- Batched UNWIND Cypher (SQL relationship edges) ---

const batchCanonicalSQLQueriesTableUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function {uid: row.source_entity_id})
MATCH (target:SqlTable {uid: row.target_entity_id})
MERGE (source)-[rel:QUERIES_TABLE]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser embedded SQL evidence resolved a function table query edge',
    rel.evidence_source = row.evidence_source`

const batchCanonicalSQLHasColumnUpsertCypher = `UNWIND $rows AS row
MATCH (source:SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn {uid: row.source_entity_id})
MATCH (target:SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn {uid: row.target_entity_id})
MERGE (source)-[rel:HAS_COLUMN]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'SQL entity metadata resolved a table-column containment edge',
    rel.evidence_source = row.evidence_source`

const batchCanonicalSQLTriggersUpsertCypher = `UNWIND $rows AS row
MATCH (source:SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn {uid: row.source_entity_id})
MATCH (target:SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn {uid: row.target_entity_id})
MERGE (source)-[rel:TRIGGERS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'SQL entity metadata resolved a trigger edge',
    rel.evidence_source = row.evidence_source`

const batchCanonicalSQLExecutesUpsertCypher = `UNWIND $rows AS row
MATCH (source:SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn {uid: row.source_entity_id})
MATCH (target:SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn {uid: row.target_entity_id})
MERGE (source)-[rel:EXECUTES]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'SQL trigger metadata resolved a routine execution edge',
    rel.evidence_source = row.evidence_source`

// --- Retraction Cypher ---

// Inheritance edge retraction (INHERITS/OVERRIDES/ALIASES/IMPLEMENTS) is built
// per child label by buildInheritanceRetractStatements in
// canonical_inheritance_retract.go, not as a single constant: NornicDB matches
// neither a node-label disjunction nor (on v1.1.11) an unlabeled child scan
// reliably (#5116/#4367).

// SQL relationship edge retraction (QUERIES_TABLE/REFERENCES_TABLE/READS_FROM/
// WRITES_TO/HAS_COLUMN/TRIGGERS/EXECUTES/INDEXES/MIGRATES) is built per source
// label by buildSQLRelationshipRetractStatements in edge_writer_sql.go, not as a single
// constant: NornicDB matches neither a node-label disjunction nor (on v1.1.11)
// an unlabeled source scan reliably, and multiple DELETEs grouped in one
// managed transaction under-apply (#5116 sibling).

const retractRepoDependencyEdgesCypher = `UNWIND $repo_ids AS repo_id
MATCH (source_repo:Repository {id: repo_id})
MATCH (source_repo)-[rel:DEPENDS_ON]->(:Repository)
WHERE rel.evidence_source = $evidence_source
DELETE rel`

const retractWorkloadDependencyEdgesCypher = `MATCH (source:Workload)-[rel:DEPENDS_ON]->(:Workload)
WHERE source.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

// Code-call edge retraction (CALLS/REFERENCES/INSTANTIATES/USES_METACLASS) is
// built per source label by buildCodeCallRetractStatements in
// canonical_retract.go, not as a single constant: NornicDB matches neither a
// node-label disjunction nor (on v1.1.11) an unlabeled source scan reliably
// (#5116).

// Platform orphan node cleanup used to live here as
// deleteOrphanPlatformNodesCypher (`WHERE ... AND NOT (p)--()`). That
// negated-pattern predicate is a permanently-false no-op on the pinned
// NornicDB backends (see docs/public/reference/nornicdb-pitfalls.md, "Every
// Relationship-Existence Predicate Is Mis-Evaluated"), and it had no
// production caller -- BuildDeleteOrphanPlatformNodes was referenced only by
// its own test. Platform orphan cleanup is covered instead by the S1/S2
// Go-side anti-join in orphan_sweep.go (OrphanSweepLabelPlatform), which
// never relies on a relationship-existence predicate. Removed by #5310.

// --- Param structs ---

// CanonicalWorkloadParams holds the parameters for a Workload + DEFINES upsert.
type CanonicalWorkloadParams struct {
	RepoID       string
	WorkloadID   string
	WorkloadName string
	WorkloadKind string
}

// CanonicalWorkloadInstanceParams holds the parameters for a WorkloadInstance +
// INSTANCE_OF upsert.
type CanonicalWorkloadInstanceParams struct {
	WorkloadID   string
	InstanceID   string
	WorkloadName string
	WorkloadKind string
	Environment  string
	RepoID       string
}

// CanonicalRuntimePlatformParams holds the parameters for a Platform + RUNS_ON
// upsert from a WorkloadInstance.
type CanonicalRuntimePlatformParams struct {
	InstanceID       string
	PlatformID       string
	PlatformName     string
	PlatformKind     string
	PlatformProvider string
	Environment      string
	PlatformRegion   string
	PlatformLocator  string
	GenerationID     string
}

// CanonicalDeploymentSourceParams holds the parameters for a
// DEPLOYMENT_SOURCE edge upsert.
type CanonicalDeploymentSourceParams struct {
	InstanceID       string
	DeploymentRepoID string
}

// CanonicalRepoDependencyParams holds the parameters for a Repository
// DEPENDS_ON edge upsert.
type CanonicalRepoDependencyParams struct {
	RepoID           string
	TargetRepoID     string
	EvidenceType     string
	ResolvedID       string
	GenerationID     string
	EvidenceCount    int
	EvidenceKinds    []string
	ResolutionSource string
	Confidence       float64
	Rationale        string
	// SourceTool is the normalized provenance token for the producing tool
	// (#3997/#3999), derived from the edge's primary evidence kind. Empty when no
	// tool is derivable; never a guessed value.
	SourceTool string
}

// CanonicalWorkloadDependencyParams holds the parameters for a Workload
// DEPENDS_ON edge upsert.
type CanonicalWorkloadDependencyParams struct {
	WorkloadID       string
	TargetWorkloadID string
}

// CanonicalCodeCallParams holds the parameters for a code-level CALLS edge
// upsert between two canonical entities.
type CanonicalCodeCallParams struct {
	CallerEntityID   string
	CalleeEntityID   string
	CallKind         string
	RelationshipType string
}

// --- Builders ---

// BuildCanonicalWorkloadUpsert builds a Workload node + DEFINES edge statement.
func BuildCanonicalWorkloadUpsert(p CanonicalWorkloadParams, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    canonicalWorkloadUpsertCypher,
		Parameters: map[string]any{
			"repo_id":         p.RepoID,
			"workload_id":     p.WorkloadID,
			"workload_name":   p.WorkloadName,
			"workload_kind":   p.WorkloadKind,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildCanonicalWorkloadInstanceUpsert builds a WorkloadInstance node +
// INSTANCE_OF edge statement.
func BuildCanonicalWorkloadInstanceUpsert(p CanonicalWorkloadInstanceParams, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    canonicalWorkloadInstanceUpsertCypher,
		Parameters: map[string]any{
			"workload_id":     p.WorkloadID,
			"instance_id":     p.InstanceID,
			"workload_name":   p.WorkloadName,
			"workload_kind":   p.WorkloadKind,
			"environment":     p.Environment,
			"repo_id":         p.RepoID,
			"evidence_source": evidenceSource,
		},
	}
}

// BuildCanonicalRuntimePlatformUpsert builds a Platform node + RUNS_ON edge
// statement from a WorkloadInstance.
func BuildCanonicalRuntimePlatformUpsert(p CanonicalRuntimePlatformParams, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    canonicalRuntimePlatformUpsertCypher,
		Parameters: map[string]any{
			"instance_id":       p.InstanceID,
			"platform_id":       p.PlatformID,
			"platform_name":     p.PlatformName,
			"platform_kind":     p.PlatformKind,
			"platform_provider": p.PlatformProvider,
			"environment":       p.Environment,
			"platform_region":   p.PlatformRegion,
			"platform_locator":  p.PlatformLocator,
			"evidence_source":   evidenceSource,
			"generation_id":     p.GenerationID,
		},
	}
}

// BuildCanonicalDeploymentSourceUpsert builds a DEPLOYMENT_SOURCE edge
// statement.
func BuildCanonicalDeploymentSourceUpsert(p CanonicalDeploymentSourceParams, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    canonicalDeploymentSourceUpsertCypher,
		Parameters: map[string]any{
			"instance_id":        p.InstanceID,
			"deployment_repo_id": p.DeploymentRepoID,
			"evidence_source":    evidenceSource,
		},
	}
}

// BuildCanonicalRepoDependencyUpsert builds a Repository DEPENDS_ON edge
// statement.
func BuildCanonicalRepoDependencyUpsert(p CanonicalRepoDependencyParams, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    canonicalRepoDependencyUpsertCypher,
		Parameters: map[string]any{
			"repo_id":           p.RepoID,
			"target_repo_id":    p.TargetRepoID,
			"evidence_type":     p.EvidenceType,
			"evidence_source":   evidenceSource,
			"resolved_id":       p.ResolvedID,
			"generation_id":     p.GenerationID,
			"evidence_count":    p.EvidenceCount,
			"evidence_kinds":    p.EvidenceKinds,
			"resolution_source": p.ResolutionSource,
			"confidence":        repoRelationshipConfidence(p.Confidence),
			"rationale":         p.Rationale,
			"source_tool":       p.SourceTool,
		},
	}
}
