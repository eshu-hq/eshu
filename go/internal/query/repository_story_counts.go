// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "context"

// repositoryStoryStringRowLimit bounds queryRepositoryStoryStringRows's
// per-read row count. 500 mirrors the repo's established defensive-backstop
// tier (code_relationships_nornicdb.go's nornicDBRelationshipRowLimit is also
// 500; its sibling nornicDBRelationshipFetchLimit over-fetches one row past
// that ceiling, to 501, so truncation is detectable -- the same limit+1
// idiom this file's own queryRepositoryStoryStringRows uses below).
// Unlike a genuinely unreachable defensive backstop, real repositories CAN
// hit this bound on the graph-fallback path -- the read-model path in
// repository_read_model_summary.go is unbounded (P1 review follow-up to
// #5764): the story's workload_count and
// platform_count fields (repository.go's "graph_summary" stage log,
// repository_story.go's "%d workload(s) and %d platform signal(s)" narrative,
// and buildRepositoryStory's own narrative sentence) are len() of these
// bounded lists, NOT separate count() queries -- only file_count and
// dependency_count come from the separate exact counts in
// repository_context_counts.go. So a healthy read that lands on this bound
// must be disclosed the same way repositoryInfrastructureEntityLimit is
// (storyRowsTruncatedReason below), or the story silently reports a
// truncated list's length as if it were the exact total.
const repositoryStoryStringRowLimit = 500

// storyRowsTruncatedReason is the shared limitations/partial_reasons value
// getRepositoryStory (repository.go) appends when a HEALTHY
// queryRepositoryStoryStringRows read (workload_names, platform_types, or
// languages) landed past repositoryStoryStringRowLimit and was capped in Go
// (P1 review follow-up to #5764). Mirrors infrastructureTruncatedReason's
// role: a truncated read still returned real rows, it just cannot rule out
// more past the bound, so the story's workload/platform counts (len() of
// these bounded lists, not a separate count() query) must not be presented as
// exact without this disclosure.
const storyRowsTruncatedReason = "story_rows_truncated"

type repositoryStoryGraphSummary struct {
	fileCount       int
	languages       []string
	workloadNames   []string
	platformTypes   []string
	dependencyCount int
	// truncated is true when any of the workload_names/platform_types/
	// languages reads landed past repositoryStoryStringRowLimit and was
	// capped in Go (P1 review follow-up to #5764).
	truncated bool
}

// queryRepositoryStoryGraphSummary uses narrow, repo-anchored reads because
// broad OPTIONAL aggregation is backend-sensitive and can corrupt story truth.
// A bounded graph-read error from any one narrow read aborts the whole summary
// instead of silently narrating from an empty/zero result (#5764): the story
// route builds its headline sentence ("Repository X defines N workload(s):
// ...") directly from these values, so a fabricated empty answer is a wrong
// narrative, not a missing one.
func queryRepositoryStoryGraphSummary(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	contentCoverage *RepositoryContentCoverage,
	readModelSummary *RepositoryReadModelSummary,
) (repositoryStoryGraphSummary, error) {
	fileCount, err := queryRepositoryFileCount(ctx, reader, params, fallback, contentCoverage)
	if err != nil {
		return repositoryStoryGraphSummary{}, err
	}
	languages, languagesTruncated, err := queryRepositoryStoryLanguages(ctx, reader, params, fallback, contentCoverage)
	if err != nil {
		return repositoryStoryGraphSummary{}, err
	}
	workloadNames, workloadNamesTruncated, err := queryRepositoryStoryWorkloadNames(ctx, reader, params, fallback, readModelSummary)
	if err != nil {
		return repositoryStoryGraphSummary{}, err
	}
	platformTypes, platformTypesTruncated, err := queryRepositoryStoryPlatformTypes(ctx, reader, params, fallback, readModelSummary)
	if err != nil {
		return repositoryStoryGraphSummary{}, err
	}
	dependencyCount, err := queryRepositoryDependencyCount(ctx, reader, params, fallback, readModelSummary)
	if err != nil {
		return repositoryStoryGraphSummary{}, err
	}
	return repositoryStoryGraphSummary{
		fileCount:       fileCount,
		languages:       languages,
		workloadNames:   workloadNames,
		platformTypes:   platformTypes,
		dependencyCount: dependencyCount,
		truncated:       languagesTruncated || workloadNamesTruncated || platformTypesTruncated,
	}, nil
}

func queryRepositoryStoryWorkloadNames(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	readModelSummary *RepositoryReadModelSummary,
) ([]string, bool, error) {
	if readModelSummary != nil && readModelSummary.Available {
		return readModelSummary.WorkloadNames, false, nil
	}
	// RETURN DISTINCT here (P1 review follow-up to #5764): two Workload nodes
	// can share a name, so this Cypher can return duplicate raw rows -- the
	// same starvation risk queryRepositoryStoryPlatformTypes's doc comment
	// below describes. Before this fix, queryRepositoryStoryStringRows's own
	// seen-map dedup ran AFTER the LIMIT/truncation cap, so a repository with
	// many duplicate-name Workload rows (alphabetically first) could starve a
	// different, real workload name out of the story entirely once the LIMIT
	// landed mid-duplicate-run -- a WRONG story, not merely a truncated one.
	// DISTINCT is on w.name, a plain property projection (not a function
	// call), matching the already-safe RETURN DISTINCT p.type shape below.
	return queryRepositoryStoryStringRows(ctx, reader, params, "workload_names", "workload_name", `
			MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
			RETURN DISTINCT w.name AS workload_name
			ORDER BY workload_name
		`, fallback)
}

