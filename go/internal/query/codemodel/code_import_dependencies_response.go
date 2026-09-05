// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

func ImportDependencyResponse(req ImportDependencyRequest, rows []map[string]any) map[string]any {
	limit := req.normalizedLimit()
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := importDependencyResults(req, rows)
	response := map[string]any{
		"query_type":     req.EffectiveQueryType(),
		"scope":          importDependencyScope(req),
		"limit":          limit,
		"offset":         req.Offset,
		"truncated":      truncated,
		"next_offset":    nextImportDependencyOffset(req.Offset, len(results), truncated),
		"source_backend": "graph",
		"coverage":       importDependencyCoverage(req, truncated),
	}
	switch req.EffectiveQueryType() {
	case "file_import_cycles":
		response["cycles"] = results
		response["count"] = len(results)
	case "cross_module_calls":
		response["cross_module_calls"] = results
		response["count"] = len(results)
	case "package_imports":
		modules := ImportDependencyUniqueModules(rows)
		response["modules"] = modules
		response["count"] = len(modules)
	default:
		response["dependencies"] = results
		response["count"] = len(results)
	}
	return response
}

// ImportDependencyUniqueModules de-duplicates logical module rows.
func ImportDependencyUniqueModules(rows []map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(rows))
	modules := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		moduleName := querycontract.StringVal(row, "target_module")
		if moduleName == "" {
			continue
		}
		repoID := querycontract.StringVal(row, "repo_id")
		language := querycontract.StringVal(row, "language")
		logicalKey := strings.Join([]string{repoID, moduleName, language}, "\x00")
		if _, ok := seen[logicalKey]; ok {
			continue
		}
		seen[logicalKey] = struct{}{}
		modules = append(modules, map[string]any{
			"repo_id":        repoID,
			"module":         moduleName,
			"language":       language,
			"source_backend": "graph",
		})
	}
	return modules
}

func importDependencyResults(req ImportDependencyRequest, rows []map[string]any) []map[string]any {
	results := make([]map[string]any, 0, len(rows))
	for index, row := range rows {
		item := cloneQueryAnyMap(row)
		item["rank"] = index + 1
		item["source_backend"] = "graph"
		item["source_handle"] = importDependencySourceHandle(row)
		if req.EffectiveQueryType() == "file_import_cycles" {
			item["cycle_length"] = 2
			item["cycle_path"] = []string{querycontract.StringVal(row, "source_file"), querycontract.StringVal(row, "target_file"), querycontract.StringVal(row, "source_file")}
			item["cycle_edges"] = importDependencyCycleEdges(row)
		}
		if req.EffectiveQueryType() == "cross_module_calls" {
			item["relationship_type"] = "CALLS"
			if sourceID := querycontract.StringVal(row, "source_id"); sourceID != "" {
				item["source_entity_handle"] = "entity:" + sourceID
			}
			if targetID := querycontract.StringVal(row, "target_id"); targetID != "" {
				item["target_entity_handle"] = "entity:" + targetID
			}
		} else {
			item["relationship_type"] = "IMPORTS"
			item["dependency_handle"] = importDependencyModuleHandle(row)
		}
		results = append(results, item)
	}
	return results
}

func importDependencyCycleEdges(row map[string]any) []map[string]any {
	return []map[string]any{
		importDependencyCycleEdge(row, "source_file", "target_file", "source_module", "target_module", "source_line_number"),
		importDependencyCycleEdge(row, "target_file", "source_file", "target_module", "source_module", "back_edge_line_number"),
	}
}

func importDependencyCycleEdge(row map[string]any, sourceFileKey, targetFileKey, sourceModuleKey, targetModuleKey, lineKey string) map[string]any {
	edge := map[string]any{
		"relationship_type": "IMPORTS",
		"source_file":       querycontract.StringVal(row, sourceFileKey),
		"target_file":       querycontract.StringVal(row, targetFileKey),
		"source_module":     querycontract.StringVal(row, sourceModuleKey),
		"target_module":     querycontract.StringVal(row, targetModuleKey),
	}
	if lineNumber := querycontract.IntVal(row, lineKey); lineNumber > 0 {
		edge["line_number"] = lineNumber
	}
	return edge
}

func importDependencySourceHandle(row map[string]any) map[string]any {
	return map[string]any{
		"repo_id":       querycontract.StringVal(row, "repo_id"),
		"file_path":     querycontract.StringVal(row, "source_file"),
		"relative_path": querycontract.StringVal(row, "source_file"),
		"content_tool":  "get_file_content",
	}
}

func importDependencyModuleHandle(row map[string]any) map[string]any {
	return map[string]any{
		"repo_id":       querycontract.StringVal(row, "repo_id"),
		"target_module": querycontract.StringVal(row, "target_module"),
		"tool":          "investigate_import_dependencies",
	}
}

func importDependencyScope(req ImportDependencyRequest) map[string]any {
	return map[string]any{
		"repo_id":       strings.TrimSpace(req.RepoID),
		"language":      req.NormalizedLanguage(),
		"source_file":   strings.TrimSpace(req.SourceFile),
		"target_file":   strings.TrimSpace(req.TargetFile),
		"source_module": strings.TrimSpace(req.SourceModule),
		"target_module": strings.TrimSpace(req.TargetModule),
		"limit":         req.normalizedLimit(),
		"offset":        req.Offset,
	}
}

func importDependencyCoverage(req ImportDependencyRequest, truncated bool) map[string]any {
	queryShape := "repo_file_imports"
	if req.EffectiveQueryType() == "file_import_cycles" {
		queryShape = "python_file_import_two_cycle"
	}
	if req.EffectiveQueryType() == "cross_module_calls" {
		queryShape = "module_anchored_call_edges"
	}
	return map[string]any{
		"query_shape":          queryShape,
		"relationship_types":   importDependencyRelationshipTypes(req),
		"truncated":            truncated,
		"bounded":              true,
		"candidate_scan_limit": querycontract.ImportDependencyInternalScanLimit,
	}
}

func importDependencyRelationshipTypes(req ImportDependencyRequest) []string {
	if req.EffectiveQueryType() == "cross_module_calls" {
		return []string{"CALLS"}
	}
	return []string{"IMPORTS"}
}

func nextImportDependencyOffset(offset, count int, truncated bool) any {
	if !truncated {
		return nil
	}
	return offset + count
}
