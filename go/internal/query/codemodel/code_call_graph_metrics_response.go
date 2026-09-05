// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

// CallGraphMetricsResponse shapes ranked metric rows into the
// call-graph-metrics response envelope, applying the request's limit+1
// truncation probe.
func CallGraphMetricsResponse(req CallGraphMetricsRequest, rows []map[string]any) map[string]any {
	limit := req.normalizedLimit()
	truncated := len(rows) > limit
	if truncated {
		rows = rows[:limit]
	}
	functions := callGraphMetricFunctions(req, rows)
	return map[string]any{
		"metric_type":    req.EffectiveMetricType(),
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

func callGraphMetricFunctions(req CallGraphMetricsRequest, rows []map[string]any) []map[string]any {
	functions := make([]map[string]any, 0, len(rows))
	for index, row := range rows {
		item := cloneQueryAnyMap(row)
		delete(item, "function_key")
		delete(item, "partner_key")
		item["rank"] = req.Offset + index + 1
		item["source_backend"] = "graph"
		item["source_handle"] = callGraphMetricSourceHandle(row)
		if functionID := CallGraphMetricIdentity(row, "function_key", "function_id"); functionID != "" {
			item["entity_handle"] = "entity:" + functionID
		}
		if req.EffectiveMetricType() == "recursive_functions" {
			item["recursion_kind"] = callGraphRecursionKind(row)
			item["recursion_evidence"] = callGraphRecursionEvidence(row)
		}
		functions = append(functions, item)
	}
	return functions
}

func callGraphMetricSourceHandle(row map[string]any) map[string]any {
	return map[string]any{
		"repo_id":       querycontract.StringVal(row, "repo_id"),
		"file_path":     querycontract.StringVal(row, "file_path"),
		"relative_path": querycontract.StringVal(row, "file_path"),
		"content_tool":  "get_file_content",
	}
}

// CallGraphMetricIdentity resolves the canonical identity for a metric row.
func CallGraphMetricIdentity(row map[string]any, canonicalKey string, legacyKey string) string {
	if canonicalID := querycontract.StringVal(row, canonicalKey); canonicalID != "" {
		return canonicalID
	}
	return querycontract.StringVal(row, legacyKey)
}

func callGraphRecursionKind(row map[string]any) string {
	functionKey := querycontract.StringVal(row, "function_key")
	if functionKey == "" {
		functionKey = querycontract.StringVal(row, "function_id")
	}
	partnerKey := querycontract.StringVal(row, "partner_key")
	if partnerKey == "" {
		partnerKey = querycontract.StringVal(row, "partner_id")
	}
	if functionKey == partnerKey {
		return "self_call"
	}
	return "mutual_call"
}

func callGraphRecursionEvidence(row map[string]any) map[string]any {
	source := querycontract.StringVal(row, "function_name")
	partner := querycontract.StringVal(row, "partner_name")
	partnerFile := querycontract.StringVal(row, "partner_file")
	evidence := map[string]any{
		"relationship_type": "CALLS",
		"cycle_path":        []string{source, partner, source},
	}
	if partnerFile != "" {
		evidence["partner_source_handle"] = map[string]any{
			"repo_id":       querycontract.StringVal(row, "repo_id"),
			"file_path":     partnerFile,
			"relative_path": partnerFile,
			"content_tool":  "get_file_content",
		}
	}
	if partnerID := CallGraphMetricIdentity(row, "partner_key", "partner_id"); partnerID != "" {
		evidence["partner_entity_handle"] = "entity:" + partnerID
	}
	return evidence
}

func callGraphMetricsScope(req CallGraphMetricsRequest) map[string]any {
	return map[string]any{
		"repo_id":  strings.TrimSpace(req.RepoID),
		"language": req.normalizedLanguage(),
		"limit":    req.normalizedLimit(),
		"offset":   req.Offset,
	}
}

func callGraphMetricsCoverage(req CallGraphMetricsRequest, truncated bool) map[string]any {
	return map[string]any{
		"query_shape":        req.EffectiveMetricType(),
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
	if nextOffset > CallGraphMetricsMaxOffset {
		return nil
	}
	return nextOffset
}
