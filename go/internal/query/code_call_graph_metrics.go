// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	callGraphMetricsCapability    = "call_graph.metrics"
	callGraphMetricsDefaultLimit  = 25
	callGraphMetricsEdgeScanLimit = 50000
	callGraphMetricsMaxLimit      = 200
	callGraphMetricsMaxOffset     = 10000
)

var (
	errCallGraphMetricsScopeTooBroad = errors.New("call graph metrics scope exceeds internal edge scan limit")
	errCallGraphMetricsUnavailable   = errors.New("call graph metrics are unavailable")
)

type callGraphMetricsRequest struct {
	MetricType string `json:"metric_type"`
	RepoID     string `json:"repo_id"`
	Language   string `json:"language"`
	Limit      *int   `json:"limit"`
	Offset     int    `json:"offset"`
}

func (h *CodeHandler) handleCallGraphMetrics(w http.ResponseWriter, r *http.Request) {
	r, span := startQueryHandlerSpan(
		r,
		telemetry.SpanQueryCallGraphMetrics,
		"POST /api/v0/code/call-graph/metrics",
		callGraphMetricsCapability,
	)
	defer span.End()

	var req callGraphMetricsRequest
	if err := ReadJSON(r, &req); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if capabilityUnsupported(h.profile(), callGraphMetricsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"call graph metrics require a supported query profile",
			ErrorCodeUnsupportedCapability,
			callGraphMetricsCapability,
			h.profile(),
			requiredProfile(callGraphMetricsCapability),
		)
		return
	}
	if err := req.validate(); err != nil {
		WriteError(w, http.StatusBadRequest, err.Error())
		return
	}
	if !h.applyRepositorySelectorForCapability(w, r, &req.RepoID, callGraphMetricsCapability) {
		return
	}

	data, err := h.callGraphMetricsData(r.Context(), req)
	if err != nil {
		span.RecordError(err)
		if errors.Is(err, errCallGraphMetricsUnavailable) {
			WriteError(w, http.StatusServiceUnavailable, err.Error())
			return
		}
		if errors.Is(err, errCallGraphMetricsScopeTooBroad) {
			WriteError(w, http.StatusUnprocessableEntity, err.Error())
			return
		}
		if WriteGraphReadError(w, r, err, callGraphMetricsCapability) {
			return
		}
		WriteError(w, http.StatusInternalServerError, err.Error())
		return
	}
	WriteSuccess(
		w,
		r,
		http.StatusOK,
		data,
		BuildTruthEnvelope(h.profile(), callGraphMetricsCapability, TruthBasisAuthoritativeGraph, "resolved from bounded call graph metrics lookup"),
	)
}

func (r callGraphMetricsRequest) validate() error {
	if strings.TrimSpace(r.RepoID) == "" {
		return fmt.Errorf("repo_id is required")
	}
	if _, ok := callGraphMetricTypes()[r.metricType()]; !ok {
		return fmt.Errorf("metric_type must be one of: %s", strings.Join(callGraphMetricTypeNames(), ", "))
	}
	if r.Offset < 0 {
		return fmt.Errorf("offset must be >= 0")
	}
	if r.Offset > callGraphMetricsMaxOffset {
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

func (r callGraphMetricsRequest) metricType() string {
	metricType := strings.ToLower(strings.TrimSpace(r.MetricType))
	if metricType == "" {
		return "hub_functions"
	}
	return metricType
}

func (r callGraphMetricsRequest) normalizedLanguage() string {
	return strings.ToLower(strings.TrimSpace(r.Language))
}

func (r callGraphMetricsRequest) normalizedLimit() int {
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

func (r callGraphMetricsRequest) queryLimit() int {
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

func (h *CodeHandler) callGraphMetricsData(ctx context.Context, req callGraphMetricsRequest) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, errCallGraphMetricsUnavailable
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("eshu.query.call_graph.metric_type", req.metricType()),
		attribute.Int("eshu.query.call_graph.edge_scan_limit", callGraphMetricsEdgeScanLimit),
	)
	cypher, params := callGraphMetricsEdgesCypher(req.RepoID)
	edges, err := h.Neo4j.Run(ctx, cypher, params)
	if err != nil {
		return nil, err
	}
	scanOverflow := len(edges) > callGraphMetricsEdgeScanLimit
	span.SetAttributes(
		attribute.Int("eshu.query.call_graph.expanded_edge_count", len(edges)),
		attribute.Bool("eshu.query.call_graph.scan_overflow", scanOverflow),
	)
	if scanOverflow {
		return nil, fmt.Errorf(
			"%w: reached the %d-edge sentinel; maximum exact scope is %d",
			errCallGraphMetricsScopeTooBroad,
			len(edges),
			callGraphMetricsEdgeScanLimit,
		)
	}
	rows, stats := callGraphMetricsRowsWithStats(req, edges)
	data := callGraphMetricsResponse(req, rows)
	span.SetAttributes(
		attribute.Int("eshu.query.call_graph.expanded_node_count", stats.expandedNodes),
		attribute.Int("eshu.query.call_graph.result_count", IntVal(data, "count")),
		attribute.Bool("eshu.query.call_graph.truncated", BoolVal(data, "truncated")),
	)
	return data, nil
}

func callGraphMetricsEdgesCypher(repoID string) (string, map[string]any) {
	return `MATCH (source:Function {repo_id: $repo_id})-[call:CALLS]->(target:Function {repo_id: $repo_id})
RETURN source.uid AS source_uid,
       coalesce(source.id, source.uid) AS source_id,
       source.relative_path AS source_path,
       source.language AS source_language,
       source.name AS source_name,
       source.start_line AS source_start_line,
       source.end_line AS source_end_line,
       target.uid AS target_uid,
       coalesce(target.id, target.uid) AS target_id,
       target.relative_path AS target_path,
       target.language AS target_language,
       target.name AS target_name,
       target.start_line AS target_start_line,
       target.end_line AS target_end_line
LIMIT $edge_scan_limit`, map[string]any{
			"edge_scan_limit": callGraphMetricsEdgeScanLimit + 1,
			"repo_id":         strings.TrimSpace(repoID),
		}
}
