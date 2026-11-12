// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"fmt"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// APISurfaceEndpointLimit bounds the API-surface endpoint detail
// rows one read carries. Exported for #6060 so root performance tests can
// name it from outside this package.
const APISurfaceEndpointLimit = 50

const repositoryAPISurfaceEndpointLimit = APISurfaceEndpointLimit

// queryRepoAPISurface reads API endpoint graph truth for repository context.
func QueryRepoAPISurface(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) (map[string]any, bool) {
	countRows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)
		RETURN count(endpoint) AS endpoint_count
	`, params)
	if err != nil {
		return nil, true
	}
	endpointCount := 0
	if len(countRows) > 0 {
		endpointCount = querycontract.IntVal(countRows[0], "endpoint_count")
	}
	if endpointCount == 0 {
		return nil, false
	}
	detailParams := querycontract.CopyMap(params)
	detailParams["limit"] = repositoryAPISurfaceEndpointLimit
	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)
		RETURN endpoint.id AS endpoint_id,
		       endpoint.path AS path,
		       endpoint.methods AS methods,
		       endpoint.operation_ids AS operation_ids,
		       endpoint.source_kinds AS source_kinds,
		       endpoint.source_paths AS source_paths,
		       endpoint.spec_versions AS spec_versions,
		       endpoint.api_versions AS api_versions,
		       endpoint.evidence_source AS evidence_source,
		       endpoint.workload_id AS workload_id,
		       endpoint.workload_name AS workload_name
		ORDER BY path, endpoint_id
		LIMIT $limit
	`, detailParams)
	if err != nil {
		return nil, true
	}
	if len(rows) == 0 {
		return nil, false
	}
	return buildGraphAPISurface(rows, endpointCount), false
}

// QueryRepoAPISurfacePaged reads one API endpoint page for a repository at
// the Cypher read (#10477): the same count query reports the true total while
// the detail read carries SKIP $offset LIMIT $limit beside ORDER BY path,
// endpoint_id (the repository-list pagination shape proven on the pinned
// backend), so page N never re-reads or Go-slices an earlier page. The
// unpaged QueryRepoAPISurface above keeps its own query text byte-identical
// (cassette and snapshot stability); this variant shares only the row mapping.
// A non-positive limit falls back to the endpoint cap and a negative offset
// to zero; callers that must answer 400 on those validate before calling.
func QueryRepoAPISurfacePaged(ctx context.Context, reader querycontract.GraphQuery, params map[string]any, limit, offset int) (map[string]any, error) {
	if limit <= 0 {
		limit = repositoryAPISurfaceEndpointLimit
	}
	if offset < 0 {
		offset = 0
	}
	countRows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)
		RETURN count(endpoint) AS endpoint_count
	`, params)
	if err != nil {
		return nil, fmt.Errorf("query repository API surface endpoint count: %w", err)
	}
	endpointCount := 0
	if len(countRows) > 0 {
		endpointCount = querycontract.IntVal(countRows[0], "endpoint_count")
	}
	detailParams := querycontract.CopyMap(params)
	detailParams["limit"] = limit
	detailParams["offset"] = offset
	rows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)
		RETURN endpoint.id AS endpoint_id,
		       endpoint.path AS path,
		       endpoint.methods AS methods,
		       endpoint.operation_ids AS operation_ids,
		       endpoint.source_kinds AS source_kinds,
		       endpoint.source_paths AS source_paths,
		       endpoint.spec_versions AS spec_versions,
		       endpoint.api_versions AS api_versions,
		       endpoint.evidence_source AS evidence_source,
		       endpoint.workload_id AS workload_id,
		       endpoint.workload_name AS workload_name
		ORDER BY path, endpoint_id
		SKIP $offset
		LIMIT $limit
	`, detailParams)
	if err != nil {
		return nil, fmt.Errorf("query repository API surface endpoint page: %w", err)
	}
	// An empty result still builds the surface (endpoint_count 0, or an
	// offset past the end) so the paging contract holds either way: the
	// caller reads the true total off every page. Only a read error returns
	// nil.
	surface := buildGraphAPISurface(rows, endpointCount)
	// The shared mapper stamps the legacy endpoint cap; a page is bounded by
	// the caller's limit instead, and detail_truncated means rows exist past
	// this page (not merely past the preview cap).
	surface["detail_limit"] = limit
	pageTruncated := offset+len(rows) < endpointCount
	surface["detail_truncated"] = pageTruncated
	surface["offset"] = offset
	if pageTruncated {
		surface["next_offset"] = offset + len(rows)
	}
	return surface, nil
}

