// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// ImportDependencyResponse shapes one import-dependency page. For
// file_import_cycles it reports page truncation only; use
// ImportDependencyResponseWithCycleEnumeration when the enumeration cap
// state is known so a capped list still says truncated:true.
func ImportDependencyResponse(req ImportDependencyRequest, rows []map[string]any) map[string]any {
	return ImportDependencyResponseWithCycleEnumeration(req, rows, CycleEnumeration{
		StopReason: CycleStopNone,
		StepBudget: importCycleEnumerationStepBudget,
	})
}

// ImportDependencyResponseWithCycleEnumeration shapes one
// import-dependency page with the cycle enumeration state attached. When
// enumeration.Truncated is true the walk stopped at
// importCycleEnumerationCap or importCycleEnumerationStepBudget, so the
// page reports truncated:true and the coverage carries the cap, the step
// budget, and the stop reason: a capped cycle list is never silently
// partial.
func ImportDependencyResponseWithCycleEnumeration(
	req ImportDependencyRequest,
	rows []map[string]any,
	enumeration CycleEnumeration,
) map[string]any {
	limit := req.normalizedLimit()
	// hasMore is the paging signal: another page exists. truncated is the
	// completeness signal: the answer is partial, either because another page
	// exists or because the cycle enumeration stopped early. Keeping them apart
	// is what lets the last page of a capped run say truncated:true (the list is
	// still partial) while ending the pager (has_more:false, next_offset:null).
	hasMore := len(rows) > limit
	truncated := hasMore
	if req.EffectiveQueryType() == "file_import_cycles" && enumeration.Truncated {
		truncated = true
	}
	if len(rows) > limit {
		rows = rows[:limit]
	}
	results := importDependencyResults(req, rows)
	response := map[string]any{
		"query_type":     req.EffectiveQueryType(),
		"scope":          importDependencyScope(req),
		"limit":          limit,
		"offset":         req.Offset,
		"truncated":      truncated,
		"has_more":       hasMore,
		"next_offset":    nextImportDependencyOffset(req.Offset, len(results), hasMore),
		"source_backend": "graph",
		"coverage":       importDependencyCoverage(req, truncated, enumeration),
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
			item["cycle_length"], item["cycle_path"], item["cycle_edges"] = importDependencyCycleProof(row)
			for _, internal := range []string{"cycle_files", "cycle_source_modules", "cycle_target_modules", "cycle_lines"} {
				delete(item, internal)
			}
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

// importDependencyCycleProof renders one enumerated cycle's public proof:
// its length, its normalized file path closed back on its start, and one
// IMPORTS edge per hop. Rows shaped before the enumeration change carry
// no internal cycle lists; those fall back to the reciprocal-era two-edge
// proof from the scalar keys so old pages still read.
func importDependencyCycleProof(row map[string]any) (int, []string, []map[string]any) {
	files := importCycleStringList(row, "cycle_files")
	if len(files) < 2 {
		return 2, []string{
				querycontract.StringVal(row, "source_file"),
				querycontract.StringVal(row, "target_file"),
				querycontract.StringVal(row, "source_file"),
			}, []map[string]any{
				importDependencyCycleEdge(row, "source_file", "target_file", "source_module", "target_module", "source_line_number"),
				importDependencyCycleEdge(row, "target_file", "source_file", "target_module", "source_module", "back_edge_line_number"),
			}
	}
	sourceModules := importCycleStringList(row, "cycle_source_modules")
	targetModules := importCycleStringList(row, "cycle_target_modules")
	lines := importCycleIntList(row, "cycle_lines")
	path := append(append([]string{}, files...), files[0])
	edges := make([]map[string]any, 0, len(files))
	for index, file := range files {
		edge := map[string]any{
			"relationship_type": "IMPORTS",
			"source_file":       file,
			"target_file":       files[(index+1)%len(files)],
		}
		if index < len(sourceModules) {
			edge["source_module"] = sourceModules[index]
		}
		if index < len(targetModules) {
			edge["target_module"] = targetModules[index]
		}
		if index < len(lines) && lines[index] > 0 {
			edge["line_number"] = lines[index]
		}
		edges = append(edges, edge)
	}
	return len(files), path, edges
}

// importCycleIntList reads one internal cycle line list back as ints,
// tolerating the float64 form JSON round-trips produce.
func importCycleIntList(row map[string]any, key string) []int {
	switch values := row[key].(type) {
	case []int:
		return append([]int{}, values...)
	case []any:
		lines := make([]int, 0, len(values))
		for _, value := range values {
			switch number := value.(type) {
			case int:
				lines = append(lines, number)
			case float64:
				lines = append(lines, int(number))
			default:
				return nil
			}
		}
		return lines
	default:
		return nil
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
	scope := map[string]any{
		"repo_id":       strings.TrimSpace(req.RepoID),
		"language":      req.NormalizedLanguage(),
		"source_file":   strings.TrimSpace(req.SourceFile),
		"target_file":   strings.TrimSpace(req.TargetFile),
		"source_module": strings.TrimSpace(req.SourceModule),
		"target_module": strings.TrimSpace(req.TargetModule),
		"limit":         req.normalizedLimit(),
		"offset":        req.Offset,
	}
	if req.EffectiveQueryType() == "file_import_cycles" {
		scope["max_cycle_length"] = req.effectiveMaxCycleLength()
	}
	return scope
}

func importDependencyCoverage(req ImportDependencyRequest, truncated bool, enumeration CycleEnumeration) map[string]any {
	queryShape := "repo_file_imports"
	if req.EffectiveQueryType() == "file_import_cycles" {
		queryShape = "python_file_import_cycle"
	}
	if req.EffectiveQueryType() == "cross_module_calls" {
		queryShape = "module_anchored_call_edges"
	}
	coverage := map[string]any{
		"query_shape":          queryShape,
		"relationship_types":   importDependencyRelationshipTypes(req),
		"truncated":            truncated,
		"bounded":              true,
		"candidate_scan_limit": querycontract.ImportDependencyInternalScanLimit,
	}
	if req.EffectiveQueryType() == "file_import_cycles" {
		coverage["cycle_max_length"] = req.effectiveMaxCycleLength()
		coverage["cycle_enumeration_cap"] = importCycleEnumerationCap
		coverage["cycle_enumeration_truncated"] = enumeration.Truncated
		coverage["cycle_enumeration_stop_reason"] = enumeration.StopReason
		// A caller that passes the zero enumeration must not publish a budget of
		// 0, which would read as "no budget"; the walk always carries its own.
		stepBudget := enumeration.StepBudget
		if stepBudget == 0 {
			stepBudget = importCycleEnumerationStepBudget
		}
		coverage["cycle_enumeration_step_budget"] = stepBudget
	}
	return coverage
}

func importDependencyRelationshipTypes(req ImportDependencyRequest) []string {
	if req.EffectiveQueryType() == "cross_module_calls" {
		return []string{"CALLS"}
	}
	return []string{"IMPORTS"}
}

// nextImportDependencyOffset returns the cursor for the next page, or nil when
// there is none. It keys on hasMore, not truncated: a capped enumeration stays
// truncated on its last page, and echoing offset+0 there sent a pager into a
// fixed-cursor loop.
func nextImportDependencyOffset(offset, count int, hasMore bool) any {
	if !hasMore {
		return nil
	}
	return offset + count
}
