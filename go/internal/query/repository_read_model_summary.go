package query

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

type repositoryReadModelSummary struct {
	Available       bool
	WorkloadNames   []string
	PlatformCount   int
	PlatformTypes   []string
	DependencyCount int
}

type repositoryReadModelSummaryStore interface {
	repositoryReadModelSummary(context.Context, string) (repositoryReadModelSummary, error)
}

func loadRepositoryReadModelSummary(ctx context.Context, content ContentStore, repoID string) *repositoryReadModelSummary {
	store, ok := content.(repositoryReadModelSummaryStore)
	if !ok || repoID == "" {
		return nil
	}
	summary, err := store.repositoryReadModelSummary(ctx, repoID)
	if err != nil || !summary.Available {
		return nil
	}
	return &summary
}

func (cr *ContentReader) repositoryReadModelSummary(ctx context.Context, repoID string) (repositoryReadModelSummary, error) {
	if cr == nil || cr.db == nil || repoID == "" {
		return repositoryReadModelSummary{}, nil
	}
	scopeID, err := cr.repositoryScopeID(ctx, repoID)
	if err != nil {
		return repositoryReadModelSummary{}, err
	}

	workloadNames, err := cr.repositoryWorkloadNames(ctx, scopeID)
	if err != nil {
		return repositoryReadModelSummary{}, err
	}
	platformCount, err := cr.repositoryPlatformMaterializationCount(ctx, scopeID)
	if err != nil {
		return repositoryReadModelSummary{}, err
	}
	dependencyCount, err := cr.repositoryDependencyCount(ctx, repoID)
	if err != nil {
		return repositoryReadModelSummary{}, err
	}
	return repositoryReadModelSummary{
		Available:       scopeID != "" || dependencyCount > 0,
		WorkloadNames:   workloadNames,
		PlatformCount:   platformCount,
		DependencyCount: dependencyCount,
	}, nil
}

func (cr *ContentReader) repositoryScopeID(ctx context.Context, repoID string) (string, error) {
	var scopeID string
	err := cr.db.QueryRowContext(ctx, `
		SELECT scope_id
		FROM ingestion_scopes
		WHERE scope_kind = 'repository'
		  AND (
			scope_id = $1 OR
			source_key = $1 OR
			payload->>'repo_id' = $1 OR
			payload->>'id' = $1
		  )
		ORDER BY scope_id
		LIMIT 1
	`, repoID).Scan(&scopeID)
	if err == sql.ErrNoRows {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("query repository scope id: %w", err)
	}
	return scopeID, nil
}

func (cr *ContentReader) repositoryWorkloadNames(ctx context.Context, scopeID string) ([]string, error) {
	if scopeID == "" {
		return nil, nil
	}
	rows, err := cr.db.QueryContext(ctx, `
		SELECT DISTINCT entity_key
		FROM fact_records,
		     jsonb_array_elements_text(coalesce(payload->'entity_keys', '[]'::jsonb)) AS entity_key
		WHERE scope_id = $1
		  AND fact_kind = 'reducer_workload_identity'
		  AND NOT is_tombstone
		ORDER BY entity_key
	`, scopeID)
	if err != nil {
		return nil, fmt.Errorf("query repository workload names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	names := make([]string, 0)
	for rows.Next() {
		var entityKey string
		if err := rows.Scan(&entityKey); err != nil {
			return nil, fmt.Errorf("scan repository workload name: %w", err)
		}
		names = append(names, strings.TrimPrefix(entityKey, "workload:"))
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate repository workload names: %w", err)
	}
	return names, nil
}

func (cr *ContentReader) repositoryPlatformMaterializationCount(ctx context.Context, scopeID string) (int, error) {
	if scopeID == "" {
		return 0, nil
	}
	var count int
	err := cr.db.QueryRowContext(ctx, `
		SELECT count(DISTINCT entity_key)
		FROM fact_records,
		     jsonb_array_elements_text(coalesce(payload->'entity_keys', '[]'::jsonb)) AS entity_key
		WHERE scope_id = $1
		  AND fact_kind = 'reducer_platform_materialization'
		  AND NOT is_tombstone
	`, scopeID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query repository platform materialization count: %w", err)
	}
	return count, nil
}

func (cr *ContentReader) repositoryDependencyCount(ctx context.Context, repoID string) (int, error) {
	var count int
	err := cr.db.QueryRowContext(ctx, `
		SELECT count(DISTINCT target_repo_id)
		FROM resolved_relationships
		WHERE source_repo_id = $1
		  AND relationship_type IN (
			'DEPENDS_ON',
			'USES_MODULE',
			'DEPLOYS_FROM',
			'DISCOVERS_CONFIG_IN',
			'PROVISIONS_DEPENDENCY_FOR',
			'READS_CONFIG_FROM',
			'RUNS_ON'
		  )
	`, repoID).Scan(&count)
	if err != nil {
		return 0, fmt.Errorf("query repository dependency count: %w", err)
	}
	return count, nil
}
