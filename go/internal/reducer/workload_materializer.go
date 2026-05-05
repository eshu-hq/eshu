package reducer

import (
	"context"
	"fmt"
	"time"
)

// CypherExecutor executes one parameterised Cypher statement against the canonical graph backend.
// This is the reducer-local interface — adapters bridge it to the storage
// layer's cypher.Executor.
type CypherExecutor interface {
	ExecuteCypher(ctx context.Context, cypher string, params map[string]any) error
}

// DefaultMaterializerBatchSize is the default batch size for UNWIND operations.
const DefaultMaterializerBatchSize = 500

// MaterializeResult holds counts from one materialization pass.
type MaterializeResult struct {
	WorkloadsWritten            int
	InstancesWritten            int
	DeploymentSourcesWritten    int
	RuntimePlatformsWritten     int
	EndpointsWritten            int
	RepoDependenciesWritten     int
	WorkloadDependenciesWritten int
	WorkloadWriteDuration       time.Duration
	InstanceWriteDuration       time.Duration
	DeploymentSourceDuration    time.Duration
	RuntimePlatformDuration     time.Duration
	EndpointWriteDuration       time.Duration
}

// WorkloadMaterializer converts projection results into canonical graph
// writes. It is the Go equivalent of Python's materialize_workloads orchestrator.
type WorkloadMaterializer struct {
	executor  CypherExecutor
	BatchSize int
}

// NewWorkloadMaterializer returns a WorkloadMaterializer backed by the given
// CypherExecutor.
func NewWorkloadMaterializer(executor CypherExecutor) *WorkloadMaterializer {
	return &WorkloadMaterializer{executor: executor}
}

// CypherExecutor returns the underlying reducer Cypher executor.
func (m *WorkloadMaterializer) CypherExecutor() CypherExecutor {
	if m == nil {
		return nil
	}

	return m.executor
}

// batchSize returns the configured batch size or DefaultMaterializerBatchSize if zero.
func (m *WorkloadMaterializer) batchSize() int {
	if m.BatchSize <= 0 {
		return DefaultMaterializerBatchSize
	}
	return m.BatchSize
}

// executeBatched splits rows into chunks and executes the given Cypher query with UNWIND.
func (m *WorkloadMaterializer) executeBatched(ctx context.Context, cypher string, rows []map[string]any) error {
	batchSize := m.batchSize()
	for i := 0; i < len(rows); i += batchSize {
		end := i + batchSize
		if end > len(rows) {
			end = len(rows)
		}
		chunk := rows[i:end]
		if err := m.executor.ExecuteCypher(ctx, cypher, map[string]any{"rows": chunk}); err != nil {
			return err
		}
	}
	return nil
}

