package query

import "strings"

func callGraphMetricsResponse(req callGraphMetricsRequest, rows []map[string]any) map[string]any {
	limit := req.normalizedLimit()
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	functions := callGraphMetricFunctions(req, rows)
	return map[string]any{
		"metric_type":    req.metricType(),
		"scope":          callGraphMetricsScope(req),
		"functions":      functions,
		"count":          len(functions),
		"limit":          limit,
		"offset":         req.Offset,
		"truncated":      truncated,
		"next_offset":    nextCallGraphMetricsOffset(req.Offset, len(functions), truncated),
		"source_backend": "graph",
		"coverage":       callGraphMetricsCoverage(req, truncated),
	}
}

func callGraphMetricFunctions(req callGraphMetricsRequest, rows []map[string]any) []map[string]any {
	functions := make([]map[string]any, 0, len(rows))
	for index, row := range rows {
		item := cloneQueryAnyMap(row)
		item["rank"] = req.Offset + index + 1
		item["source_backend"] = "graph"
		item["source_handle"] = callGraphMetricSourceHandle(row)
		if functionID := StringVal(row, "function_id"); functionID != "" {
			item["entity_handle"] = "entity:" + functionID
		}
		if req.metricType() == "recursive_functions" {
			item["recursion_kind"] = callGraphRecursionKind(row)
			item["recursion_evidence"] = callGraphRecursionEvidence(row)
		}
		functions = append(functions, item)
	}
	return functions
}

func callGraphMetricSourceHandle(row map[string]any) map[string]any {
	return map[string]any{
		"repo_id":       StringVal(row, "repo_id"),
		"file_path":     StringVal(row, "file_path"),
		"relative_path": StringVal(row, "file_path"),
		"content_tool":  "get_file_content",
	}
}

func callGraphRecursionKind(row map[string]any) string {
	if StringVal(row, "function_id") == StringVal(row, "partner_id") {
		return "self_call"
	}
	return "mutual_call"
}

func callGraphRecursionEvidence(row map[string]any) map[string]any {
	source := StringVal(row, "function_name")
	partner := StringVal(row, "partner_name")
	partnerFile := StringVal(row, "partner_file")
	evidence := map[string]any{
		"relationship_type": "CALLS",
		"cycle_path":        []string{source, partner, source},
	}
	if partnerFile != "" {
		evidence["partner_source_handle"] = map[string]any{
			"repo_id":       StringVal(row, "repo_id"),
			"file_path":     partnerFile,
			"relative_path": partnerFile,
			"content_tool":  "get_file_content",
		}
	}
	if partnerID := StringVal(row, "partner_id"); partnerID != "" {
		evidence["partner_entity_handle"] = "entity:" + partnerID
	}
	return evidence
}

func callGraphMetricsScope(req callGraphMetricsRequest) map[string]any {
	return map[string]any{
		"repo_id":  strings.TrimSpace(req.RepoID),
		"language": req.normalizedLanguage(),
		"limit":    req.normalizedLimit(),
		"offset":   req.Offset,
	}
}

func callGraphMetricsCoverage(req callGraphMetricsRequest, truncated bool) map[string]any {
	return map[string]any{
		"query_shape":        req.metricType(),
		"relationship_types": []string{"CALLS"},
		"truncated":          truncated,
		"bounded":            true,
	}
}

func nextCallGraphMetricsOffset(offset, count int, truncated bool) any {
	if !truncated {
		return nil
	}
	nextOffset := offset + count
	if nextOffset > callGraphMetricsMaxOffset {
		return nil
	}
	return nextOffset
}