func queryRepositoryStoryPlatformTypes(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	readModelSummary *RepositoryReadModelSummary,
) ([]string, bool, error) {
	if readModelSummary != nil && readModelSummary.Available {
		return readModelSummary.PlatformTypes, false, nil
	}
	// RETURN DISTINCT here: this Cypher emits one row per WorkloadInstance
	// path (two extra hops past the anchor: INSTANCE_OF, RUNS_ON), not one
	// row per platform. Without DISTINCT, repositoryStoryStringRowLimit's
	// LIMIT truncates raw path rows, and a repository with many instances of
	// one platform (alphabetically first) can starve a different, real
	// platform out of the story entirely -- a WRONG story, not merely a
	// truncated one. DISTINCT is on p.type, a plain property projection (not
	// a function call), matching the already-safe RETURN DISTINCT repo.id
	// shape in code_import_dependencies_queries.go; see
	// docs/public/reference/nornicdb-pitfalls.md for the function-call-
	// projection pitfall this avoids.
	return queryRepositoryStoryStringRows(ctx, reader, params, "platform_types", "platform_type", `
			MATCH (r:Repository {id: $repo_id})-[:DEFINES]->(w:Workload)
			MATCH (w)<-[:INSTANCE_OF]-(i:WorkloadInstance)
			MATCH (i)-[:RUNS_ON]->(p:Platform)
			RETURN DISTINCT p.type AS platform_type
			ORDER BY platform_type
		`, fallback)
}

func queryRepositoryStoryLanguages(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallback map[string]any,
	contentCoverage *RepositoryContentCoverage,
) ([]string, bool, error) {
	if languages, ok := repositoryLanguageNamesFromCoverage(contentCoverage); ok {
		return languages, false, nil
	}
	return queryRepositoryStoryStringRows(ctx, reader, params, "languages", "language", `
		MATCH (r:Repository {id: $repo_id})-[:REPO_CONTAINS]->(f:File)
		WHERE f.language IS NOT NULL
		RETURN f.language AS language, count(DISTINCT f) AS file_count
		ORDER BY file_count DESC, language
	`, fallback)
}

// queryRepositoryStoryStringRows falls back to the legacy base row only when
// the narrow read genuinely returns no rows (err == nil, len(rows) == 0). A
// non-nil err from the read -- including the bounded ErrGraphReadDeadline /
// ErrGraphUnavailable sentinels -- is returned to the caller instead of being
// folded into the same fallback path (#5764): before this fix a deadlined or
// unavailable graph read was indistinguishable from a genuine empty list, both
// silently answering the base row's (missing) key as nil via StringSliceVal.
//
// The read requests repositoryStoryStringRowLimit+1 rows (P1 review follow-up)
// so the returned bool can report EXACT truncation (len(rows) >
// repositoryStoryStringRowLimit) instead of the ambiguous len(rows) == limit
// check, which cannot distinguish "exactly limit rows exist" from "more rows
// exist past the bound." Rows past the limit are capped in Go before dedup.
// This bound is reachable for real repositories -- unlike the legacy
// defensive-backstop framing this constant once carried, the story's
// workload_count/platform_count fields are len() of these bounded lists, not
// separate count() queries (repository_story_counts.go's
// repositoryStoryStringRowLimit doc comment) -- so a caller that discards the
// truncated bool silently reports a truncated list's length as exact.
//
// Every caller's Cypher now projects distinct values: platform_types and
// workload_names use RETURN DISTINCT (workload_names: two Workload nodes can
// share a name, so its Cypher can return duplicate raw rows without it) and
// languages is implicitly grouped by its aggregate projection. The LIMIT
// therefore bounds values that are already distinct at the Cypher level; this
// function's own seen-map dedup below is a defensive backstop against a
// backend that over-returns rows for a DISTINCT projection, not the primary
// distinct-values mechanism.
func queryRepositoryStoryStringRows(
	ctx context.Context,
	reader GraphQuery,
	params map[string]any,
	fallbackKey string,
	valueKey string,
	cypher string,
	fallback map[string]any,
) ([]string, bool, error) {
	queryParams := copyMap(params)
	queryParams["limit"] = repositoryStoryStringRowLimit + 1
	rows, err := reader.Run(ctx, cypher+"\n\t\t\tLIMIT $limit", queryParams)
	if err != nil {
		return nil, false, err
	}
	if len(rows) == 0 {
		return StringSliceVal(fallback, fallbackKey), false, nil
	}
	truncated := len(rows) > repositoryStoryStringRowLimit
	if truncated {
		rows = rows[:repositoryStoryStringRowLimit]
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
	return values, truncated, nil
}
