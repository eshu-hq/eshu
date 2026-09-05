// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package codemodel

import (
	"fmt"
	"sort"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"
)

type callGraphMetricFunction struct {
	key       string
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
	sourceKey string
	targetKey string
}

type CallGraphMetricsStats struct {
	expandedEdges int
	// ExpandedNodes is read by the staying root data reader for its span
	// attribute, so it is exported; expandedEdges stays package-local.
	ExpandedNodes int
}

// CallGraphMetricsRows ranks the scanned call-graph edges into the
// request's metric page, discarding the scan stats.
func CallGraphMetricsRows(req CallGraphMetricsRequest, edgeRows []map[string]any) []map[string]any {
	rows, _ := CallGraphMetricsRowsWithStats(req, edgeRows)
	return rows
}

// CallGraphMetricsRowsWithStats ranks the scanned call-graph edges into
// the request's metric page and reports the expanded scan stats.
func CallGraphMetricsRowsWithStats(
	req CallGraphMetricsRequest,
	edgeRows []map[string]any,
) ([]map[string]any, CallGraphMetricsStats) {
	edges, functions := normalizedCallGraphMetricEdges(edgeRows)
	stats := CallGraphMetricsStats{
		expandedEdges: len(edgeRows),
		ExpandedNodes: len(functions),
	}
	if req.EffectiveMetricType() == "recursive_functions" {
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
		if edge.source.key == "" || edge.target.key == "" {
			continue
		}
		functions[edge.source.key] = edge.source
		functions[edge.target.key] = edge.target
		edges[callGraphMetricEdgeKey{sourceKey: edge.source.key, targetKey: edge.target.key}] = struct{}{}
	}
	return edges, functions
}

func callGraphMetricFunctionFromRow(row map[string]any, prefix string) callGraphMetricFunction {
	id := querycontract.StringVal(row, prefix+"_id")
	key := querycontract.StringVal(row, prefix+"_uid")
	if key == "" {
		key = id
	}
	return callGraphMetricFunction{
		key:       key,
		id:        id,
		path:      querycontract.StringVal(row, prefix+"_path"),
		language:  querycontract.StringVal(row, prefix+"_language"),
		name:      querycontract.StringVal(row, prefix+"_name"),
		startLine: querycontract.IntVal(row, prefix+"_start_line"),
		endLine:   querycontract.IntVal(row, prefix+"_end_line"),
	}
}

func hubCallGraphMetricRows(
	req CallGraphMetricsRequest,
	edges map[callGraphMetricEdgeKey]struct{},
	functions map[string]callGraphMetricFunction,
) []map[string]any {
	incoming := make(map[string]int, len(functions))
	outgoing := make(map[string]int, len(functions))
	for edge := range edges {
		outgoing[edge.sourceKey]++
		incoming[edge.targetKey]++
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
		if querycontract.IntVal(left, key) != querycontract.IntVal(right, key) {
			return querycontract.IntVal(left, key) > querycontract.IntVal(right, key)
		}
	}
	return callGraphMetricFunctionRowLess(left, right, "", "")
}

