// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query //nolint:dirgate // B3 stayer for #6060: methods on the root ContentReader must live in package query; the shared read model moved to querycontract.

import (
	"context"
	"database/sql"
	"fmt"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// RepositoryReadModelSummary is the Postgres read-model fast path for a
// repository's workload names, deployment-platform materialization count,
// and dependency count -- the same fields repository/context.go otherwise
// derives from per-field Neo4j graph counts in queryRepositoryContextCounts.
//
// It is an alias onto querycontract so the shared ContentStore double can
// name it from outside this package (#6060). See the querycontract
// declaration for the Available fallback obligation.
type RepositoryReadModelSummary = querycontract.RepositoryReadModelSummary

type repositoryReadModelSummaryStore interface {
	RepositoryReadModelSummary(context.Context, string) (RepositoryReadModelSummary, error)
}

func loadRepositoryReadModelSummary(ctx context.Context, content ContentStore, repoID string) *RepositoryReadModelSummary {
	return querycontract.LoadRepositoryReadModelSummary(ctx, content, repoID)
}

// RepositoryReadModelSummary resolves repoID's scope ID, workload names,
// platform materialization count, and dependency count from Postgres,
// returning Available=false (rather than an error) when the repository has
// no scope and no dependencies to summarize. It is the read-model fast path
// repositoryReadModelSummaryStore exposes to loadRepositoryReadModelSummary;
// callers that get a nil summary back fall through to the Neo4j graph-count
// path instead.
func (cr *ContentReader) RepositoryReadModelSummary(ctx context.Context, repoID string) (RepositoryReadModelSummary, error) {
	if cr == nil || cr.db == nil || repoID == "" {
		return RepositoryReadModelSummary{}, nil
	}
	scopeID, err := cr.repositoryScopeID(ctx, repoID)
	if err != nil {
		return RepositoryReadModelSummary{}, err
	}

	workloadNames, err := cr.repositoryWorkloadNames(ctx, scopeID)
	if err != nil {
		return RepositoryReadModelSummary{}, err
	}
	platformCount, err := cr.repositoryPlatformMaterializationCount(ctx, scopeID)
	if err != nil {
		return RepositoryReadModelSummary{}, err
	}
	dependencyCount, err := cr.repositoryDependencyCount(ctx, repoID)
	if err != nil {
		return RepositoryReadModelSummary{}, err
	}
	return RepositoryReadModelSummary{
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

// repositoryWorkloadNamesSQL resolves the scope's workload names from the
// repository fact's payload name, gated on live workload identity (#7384 Q2).
// Workload_identity follow-up keys are `workload:` plus the repository ID, so
// stripping the key prefix would report an id (`repository:r_<hex>`) as a
// name. The join returns the repository fact payload name instead -- never a
// `repository:r_...` id -- and no rows (empty, not a fallback) when the
// repository fact is tombstoned or absent. The workload-identity side keeps
// the fact_records_workload_names_scope_idx predicate; the repository side
// probes a handful of same-scope rows through
// fact_records_scope_generation_idx, so no new index is warranted.
const repositoryWorkloadNamesSQL = `
	SELECT DISTINCT repo.payload->>'name'
	FROM fact_records AS wid
	JOIN fact_records AS repo
	  ON repo.scope_id = $1
	 AND repo.fact_kind = 'repository'
	 AND NOT repo.is_tombstone
	WHERE wid.scope_id = $1
	  AND wid.fact_kind = 'reducer_workload_identity'
	  AND NOT wid.is_tombstone
	  AND repo.payload->>'name' IS NOT NULL
	ORDER BY 1
`

func (cr *ContentReader) repositoryWorkloadNames(ctx context.Context, scopeID string) ([]string, error) {
	if scopeID == "" {
		return nil, nil
	}
	ctx, span := cr.tracer.Start(
		ctx, "postgres.query",
		trace.WithAttributes(
			attribute.String("db.system", "postgresql"),
			attribute.String("db.operation", "repository_workload_names"),
			attribute.String("db.sql.table", "fact_records"),
		),
	)
	defer span.End()

	rows, err := cr.db.QueryContext(ctx, repositoryWorkloadNamesSQL, scopeID)
	if err != nil {
		span.RecordError(err)
		return nil, fmt.Errorf("query repository workload names: %w", err)
	}
	defer func() { _ = rows.Close() }()

	names := make([]string, 0)
	for rows.Next() {
		var name string
		if err := rows.Scan(&name); err != nil {
			span.RecordError(err)
			return nil, fmt.Errorf("scan repository workload name: %w", err)
		}
		names = append(names, name)
	}
	if err := rows.Err(); err != nil {
		span.RecordError(err)
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
