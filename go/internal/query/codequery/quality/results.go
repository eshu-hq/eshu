// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package quality

import (
	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// Rows shapes scan rows into inspection results with source handles.
func Rows(rows []map[string]any) []map[string]any {
	results := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		filePath := querycontract.StringVal(row, "file_path")
		repoID := querycontract.StringVal(row, "repo_id")
		startLine := querycontract.IntVal(row, "start_line")
		endLine := querycontract.IntVal(row, "end_line")
		results = append(results, map[string]any{
			"entity_id":      querycontract.StringVal(row, "entity_id"),
			"name":           querycontract.StringVal(row, "name"),
			"labels":         querycontract.StringSliceVal(row, "labels"),
			"file_path":      filePath,
			"repo_id":        repoID,
			"repo_name":      querycontract.StringVal(row, "repo_name"),
			"language":       querycontract.StringVal(row, "language"),
			"start_line":     startLine,
			"end_line":       endLine,
			"line_count":     querycontract.IntVal(row, "line_count"),
			"argument_count": querycontract.IntVal(row, "argument_count"),
			"complexity":     querycontract.IntVal(row, "complexity"),
			"source_handle":  sourceHandle(repoID, filePath, startLine, endLine),
		})
	}
	return results
}

// TrimResults bounds results to the request limit, reporting whether
// rows were truncated.
func TrimResults(results []map[string]any, limit int) ([]map[string]any, bool) {
	if len(results) <= limit {
		return results, false
	}
	return results[:limit], true
}

// Thresholds renders the effective metric floors of one request.
func Thresholds(req Request) map[string]any {
	return map[string]any{
		"min_complexity": req.MinComplexity,
		"min_lines":      req.MinLines,
		"min_arguments":  req.MinArguments,
	}
}

// NextCalls suggests one source read per result.
func NextCalls(results []map[string]any) []map[string]any {
	next := make([]map[string]any, 0, len(results))
	for _, result := range results {
		next = append(next, map[string]any{
			"tool":          "get_file_lines",
			"repo_id":       result["repo_id"],
			"relative_path": result["file_path"],
			"start_line":    result["start_line"],
			"end_line":      result["end_line"],
		})
	}
	return next
}

// sourceHandle shapes the source locator for one result. It moved with
// the only caller that named it.
func sourceHandle(repoID, path string, startLine, endLine int) map[string]any {
	return map[string]any{
		"repo_id":    repoID,
		"file_path":  path,
		"start_line": startLine,
		"end_line":   endLine,
	}
}
