// SPDX-License-Identifier: MIT
// Copyright (c) 2025-2026 eshu-hq

package query

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/eshu-hq/eshu/go/internal/query/querycontract"

	"github.com/eshu-hq/eshu/go/internal/telemetry"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

const (
	callGraphMetricsCapability    = "call_graph.metrics"
	callGraphMetricsEdgeScanLimit = 50000
)

var (
	errCallGraphMetricsScopeTooBroad = errors.New("call graph metrics scope exceeds internal edge scan limit")
	errCallGraphMetricsUnavailable   = errors.New("call graph metrics are unavailable")
)

// callGraphMetricsRequest aliases the leaf-owned request type so the staying
// handler and data reader keep their signatures; see family_code_shim.go.

func (h *CodeHandler) handleCallGraphMetrics(w http.ResponseWriter, r *http.Request) {
	r, span := startCodeQueryHandlerSpan(
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
	if querycontract.CapabilityUnsupported(h.profile(), callGraphMetricsCapability) {
		WriteContractError(
			w,
			r,
			http.StatusNotImplemented,
			"call graph metrics require a supported query profile",
			ErrorCodeUnsupportedCapability,
			callGraphMetricsCapability,
			h.profile(),
			querycontract.RequiredProfile(callGraphMetricsCapability),
		)
		return
	}
	if err := req.Validate(); err != nil {
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

// The callGraphMetricsRequest type, its methods, and the metric-type helpers
// moved to codemodel/code_call_graph_metrics_aggregation.go (#6060 lane A
// L1); the request validation and accessors run there now.

func (h *CodeHandler) callGraphMetricsData(ctx context.Context, req callGraphMetricsRequest) (map[string]any, error) {
	if h == nil || h.Neo4j == nil {
		return nil, errCallGraphMetricsUnavailable
	}
	span := trace.SpanFromContext(ctx)
	span.SetAttributes(
		attribute.String("eshu.query.call_graph.metric_type", req.EffectiveMetricType()),
		attribute.Int("eshu.query.call_graph.edge_scan_limit", callGraphMetricsEdgeScanLimit),
	)
	// #5167 code family: this route is grant-bound by its mandatory repo_id.
	// applyRepositorySelectorForCapability resolves that selector against the
	// caller's grant and rejects an ungranted one with 400 before the handler
	// body runs, so the edge Cypher needs no predicate of its own. The one case
	// the selector cannot answer is a caller that reaches this read without it:
	// a grantless scoped caller must never touch the graph.
	if querycontract.RepositoryAccessFilterFromContext(ctx).Empty() {
		return callGraphMetricsResponse(req, nil), nil
	}
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
		attribute.Int("eshu.query.call_graph.expanded_node_count", stats.ExpandedNodes),
		attribute.Int("eshu.query.call_graph.result_count", IntVal(data, "count")),
		attribute.Bool("eshu.query.call_graph.truncated", BoolVal(data, "truncated")),
	)
	return data, nil
}

// callGraphMetricsEdgesCypher builds the single indexed edge pass behind the
// hub-function and recursive-function metrics, and behind the graph-summary
// packet's hot-entity ranking.
//
// Every caller runs this exact text. Both of its routes are bound to the
// caller's grant before the read -- call-graph metrics by its mandatory,
// selector-resolved repo_id, graph-summary by the not-found it answers for an
// out-of-grant repo_id -- so a grant predicate here would be redundant by
// construction and would give this hot read a second shape with no plan behind
// it. One text is what keeps the queryplan manifest's cypher_sha256 for
// QP-CALL-GRAPH-HUBS and QP-CALL-GRAPH-RECURSIVE, plan claim included,
// describing what production emits.
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
