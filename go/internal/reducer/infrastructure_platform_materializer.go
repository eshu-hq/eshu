package reducer

import (
	"context"
	"fmt"
)

// InfrastructurePlatformRow holds one inferred PROVISIONS_PLATFORM edge payload.
type InfrastructurePlatformRow struct {
	RepoID              string
	PlatformID          string
	PlatformName        string
	PlatformKind        string
	PlatformProvider    string
	PlatformEnvironment string
	PlatformRegion      string
	PlatformLocator     string
}

// InfrastructurePlatformResult holds counts from one materialization pass.
type InfrastructurePlatformResult struct {
	PlatformEdgesWritten int
}

// InfrastructurePlatformMaterializer writes PROVISIONS_PLATFORM edges to Neo4j.
// It is the Go equivalent of Python's materialize_infrastructure_platforms
// orchestrator for the graph-write portion.
type InfrastructurePlatformMaterializer struct {
	executor  CypherExecutor
	BatchSize int
}

// NewInfrastructurePlatformMaterializer returns an InfrastructurePlatformMaterializer
// backed by the given CypherExecutor.
func NewInfrastructurePlatformMaterializer(executor CypherExecutor) *InfrastructurePlatformMaterializer {
	return &InfrastructurePlatformMaterializer{executor: executor}
}

// batchSize returns the configured batch size, defaulting to 500 if not set.
func (m *InfrastructurePlatformMaterializer) batchSize() int {
	if m.BatchSize <= 0 {
		return 500
	}
	return m.BatchSize
}

// executeBatched splits rows into batches and executes the given Cypher query
// with each batch as a "rows" parameter.
func (m *InfrastructurePlatformMaterializer) executeBatched(
	ctx context.Context,
	cypher string,
	rows []map[string]any,
) error {
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

// Materialize writes PROVISIONS_PLATFORM edges for each infrastructure
// platform row. Each row creates a Platform node (via MERGE) and a
// PROVISIONS_PLATFORM edge from the Repository.
func (m *InfrastructurePlatformMaterializer) Materialize(
	ctx context.Context,
	rows []InfrastructurePlatformRow,
) (InfrastructurePlatformResult, error) {
	if len(rows) == 0 {
		return InfrastructurePlatformResult{}, nil
	}
	if m.executor == nil {
		return InfrastructurePlatformResult{}, fmt.Errorf("infrastructure platform materializer executor is required")
	}

	batchRows := make([]map[string]any, len(rows))
	for i, row := range rows {
		batchRows[i] = map[string]any{
			"repo_id":              row.RepoID,
			"platform_id":          row.PlatformID,
			"platform_name":        row.PlatformName,
			"platform_kind":        row.PlatformKind,
			"platform_provider":    row.PlatformProvider,
			"platform_environment": row.PlatformEnvironment,
			"platform_region":      row.PlatformRegion,
			"platform_locator":     row.PlatformLocator,
			"evidence_source":      EvidenceSourceWorkloads,
		}
	}

	if err := m.executeBatched(ctx, batchInfraPlatformUpsertCypher, batchRows); err != nil {
		return InfrastructurePlatformResult{}, fmt.Errorf("write infrastructure platforms: %w", err)
	}

	return InfrastructurePlatformResult{PlatformEdgesWritten: len(rows)}, nil
}

// RetractStale removes PROVISIONS_PLATFORM edges for the given repository IDs
// that are no longer backed by current Terraform signals.
func (m *InfrastructurePlatformMaterializer) RetractStale(
	ctx context.Context,
	repoIDs []string,
) error {
	if len(repoIDs) == 0 {
		return nil
	}
	if m.executor == nil {
		return fmt.Errorf("infrastructure platform materializer executor is required")
	}

	return m.executor.ExecuteCypher(ctx, infraPlatformRetractCypher, map[string]any{
		"repo_ids":        repoIDs,
		"evidence_source": EvidenceSourceWorkloads,
	})
}

const batchInfraPlatformUpsertCypher = `UNWIND $rows AS row
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

const infraPlatformRetractCypher = `MATCH (repo:Repository)-[rel:PROVISIONS_PLATFORM]->(:Platform)
WHERE repo.id IN $repo_ids
  AND rel.evidence_source = $evidence_source
DELETE rel`
