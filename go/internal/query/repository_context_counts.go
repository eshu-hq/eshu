// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

type repositoryContextCounts struct {
	fileCount       int
	workloadCount   int
	platformCount   int
	dependencyCount int
}

// queryRepositoryContextCounts uses scalar count queries to avoid relying on a
// broad OPTIONAL MATCH aggregation for graph-backend-sensitive repo summaries.
// A bounded graph-read error from any one count aborts the whole summary
// instead of silently returning a fabricated count (#5764): these are
// top-level scalars, and a wrong number is indistinguishable from a real one,
// which is worse than the caller seeing a 503/504.
func queryRepositoryContextCounts(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	contentCoverage *RepositoryContentCoverage,
	readModelSummary *RepositoryReadModelSummary,
) (repositoryContextCounts, error) {
	fileCount, err := queryRepositoryFileCount(ctx, reader, params, fallback, contentCoverage)
	if err != nil {
		return repositoryContextCounts{}, err
	}
	workloadCount, err := queryRepositoryWorkloadCount(ctx, reader, params, fallback, readModelSummary)
	if err != nil {
		return repositoryContextCounts{}, err
	}
	platformCount, err := queryRepositoryPlatformCount(ctx, reader, params, fallback, readModelSummary)
	if err != nil {
		return repositoryContextCounts{}, err
	}
	dependencyCount, err := queryRepositoryDependencyCount(ctx, reader, params, fallback, readModelSummary)
	if err != nil {
		return repositoryContextCounts{}, err
	}
	return repositoryContextCounts{
		fileCount:       fileCount,
		workloadCount:   workloadCount,
		platformCount:   platformCount,
		dependencyCount: dependencyCount,
	}, nil
}

func queryRepositoryWorkloadCount(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	readModelSummary *RepositoryReadModelSummary,
) (int, error) {
	if readModelSummary != nil && readModelSummary.Available {
		return len(readModelSummary.WorkloadNames), nil
	}
	return queryRepositoryContextCount(ctx, reader, params, "workload_count", `
			MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
			RETURN count(DISTINCT w) AS count
		`, fallback)
}

func queryRepositoryPlatformCount(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	readModelSummary *RepositoryReadModelSummary,
) (int, error) {
	if readModelSummary != nil && readModelSummary.Available {
		return readModelSummary.PlatformCount, nil
	}
	return queryRepositoryContextCount(ctx, reader, params, "platform_count", `
			MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
			MATCH (w)<-[:INSTANCE_OF]-(i:WorkloadInstance)
			MATCH (i)-[:RUNS_ON]->(p:Platform)
			RETURN count(DISTINCT p) AS count
		`, fallback)
}

func queryRepositoryDependencyCount(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	readModelSummary *RepositoryReadModelSummary,
) (int, error) {
	if readModelSummary != nil && readModelSummary.Available {
		return readModelSummary.DependencyCount, nil
	}
	return queryRepositoryContextCount(ctx, reader, params, "dependency_count", `
			MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON|CORRELATES_DEPLOYABLE_UNIT]->(dep:Repository)
			RETURN count(DISTINCT dep) AS count
		`, fallback)
}

func queryRepositoryFileCount(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	contentCoverage *RepositoryContentCoverage,
) (int, error) {
	if contentCoverage != nil && contentCoverage.Available {
		return contentCoverage.FileCount, nil
	}
	return queryRepositoryContextCount(ctx, reader, params, "file_count", `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
		RETURN count(DISTINCT f) AS count
	`, fallback)
}

// queryRepositoryContextCount falls back to the legacy base row only when the
// narrow count query genuinely returns no rows (err == nil, len(rows) == 0).
// A non-nil err from the read -- including the bounded ErrGraphReadDeadline /
// ErrGraphUnavailable sentinels -- is returned to the caller instead of being
// folded into the same fallback path (#5764): before this fix a deadlined or
// unavailable graph read was indistinguishable from a genuine zero count, both
// silently answering the base row's (missing) count key as 0 via IntVal.
func queryRepositoryContextCount(ctx context.Context, reader GraphQuery, params map[string]any, fallbackKey string, cypher string, fallback map[string]any) (int, error) {
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil {
		return 0, err
	}
	if len(rows) == 0 {
		return IntVal(fallback, fallbackKey), nil
	}
	return IntVal(rows[0], "count"), nil
}

func loadRepositoryContentCoverage(ctx context.Context, content ContentStore, repoID string) *RepositoryContentCoverage {
	if content == nil || repoID == "" {
		return nil
	}
	coverage, err := content.RepositoryCoverage(ctx, repoID)
	if err != nil || !coverage.Available {
		return nil
	}
	return &coverage
}

func repositoryLanguageDistributionFromCoverage(contentCoverage *RepositoryContentCoverage) ([]map[string]any, bool) {
	if contentCoverage == nil || !contentCoverage.Available {
		return nil, false
	}
	languages := make([]map[string]any, 0, len(contentCoverage.Languages))
	for _, language := range contentCoverage.Languages {
		if language.Language == "" {
			continue
		}
		languages = append(languages, map[string]any{
			"language":   language.Language,
			"file_count": language.FileCount,
		})
	}
	return languages, true
}

func repositoryLanguageNamesFromCoverage(contentCoverage *RepositoryContentCoverage) ([]string, bool) {
	if contentCoverage == nil || !contentCoverage.Available {
		return nil, false
	}
	languages := make([]string, 0, len(contentCoverage.Languages))
	for _, language := range contentCoverage.Languages {
		if language.Language == "" {
			continue
		}
		languages = append(languages, language.Language)
	}
	return languages, true
}