// Materialize writes all canonical workload, instance, deployment source, and
// runtime platform nodes and edges from a ProjectionResult.
func (m *WorkloadMaterializer) Materialize(
	ctx context.Context,
	projection *ProjectionResult,
) (MaterializeResult, error) {
	total := len(projection.WorkloadRows) +
		len(projection.InstanceRows) +
		len(projection.DeploymentSourceRows) +
		len(projection.RuntimePlatformRows) +
		len(projection.EndpointRows)

	if total == 0 {
		return MaterializeResult{}, nil
	}
	if m.executor == nil {
		return MaterializeResult{}, fmt.Errorf("workload materializer executor is required")
	}

	var result MaterializeResult

	// Batch workloads
	if len(projection.WorkloadRows) > 0 {
		stageStarted := time.Now()
		rows := make([]map[string]any, len(projection.WorkloadRows))
		for i, row := range projection.WorkloadRows {
			rows[i] = map[string]any{
				"repo_id":                    row.RepoID,
				"workload_id":                row.WorkloadID,
				"workload_name":              row.WorkloadName,
				"workload_kind":              row.WorkloadKind,
				"classification":             row.Classification,
				"materialization_confidence": row.Confidence,
				"materialization_provenance": row.Provenance,
				"evidence_source":            EvidenceSourceWorkloads,
			}
		}
		if err := m.executeBatched(ctx, batchWorkloadNodeUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write workloads: %w", err)
		}
		if err := m.executeBatched(ctx, batchWorkloadDefinesEdgeUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write workload defines edges: %w", err)
		}
		result.WorkloadWriteDuration = time.Since(stageStarted)
		result.WorkloadsWritten = len(projection.WorkloadRows)
	}

	// Batch instances
	if len(projection.InstanceRows) > 0 {
		stageStarted := time.Now()
		rows := make([]map[string]any, len(projection.InstanceRows))
		for i, row := range projection.InstanceRows {
			rows[i] = map[string]any{
				"workload_id":                row.WorkloadID,
				"instance_id":                row.InstanceID,
				"workload_name":              row.WorkloadName,
				"workload_kind":              row.WorkloadKind,
				"classification":             row.Classification,
				"environment":                row.Environment,
				"repo_id":                    row.RepoID,
				"materialization_confidence": row.Confidence,
				"materialization_provenance": row.Provenance,
				"evidence_source":            EvidenceSourceWorkloads,
			}
		}
		if err := m.executeBatched(ctx, batchWorkloadInstanceNodeUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write instances: %w", err)
		}
		if err := m.executeBatched(ctx, batchWorkloadInstanceOfEdgeUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write instance edges: %w", err)
		}
		result.InstanceWriteDuration = time.Since(stageStarted)
		result.InstancesWritten = len(projection.InstanceRows)
	}

	// Batch deployment sources
	if len(projection.DeploymentSourceRows) > 0 {
		stageStarted := time.Now()
		rows := make([]map[string]any, len(projection.DeploymentSourceRows))
		for i, row := range projection.DeploymentSourceRows {
			rows[i] = map[string]any{
				"instance_id":           row.InstanceID,
				"deployment_repo_id":    row.DeploymentRepoID,
				"deployment_confidence": row.Confidence,
				"deployment_provenance": row.Provenance,
				"evidence_source":       EvidenceSourceWorkloads,
			}
		}
		if err := m.executeBatched(ctx, batchDeploymentSourceUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write deployment sources: %w", err)
		}
		result.DeploymentSourceDuration = time.Since(stageStarted)
		result.DeploymentSourcesWritten = len(projection.DeploymentSourceRows)
	}

	// Batch runtime platforms
	if len(projection.RuntimePlatformRows) > 0 {
		stageStarted := time.Now()
		rows := make([]map[string]any, len(projection.RuntimePlatformRows))
		for i, row := range projection.RuntimePlatformRows {
			rows[i] = map[string]any{
				"instance_id":         row.InstanceID,
				"platform_id":         row.PlatformID,
				"platform_name":       row.PlatformName,
				"platform_kind":       row.PlatformKind,
				"platform_provider":   row.PlatformProvider,
				"environment":         row.Environment,
				"platform_region":     row.PlatformRegion,
				"platform_locator":    row.PlatformLocator,
				"platform_confidence": runtimePlatformConfidence(row.Confidence),
				"evidence_source":     EvidenceSourceWorkloads,
			}
		}
		if err := m.executeBatched(ctx, batchRuntimePlatformNodeUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write runtime platforms: %w", err)
		}
		if err := m.executeBatched(ctx, batchRuntimePlatformRunsOnEdgeUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write runtime platform edges: %w", err)
		}
		result.RuntimePlatformDuration = time.Since(stageStarted)
		result.RuntimePlatformsWritten = len(projection.RuntimePlatformRows)
	}

	// Batch API endpoints.
	if len(projection.EndpointRows) > 0 {
		stageStarted := time.Now()
		rows := make([]map[string]any, len(projection.EndpointRows))
		for i, row := range projection.EndpointRows {
			rows[i] = map[string]any{
				"endpoint_id":     row.EndpointID,
				"repo_id":         row.RepoID,
				"workload_id":     row.WorkloadID,
				"workload_name":   row.WorkloadName,
				"path":            row.Path,
				"methods":         row.Methods,
				"operation_ids":   row.OperationIDs,
				"source_kinds":    row.SourceKinds,
				"source_paths":    row.SourcePaths,
				"spec_versions":   row.SpecVersions,
				"api_versions":    row.APIVersions,
				"evidence_source": EvidenceSourceWorkloads,
			}
		}
		if err := m.executeBatched(ctx, batchAPIEndpointNodeUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write api endpoints: %w", err)
		}
		if err := m.executeBatched(ctx, batchAPIEndpointRepoEdgeUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write api endpoint repository edges: %w", err)
		}
		if err := m.executeBatched(ctx, batchAPIEndpointWorkloadEdgeUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write api endpoint workload edges: %w", err)
		}
		result.EndpointWriteDuration = time.Since(stageStarted)
		result.EndpointsWritten = len(projection.EndpointRows)
	}

	return result, nil
}

