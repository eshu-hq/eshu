package query

import "context"

type repositoryStoryGraphSummary struct {
	fileCount       int
	languages       []string
	workloadNames   []string
	platformTypes   []string
	dependencyCount int
}

// queryRepositoryStoryGraphSummary uses narrow, repo-anchored reads because
// broad OPTIONAL aggregation is backend-sensitive and can corrupt story truth.
func queryRepositoryStoryGraphSummary(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	contentCoverage *RepositoryContentCoverage,
	readModelSummary *repositoryReadModelSummary,
) repositoryStoryGraphSummary {
	return repositoryStoryGraphSummary{
		fileCount:       queryRepositoryFileCount(ctx, reader, params, fallback, contentCoverage),
		languages:       queryRepositoryStoryLanguages(ctx, reader, params, fallback, contentCoverage),
		workloadNames:   queryRepositoryStoryWorkloadNames(ctx, reader, params, fallback, readModelSummary),
		platformTypes:   queryRepositoryStoryPlatformTypes(ctx, reader, params, fallback, readModelSummary),
		dependencyCount: queryRepositoryDependencyCount(ctx, reader, params, fallback, readModelSummary),
	}
}

func queryRepositoryStoryWorkloadNames(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	readModelSummary *repositoryReadModelSummary,
) []string {
	if readModelSummary != nil && readModelSummary.Available {
		return readModelSummary.WorkloadNames
	}
	return queryRepositoryStoryStringRows(ctx, reader, params, "workload_names", "workload_name", `
			MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
			RETURN w.name AS workload_name
			ORDER BY workload_name
		`, fallback)
}

func queryRepositoryStoryPlatformTypes(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	readModelSummary *repositoryReadModelSummary,
) []string {
	if readModelSummary != nil && readModelSummary.Available {
		return readModelSummary.PlatformTypes
	}
	return queryRepositoryStoryStringRows(ctx, reader, params, "platform_types", "platform_type", `
			MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
			MATCH (w)<-[:INSTANCE_OF]-(i:WorkloadInstance)
			MATCH (i)-[:RUNS_ON]->(p:Platform)
			RETURN p.type AS platform_type
			ORDER BY platform_type
		`, fallback)
}

func queryRepositoryStoryLanguages(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	contentCoverage *RepositoryContentCoverage,
) []string {
	if languages, ok := repositoryLanguageNamesFromCoverage(contentCoverage); ok {
		return languages
	}
	return queryRepositoryStoryStringRows(ctx, reader, params, "languages", "language", `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
		WHERE f.language IS NOT NULL
		RETURN f.language AS language, count(DISTINCT f) AS file_count
		ORDER BY file_count DESC
	`, fallback)
}

func queryRepositoryStoryStringRows(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallbackKey string,
	valueKey string,
	cypher string,
	fallback map[string]any,
) []string {
	rows, err := reader.Run(ctx, cypher, params)
	if err != nil || len(rows) == 0 {
		return StringSliceVal(fallback, fallbackKey)
	}
	values := make([]string, 0, len(rows))
	seen := make(map[string]struct{}, len(rows))
	for _, row := range rows {
		value := StringVal(row, valueKey)
		if value == "" {
			continue
		}
		if _, ok := seen[value]; ok {
			continue
		}
		seen[value] = struct{}{}
		values = append(values, value)
	}
	return values
}