func recursiveCallGraphMetricRows(
	req CallGraphMetricsRequest,
	edges map[callGraphMetricEdgeKey]struct{},
	functions map[string]callGraphMetricFunction,
) []map[string]any {
	rows := make([]map[string]any, 0)
	for edge := range edges {
		if edge.sourceKey > edge.targetKey {
			continue
		}
		if _, ok := edges[callGraphMetricEdgeKey{sourceKey: edge.targetKey, targetKey: edge.sourceKey}]; !ok {
			continue
		}
		source := functions[edge.sourceKey]
		target := functions[edge.targetKey]
		if !callGraphMetricLanguageMatches(req, source) || !callGraphMetricLanguageMatches(req, target) {
			continue
		}
		rows = append(rows, callGraphMetricFunctionRow(req.RepoID, source, map[string]any{
			"partner_key":        target.key,
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

func callGraphMetricLanguageMatches(req CallGraphMetricsRequest, function callGraphMetricFunction) bool {
	language := req.normalizedLanguage()
	return language == "" || function.language == language
}

func callGraphMetricFunctionRow(
	repoID string,
	function callGraphMetricFunction,
	extra map[string]any,
) map[string]any {
	row := map[string]any{
		"function_key":  function.key,
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
	for _, key := range []string{"file_path", "start_line", "function_name", "function_id", "function_key"} {
		if less, decided := callGraphMetricValueLess(left, right, key, key); decided {
			return less
		}
	}
	for _, suffix := range []string{"file", "start_line", "name", "id", "key"} {
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
		leftValue := querycontract.IntVal(left, leftKey)
		rightValue := querycontract.IntVal(right, rightKey)
		return leftValue < rightValue, leftValue != rightValue
	}
	leftValue := querycontract.StringVal(left, leftKey)
	rightValue := querycontract.StringVal(right, rightKey)
	return leftValue < rightValue, leftValue != rightValue
}

func callGraphMetricPage(req CallGraphMetricsRequest, rows []map[string]any) []map[string]any {
	if req.Offset >= len(rows) {
		return []map[string]any{}
	}
	end := min(req.Offset+req.queryLimit(), len(rows))
	return append([]map[string]any(nil), rows[req.Offset:end]...)
}

// CallGraphMetricsRequest is the decoded call-graph-metrics request. It
// split here from root code_call_graph_metrics.go (#6060 lane A L1): the
// aggregation and response builders above take it, and Go requires a
// type's methods to live with its declaration, so the request's methods
// move with it. The *CodeHandler route methods stay in root; root's
// family_code_shim.go aliases this type back so the staying handler, data
// reader, and tests keep their names. Validate and EffectiveMetricType are
// exported because staying root callers use them; the remaining accessors
// serve only this package.
type CallGraphMetricsRequest struct {
	MetricType string `json:"metric_type"`
	RepoID     string `json:"repo_id"`
	Language   string `json:"language"`
	Limit      *int   `json:"limit"`
	Offset     int    `json:"offset"`
}

const (
	callGraphMetricsDefaultLimit = 25
	callGraphMetricsMaxLimit     = 200
	// CallGraphMetricsMaxOffset bounds the call-graph-metrics page window.
	// It is exported because staying root tests bind through it via the
	// root forward; the other two limits serve only this package.
	CallGraphMetricsMaxOffset = 10000
)

// Validate rejects an unbounded or malformed metrics request before any
// graph read runs.
func (r CallGraphMetricsRequest) Validate() error {
	if strings.TrimSpace(r.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	if _, ok := callGraphMetricTypes()[r.EffectiveMetricType()]; !ok {
		return fmt.Errorf("metric_type must be one of: %s", strings.Join(callGraphMetricTypeNames(), ", "))
	}
	if r.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	if r.Offset > CallGraphMetricsMaxOffset {
		return fmt.Errorf("offset must be <= 10000")
	}
	if r.Limit == nil {
		return nil
	}
	if *r.Limit > callGraphMetricsMaxLimit {
		return fmt.Errorf("limit must be <= 200")
	}
	if *r.Limit < 1 {
		return fmt.Errorf("limit must be >= 1")
	}
	return nil
}

// EffectiveMetricType normalizes the requested metric family, defaulting
// to the hub-functions ranking. It cannot be named MetricType: the request
// struct already has a MetricType field carrying the raw decoded value.
func (r CallGraphMetricsRequest) EffectiveMetricType() string {
	metricType := strings.ToLower(strings.TrimSpace(r.MetricType))
	if metricType == "" {
		return "hub_functions"
	}
	return metricType
}

func (r CallGraphMetricsRequest) normalizedLanguage() string {
	return strings.ToLower(strings.TrimSpace(r.Language))
}

func (r CallGraphMetricsRequest) normalizedLimit() int {
	if r.Limit == nil {
		return callGraphMetricsDefaultLimit
	}
	switch {
	case *r.Limit > callGraphMetricsMaxLimit:
		return callGraphMetricsMaxLimit
	default:
		return *r.Limit
	}
}

func (r CallGraphMetricsRequest) queryLimit() int {
	return r.normalizedLimit() + 1
}

func callGraphMetricTypes() map[string]struct{} {
	return map[string]struct{}{
		"hub_functions":       {},
		"recursive_functions": {},
	}
}

func callGraphMetricTypeNames() []string {
	return []string{"hub_functions", "recursive_functions"}
}