// MaterializeDependencies writes repo and workload DEPENDS_ON edges.
func (m *WorkloadMaterializer) MaterializeDependencies(
	ctx context.Context,
	repoDeps []RepoDependencyRow,
	workloadDeps []WorkloadDependencyRow,
) (MaterializeResult, error) {
	total := len(repoDeps) + len(workloadDeps)
	if total == 0 {
		return MaterializeResult{}, nil
	}
	if m.executor == nil {
		return MaterializeResult{}, fmt.Errorf("workload materializer executor is required")
	}

	var result MaterializeResult

	// Batch repo dependencies
	if len(repoDeps) > 0 {
		rows := make([]map[string]any, len(repoDeps))
		for i, row := range repoDeps {
			rows[i] = map[string]any{
				"repo_id":         row.RepoID,
				"target_repo_id":  row.TargetRepoID,
				"evidence_source": EvidenceSourceWorkloads,
			}
		}
		if err := m.executeBatched(ctx, batchRepoDependencyUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write repo dependencies: %w", err)
		}
		result.RepoDependenciesWritten = len(repoDeps)
	}

	// Batch workload dependencies
	if len(workloadDeps) > 0 {
		rows := make([]map[string]any, len(workloadDeps))
		for i, row := range workloadDeps {
			rows[i] = map[string]any{
				"workload_id":        row.WorkloadID,
				"target_workload_id": row.TargetWorkloadID,
				"evidence_source":    EvidenceSourceWorkloads,
			}
		}
		if err := m.executeBatched(ctx, batchWorkloadDependencyUpsertCypher, rows); err != nil {
			return result, fmt.Errorf("write workload dependencies: %w", err)
		}
		result.WorkloadDependenciesWritten = len(workloadDeps)
	}

	return result, nil
}

func runtimePlatformConfidence(confidence float64) float64 {
	if confidence <= 0 {
		return 0.9
	}
	return confidence
}

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
    w.evidence_source = row.evidence_source`

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
    i.evidence_source = row.evidence_source`

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

	batchRuntimePlatformRunsOnEdgeUpsertCypher = `UNWIND $rows AS row
MATCH (i:WorkloadInstance {id: row.instance_id})
MATCH (p:Platform {id: row.platform_id})
MERGE (i)-[rel:RUNS_ON]->(p)
SET rel.confidence = row.platform_confidence,
    rel.reason = 'Workload instance runs on inferred platform',
    rel.evidence_source = row.evidence_source`

	batchRepoDependencyUpsertCypher = `UNWIND $rows AS row
MATCH (source_repo:Repository {id: row.repo_id})
MATCH (target_repo:Repository {id: row.target_repo_id})
MERGE (source_repo)-[rel:DEPENDS_ON]->(target_repo)
SET rel.confidence = 0.9,
    rel.reason = 'Runtime services list declares repository dependency',
    rel.evidence_source = row.evidence_source`

	batchWorkloadDependencyUpsertCypher = `UNWIND $rows AS row
MATCH (source:Workload {id: row.workload_id})
MATCH (target:Workload {id: row.target_workload_id})
MERGE (source)-[rel:DEPENDS_ON]->(target)
SET rel.confidence = 0.9,
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
    endpoint.evidence_source = row.evidence_source`

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