// buildGraphAPISurface converts Endpoint nodes into the API surface contract.
func buildGraphAPISurface(rows []map[string]any, endpointCount int) map[string]any {
	endpoints := make([]map[string]any, 0, len(rows))
	var (
		methodCount      int
		operationIDCount int
		sourcePaths      []string
		specVersions     []string
		apiVersions      []string
		frameworks       []string
	)
	for _, row := range rows {
		methods := querycontract.LowerStrings(querycontract.StringSliceVal(row, "methods"))
		operationIDs := querycontract.UniqueSortedStrings(querycontract.StringSliceVal(row, "operation_ids"))
		sourceKinds := querycontract.UniqueSortedStrings(querycontract.StringSliceVal(row, "source_kinds"))
		rowSourcePaths := querycontract.UniqueSortedStrings(querycontract.StringSliceVal(row, "source_paths"))
		methodCount += len(methods)
		operationIDCount += len(operationIDs)
		sourcePaths = append(sourcePaths, rowSourcePaths...)
		specVersions = append(specVersions, querycontract.StringSliceVal(row, "spec_versions")...)
		apiVersions = append(apiVersions, querycontract.StringSliceVal(row, "api_versions")...)
		frameworks = append(frameworks, frameworkNamesFromSourceKinds(sourceKinds)...)

		endpoint := map[string]any{
			"id":              querycontract.StringVal(row, "endpoint_id"),
			"path":            querycontract.StringVal(row, "path"),
			"methods":         methods,
			"operation_ids":   operationIDs,
			"source":          "graph",
			"source_kinds":    sourceKinds,
			"source_paths":    rowSourcePaths,
			"evidence_source": querycontract.StringVal(row, "evidence_source"),
		}
		if workloadID := querycontract.StringVal(row, "workload_id"); workloadID != "" {
			endpoint["workload_id"] = workloadID
		}
		if workloadName := querycontract.StringVal(row, "workload_name"); workloadName != "" {
			endpoint["workload_name"] = workloadName
		}
		endpoints = append(endpoints, endpoint)
	}

	result := map[string]any{
		"truth_basis":        "graph",
		"endpoint_count":     endpointCount,
		"method_count":       methodCount,
		"operation_id_count": operationIDCount,
		"endpoints":          endpoints,
		"source_paths":       querycontract.UniqueSortedStrings(sourcePaths),
		"spec_paths":         querycontract.UniqueSortedStrings(sourcePaths),
		"spec_versions":      querycontract.UniqueSortedStrings(specVersions),
		"api_versions":       querycontract.UniqueSortedStrings(apiVersions),
		"detail_limit":       repositoryAPISurfaceEndpointLimit,
		"detail_truncated":   endpointCount > len(endpoints),
	}
	if frameworks = querycontract.UniqueSortedStrings(frameworks); len(frameworks) > 0 {
		result["frameworks"] = frameworks
		result["framework_route_count"] = countFrameworkEndpointRows(rows)
	}
	return result
}

// frameworkNamesFromSourceKinds extracts parser framework names from endpoint
// source kinds such as "framework:fastapi".
func frameworkNamesFromSourceKinds(sourceKinds []string) []string {
	frameworks := make([]string, 0, len(sourceKinds))
	for _, kind := range sourceKinds {
		name, ok := strings.CutPrefix(kind, "framework:")
		if ok && strings.TrimSpace(name) != "" {
			frameworks = append(frameworks, name)
		}
	}
	return frameworks
}

func countFrameworkEndpointRows(rows []map[string]any) int {
	count := 0
	for _, row := range rows {
		if len(frameworkNamesFromSourceKinds(querycontract.StringSliceVal(row, "source_kinds"))) > 0 {
			count++
		}
	}
	return count
}

// queryRepoAPISurface keeps the in-package spelling after the #6060 export;
// root stayers name QueryRepoAPISurface.
func queryRepoAPISurface(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) (map[string]any, bool) {
	return QueryRepoAPISurface(ctx, reader, params)
}
