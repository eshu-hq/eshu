// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import "sort"

type callGraphMetricFunction struct {
	id        string
	path      string
	language  string
	name      string
	startLine int
	endLine   int
}

type callGraphMetricEdge struct {
	source callGraphMetricFunction
	target callGraphMetricFunction
}

type callGraphMetricEdgeKey struct {
	sourceID string
	targetID string
}

type callGraphMetricsStats struct {
	expandedEdges int
	expandedNodes int
}

func callGraphMetricsRows(req callGraphMetricsRequest, edgeRows []map[string]any) []map[string]any {
	rows, _ := callGraphMetricsRowsWithStats(req, edgeRows)
	return rows
}

func callGraphMetricsRowsWithStats(
	req callGraphMetricsRequest,
	edgeRows []map[string]any,
) ([]map[string]any, callGraphMetricsStats) {
	edges, functions := normalizedCallGraphMetricEdges(edgeRows)
	stats := callGraphMetricsStats{
		expandedEdges: len(edgeRows),
		expandedNodes: len(functions),
	}
	if req.metricType() == "recursive_functions" {
		return recursiveCallGraphMetricRows(req, edges, functions), stats
	}
	return hubCallGraphMetricRows(req, edges, functions), stats
}

func normalizedCallGraphMetricEdges(
	rows []map[string]any,
) (map[callGraphMetricEdgeKey]struct{}, map[string]callGraphMetricFunction) {
	edges := make(map[callGraphMetricEdgeKey]struct{}, len(rows))
	functions := make(map[string]callGraphMetricFunction, len(rows))
	for _, row := range rows {
		edge := callGraphMetricEdge{
			source: callGraphMetricFunctionFromRow(row, "source"),
			target: callGraphMetricFunctionFromRow(row, "target"),
		}
		if edge.source.id == "" || edge.target.id == "" {
			continue
		}
		functions[edge.source.id] = edge.source
		functions[edge.target.id] = edge.target
		edges[callGraphMetricEdgeKey{sourceID: edge.source.id, targetID: edge.target.id}] = struct{}{}
	}
	return edges, functions
}

func callGraphMetricFunctionFromRow(row map[string]any, prefix string) callGraphMetricFunction {
	return callGraphMetricFunction{
		id:        StringVal(row, prefix+"_id"),
		path:      StringVal(row, prefix+"_path"),
		language:  StringVal(row, prefix+"_language"),
		name:      StringVal(row, prefix+"_name"),
		startLine: IntVal(row, prefix+"_start_line"),
		endLine:   IntVal(row, prefix+"_end_line"),
	}
}

func hubCallGraphMetricRows(
	req callGraphMetricsRequest,
	edges map[callGraphMetricEdgeKey]struct{},
	functions map[string]callGraphMetricFunction,
) []map[string]any {
	incoming := make(map[string]int, len(functions))
	outgoing := make(map[string]int, len(functions))
	for edge := range edges {
		outgoing[edge.sourceID]++
		incoming[edge.targetID]++
	}

	rows := make([]map[string]any, 0, len(functions))
	for functionID, function := range functions {
		if !callGraphMetricLanguageMatches(req, function) {
			continue
		}
		incomingCalls := incoming[functionID]
		outgoingCalls := outgoing[functionID]
		rows = append(rows, callGraphMetricFunctionRow(req.RepoID, function, map[string]any{
			"incoming_calls": incomingCalls,
			"outgoing_calls": outgoingCalls,
			"total_degree":   incomingCalls + outgoingCalls,
		}))
	}
	sort.Slice(rows, func(i, j int) bool { return hubCallGraphMetricRowLess(rows[i], rows[j]) })
	return callGraphMetricPage(req, rows)
}

func hubCallGraphMetricRowLess(left map[string]any, right map[string]any) bool {
	for _, key := range []string{"total_degree", "incoming_calls", "outgoing_calls"} {
		if IntVal(left, key) != IntVal(right, key) {
			return IntVal(left, key) > IntVal(right, key)
		}
	}
	return callGraphMetricFunctionRowLess(left, right, "", "")
}

func recursiveCallGraphMetricRows(
	req callGraphMetricsRequest,
	edges map[callGraphMetricEdgeKey]struct{},
	functions map[string]callGraphMetricFunction,
) []map[string]any {
	rows := make([]map[string]any, 0)
	for edge := range edges {
		if edge.sourceID > edge.targetID {
			continue
		}
		if _, ok := edges[callGraphMetricEdgeKey{sourceID: edge.targetID, targetID: edge.sourceID}]; !ok {
			continue
		}
		source := functions[edge.sourceID]
		target := functions[edge.targetID]
		if !callGraphMetricLanguageMatches(req, source) || !callGraphMetricLanguageMatches(req, target) {
			continue
		}
		rows = append(rows, callGraphMetricFunctionRow(req.RepoID, source, map[string]any{
			"partner_file":       target.path,
			"partner_id":         target.id,
			"partner_name":       target.name,
			"partner_start_line": target.startLine,
			"partner_end_line":   target.endLine,
		}))
	}
	sort.Slice(rows, func(i, j int) bool {
		return callGraphMetricFunctionRowLess(rows[i], rows[j], "partner_", "partner_")
	})
	return callGraphMetricPage(req, rows)
}

func callGraphMetricLanguageMatches(req callGraphMetricsRequest, function callGraphMetricFunction) bool {
	language := req.normalizedLanguage()
	return language == "" || function.language == language
}

func callGraphMetricFunctionRow(
	repoID string,
	function callGraphMetricFunction,
	extra map[string]any,
) map[string]any {
	row := map[string]any{
		"repo_id":       repoID,
		"file_path":     function.path,
		"language":      function.language,
		"function_id":   function.id,
		"function_name": function.name,
		"start_line":    function.startLine,
		"end_line":      function.endLine,
	}
	for key, value := range extra {
		row[key] = value
	}
	return row
}

func callGraphMetricFunctionRowLess(
	left map[string]any,
	right map[string]any,
	leftPartnerPrefix string,
	rightPartnerPrefix string,
) bool {
	for _, key := range []string{"file_path", "start_line", "function_name", "function_id"} {
		if less, decided := callGraphMetricValueLess(left, right, key, key); decided {
			return less
		}
	}
	for _, suffix := range []string{"file", "start_line", "name", "id"} {
		leftKey := leftPartnerPrefix + suffix
		rightKey := rightPartnerPrefix + suffix
		if less, decided := callGraphMetricValueLess(left, right, leftKey, rightKey); decided {
			return less
		}
	}
	return false
}

func callGraphMetricValueLess(
	left map[string]any,
	right map[string]any,
	leftKey string,
	rightKey string,
) (bool, bool) {
	if leftKey == "start_line" || leftKey == "partner_start_line" {
		leftValue := IntVal(left, leftKey)
		rightValue := IntVal(right, rightKey)
		return leftValue < rightValue, leftValue != rightValue
	}
	leftValue := StringVal(left, leftKey)
	rightValue := StringVal(right, rightKey)
	return leftValue < rightValue, leftValue != rightValue
}

func callGraphMetricPage(req callGraphMetricsRequest, rows []map[string]any) []map[string]any {
	if req.Offset >= len(rows) {
		return []map[string]any{}
	}
	end := min(req.Offset+req.queryLimit(), len(rows))
	return append([]map[string]any(nil), rows[req.Offset:end]...)
}
