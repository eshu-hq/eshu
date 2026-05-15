package query

import "strings"

func importDependencyResponse(req importDependencyRequest, rows []map[string]any) map[string]any {
	limit := req.normalizedLimit()
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	results := importDependencyResults(req, rows)
	response := map[string]any{
		"query_type":     req.queryType(),
		"scope":          importDependencyScope(req),
		"results":        results,
		"matches":        results,
		"count":          len(results),
		"limit":          limit,
		"offset":         req.Offset,
		"truncated":      truncated,
		"next_offset":    nextImportDependencyOffset(req.Offset, len(results), truncated),
		"source_backend": "graph",
		"coverage":       importDependencyCoverage(req, truncated),
	}
	switch req.queryType() {
	case "file_import_cycles":
		response["cycles"] = results
	case "cross_module_calls":
		response["cross_module_calls"] = results
	case "package_imports":
		response["modules"] = importDependencyUniqueModules(rows)
	default:
		response["dependencies"] = results
	}
	return response
}

func importDependencyUniqueModules(rows []map[string]any) []map[string]any {
	seen := make(map[string]struct{}, len(rows))
	modules := make([]map[string]any, 0, len(rows))
	for _, row := range rows {
		moduleName := StringVal(row, "target_module")
		if moduleName == "" {
			continue
		}
		if _, ok := seen[moduleName]; ok {
			continue
		}
		seen[moduleName] = struct{}{}
		modules = append(modules, map[string]any{
			"repo_id":        StringVal(row, "repo_id"),
			"module":         moduleName,
			"language":       StringVal(row, "language"),
			"source_backend": "graph",
		})
	}
	return modules
}

func importDependencyResults(req importDependencyRequest, rows []map[string]any) []map[string]any {
	results := make([]map[string]any, 0, len(rows))
	for index, row := range rows {
		item := cloneQueryAnyMap(row)
		item["rank"] = index + 1
		item["source_backend"] = "graph"
		item["source_handle"] = importDependencySourceHandle(row)
		if req.queryType() == "file_import_cycles" {
			item["cycle_length"] = 2
			item["cycle_path"] = []string{StringVal(row, "source_file"), StringVal(row, "target_file"), StringVal(row, "source_file")}
		}
		if req.queryType() == "cross_module_calls" {
			item["relationship_type"] = "CALLS"
			if sourceID := StringVal(row, "source_id"); sourceID != "" {
				item["source_entity_handle"] = "entity:" + sourceID
			}
			if targetID := StringVal(row, "target_id"); targetID != "" {
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

func importDependencySourceHandle(row map[string]any) map[string]any {
	return map[string]any{
		"repo_id":       StringVal(row, "repo_id"),
		"file_path":     StringVal(row, "source_file"),
		"relative_path": StringVal(row, "source_file"),
		"content_tool":  "get_file_content",
	}
}

func importDependencyModuleHandle(row map[string]any) map[string]any {
	return map[string]any{
		"repo_id":       StringVal(row, "repo_id"),
		"target_module": StringVal(row, "target_module"),
		"tool":          "investigate_import_dependencies",
	}
}

func importDependencyScope(req importDependencyRequest) map[string]any {
	return map[string]any{
		"repo_id":       strings.TrimSpace(req.RepoID),
		"language":      req.normalizedLanguage(),
		"source_file":   strings.TrimSpace(req.SourceFile),
		"target_file":   strings.TrimSpace(req.TargetFile),
		"source_module": strings.TrimSpace(req.SourceModule),
		"target_module": strings.TrimSpace(req.TargetModule),
		"limit":         req.normalizedLimit(),
		"offset":        req.Offset,
	}
}

func importDependencyCoverage(req importDependencyRequest, truncated bool) map[string]any {
	queryShape := "repo_file_imports"
	if req.queryType() == "file_import_cycles" {
		queryShape = "python_file_import_two_cycle"
	}
	if req.queryType() == "cross_module_calls" {
		queryShape = "module_anchored_call_edges"
	}
	return map[string]any{
		"query_shape":        queryShape,
		"relationship_types": importDependencyRelationshipTypes(req),
		"truncated":          truncated,
		"bounded":            true,
	}
}

func importDependencyRelationshipTypes(req importDependencyRequest) []string {
	if req.queryType() == "cross_module_calls" {
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
