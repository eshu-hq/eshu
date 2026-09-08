// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package repository

import (
	"context"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// RepositoryAPISurfaceEndpointLimit bounds the API-surface endpoint detail
// rows one read carries. Exported for #6060 so root performance tests can
// name it from outside this package.
const RepositoryAPISurfaceEndpointLimit = 50

const repositoryAPISurfaceEndpointLimit = RepositoryAPISurfaceEndpointLimit

// queryRepoAPISurface reads API endpoint graph truth for repository context.
func QueryRepoAPISurface(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) map[string]any {
	countRows, err := reader.Run(ctx, `
		MATCH (r:Repository {id: $repo_id})-[:EXPOSES_ENDPOINT]->(endpoint:Endpoint)
		RETURN count(endpoint) AS endpoint_count
	`, params)
	if err != nil {
		return nil
	}
	endpointCount := 0
	if len(countRows) > 0 {
		endpointCount = querycontract.IntVal(countRows[0], "endpoint_count")
	}
	if endpointCount == 0 {
		return nil
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
	if err != nil || len(rows) == 0 {
		return nil
	}
	return buildGraphAPISurface(rows, endpointCount)
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
func queryRepoAPISurface(ctx context.Context, reader querycontract.GraphQuery, params map[string]any) map[string]any {
	return QueryRepoAPISurface(ctx, reader, params)
}
