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
ON CREATE SET p.evidence_source = $evidence_source
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

const canonicalInfrastructurePlatformUpsertCypher = `MATCH (repo:Repository {id: $repo_id})
MERGE (p:Platform {id: $platform_id})
ON CREATE SET p.evidence_source = $evidence_source
SET p.type = 'platform',
    p.name = $platform_name,
    p.kind = $platform_kind,
    p.provider = $platform_provider,
    p.environment = $platform_environment,
    p.region = $platform_region,
    p.locator = $platform_locator
MERGE (repo)-[rel:PROVISIONS_PLATFORM]->(p)
SET rel.confidence = 0.98,
    rel.reason = 'Terraform cluster and module data declare platform provisioning',
    rel.evidence_source = $evidence_source`

const canonicalDeploymentSourceUpsertCypher = `MATCH (i:WorkloadInstance {id: $instance_id})
MATCH (deployment_repo:Repository {id: $deployment_repo_id})
MERGE (i)-[rel:DEPLOYMENT_SOURCE]->(deployment_repo)
SET rel.confidence = 0.98,
    rel.reason = 'Deployment manifests for workload instance live in deployment repository',
    rel.evidence_source = $evidence_source`

const canonicalRepoDependencyUpsertCypher = `MERGE (source_repo:Repository {id: $repo_id})
MERGE (target_repo:Repository {id: $target_repo_id})
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
    rel.rationale = $rationale`

const canonicalWorkloadDependencyUpsertCypher = `MATCH (source:Workload {id: $workload_id})
MATCH (target:Workload {id: $target_workload_id})
MERGE (source)-[rel:DEPENDS_ON]->(target)
SET rel.confidence = 0.9,
    rel.reason = 'Runtime services list declares workload dependency',
    rel.evidence_source = $evidence_source`

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

const batchCanonicalInfrastructurePlatformUpsertCypher = `UNWIND $rows AS row
MATCH (repo:Repository {id: row.repo_id})
MERGE (p:Platform {id: row.platform_id})
ON CREATE SET p.evidence_source = row.evidence_source
SET p.type = 'platform',
    p.name = row.platform_name,
    p.kind = row.platform_kind,
    p.provider = row.platform_provider,
    p.environment = row.platform_environment,
    p.region = row.platform_region,
    p.locator = row.platform_locator
MERGE (repo)-[rel:PROVISIONS_PLATFORM]->(p)
SET rel.confidence = 0.98,
    rel.reason = 'Terraform cluster and module data declare platform provisioning',
    rel.evidence_source = row.evidence_source`

const batchCanonicalRepoDependencyUpsertCypher = `UNWIND $rows AS row
MERGE (source_repo:Repository {id: row.repo_id})
MERGE (target_repo:Repository {id: row.target_repo_id})
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
    rel.rationale = row.rationale`

const batchCanonicalWorkloadDependencyUpsertCypher = `UNWIND $rows AS row
MATCH (source:Workload {id: row.workload_id})
MATCH (target:Workload {id: row.target_workload_id})
MERGE (source)-[rel:DEPENDS_ON]->(target)
SET rel.confidence = 0.9,
    rel.reason = 'Runtime services list declares workload dependency',
    rel.evidence_source = row.evidence_source`

const batchCanonicalCodeCallUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|File {uid: coalesce(row.caller_entity_id, row.source_entity_id)})
MATCH (target:Function|Class|File {uid: coalesce(row.callee_entity_id, row.target_entity_id)})
MERGE (source)-[rel:CALLS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a code call edge',
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`

const batchCanonicalCodeReferenceUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|Struct|Interface|TypeAlias|File {uid: row.caller_entity_id})
MATCH (target:Function|Class|Struct|Interface|TypeAlias|File {uid: row.callee_entity_id})
MERGE (source)-[rel:REFERENCES]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a code reference edge',
    rel.evidence_source = row.evidence_source,
    rel.call_kind = row.call_kind`

const batchCanonicalMetaclassUpsertCypher = `UNWIND $rows AS row
MATCH (source:Function|Class|File {uid: row.source_entity_id})
MATCH (target:Function|Class|File {uid: row.target_entity_id})
MERGE (source)-[rel:USES_METACLASS]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'Parser and symbol analysis resolved a Python metaclass edge',
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`

// --- Batched UNWIND Cypher (inheritance edges) ---

const batchCanonicalInheritanceEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.child_entity_id})
MATCH (parent:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.parent_entity_id})
MERGE (child)-[rel:INHERITS]->(parent)
SET rel.confidence = 0.95,
    rel.reason = 'Parser entity bases metadata resolved an inheritance edge',
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`

const batchCanonicalInheritanceOverrideUpsertCypher = `UNWIND $rows AS row
MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.child_entity_id})
MATCH (parent:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.parent_entity_id})
MERGE (child)-[rel:OVERRIDES]->(parent)
SET rel.confidence = 0.95,
    rel.reason = 'Parser trait adaptation metadata resolved an override edge',
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`

const batchCanonicalInheritanceAliasUpsertCypher = `UNWIND $rows AS row
MATCH (child:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.child_entity_id})
MATCH (parent:Function|Class|Interface|Trait|Struct|Enum|Protocol {uid: row.parent_entity_id})
MERGE (child)-[rel:ALIASES]->(parent)
SET rel.confidence = 0.95,
    rel.reason = 'Parser trait adaptation metadata resolved an alias edge',
    rel.evidence_source = row.evidence_source,
    rel.relationship_type = row.relationship_type`

// --- Batched UNWIND Cypher (SQL relationship edges) ---

const batchCanonicalSQLRelationshipUpsertCypher = `UNWIND $rows AS row
MATCH (source:SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn {uid: row.source_entity_id})
MATCH (target:SqlTable|SqlView|SqlFunction|SqlTrigger|SqlIndex|SqlColumn {uid: row.target_entity_id})
MERGE (source)-[rel:REFERENCES_TABLE]->(target)
SET rel.confidence = 0.95,
    rel.reason = 'SQL entity metadata resolved a table reference edge',
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

// --- Retraction Cypher ---

const retractInheritanceEdgesCypher = `MATCH (child)-[rel:INHERITS|OVERRIDES|ALIASES]->()
WHERE child.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractSQLRelationshipEdgesCypher = `MATCH (source)-[rel:REFERENCES_TABLE|HAS_COLUMN|TRIGGERS]->()
WHERE source.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractInfrastructurePlatformEdgesCypher = `MATCH (repo:Repository)-[rel:PROVISIONS_PLATFORM]->(:Platform)
WHERE repo.id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractRepoDependencyEdgesCypher = `MATCH (source_repo:Repository)-[rel:DEPENDS_ON]->(:Repository)
WHERE source_repo.id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractWorkloadDependencyEdgesCypher = `MATCH (source:Workload)-[rel:DEPENDS_ON]->(:Workload)
WHERE source.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractCodeCallParserEdgesCypher = `MATCH (source:Function|Class|Struct|Interface|TypeAlias|File)-[rel:CALLS|REFERENCES]->()
WHERE source.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractCodeCallMetaclassEdgesCypher = `MATCH (source:Function|Class|File)-[rel:USES_METACLASS]->()
WHERE source.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const retractCodeCallFallbackEdgesCypher = `MATCH (source:Function|Class|Struct|Interface|TypeAlias|File)-[rel:CALLS|REFERENCES|USES_METACLASS]->()
WHERE source.repo_id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`

const deleteOrphanPlatformNodesCypher = `MATCH (p:Platform)
WHERE p.evidence_source = $evidence_source
  AND NOT (p)--()
DELETE p`

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
}

// CanonicalInfrastructurePlatformParams holds the parameters for a Platform +
// PROVISIONS_PLATFORM upsert from a Repository.
type CanonicalInfrastructurePlatformParams struct {
	RepoID              string
	PlatformID          string
	PlatformName        string
	PlatformKind        string
	PlatformProvider    string
	PlatformEnvironment string
	PlatformRegion      string
	PlatformLocator     string
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
		},
	}
}

// BuildCanonicalInfrastructurePlatformUpsert builds a Platform node +
// PROVISIONS_PLATFORM edge statement from a Repository.
func BuildCanonicalInfrastructurePlatformUpsert(p CanonicalInfrastructurePlatformParams, evidenceSource string) Statement {
	return Statement{
		Operation: OperationCanonicalUpsert,
		Cypher:    canonicalInfrastructurePlatformUpsertCypher,
		Parameters: map[string]any{
			"repo_id":              p.RepoID,
			"platform_id":          p.PlatformID,
			"platform_name":        p.PlatformName,
			"platform_kind":        p.PlatformKind,
			"platform_provider":    p.PlatformProvider,
			"platform_environment": p.PlatformEnvironment,
			"platform_region":      p.PlatformRegion,
			"platform_locator":     p.PlatformLocator,
			"evidence_source":      evidenceSource,
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
		},
	}
}
