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
func queryRepositoryContextCounts(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	contentCoverage *RepositoryContentCoverage,
	readModelSummary *repositoryReadModelSummary,
) repositoryContextCounts {
	return repositoryContextCounts{
		fileCount:       queryRepositoryFileCount(ctx, reader, params, fallback, contentCoverage),
		workloadCount:   queryRepositoryWorkloadCount(ctx, reader, params, fallback, readModelSummary),
		platformCount:   queryRepositoryPlatformCount(ctx, reader, params, fallback, readModelSummary),
		dependencyCount: queryRepositoryDependencyCount(ctx, reader, params, fallback, readModelSummary),
	}
}

func queryRepositoryWorkloadCount(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	readModelSummary *repositoryReadModelSummary,
) int {
	if readModelSummary != nil && readModelSummary.Available {
		return len(readModelSummary.WorkloadNames)
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
	readModelSummary *repositoryReadModelSummary,
) int {
	if readModelSummary != nil && readModelSummary.Available {
		return readModelSummary.PlatformCount
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
	readModelSummary *repositoryReadModelSummary,
) int {
	if readModelSummary != nil && readModelSummary.Available {
		return readModelSummary.DependencyCount
	}
	return queryRepositoryContextCount(ctx, reader, params, "dependency_count", `
			MATCH (r:Repository {id: $repo_id})-[rel:DEPENDS_ON|USES_MODULE|DEPLOYS_FROM|DISCOVERS_CONFIG_IN|PROVISIONS_DEPENDENCY_FOR|READS_CONFIG_FROM|RUNS_ON]->(dep:Repository)
			RETURN count(DISTINCT dep) AS count
		`, fallback)
}

func queryRepositoryFileCount(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	contentCoverage *RepositoryContentCoverage,
) int {
	if contentCoverage != nil && contentCoverage.Available {
		return contentCoverage.FileCount
	}
	return queryRepositoryContextCount(ctx, reader, params, "file_count", `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
		RETURN count(DISTINCT f) AS count
	`, fallback)
}

// queryRepositoryContextCount falls back to the legacy base row if a narrow
// count query fails or returns no rows, preserving degraded read behavior.
func queryRepositoryContextCount(ctx context.Context, reader GraphQuery, params map[string]any, fallbackKey string, cypher string, fallback map[string]any) int {
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return IntVal(fallback, fallbackKey)
	}
	return IntVal(rows[0], "count")
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
